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
	"log"
	"time"

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
		Short: "Adds a process group (or multiple) to the remove list of the given cluster",
		Long:  "Adds a process group (or multiple) to the remove list field of the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			sleep, err := cmd.Root().Flags().GetUint16("sleep")
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
			useProcessGroupID, err := cmd.Flags().GetBool("use-process-group-id")
			if err != nil {
				return err
			}
			removeAllFailed, err := cmd.Flags().GetBool("remove-all-failed")
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

			return replaceProcessGroups(kubeClient, cluster, args, namespace, withExclusion, wait, removeAllFailed, useProcessGroupID, sleep)
		},
		Example: `
# Remove process groups for a cluster in the current namespace
kubectl fdb remove process-group -c cluster pod-1 -i pod-2

# Remove process groups for a cluster in the namespace default
kubectl fdb -n default remove process-group -c cluster pod-1 pod-2

# Remove process groups for a cluster with the process group ID.
# The process group ID of a Pod can be fetched with "kubectl get po -L foundationdb.org/fdb-process-group-id"
kubectl fdb -n default remove process-group --use-process-group-id -c cluster storage-1 storage-2

# Remove all failed process groups for a cluster (all process groups that have a missing process)
kubectl fdb -n default remove process-group -c cluster --remove-all-failed
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "remove process groupss from the provided cluster.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the process groups should be removed with exclusion.")
	cmd.Flags().Bool("remove-all-failed", false, "define if all failed processes should be replaced.")
	cmd.Flags().Bool("use-process-group-id", false, "define if the process-group should be used instead of the Pod name.")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// replaceProcessGroups adds process groups to the removal list of the cluster
func replaceProcessGroups(kubeClient client.Client, clusterName string, ids []string, namespace string, withExclusion bool, wait bool, removeAllFailed bool, useProcessGroupID bool, sleep uint16) error {
	if len(ids) == 0 && !removeAllFailed {
		return nil
	}

	cluster, err := loadCluster(kubeClient, namespace, clusterName)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return err
	}

	// In this case the user has Pod name specified
	var processGroupIDs []fdbv1beta2.ProcessGroupID
	if !useProcessGroupID {
		processGroupIDs, err = getProcessGroupIDsFromPodName(cluster, ids)
		if err != nil {
			return err
		}
	} else {
		for _, id := range ids {
			processGroupIDs = append(processGroupIDs, fdbv1beta2.ProcessGroupID(id))
		}
	}

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

			needsReplacement, _ := processGroupStatus.NeedsReplacement(0)
			if needsReplacement {
				processGroupIDs = append(processGroupIDs, processGroupStatus.ProcessGroupID)
			}
		}
	}

	if wait {
		if !confirmAction(fmt.Sprintf("Remove %v from cluster %s/%s with exclude: %t", processGroupIDs, namespace, clusterName, withExclusion)) {
			return fmt.Errorf("user aborted the removal")
		}
	}

	if sleep > 0 {
		for _, processGroupID := range processGroupIDs {
			addProcessGroups([]fdbv1beta2.ProcessGroupID{processGroupID}, withExclusion, cluster)
			time.Sleep(time.Duration(sleep) * time.Second)
		}
	} else {
		addProcessGroups(processGroupIDs, withExclusion, cluster)
	}

	return kubeClient.Patch(ctx.TODO(), cluster, patch)
}

func addProcessGroups(processGroupIDs []fdbv1beta2.ProcessGroupID, withExclusion bool, cluster *fdbv1beta2.FoundationDBCluster) {
	if withExclusion {
		cluster.AddProcessGroupsToRemovalList(processGroupIDs)
	} else {
		cluster.AddProcessGroupsToRemovalWithoutExclusionList(processGroupIDs)
	}
}
