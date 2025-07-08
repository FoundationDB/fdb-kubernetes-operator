/*
 * remove_process_group.go
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
	ctx "context"
	"errors"
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newRemoveProcessGroupCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "process-groups",
		Short: "Adds a process group (or multiple) to the remove list of the given cluster, or matching given pod names",
		Long:  "Adds a process group (or multiple) to the remove list field of the given cluster, or matching given pod names",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			withExclusion, err := cmd.Flags().GetBool("exclusion")
			if err != nil {
				return err
			}
			removeAllFailed, err := cmd.Flags().GetBool("remove-all-failed")
			if err != nil {
				return err
			}

			processGroupSelectionOpts, err := getProcessSelectionOptsFromFlags(cmd, o, args)
			if err != nil {
				return err
			}
			kubeClient, err := getKubeClient(cmd.Context(), o)
			if err != nil {
				return err
			}

			totalRemoved, err := replaceProcessGroups(cmd, kubeClient,
				processGroupSelectionOpts,
				replaceProcessGroupsOptions{
					withExclusion:   withExclusion,
					wait:            wait,
					removeAllFailed: removeAllFailed,
				})
			cmd.Printf("\nCompleted removal of %d processGroups\n", totalRemoved)
			return err
		},
		Example: `
# Remove process groups for a cluster in the current namespace
kubectl fdb remove process-groups -c cluster pod-1 pod-2

# Remove process groups across clusters in the current namespace
kubectl fdb remove process-groups pod-1-cluster-A pod-2-cluster-B -l your-cluster-label

# Remove process groups for a cluster in the namespace default
kubectl fdb -n default remove process-groups -c cluster pod-1 pod-2

# Remove all failed process groups for a cluster (all process groups that have a missing process)
kubectl fdb -n default remove process-groups -c cluster --remove-all-failed

# Remove all processes in the cluster that have the given process-class (incompatible with passing pod names or process group IDs)
kubectl fdb -n default remove process-groups -c cluster --process-class="stateless"

# Remove all processes in the cluster that match the given label set
kubectl fdb remove process-groups --match-labels="label-key=label-value,other-key=other-value" -c cluster
`,
	}

	addProcessSelectionFlags(cmd)
	cmd.Flags().
		BoolP("exclusion", "e", true, "define if the process groups should be removed with exclusion.")
	cmd.Flags().
		Bool("remove-all-failed", false, "define if all failed processes should be replaced.")

	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

type replaceProcessGroupsOptions struct {
	withExclusion   bool
	wait            bool
	removeAllFailed bool
}

// replaceProcessGroups adds process groups to the removal list of their respective clusters, and returns a count of
// the number of removed processGroups (pods) and any encountered error.
// If clusterName is specified, it will ONLY do so for the specified cluster.
// If processClass is specified, it will ignore the given ids and remove all processes in the given cluster whose pods
// have a processClassLabel matching the processClass.
func replaceProcessGroups(
	cmd *cobra.Command,
	kubeClient client.Client,
	processGroupOpts processGroupSelectionOptions,
	opts replaceProcessGroupsOptions,
) (int, error) {
	// TODO(nic): consider putting "allProcesses" into the process selection functions to avoid having these checks outside for more sensitive commands
	if len(processGroupOpts.ids) == 0 && !opts.removeAllFailed &&
		processGroupOpts.processClass == "" &&
		len(processGroupOpts.conditions) == 0 &&
		len(processGroupOpts.matchLabels) == 0 {
		return 0, errors.New("no processGroups could be selected with the provided options")
	}

	processGroupsByCluster, err := getProcessGroupsByCluster(cmd, kubeClient, processGroupOpts)
	if err != nil {
		return 0, err
	}

	return replaceProcessGroupsFromCluster(
		cmd,
		kubeClient,
		processGroupsByCluster,
		processGroupOpts.namespace,
		opts,
	)
}

// replaceProcessGroupsFromCluster removes the process groups ONLY from the specified cluster.
// It also returns the list of processGroupIDs that it removed from the cluster.
func replaceProcessGroupsFromCluster(
	cmd *cobra.Command,
	kubeClient client.Client,
	processGroupsByCluster map[*fdbv1beta2.FoundationDBCluster][]fdbv1beta2.ProcessGroupID,
	namespace string,
	opts replaceProcessGroupsOptions,
) (int, error) {
	totalRemoved := 0
	for cluster, processGroupIDs := range processGroupsByCluster {
		cmd.Printf("Cluster %v/%v:\n", namespace, cluster.Name)
		patch := client.MergeFrom(cluster.DeepCopy())

		processGroupSet := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
		for _, processGroup := range processGroupIDs {
			processGroupSet[processGroup] = fdbv1beta2.None{}
		}

		if opts.removeAllFailed {
			for _, processGroupStatus := range cluster.Status.ProcessGroups {
				// Those are already included so we can skip the check and don't add duplicates
				if _, ok := processGroupSet[processGroupStatus.ProcessGroupID]; ok {
					continue
				}

				_, failureTime := processGroupStatus.NeedsReplacement(0, 0)
				if failureTime > 0 {
					processGroupIDs = append(processGroupIDs, processGroupStatus.ProcessGroupID)
				}
			}
		}

		if opts.wait {
			if !confirmAction(
				fmt.Sprintf(
					"Remove %v from cluster %s/%s with exclude: %t",
					processGroupIDs,
					namespace,
					cluster.Name,
					opts.withExclusion,
				),
			) {
				return totalRemoved, fmt.Errorf("user aborted the removal")
			}
		}

		var processGroupIDsForRemoval []fdbv1beta2.ProcessGroupID
		if opts.withExclusion {
			processGroupIDsForRemoval = cluster.GetProcessGroupsToRemove(processGroupIDs)
			cluster.Spec.ProcessGroupsToRemove = processGroupIDsForRemoval
		} else {
			processGroupIDsForRemoval = cluster.GetProcessGroupsToRemoveWithoutExclusion(processGroupIDs)
			cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = processGroupIDsForRemoval
		}

		err := kubeClient.Patch(ctx.TODO(), cluster, patch)
		if err != nil {
			return totalRemoved, err
		}
		totalRemoved += len(processGroupIDs)
		cmd.Printf("removed %v (exclude: %t)\n", processGroupIDs, opts.withExclusion)
	}
	return totalRemoved, nil
}
