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
	ctx "context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
)

func newFixCoordinatorIPsCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "fix-coordinator-ips",
		Short: "Update the coordinator IPs in the cluster file",
		Long:  "Update the coordinator IPs in the cluster file",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}

			dryRun, err := cmd.Flags().GetBool("dry-run")
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)

			kubeClient, err := client.New(config, client.Options{Scheme: scheme})
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

			err = runFixCoordinatorIPs(kubeClient, cluster, *o.configFlags.Context, namespace, dryRun)
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
func buildClusterFileUpdateCommands(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client, context string, namespace string, kubectlPath string) ([]exec.Cmd, error) {
	pods := &corev1.PodList{}

	selector := labels.NewSelector()

	err := internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
	if err != nil {
		return nil, err
	}

	for key, value := range cluster.Spec.LabelConfig.MatchLabels {
		requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*requirement)
	}

	processClassRequirement, err := labels.NewRequirement(fdbtypes.FDBProcessClassLabel, selection.Exists, nil)
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*processClassRequirement)

	err = kubeClient.List(ctx.Background(), pods,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
		client.MatchingFields{"status.phase": "Running"},
	)
	if err != nil {
		return nil, err
	}

	baseArgs := []string{kubectlPath, "--namespace", namespace}
	if context != "" {
		baseArgs = append(baseArgs, "--context", context)
	}
	baseArgs = append(baseArgs, "exec", "-it", "-c", "foundationdb")

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
func updateIPsInConnectionString(cluster *fdbtypes.FoundationDBCluster) error {
	connectionString, err := fdbtypes.ParseConnectionString(cluster.Status.ConnectionString)
	if err != nil {
		return err
	}
	newCoordinators := make([]string, len(connectionString.Coordinators))
	for coordinatorIndex, coordinator := range connectionString.Coordinators {
		coordinatorAddress, err := fdbtypes.ParseProcessAddress(coordinator)
		if err != nil {
			return err
		}
		for _, processGroup := range cluster.Status.ProcessGroups {
			for _, address := range processGroup.Addresses {
				if address == coordinatorAddress.IPAddress.String() {
					coordinatorAddress.IPAddress = net.ParseIP(processGroup.Addresses[len(processGroup.Addresses)-1])
					newCoordinators[coordinatorIndex] = coordinatorAddress.String()
				}
			}
		}
		if newCoordinators[coordinatorIndex] == "" {
			log.Printf("Could not find process for coordinator IP %s", coordinator)
			newCoordinators[coordinatorIndex] = coordinator
		}
	}
	connectionString.Coordinators = newCoordinators
	cluster.Status.ConnectionString = connectionString.String()

	return nil
}

func runFixCoordinatorIPs(kubeClient client.Client, cluster *fdbtypes.FoundationDBCluster, context string, namespace string, dryRun bool) error {
	patch := client.MergeFrom(cluster.DeepCopy())
	err := updateIPsInConnectionString(cluster)
	if err != nil {
		return err
	}

	log.Printf("New connection string: %s", cluster.Status.ConnectionString)

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
			log.Printf("Update command: %s", strings.Join(command.Args, " "))
		} else {
			err := command.Run()
			if err != nil {
				log.Print(err.Error())
			}
		}
	}

	if !dryRun {
		err = kubeClient.Status().Patch(ctx.Background(), cluster, patch)
		if err != nil {
			return err
		}
	}

	return nil
}
