/*
 * buggify_empty_monitor_conf.go
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

func newBuggifyEmptyMonitorConf(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "empty-monitor-conf",
		Short: "Instructs the operator to update all of the fdbmonitor.conf files to have zero fdbserver processes configured",
		Long:  "Instructs the operator to update all of the fdbmonitor.conf files to have zero fdbserver processes configured",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			unset, err := cmd.Flags().GetBool("unset")
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

			return updateMonitorConf(kubeClient, cluster, namespace, wait, unset)
		},
		Example: `
# Setting empty-monitor-conf to true
kubectl fdb buggify empty-monitor-conf -c cluster

# Setting empty-monitor-conf to false
kubectl fdb buggify empty-monitor-conf --unset -c cluster
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "updates the empty-monitor-conf for the cluster.")
	cmd.Flags().Bool("unset", false, "unset the empty-monitor-conf to false.")
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
func updateMonitorConf(kubeClient client.Client, clusterName string, namespace string, wait bool, unset bool) error {
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return err
	}

	patch := client.MergeFrom(cluster.DeepCopy())

	if wait {
		confirmed := confirmAction(fmt.Sprintf("Setting empty-monitor-conf to %v for cluster %s/%s", !unset, namespace, clusterName))
		if !confirmed {
			return fmt.Errorf("user aborted the removal")
		}
	}

	cluster.Spec.Buggify.EmptyMonitorConf = !unset
	return kubeClient.Patch(ctx.TODO(), cluster, patch)
}
