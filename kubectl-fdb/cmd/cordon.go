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
	"k8s.io/apimachinery/pkg/fields"
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	corev1 "k8s.io/api/core/v1"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

func newCordonCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)
	var nodeSelectors map[string]string

	cmd := &cobra.Command{
		Use:   "cordon",
		Short: "Adds all process groups (or multiple) that run on a node to the remove list of the given cluster",
		Long:  "Adds all process groups (or multiple) that run on a node to the remove list of the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			sleep, err := cmd.Root().Flags().GetUint16("sleep")
			if err != nil {
				return err
			}
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
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
			useCustomLabel, err := cmd.Flags().GetBool("use-custom-label")
			if err != nil {
				return err
			}
			customLabel, err := cmd.Flags().GetString("custom-label")
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(o)
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			if !useCustomLabel && len(clusterName) == 0 {
				return fmt.Errorf("either cluster name or custom label should be provided")
			}

			cluster, err := loadCluster(kubeClient, namespace, clusterName)
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

				return cordonNode(kubeClient, cluster, nodes, namespace, withExclusion, wait, sleep, useCustomLabel, customLabel)
			}

			return cordonNode(kubeClient, cluster, args, namespace, withExclusion, wait, sleep, useCustomLabel, customLabel)
		},
		Example: `
# Evacuate all process groups for a cluster in the current namespace that are hosted on node-1
kubectl fdb cordon -c cluster node-1

# Evacuate all process groups for a cluster in the default namespace that are hosted on node-1
kubectl fdb cordon -n default -c cluster node-1

# Evacuate all process groups for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast

# Evacuate all process groups in the current namespace that are hosted on node-1, the default label is fdb-cluster-name
kubectl fdb cordon --use-custom-label node-1

# Evacuate all process groups in the current namespace that are hosted on node-1 with custom label
kubectl fdb cordon --use-custom-label -l "fdb-cluster-name fdb-cluster-group" node-1

# Evacuate all process groups for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast

# Evacuate all process groups in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon --use-custom-label --node-selector machine=a,disk=fast
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "evacuate process group(s) from the provided cluster.")
	cmd.Flags().StringToStringVarP(&nodeSelectors, "node-selector", "", nil, "node-selector to select all nodes that should be cordoned. Can't be used with specific nodes.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the process groups should be removed with exclusion.")
	cmd.Flags().Bool("use-custom-label", false, "define if the process groups should be removed using label instead of cluster name.")
	cmd.Flags().StringP("custom-label", "l", "fdb-cluster-name", "evacuate process group(s) from the provided cluster.")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// cordonNode gets all process groups of this cluster that run on the given nodes and add them to the remove list
func cordonNode(kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, nodes []string, namespace string, withExclusion bool, wait bool, sleep uint16, useCustomLabel bool, customLabel string) error {
	fmt.Printf("Start to cordon %d nodes\n", len(nodes))
	if len(nodes) == 0 {
		return nil
	}

	var processGroups []string

	for _, node := range nodes {
		var pods corev1.PodList
		var err error
		if useCustomLabel {
			err = kubeClient.List(ctx.Background(), &pods,
				client.InNamespace(namespace),
				client.HasLabels(strings.Split(customLabel, " ")),
				client.MatchingFieldsSelector{
					Selector: fields.OneTermEqualSelector("spec.nodeName", node),
				})
		} else {
			err = kubeClient.List(ctx.Background(), &pods,
				client.InNamespace(namespace),
				client.MatchingLabels(cluster.GetMatchLabels()),
				client.MatchingFieldsSelector{
					Selector: fields.OneTermEqualSelector("spec.nodeName", node),
				})
		}
		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			// With the field selector above this shouldn't be required, but it's good to
			// have a second check.
			if pod.Spec.NodeName != node {
				fmt.Printf("Pod: %s is not running on node %s will be ignored\n", pod.Name, node)
				continue
			}

			processGroup, ok := pod.Labels[cluster.GetProcessGroupIDLabel()]
			if !ok {
				fmt.Printf("could not fetch process group ID from Pod: %s\n", pod.Name)
				continue
			}
			processGroups = append(processGroups, processGroup)
		}
	}

	return replaceProcessGroups(kubeClient, cluster.Name, processGroups, namespace, withExclusion, wait, false, true, sleep)
}
