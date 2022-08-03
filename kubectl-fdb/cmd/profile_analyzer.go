/*
 * profile_analyzer.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"bytes"
	ctx "context"
	"github.com/spf13/cobra"
	"io"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"strconv"
	"text/template"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"log"
)

type profileConfig struct {
	Namespace   string
	ClusterName string
	JobName     string
	CommandArgs string
}

func newProfileAnalyzerCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "analyze-profile",
		Short: "Analyze FDB shards to find the busiest team",
		Long:  "Analyze FDB shards to find the busiest team",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}
			clientSet, err := kubernetes.NewForConfig(config)
			if err != nil {
				return err
			}
			dynamicConfig, err := dynamic.NewForConfig(config)
			if err != nil {
				return err
			}
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			startTime, err := cmd.Flags().GetString("start-time")
			if err != nil {
				return err
			}
			endTime, err := cmd.Flags().GetString("end-time")
			if err != nil {
				return err
			}
			topRequests, err := cmd.Flags().GetInt("top-requests")
			if err != nil {
				return err
			}
			templateName, err := cmd.Flags().GetString("template-name")
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			return runProfileAnalyzer(clientSet, dynamicConfig, namespace, clusterName, startTime, endTime, topRequests, templateName)
		},
		Example: `
# Run the profiler for cluster-1. We require --cluster option explicitly because analyze commands take lot many arguments.
kubectl fdb analyze-profile -c cluster-1 --start-time "01:01 20/07/2022 BST" --end-time "01:30 20/07/2022 BST" --top-requests 100
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "cluster name for running hot shard tool.")
	cmd.Flags().String("start-time", "", "start time for the analyzing transaction '01:30 30/07/2022 BST'")
	cmd.Flags().String("end-time", "", "end time for analyzing the transaction '02:30 30/07/2022 BST'")
	cmd.Flags().String("template-name", "", "Name of the Job template")
	cmd.Flags().Int("top-requests", 100, "")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}

	o.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func runProfileAnalyzer(kubeClient *kubernetes.Clientset, dynamicConfig dynamic.Interface, namespace string, clusterName string, startTime string, endTime string, topRequests int, templateName string) error {
	pc := profileConfig{
		Namespace:   namespace,
		ClusterName: clusterName,
		JobName:     clusterName + "-hot-shard-tool",
		CommandArgs: " -C " + " /var/dynamic-conf/fdb.cluster" + " -s \"" + startTime + "\"" + " -e \"" + endTime + "\"" + " --filter-get-range " + " --top-requests " + strconv.Itoa(topRequests),
	}
	t, err := template.ParseFiles(templateName)
	if err != nil {
		return err
	}
	buf := bytes.Buffer{}
	err = t.Execute(&buf, pc)
	if err != nil {
		return err
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(&buf, 100000)

	for {
		var rawObj runtime.RawExtension
		err := decoder.Decode(&rawObj)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		obj, gvk, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).
			Decode(rawObj.Raw, nil, nil)
		if err != nil {
			log.Println("NewDecodingSerializer Error ->" + err.Error())
			return err
		}
		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			log.Println("ToUnstructured Error ->" + err.Error())
			return err
		}
		unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}
		gr, err := restmapper.GetAPIGroupResources(kubeClient.Discovery())
		if err != nil {
			return err
		}
		mapper := restmapper.NewDiscoveryRESTMapper(gr)
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			log.Println("rest mapping Error ->" + err.Error())
			return err
		}
		var dynamicInterface dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if unstructuredObj.GetNamespace() == "" {
				unstructuredObj.SetNamespace(namespace)
			}
			dynamicInterface = dynamicConfig.Resource(mapping.Resource).
				Namespace(unstructuredObj.GetNamespace())
		} else {
			dynamicInterface = dynamicConfig.Resource(mapping.Resource)
		}
		if _, err := dynamicInterface.Create(
			ctx.Background(),
			unstructuredObj,
			metav1.CreateOptions{
				FieldManager: "fdb-hot-shard-tool",
			}); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				log.Printf("%s job already present, Delete the job and re-run.", pc.JobName)
				continue
			}
			log.Println("DynamicInterface Error ->" + err.Error())
			return err
		}
	}
	log.Printf("%s Job created.", pc.JobName)
	return nil
}
