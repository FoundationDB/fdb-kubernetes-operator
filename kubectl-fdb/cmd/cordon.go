/*
 * cordon.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"log"

	"k8s.io/apimachinery/pkg/fields"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	corev1 "k8s.io/api/core/v1"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

func newCordonCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)
	var nodeSelectors map[string]string

	cmd := &cobra.Command{
		Use:   "cordon",
		Short: "Adds all instance (or multiple) that run on a node to the remove list of the given cluster",
		Long:  "Adds all instance (or multiple) that run on a node to the remove list of the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Root().Flags().GetBool("force")
			if err != nil {
				return err
			}
			cluster, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			withExclusion, err := cmd.Flags().GetBool("exclusion")
			if err != nil {
				return err
			}
			nodeSelector, err := cmd.Flags().GetStringToString("node-selector")
			if err != nil {
				return err
			}
			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)

			kubeClient, err := client.New(config, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			if len(nodeSelector) != 0 && len(args) != 0 {
				return fmt.Errorf("it's not allowed to use the node-selector and pass nodes")
			}

			if len(nodeSelector) != 0 {
				nodes, err := getNodes(kubeClient, nodeSelector)
				if err != nil {
					return err
				}

				return cordonNode(kubeClient, cluster, nodes, namespace, withExclusion, force)
			}

			return cordonNode(kubeClient, cluster, args, namespace, withExclusion, force)
		},
		Example: `
# Evacuate all instances for a cluster in the current namespace that are hosted on node-1
kubectl fdb cordon -c cluster node-1

# Evacuate all instances for a cluster in the default namespace that are hosted on node-1
kubectl fdb cordon -n default -c cluster node-1

# Evacuate all instances for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "evacuate instance(s) from the provided cluster.")
	cmd.Flags().StringToStringVarP(&nodeSelectors, "node-selector", "", nil, "node-selector to select all nodes that should be cordoned. Can't be used with specific nodes.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the instances should be removed with exclusion.")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// cordonNode gets all instances of this cluster that run on the given nodes and add them to the remove list
func cordonNode(kubeClient client.Client, clusterName string, nodes []string, namespace string, withExclusion bool, force bool) error {
	fmt.Printf("Start to cordon %d nodes\n", len(nodes))
	if len(nodes) == 0 {
		return nil
	}

	var instances []string

	for _, node := range nodes {
		var pods corev1.PodList
		err := kubeClient.List(ctx.Background(), &pods,
			client.InNamespace(namespace),
			client.MatchingLabels(map[string]string{
				fdbtypes.FDBClusterLabel: clusterName,
			}),
			client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector("spec.nodeName", node),
			})

		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			// With the field selector above this shouldn't be required but it's good to
			// have a second check.
			if pod.Spec.NodeName != node {
				fmt.Printf("Pod: %s is not running on node %s will be ignored\n", pod.Name, node)
				continue
			}

			instanceID, ok := pod.Labels[fdbtypes.FDBInstanceIDLabel]
			if !ok {
				fmt.Printf("could not fetch instance ID from Pod: %s\n", pod.Name)
				continue
			}
			instances = append(instances, instanceID)
		}
	}

	return removeInstances(kubeClient, clusterName, instances, namespace, withExclusion, false, force, false)
}
