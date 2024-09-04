/*
 * buggify_no_schedule.go
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
	ctx "context"
	"fmt"

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
		Long:  "Updates the no-schedule list of the given cluster\"",
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

			processGroupSelectionOpts, err := getProcessSelectionOptsFromFlags(cmd, o, args)
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(cmd.Context(), o)
			if err != nil {
				return err
			}

			return updateNoScheduleList(cmd, kubeClient,
				buggifyProcessGroupOptions{
					wait:  wait,
					clear: clear,
					clean: clean,
				}, processGroupSelectionOpts)
		},
		Example: `
# Add process groups into no-schedule state for a cluster in the current namespace
kubectl fdb buggify no-schedule -c cluster pod-1 pod-2

# Remove process groups from no-schedule state from a cluster in the current namespace
kubectl fdb buggify no-schedule --clear -c cluster pod-1 pod-2

# Remove process groups from no-schedule state from a cluster in the current namespace across clusters
kubectl fdb buggify no-schedule --clear pod-1-cluster-A pod-2-cluster-B -l your-cluster-label

# Clean no-schedule list of a cluster in the current namespace
kubectl fdb buggify no-schedule  --clean -c cluster

# Add process groups into no-schedule state for a cluster in the namespace default
kubectl fdb -n default buggify no-schedule  -c cluster pod-1 pod-2

See help for even more process group selection options, such as by processClass, conditions, and processGroupID!
`,
	}
	addProcessSelectionFlags(cmd)
	cmd.Flags().Bool("clear", false, "removes the process groups from the no-schedule list.")
	cmd.Flags().Bool("clean", false, "removes all process groups from the no-schedule list.")
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// updateNoScheduleList updates the removal list of the cluster
func updateNoScheduleList(cmd *cobra.Command, kubeClient client.Client, opts buggifyProcessGroupOptions, processGroupOpts processGroupSelectionOptions) error {
	if opts.clean {
		if processGroupOpts.clusterName == "" {
			return fmt.Errorf("clean option requires cluster-name argument")
		}
		cluster, err := loadCluster(kubeClient, processGroupOpts.namespace, processGroupOpts.clusterName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return fmt.Errorf("could not get cluster: %s/%s", processGroupOpts.namespace, processGroupOpts.clusterName)
			}
			return err
		}
		patch := client.MergeFrom(cluster.DeepCopy())
		if opts.wait {
			if !confirmAction(fmt.Sprintf("Clearing no-schedule list from cluster %s/%s", processGroupOpts.namespace, processGroupOpts.clusterName)) {
				return fmt.Errorf("user aborted the removal")
			}
		}
		cluster.Spec.Buggify.NoSchedule = nil
		return kubeClient.Patch(ctx.TODO(), cluster, patch)
	}

	processGroupsByCluster, err := getProcessGroupsByCluster(cmd, kubeClient, processGroupOpts)
	if err != nil {
		return err
	}

	for cluster, processGroupIDs := range processGroupsByCluster {
		patch := client.MergeFrom(cluster.DeepCopy())
		if len(processGroupIDs) == 0 {
			return fmt.Errorf("please provide at least one pod")
		}

		if opts.clear {
			if opts.wait && !confirmAction(fmt.Sprintf("Removing %v from no-schedule from cluster %s/%s", processGroupIDs, processGroupOpts.namespace, processGroupOpts.clusterName)) {
				return fmt.Errorf("user aborted the removal")
			}
			cluster.RemoveProcessGroupsFromNoScheduleList(processGroupIDs)
		} else {
			if opts.wait && !confirmAction(fmt.Sprintf("Adding %v from no-schedule from cluster %s/%s", processGroupIDs, processGroupOpts.namespace, processGroupOpts.clusterName)) {
				return fmt.Errorf("user aborted the removal")
			}
			cluster.AddProcessGroupsToNoScheduleList(processGroupIDs)
		}
		err = kubeClient.Patch(ctx.TODO(), cluster, patch)
		if err != nil {
			return err
		}
	}
	return nil
}
