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
	"context"
	"fmt"
	"log"
	"strings"
	"text/template"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			kubeClient, err := getKubeClient(o)
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

			return runProfileAnalyzer(kubeClient, namespace, clusterName, startTime, endTime, topRequests, templateName)
		},
		Example: `
# This sub-command is still experimental. It still has multiple things to do before making it a stable command. Run the profiler for cluster-1 in the default namespace for the provided state and end time.
kubectl fdb analyze-profile -c cluster-1 --start-time "01:01 20/07/2022 BST" --end-time "01:30 20/07/2022 BST" --top-requests 100 --template-name job.yaml
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "cluster name for running the analyze profile against.")
	cmd.Flags().String("start-time", "", "start time for the analyzing transaction '01:30 30/07/2022 BST'.")
	cmd.Flags().String("end-time", "", "end time for analyzing the transaction '02:30 30/07/2022 BST'.")
	cmd.Flags().String("template-name", "", "name of the Job template.")
	cmd.Flags().Int("top-requests", 100, "")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}
	err = cmd.MarkFlagRequired("start-time")
	if err != nil {
		log.Fatal(err)
	}
	err = cmd.MarkFlagRequired("end-time")
	if err != nil {
		log.Fatal(err)
	}
	err = cmd.MarkFlagRequired("template-name")
	if err != nil {
		log.Fatal(err)
	}

	o.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func runProfileAnalyzer(kubeClient client.Client, namespace string, clusterName string, startTime string, endTime string, topRequests int, templateName string) error {
	config := profileConfig{
		Namespace:   namespace,
		ClusterName: clusterName,
		JobName:     clusterName + "-hot-shard-tool",
		CommandArgs: fmt.Sprintf(" -C /var/dynamic-conf/fdb.cluster -s \"%s\" -e \"%s\" --filter-get-range --top-requests  %d", startTime, endTime, topRequests),
	}
	t, err := template.ParseFiles(templateName)
	if err != nil {
		return err
	}
	buf := bytes.Buffer{}
	err = t.Execute(&buf, config)
	if err != nil {
		return err
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(&buf, 100000)

	job := &batchv1.Job{}
	err = decoder.Decode(&job)
	if err != nil {
		return err
	}
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		return err
	}

	version, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		return err
	}
	for _, container := range job.Spec.Template.Spec.InitContainers {
		if !strings.Contains(container.Image, version.Compact()) {
			continue
		}
		imageVersion := strings.Split(container.Image, ":")
		fdbVersion := strings.Split(imageVersion[1], "-1")
		envValue := v1.EnvVar{
			Name:  "FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY",
			Value: fmt.Sprintf("/usr/bin/fdb/%s/lib/", fdbVersion[0]),
		}
		job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, envValue)
		break
	}
	log.Printf("creating job %s", config.JobName)
	return kubeClient.Create(context.TODO(), job)
}
