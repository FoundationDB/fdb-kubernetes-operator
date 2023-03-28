/*
 * buggify_crash_loop.go
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
	"log"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newBuggifyCrashLoop(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "crash-loop",
		Short: "Updates the crash-loop list of the given cluster",
		Long:  "Updates the crash-loop list of the given cluster",
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
			containerName, err := cmd.Flags().GetString("container-name")
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

			return updateCrashLoopContainerList(kubeClient, cluster, containerName, args, namespace, wait, clear, clean)
		},
		Example: `

# Add process groups into crash loop state for a cluster in the current namespace with container name
kubectl fdb buggify crash-loop -c cluster --container-name container-name pod-1 pod-2

# Remove process groups from crash loop state from a cluster in the current namespace with container name
kubectl fdb buggify crash-loop --clear -c cluster --container-name container-name pod-1 pod-2

# Clean crash loop list of a cluster in the current namespace with container name
kubectl fdb buggify crash-loop --clean -c cluster --container-name container-name
`,
	}
	cmd.Flags().StringP("fdb-cluster", "c", "", "updates the crash-loop list in the provided cluster.")
	cmd.Flags().String("container-name", fdbv1beta2.MainContainerName, "container name to which we want to add/remove process groups.")
	cmd.Flags().Bool("clear", false, "removes the process groups from the crash-loop list.")
	cmd.Flags().Bool("clean", false, "removes all process groups from the crash-loop list.")
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

// updateCrashLoopContainerList updates the crash-loop container-list of the cluster
func updateCrashLoopContainerList(kubeClient client.Client, clusterName string, containerName string, pods []string, namespace string, wait bool, clear bool, clean bool) error {
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return err
	}

	processGroupIDs, err := getProcessGroupIDsFromPodName(cluster, pods)
	if err != nil {
		return err
	}

	patch := client.MergeFrom(cluster.DeepCopy())
	if clean {
		if wait {
			if !confirmAction(fmt.Sprintf("Clearing crash-loop list for %s from cluster %s/%s", containerName, namespace, clusterName)) {
				return fmt.Errorf("user aborted the removal")
			}
		}
		containerIdx := 0
		for _, crashLoopContainerObj := range cluster.Spec.Buggify.CrashLoopContainers {
			if crashLoopContainerObj.ContainerName != containerName {
				containerIdx++
				continue
			}
			crashLoopContainerObj.Targets = nil
			cluster.Spec.Buggify.CrashLoopContainers[containerIdx] = crashLoopContainerObj
			break
		}
		return kubeClient.Patch(ctx.TODO(), cluster, patch)
	}

	if len(processGroupIDs) == 0 {
		return fmt.Errorf("please provide at least one Pod")
	}

	if wait {
		if clear {
			if !confirmAction(fmt.Sprintf("Removing %v from container: %s in crash-loop container list of the cluster %s/%s", processGroupIDs, containerName, namespace, clusterName)) {
				return fmt.Errorf("user aborted the removal")
			}
		} else {
			if !confirmAction(fmt.Sprintf("Adding %v to container: %s in crash-loop container list of the cluster %s/%s", processGroupIDs, containerName, namespace, clusterName)) {
				return fmt.Errorf("user aborted the removal")
			}
		}
	}

	if clear {
		cluster.RemoveProcessGroupsFromCrashLoopContainerList(processGroupIDs, containerName)
	} else {
		cluster.AddProcessGroupsToCrashLoopContainerList(processGroupIDs, containerName)
	}

	return kubeClient.Patch(ctx.TODO(), cluster, patch)
}
