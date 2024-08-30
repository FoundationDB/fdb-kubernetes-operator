/*
 * fix_coordinator_ips.go
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
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
)

func newFixCoordinatorIPsCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "fix-coordinator-ips",
		Short: "Update the coordinator IPs in the cluster file",
		Long:  "Update the coordinator IPs in the cluster file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}

			dryRun, err := cmd.Flags().GetBool("dry-run")
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

			cluster, err := loadCluster(kubeClient, namespace, clusterName)
			if err != nil {
				return err
			}

			err = runFixCoordinatorIPs(cmd.Context(), cmd, kubeClient, cluster, *o.configFlags.Context, namespace, dryRun)
			if err != nil {
				return err
			}

			return nil
		},
		Example: `
  # Update the coordinator IPs for the cluster
  kubectl fdb fix-coordinator-ips -c cluster
  `,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "update the provided cluster.")
	err := cmd.MarkFlagRequired("fdb-cluster")
	cmd.Flags().Bool("dry-run", false, "Print the new connection string without updating it")
	if err != nil {
		log.Fatal(err)
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// buildClusterFileUpdateCommands generates commands for using kubectl exec to
// update the cluster file in the pods for a cluster.
func buildClusterFileUpdateCommands(cluster *fdbv1beta2.FoundationDBCluster, kubeClient client.Client, kubeContext string, namespace string, kubectlPath string) ([]exec.Cmd, error) {
	pods := &corev1.PodList{}

	selector := labels.NewSelector()

	err := internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
	if err != nil {
		return nil, err
	}

	for key, value := range cluster.GetMatchLabels() {
		requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*requirement)
	}

	processClassRequirement, err := labels.NewRequirement(cluster.GetProcessClassLabel(), selection.Exists, nil)
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*processClassRequirement)

	err = kubeClient.List(context.Background(), pods,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
		client.MatchingFields{"status.phase": "Running"},
	)
	if err != nil {
		return nil, err
	}

	baseArgs := []string{kubectlPath, "--namespace", namespace}
	if kubeContext != "" {
		baseArgs = append(baseArgs, "--context", kubeContext)
	}
	baseArgs = append(baseArgs, "exec", "-it", "-c", fdbv1beta2.MainContainerName)

	execArgs := []string{"--", "bash", "-c", fmt.Sprintf("echo %s > /var/fdb/data/fdb.cluster && pkill fdbserver", cluster.Status.ConnectionString)}
	execCommands := make([]exec.Cmd, 0, len(pods.Items))

	for _, pod := range pods.Items {
		args := append(baseArgs, pod.Name)
		args = append(args, execArgs...)
		execCommands = append(execCommands, exec.Cmd{
			Path:   kubectlPath,
			Args:   args,
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		})
	}

	return execCommands, nil
}

// updateIPsInConnectionString updates the connection string in the cluster
// status by replacing old coordinator IPs with the latest IPs.
func updateIPsInConnectionString(ctx context.Context, cmd *cobra.Command, cluster *fdbv1beta2.FoundationDBCluster, kubeClient client.Client) error {
	connectionString, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)
	if err != nil {
		return err
	}

	// Fetch the associated process group from the coordinator address.
	coordinatorProcessGroup := map[string]*fdbv1beta2.ProcessGroupStatus{}
	for _, coordinator := range connectionString.Coordinators {
		coordinatorAddress, err := fdbv1beta2.ParseProcessAddress(coordinator)
		if err != nil {
			return err
		}

		var processGroupFound bool
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroupFound {
				break
			}
			for _, address := range processGroup.Addresses {
				if address == coordinatorAddress.IPAddress.String() {
					coordinatorProcessGroup[coordinatorAddress.MachineAddress()] = processGroup

					cmd.Println(coordinatorAddress.MachineAddress(), "is associated with process group:", processGroup.ProcessGroupID)
					processGroupFound = true
				}
				break
			}
		}
	}

	// Update the new coordinators
	newCoordinators := make([]string, len(connectionString.Coordinators))
	for coordinatorIndex, coordinator := range connectionString.Coordinators {
		coordinatorAddress, err := fdbv1beta2.ParseProcessAddress(coordinator)
		if err != nil {
			return err
		}

		processGroup, ok := coordinatorProcessGroup[coordinatorAddress.MachineAddress()]
		if !ok {
			// Keep the old address if the coordinator process group is missing.
			newCoordinators[coordinatorIndex] = coordinatorAddress.String()
			cmd.Println("ProcessGroup for", coordinatorAddress.MachineAddress(), "is missing in the FoundationDBCluster status, coordinator address will be ignored")
			continue
		}

		// Fetch the IP address from the running Pod, if the Pod doesn't exist or is not running, we fall back to the process group address.
		pod := &corev1.Pod{}
		kubeErr := kubeClient.Get(ctx, client.ObjectKey{Name: processGroup.GetPodName(cluster), Namespace: cluster.Namespace}, pod)
		if k8serrors.IsNotFound(kubeErr) || len(pod.Status.PodIPs) == 0 {
			cmd.Println("Pod for process group", processGroup.ProcessGroupID, "not found will try to read information from FoundationDBCluster status")
			for _, address := range processGroup.Addresses {
				if address == coordinatorAddress.IPAddress.String() {
					coordinatorAddress.IPAddress = net.ParseIP(processGroup.Addresses[len(processGroup.Addresses)-1])
				}
			}
		} else { // Update the Coordinator address from the running Pod information.
			// Logs are discarded right now until we implement a log.Logger in the plugin.
			publicIPs := internal.GetPublicIPsForPod(pod, logr.Discard())
			if len(publicIPs) == 0 {
				cmd.Println("Couldn't find addresses for Pod", pod.Name)
			}

			cmd.Println("Update the coordinator address for", coordinatorAddress.IPAddress.String(), "to new IP address:", publicIPs[0])
			coordinatorAddress.IPAddress = net.ParseIP(publicIPs[0])
		}

		newCoordinators[coordinatorIndex] = coordinatorAddress.String()

		if newCoordinators[coordinatorIndex] == "" {
			cmd.Println("Could not find process for coordinator IP", coordinator)
			newCoordinators[coordinatorIndex] = coordinator
		}
	}

	connectionString.Coordinators = newCoordinators
	cluster.Status.ConnectionString = connectionString.String()

	return nil
}

func runFixCoordinatorIPs(ctx context.Context, cmd *cobra.Command, kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, context string, namespace string, dryRun bool) error {
	cmd.Println("Current connection string:", cluster.Status.ConnectionString)
	patch := client.MergeFrom(cluster.DeepCopy())
	err := updateIPsInConnectionString(ctx, cmd, cluster, kubeClient)
	if err != nil {
		return err
	}

	cmd.Println("New connection string:", cluster.Status.ConnectionString)

	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		return err
	}

	commands, err := buildClusterFileUpdateCommands(cluster, kubeClient, context, namespace, kubectlPath)
	if err != nil {
		return err
	}

	for _, command := range commands {
		if dryRun {
			cmd.Println("Update command:", strings.Join(command.Args, " "))
		} else {
			err := command.Run()
			if err != nil {
				cmd.Println(err.Error())
			}
		}
	}

	if !dryRun {
		// Update the ConfigMap to sync the new connection string.
		newConfigMap, err := internal.GetConfigMap(cluster)
		if err != nil {
			cmd.Println(err.Error())
		}

		kubeErr := kubeClient.Update(ctx, newConfigMap)
		if kubeErr != nil {
			cmd.Print(kubeErr.Error())
		}

		kubeErr = kubeClient.Status().Patch(ctx, cluster, patch)
		if kubeErr != nil {
			return kubeErr
		}
	}

	return nil
}
