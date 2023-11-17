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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"
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
			clusterLabel, err := cmd.Flags().GetString("cluster-label")
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(cmd.Context(), o)
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

				return cordonNode(cmd, kubeClient, clusterName, nodes, namespace, withExclusion, wait, clusterLabel)
			}

			return cordonNode(cmd, kubeClient, clusterName, args, namespace, withExclusion, wait, clusterLabel)
		},
		Example: `
# Evacuate all process groups for a cluster in the current namespace that are hosted on node-1
kubectl fdb cordon -c cluster node-1

# Evacuate all process groups for a cluster in the default namespace that are hosted on node-1
kubectl fdb cordon -n default -c cluster node-1

# Evacuate all process groups for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast

# Evacuate all process groups in the current namespace that are hosted on node-1 with cluster-label
kubectl fdb cordon -l fdb-cluster-label node-1

# Evacuate all process groups for a cluster in the default namespace that are hosted on node-1 with cluster-label
kubectl fdb cordon -n default -l fdb-cluster-label node-1

# Evacuate all process groups for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast

# Evacuate all process groups in the current namespace that are hosted on nodes with the labels machine=a,disk=fast with cluster-label
kubectl fdb cordon --node-selector machine=a,disk=fast -l fdb-cluster-label
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "evacuate process group(s) from the provided cluster.")
	cmd.Flags().StringToStringVarP(&nodeSelectors, "node-selector", "", nil, "node-selector to select all nodes that should be cordoned. Can't be used with specific nodes.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the process groups should be removed with exclusion.")
	cmd.Flags().StringP("cluster-label", "l", fdbv1beta2.FDBClusterLabel, "cluster label to fetch the appropriate Pods and identify the according cluster.")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// cordonNode gets all process groups of this cluster that run on the given nodes and add them to the remove list
func cordonNode(cmd *cobra.Command, kubeClient client.Client, inputClusterName string, nodes []string, namespace string, withExclusion bool, wait bool, clusterLabel string) error {
	cmd.Printf("Start to cordon %d nodes\n", len(nodes))
	if len(nodes) == 0 {
		return nil
	}

	var errors []string
	for _, node := range nodes {
		pods, err := fetchPods(kubeClient, inputClusterName, namespace, node, clusterLabel)
		if err != nil {
			internalErr := fmt.Sprintf("Issue fetching Pods running on node: %s. Error: %s\n", node, err)
			cmd.PrintErr(internalErr)
			errors = append(errors, internalErr)
			continue
		}

		clusterNames := getClusterNames(cmd, inputClusterName, pods, clusterLabel)
		for clusterName := range clusterNames {
			cmd.Printf("Starting operation on %s, node: %s\n", clusterName, node)
			cluster, err := loadCluster(kubeClient, namespace, clusterName)
			if err != nil {
				internalErr := fmt.Sprintf("unable to load cluster: %s, skipping\n", clusterName)
				errors = append(errors, internalErr)
				cmd.PrintErr(internalErr)
				continue
			}

			var processGroups []string
			for _, pod := range pods.Items {
				// With the field selector above this shouldn't be required, but it's good to
				// have a second check.
				if pod.Spec.NodeName != node {
					cmd.Printf("Pod: %s is not running on node %s will be ignored\n", pod.Name, node)
					continue
				}

				if internal.ContainsPod(cluster, pod) {
					processGroup, ok := pod.Labels[cluster.GetProcessGroupIDLabel()]
					if !ok {
						cmd.Printf("could not fetch process group ID from Pod: %s\n", pod.Name)
						continue
					}
					processGroups = append(processGroups, processGroup)
				}
			}
			err = replaceProcessGroups(kubeClient, cluster.Name, processGroups, namespace, withExclusion, wait, false, true)
			if err != nil {
				internalErr := fmt.Sprintf("unable to cordon all Pods for cluster %s\n", cluster.Name)
				errors = append(errors, internalErr)
				cmd.PrintErr(internalErr)
			}
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("following operation failed, please check and retry: \n, %s", errors)
	}
	return nil
}

func fetchPods(kubeClient client.Client, clusterName string, namespace string, node string, clusterLabel string) (corev1.PodList, error) {
	var pods corev1.PodList
	var err error

	var podLabelSelector client.ListOption

	if clusterName == "" {
		podLabelSelector = client.HasLabels([]string{clusterLabel})
	} else {
		cluster, err := loadCluster(kubeClient, namespace, clusterName)
		if err != nil {
			return pods, fmt.Errorf("unable to load cluster: %s. Error: %w", clusterName, err)
		}

		podLabelSelector = client.MatchingLabels(cluster.GetMatchLabels())
	}

	err = kubeClient.List(ctx.Background(), &pods,
		client.InNamespace(namespace),
		podLabelSelector,
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("spec.nodeName", node),
		})
	if err != nil {
		return pods, fmt.Errorf("unable to fetch pods. Error: %w", err)
	}
	return pods, nil
}

func getClusterNames(cmd *cobra.Command, clusterName string, pods corev1.PodList, clusterLabel string) map[string]fdbv1beta2.None {
	if clusterName != "" {
		return map[string]fdbv1beta2.None{clusterName: {}}
	}

	clusterNames := make(map[string]fdbv1beta2.None)
	for _, pod := range pods.Items {
		clusterName, ok := pod.Labels[clusterLabel]
		if !ok {
			cmd.PrintErrf("could not fetch cluster name from Pod: %s\n", pod.Name)
			continue
		}
		clusterNames[clusterName] = fdbv1beta2.None{}
	}

	return clusterNames
}
