/*
 * buggify_no_schedule.go
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

	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newBuggifyNoSchedule(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "no-schedule",
		Short: "Updates the no-schedule list of the given cluster",
		Long:  "Updates the no-schedule list field of the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			clear, err := cmd.Flags().GetBool("clear")
			if err != nil {
				return err
			}
			clean, err := cmd.Flags().GetBool("clean")
			if err != nil {
				return err
			}
			cluster, err := cmd.Flags().GetString("fdb-cluster")
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

			processGroups := args
			processGroups, err = getProcessGroupIDsFromPod(kubeClient, cluster, processGroups, namespace)
			if err != nil {
				return err
			}

			return updateNoScheduleList(kubeClient, cluster, processGroups, namespace, wait, clear, clean)
		},
		Example: `
# Add process groups into NoSchedule state for a cluster in the current namespace
kubectl fdb buggify no-schedule -c cluster pod-1 pod-2

# Remove process groups from NoSchedule state from a cluster in the current namespace
kubectl fdb buggify no-schedule --clear -c cluster pod-1 pod-2

# Clean NoSchedule list of a cluster in the current namespace
kubectl fdb buggify no-schedule  --clean -c cluster

# Add process groups into NoSchedule state for a cluster in the namespace default
kubectl fdb -n default buggify no-schedule  -c cluster pod-1 pod-2
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "updates the no-schedule list in the provided cluster.")
	cmd.Flags().Bool("clear", false, "removes the process groups from the no-schedule list.")
	cmd.Flags().Bool("clean", false, "removes all process groups from the no-schedule list.")
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

// updateNoScheduleList updates the removal list of the cluster
func updateNoScheduleList(kubeClient client.Client, clusterName string, processGroups []string, namespace string, wait bool, clear bool, clean bool) error {
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return err
	}

	patch := client.MergeFrom(cluster.DeepCopy())
	if clean {
		if wait {
			confirmed := confirmAction(fmt.Sprintf("Clearing no-schedule list from cluster %s/%s", namespace, clusterName))
			if !confirmed {
				return fmt.Errorf("user aborted the removal")
			}
		}
		cluster.Spec.Buggify.NoSchedule = nil
		return kubeClient.Patch(ctx.TODO(), cluster, patch)
	}

	if len(processGroups) == 0 {
		return fmt.Errorf("please provide atleast one pod")
	}

	if wait {
		if clear {
			confirmed := confirmAction(fmt.Sprintf("Removing %v from no-schedule from cluster %s/%s", processGroups, namespace, clusterName))
			if !confirmed {
				return fmt.Errorf("user aborted the removal")
			}
		} else {
			confirmed := confirmAction(fmt.Sprintf("Adding %v from no-schedule from cluster %s/%s", processGroups, namespace, clusterName))
			if !confirmed {
				return fmt.Errorf("user aborted the removal")
			}
		}
	}

	if clear {
		cluster.RemoveProcessGroupsFromNoScheduleList(processGroups)
	} else {
		cluster.AddProcessGroupsToNoScheduleList(processGroups)
	}

	return kubeClient.Patch(ctx.TODO(), cluster, patch)
}
