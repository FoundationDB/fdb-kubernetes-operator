/*
 * recover_multi_region_cluster.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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
	"os"
	"path"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/internal/kubernetes"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// recoverMultiRegionClusterOpts struct to pass down all args to the actual runner.
type recoverMultiRegionClusterOpts struct {
	client      client.Client
	config      *rest.Config
	clusterName string
	namespace   string
}

func newRecoverMultiRegionClusterCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "recover-multi-region-cluster",
		Short: "Recover a multi-region cluster if a majority of coordinators is lost permanently",
		Long:  "Recover a multi-region cluster if a majority of coordinators is lost permanently",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}

			if len(args) != 1 {
				return fmt.Errorf("exactly one cluster name must be specified, provided args: %v", args)
			}

			clusterName := args[0]

			kubeClient, err := getKubeClient(cmd.Context(), o)
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			if wait {
				confirmed := confirmAction(fmt.Sprintf("WARNING:\nThe cluster: %s/%s will be force recovered.\nOnly perform those steps if you are unable to recover the coordinator state.\nPerforming this action can lead to data loss.",
					namespace, clusterName))
				if !confirmed {
					return fmt.Errorf("aborted recover multi-region aciton")
				}

				confirmed = confirmAction("WARNING:\nIf this is a multi-region cluster, or is spread across different namespaces/Kubernetes clusters.\nEnsure that all Pods of this FDB cluster: %s in the other namespaces/Kubernetes clusters are deleted and shutdown.")
				if !confirmed {
					return fmt.Errorf("aborted recover multi-region aciton")
				}
			}

			return recoverMultiRegionCluster(cmd,
				recoverMultiRegionClusterOpts{
					client:      kubeClient,
					config:      config,
					clusterName: clusterName,
					namespace:   namespace,
				})
		},
		Example: `
# Recover the multi-region cluster "sample-cluster-1" in the current namespace
kubectl fdb recover-multi-region-cluster sample-cluster-1

# Recover the multi-region cluster "sample-cluster-1" in the "testing" namespace
kubectl fdb recover-multi-region-cluster -n testing sample-cluster-1
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// recoverMultiRegionCluster will forcefully recover a multi-region cluster if a majority of coordinators are lost.
// Performing this action can result in data loss.
func recoverMultiRegionCluster(cmd *cobra.Command, opts recoverMultiRegionClusterOpts) error {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := opts.client.Get(cmd.Context(), client.ObjectKey{Name: opts.clusterName, Namespace: opts.namespace}, cluster)
	if err != nil {
		return err
	}

	err = checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable(cmd, opts.client, opts.config, cluster)
	if err != nil {
		return err
	}

	// Skip the cluster, make sure the operator is not taking any action on the cluster.
	err = setSkipReconciliation(cmd.Context(), opts.client, cluster, true)
	if err != nil {
		return err
	}

	// Fetch the last connection string from the `FoundationDBCluster` status, e.g. `kubectl get fdb ${cluster} -o jsonpath='{ .status.connectionString }'`.
	lastConnectionString := cluster.Status.ConnectionString
	lastConnectionStringParts := strings.Split(lastConnectionString, "@")
	addresses := strings.Split(lastConnectionStringParts[1], ",")
	// Since this is a multi-region cluster, we expect 9 coordinators.
	if len(addresses) != 9 {
		return fmt.Errorf("expected exactly 9 addresses, got %d", len(addresses))
	}
	cmd.Println("current connection string", lastConnectionString)

	var useTLS bool
	coordinators := map[string]fdbv1beta2.ProcessAddress{}
	for _, addr := range addresses {
		parsed, parseErr := fdbv1beta2.ParseProcessAddress(addr)
		if parseErr != nil {
			return parseErr
		}

		cmd.Println("found coordinator", parsed.String())
		coordinators[parsed.MachineAddress()] = parsed
		// If the tls flag is present we assume that the coordinators should make use of TLS.
		_, useTLS = parsed.Flags["tls"]
	}

	cmd.Println("current coordinators", coordinators, "useTLS", useTLS)
	// Fetch all Pods and coordinators for the remote and remote satellite.
	runningCoordinators := map[string]fdbv1beta2.None{}
	newCoordinators := make([]fdbv1beta2.ProcessAddress, 0, 5)
	processCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return err
	}
	candidates := make([]*corev1.Pod, 0, processCounts.Total())

	pods, err := getRunningPodsForCluster(cmd.Context(), opts.client, cluster)
	if err != nil {
		return err
	}

	// Find a running coordinator to copy the coordinator files from.
	var runningCoordinator *corev1.Pod
	for _, pod := range pods.Items {
		addr, parseErr := fdbv1beta2.ParseProcessAddress(pod.Status.PodIP)
		if parseErr != nil {
			return parseErr
		}

		loopPod := pod
		if coordinatorAddr, ok := coordinators[addr.MachineAddress()]; ok {
			cmd.Println("Found coordinator for cluster", pod.Name, "address", addr.MachineAddress())
			runningCoordinators[addr.MachineAddress()] = fdbv1beta2.None{}
			newCoordinators = append(newCoordinators, coordinatorAddr)

			runningCoordinator = &loopPod
			continue
		}

		if !internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta).IsStateful() {
			continue
		}

		candidates = append(candidates, &loopPod)
	}

	if runningCoordinator == nil {
		return fmt.Errorf("could not find any running coordinator for this cluster")
	}

	// Pick 5 new coordinators.
	needsUpload := make([]*corev1.Pod, 0, 5)
	for len(newCoordinators) < 5 {
		cmd.Println("Current coordinators:", len(newCoordinators))
		candidate := candidates[len(newCoordinators)]
		addr, parseErr := fdbv1beta2.ParseProcessAddress(candidate.Status.PodIP)
		if parseErr != nil {
			return parseErr
		}
		cmd.Println("Adding pod as new coordinators:", candidate.Name)
		if useTLS {
			addr.Port = 4500
			addr.Flags = map[string]bool{"tls": true}
		} else {
			addr.Port = 4501
		}
		newCoordinators = append(newCoordinators, addr)
		needsUpload = append(needsUpload, candidate)
	}

	// Copy the coordinator state from one of the running coordinators to your local machine:
	coordinatorFiles := []string{"coordination-0.fdq", "coordination-1.fdq"}
	tmpCoordinatorFiles := make([]string, 2)
	tmpDir := os.TempDir()
	for idx, coordinatorFile := range coordinatorFiles {
		tmpCoordinatorFiles[idx] = path.Join(tmpDir, coordinatorFile)
	}

	cmd.Println("tmpCoordinatorFiles", tmpCoordinatorFiles, "checking the location of the coordination-0.fdq in Pod", runningCoordinator.Name)
	stdout, stderr, err := kubeHelper.ExecuteCommandOnPod(context.Background(), opts.client, opts.config, runningCoordinator, fdbv1beta2.MainContainerName, "find /var/fdb/data/ -type f -name 'coordination-0.fdq' -print -quit | head -n 1", false)
	if err != nil {
		cmd.Println(stderr)
		return err
	}

	lines := strings.Split(stdout, "\n")
	if len(lines) == 0 {
		return fmt.Errorf("no coordination file found in %s", runningCoordinator.Name)
	}

	dataDir := path.Dir(strings.TrimSpace(lines[0]))
	cmd.Println("dataDir:", dataDir)
	for idx, coordinatorFile := range coordinatorFiles {
		err = downloadCoordinatorFile(cmd, opts.client, opts.config, runningCoordinator, path.Join(dataDir, coordinatorFile), tmpCoordinatorFiles[idx])
		if err != nil {
			return err
		}
	}

	for _, target := range needsUpload {
		targetDataDir := getDataDir(dataDir, target, cluster)

		for idx, coordinatorFile := range coordinatorFiles {
			err = uploadCoordinatorFile(cmd, opts.client, opts.config, target, tmpCoordinatorFiles[idx], path.Join(targetDataDir, coordinatorFile))
			if err != nil {
				return err
			}
		}
	}

	// Update the `ConfigMap` to contain the new connection string, the new connection string must contain the still existing coordinators and the new coordinators. The old entries must be removed.
	var newConnectionString strings.Builder
	newConnectionString.WriteString(lastConnectionStringParts[0])
	newConnectionString.WriteString("@")
	for idx, coordinator := range newCoordinators {
		newConnectionString.WriteString(coordinator.String())
		if idx == len(newCoordinators)-1 {
			break
		}

		newConnectionString.WriteString(",")
	}

	newCS := newConnectionString.String()
	cmd.Println("new connection string:", newCS)
	err = updateConnectionString(cmd.Context(), opts.client, cluster, newCS)
	if err != nil {
		return err
	}

	// Wait ~1 min until the `ConfigMap` is synced to all Pods, you can check the `/var/dynamic-conf/fdb.cluster` inside a Pod if you are unsure.
	time.Sleep(2 * time.Minute)

	cmd.Println("Killing fdbserver processes")
	// Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).
	err = restartFdbserverInCluster(cmd.Context(), opts.client, opts.config, cluster)
	if err != nil {
		return err
	}

	// Wait until all fdbservers have started again.
	time.Sleep(1 * time.Minute)

	command := []string{"fdbcli", "--exec", fmt.Sprintf("force_recovery_with_data_loss %s", cluster.Spec.DataCenter)}
	// Now you can exec into a container and use `fdbcli` to connect to the cluster.
	// If you use a multi-region cluster you have to issue `force_recovery_with_data_loss`
	var attempts int
	var failOverErr error
	for attempts < 5 {
		cmd.Println("Triggering force recovery with command:", command, "attempt:", attempts)
		failOverErr = kubeHelper.ExecuteCommandRaw(cmd.Context(), opts.client, opts.config, runningCoordinator.Namespace, runningCoordinator.Name, fdbv1beta2.MainContainerName, command, nil, cmd.OutOrStdout(), cmd.OutOrStderr(), false)
		if failOverErr != nil {
			cmd.Println("failed:", failOverErr.Error(), "waiting 15 seconds")
			time.Sleep(15 * time.Second)
			attempts++
			continue
		}

		break
	}

	if failOverErr != nil {
		return failOverErr
	}

	newDatabaseConfiguration := cluster.Spec.DatabaseConfiguration.FailOver()
	// Drop the multi-region configuration.
	newDatabaseConfiguration.Regions = []fdbv1beta2.Region{
		{
			DataCenters: []fdbv1beta2.DataCenter{
				{
					ID: cluster.Spec.DataCenter,
				},
			},
		},
	}

	err = updateDatabaseConfiguration(cmd.Context(), opts.client, cluster, newDatabaseConfiguration)
	if err != nil {
		return err
	}
	// Now you can set `spec.Skip = false` to let the operator take over again.
	// Skip the cluster, make sure the operator is not taking any action on the cluster.
	err = setSkipReconciliation(cmd.Context(), opts.client, cluster, false)
	if err != nil {
		return err
	}

	return nil
}

// getDataDir will return the target data directory to upload the coordinator files to. The directory can be different, depending
// on the used image type and if more than one process should be running inside the Pod.
func getDataDir(dataDir string, pod *corev1.Pod, cluster *fdbv1beta2.FoundationDBCluster) string {
	baseDir := dataDir
	// If the dataDir has a suffix for the process we remove it.
	if dataDir != "/var/fdb/data" {
		baseDir = path.Dir(dataDir)
	}

	// If the unified image is used we can simply return /var/fdb/data/1, as the unified image will always add the process
	// directory, even if only a single process is running inside the Pod.
	if cluster.UseUnifiedImage() {
		return path.Join(baseDir, "/1")
	}

	// In this path we use the split image, so the process directory is only added if more than one process should be running
	processClass := internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta)

	if processClass.IsLogProcess() && cluster.GetLogServersPerPod() > 1 {
		return path.Join(baseDir, "/1")
	}

	if processClass == fdbv1beta2.ProcessClassStorage && cluster.GetStorageServersPerPod() > 1 {
		return path.Join(baseDir, "/1")
	}

	// This is the default case if we are running one process per Pod for this storage class and using the split image.
	return baseDir
}

func downloadCoordinatorFile(cmd *cobra.Command, kubeClient client.Client, config *rest.Config, pod *corev1.Pod, src string, dst string) error {
	tmpCoordinatorFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}

	defer func() {
		_ = tmpCoordinatorFile.Close()
	}()

	cmd.Println("Download files, target:", dst, "source", src, "pod", pod.Name, "namespace", pod.Namespace)
	err = kubeHelper.DownloadFile(cmd.Context(), kubeClient, config, pod, fdbv1beta2.MainContainerName, src, tmpCoordinatorFile)
	if err != nil {
		return err
	}

	fileInfo, err := os.Stat(tmpCoordinatorFile.Name())
	if err != nil {
		return err
	}

	if fileInfo.Size() <= 0 {
		return fmt.Errorf("file %s is empty", tmpCoordinatorFile.Name())
	}

	return nil
}

func uploadCoordinatorFile(cmd *cobra.Command, kubeClient client.Client, config *rest.Config, pod *corev1.Pod, src string, dst string) error {
	tmpCoordinatorFile, err := os.OpenFile(src, os.O_RDONLY, 0600)
	if err != nil {
		return err
	}

	defer func() {
		_ = tmpCoordinatorFile.Close()
	}()

	cmd.Println("Upload files, target:", dst, "source", src, "pod", pod.Name, "namespace", pod.Namespace)

	return kubeHelper.UploadFile(cmd.Context(), kubeClient, config, pod, fdbv1beta2.MainContainerName, tmpCoordinatorFile, dst)
}

func restartFdbserverInCluster(ctx context.Context, kubeClient client.Client, config *rest.Config, cluster *fdbv1beta2.FoundationDBCluster) error {
	pods, err := getRunningPodsForCluster(ctx, kubeClient, cluster)
	if err != nil {
		return err
	}

	// Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).
	for _, pod := range pods.Items {
		_, _, err := kubeHelper.ExecuteCommand(context.Background(), kubeClient, config, pod.Namespace, pod.Name, fdbv1beta2.MainContainerName, "pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true", false)
		if err != nil {
			return err
		}
	}

	return nil
}

func checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable(cmd *cobra.Command, kubeClient client.Client, config *rest.Config, cluster *fdbv1beta2.FoundationDBCluster) error {
	pods, err := getRunningPodsForCluster(cmd.Context(), kubeClient, cluster)
	if err != nil {
		return err
	}

	clientPod, err := kubeHelper.PickRandomPod(pods)
	if err != nil {
		return err
	}

	cmd.Println("Getting the status from:", clientPod.Name)
	status, err := getStatus(cmd.Context(), kubeClient, config, clientPod)
	if err != nil {
		return err
	}

	if status.Client.DatabaseStatus.Available {
		return fmt.Errorf("cluster is available, will abort any further actions")
	}

	if status.Client.DatabaseStatus.Healthy {
		return fmt.Errorf("cluster is healthy, will abort any further actions")
	}

	if status.Client.Coordinators.QuorumReachable {
		return fmt.Errorf("quorum of coordinators are reachable, will abort any further actions")
	}

	return nil
}
