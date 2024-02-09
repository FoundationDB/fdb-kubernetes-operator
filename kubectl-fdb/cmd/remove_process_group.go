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
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
			cluster, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			clusterLabel, err := cmd.Flags().GetString("cluster-label")
			if err != nil {
				return err
			}
			processClass, err := cmd.Flags().GetString("process-class")
			if err != nil {
				return err
			}
			withExclusion, err := cmd.Flags().GetBool("exclusion")
			if err != nil {
				return err
			}
			useProcessGroupID, err := cmd.Flags().GetBool("use-process-group-id")
			if err != nil {
				return err
			}
			removeAllFailed, err := cmd.Flags().GetBool("remove-all-failed")
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

			totalRemoved, err := replaceProcessGroups(cmd, kubeClient, cluster, args, namespace, replaceProcessGroupsOptions{
				clusterLabel:      clusterLabel,
				processClass:      processClass,
				withExclusion:     withExclusion,
				wait:              wait,
				removeAllFailed:   removeAllFailed,
				useProcessGroupID: useProcessGroupID,
			})
			cmd.Printf("\nCompleted removal of %d processGroups\n", totalRemoved)
			return err
		},
		Example: `
# Remove process groups for a cluster in the current namespace
kubectl fdb remove process-group -c cluster pod-1 pod-2

# Remove process groups across clusters in the current namespace
kubectl fdb remove process-group pod-1-cluster-A pod-2-cluster-B -l your-cluster-label

# Remove process groups for a cluster in the namespace default
kubectl fdb -n default remove process-group -c cluster pod-1 pod-2

# Remove process groups for a cluster with the process group ID.
# The process group ID of a Pod can be fetched with "kubectl get po -L foundationdb.org/fdb-process-group-id"
kubectl fdb -n default remove process-group --use-process-group-id -c cluster storage-1 storage-2

# Remove all failed process groups for a cluster (all process groups that have a missing process)
kubectl fdb -n default remove process-group -c cluster --remove-all-failed

# Remove all processes in the cluster that have the given process-class (incompatible with passing pod names or process group IDs)
kubectl fdb -n default remove process-group -c cluster --process-class="stateless"
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "remove process groups from the provided cluster.")
	cmd.Flags().String("process-class", "", "remove process groups matching the provided value in the provided cluster.  Using this option ignores provided ids.")
	cmd.Flags().StringP("cluster-label", "l", fdbv1beta2.FDBClusterLabel, "cluster label used to identify the cluster for a requested pod")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the process groups should be removed with exclusion.")
	cmd.Flags().Bool("remove-all-failed", false, "define if all failed processes should be replaced.")
	cmd.Flags().Bool("use-process-group-id", false, "define if the process-group should be used instead of the Pod name.")

	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

type replaceProcessGroupsOptions struct {
	clusterLabel      string
	processClass      string
	withExclusion     bool
	wait              bool
	removeAllFailed   bool
	useProcessGroupID bool
}

// replaceProcessGroups adds process groups to the removal list of their respective clusters, and returns a count of
// the number of removed processGroups (pods) and any encountered error.
// If clusterName is specified, it will ONLY do so for the specified cluster.
// If processClass is specified, it will ignore the given ids and remove all processes in the given cluster whose pods
// have a processClassLabel matching the processClass.
func replaceProcessGroups(cmd *cobra.Command, kubeClient client.Client, clusterName string, ids []string, namespace string, opts replaceProcessGroupsOptions) (int, error) {
	if len(ids) == 0 && !opts.removeAllFailed && opts.processClass == "" {
		return 0, nil
	}

	// cross-cluster logic: given a list of Pod names, we can look up the FDB clusters by pod label, and work across clusters
	if !opts.useProcessGroupID && clusterName == "" {
		pgsByCluster, err := fetchProcessGroupsCrossCluster(kubeClient, namespace, opts.clusterLabel, ids...)
		if err != nil {
			return 0, err
		}
		totalRemoved := 0
		for cluster, pgs := range pgsByCluster {
			err := replaceProcessGroupsFromCluster(kubeClient, cluster, pgs, namespace, opts.withExclusion, opts.wait, opts.removeAllFailed)
			if err != nil {
				return 0, err
			}
			totalRemoved += len(pgs)
			cmd.Printf("Cluster %v/%v:\n", namespace, cluster.Name)
			cmd.Printf("removed %v (exclude: %t)\n", pgs, opts.withExclusion)
		}
		return totalRemoved, nil
	}

	// single-cluster logic
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return 0, fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return 0, err
	}

	var processGroupIDs []fdbv1beta2.ProcessGroupID
	if opts.processClass != "" { // match against a whole process class, ignore provided ids
		if len(ids) != 0 {
			return 0, fmt.Errorf("process identifiers were provided along with a processClass and would be ignored, please only provide one or the other")
		}
		processGroupIDs = getProcessGroupIdsWithClass(cluster, opts.processClass)
		if len(processGroupIDs) == 0 {
			return 0, fmt.Errorf("found no processGroups of processClass '%s' in cluster %s", opts.processClass, clusterName)
		}
	} else if !opts.useProcessGroupID { // match by pod name
		processGroupIDs, err = getProcessGroupIDsFromPodName(cluster, ids)
		if err != nil {
			return 0, err
		}
	} else { // match by process group ID
		for _, id := range ids {
			processGroupIDs = append(processGroupIDs, fdbv1beta2.ProcessGroupID(id))
		}
	}
	err = replaceProcessGroupsFromCluster(kubeClient, cluster, processGroupIDs, namespace, opts.withExclusion, opts.wait, opts.removeAllFailed)
	if err != nil {
		return 0, err
	}
	cmd.Printf("Cluster %v/%v:\n", namespace, clusterName)
	cmd.Printf("removed %v (exclude: %t)\n", processGroupIDs, opts.withExclusion)
	return len(processGroupIDs), nil
}

// replaceProcessGroupsFromCluster removes the process groups ONLY from the specified cluster.
// It also returns the list of processGroupIDs that it removed from the cluster.
func replaceProcessGroupsFromCluster(kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, processGroupIDs []fdbv1beta2.ProcessGroupID, namespace string, withExclusion bool, wait bool, removeAllFailed bool) error {
	patch := client.MergeFrom(cluster.DeepCopy())

	processGroupSet := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
	for _, processGroup := range processGroupIDs {
		processGroupSet[processGroup] = fdbv1beta2.None{}
	}

	if removeAllFailed {
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

	if wait {
		if !confirmAction(fmt.Sprintf("Remove %v from cluster %s/%s with exclude: %t", processGroupIDs, namespace, cluster.Name, withExclusion)) {
			return fmt.Errorf("user aborted the removal")
		}
	}

	var processGroupIDsForRemoval []fdbv1beta2.ProcessGroupID
	if withExclusion {
		processGroupIDsForRemoval = cluster.GetProcessGroupsToRemove(processGroupIDs)
		cluster.Spec.ProcessGroupsToRemove = processGroupIDsForRemoval
	} else {
		processGroupIDsForRemoval = cluster.GetProcessGroupsToRemoveWithoutExclusion(processGroupIDs)
		cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = processGroupIDsForRemoval
	}

	return kubeClient.Patch(ctx.TODO(), cluster, patch)
}
