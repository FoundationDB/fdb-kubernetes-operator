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
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/kubernetes"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RecoverMultiRegionClusterOpts struct to pass down all args to the actual runner.
type RecoverMultiRegionClusterOpts struct {
	// Client is the client.Client to interact with the Kubernetes API.
	Client client.Client
	// Config is the rest.Config to interact with the Kubernetes API
	Config *rest.Config
	// ClusterName represents the cluster name of the targeted cluster.
	ClusterName string
	// Namespace represents the namespace of the targeted cluster.
	Namespace string
	// Stdout to print commands stdout output.
	Stdout io.Writer
	// Stderr to print commands stderr output.
	Stderr io.Writer
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

			return RecoverMultiRegionCluster(cmd.Context(),
				RecoverMultiRegionClusterOpts{
					Client:      kubeClient,
					Config:      config,
					ClusterName: clusterName,
					Namespace:   namespace,
					Stdout:      cmd.OutOrStdout(),
					Stderr:      cmd.OutOrStderr(),
				})
		},
		Example: `
# Recover the multi-region cluster "sample-cluster-1" in the current Namespace
kubectl fdb recover-multi-region-cluster sample-cluster-1

# Recover the multi-region cluster "sample-cluster-1" in the "testing" Namespace
kubectl fdb recover-multi-region-cluster -n testing sample-cluster-1
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// RecoverMultiRegionCluster will forcefully recover a multi-region cluster if a majority of coordinators are lost.
// Performing this action can result in data loss.
func RecoverMultiRegionCluster(ctx context.Context, opts RecoverMultiRegionClusterOpts) error {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := opts.Client.Get(ctx, client.ObjectKey{Name: opts.ClusterName, Namespace: opts.Namespace}, cluster)
	if err != nil {
		return err
	}

	err = checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable(ctx, opts.Client, opts.Config, cluster)
	if err != nil {
		return err
	}

	// Skip the cluster, make sure the operator is not taking any action on the cluster.
	err = setSkipReconciliation(ctx, opts.Client, cluster, true)
	if err != nil {
		return err
	}

	// Fetch the last connection string from the `FoundationDBCluster` status, e.g. `kubectl get fdb ${cluster} -o jsonpath='{ .status.connectionString }'`.
	lastConnectionString := cluster.Status.ConnectionString
	lastConnectionStringParts := strings.Split(lastConnectionString, "@")
	addresses := strings.Split(lastConnectionStringParts[1], ",")
	usesDNSInClusterFile := cluster.UseDNSInClusterFile()

	log.Println("current connection string", lastConnectionString, "cluster uses DNS:", usesDNSInClusterFile)
	var useTLS bool
	coordinators := map[string]fdbv1beta2.ProcessAddress{}
	for _, addr := range addresses {
		parsed, parseErr := fdbv1beta2.ParseProcessAddress(addr)
		if parseErr != nil {
			return parseErr
		}

		log.Println("found coordinator", parsed.String())
		coordinators[parsed.MachineAddress()] = parsed
		// If the tls flag is present we assume that the coordinators should make use of TLS.
		_, useTLS = parsed.Flags["tls"]
	}

	log.Println("Current coordinators", coordinators, "useTLS", useTLS)
	// Fetch all Pods and coordinators for the remote and remote satellite.
	runningCoordinators := map[string]fdbv1beta2.None{}
	newCoordinators := make([]fdbv1beta2.ProcessAddress, 0, 5)
	processCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return err
	}
	candidates := make([]*corev1.Pod, 0, processCounts.Total())

	pods, err := getRunningPodsForCluster(ctx, opts.Client, cluster)
	if err != nil {
		return err
	}

	// Find a running coordinator to copy the coordinator files from.
	var runningCoordinator *corev1.Pod
	for _, pod := range pods.Items {
		var addr fdbv1beta2.ProcessAddress
		if usesDNSInClusterFile {
			dnsName := internal.GetPodDNSName(cluster, pod.GetName())
			addr = fdbv1beta2.ProcessAddress{StringAddress: dnsName}
		} else {
			currentPod := pod
			publicIPs := internal.GetPublicIPsForPod(&currentPod, logr.Discard())
			if len(publicIPs) == 0 {
				log.Println("Found no public IPs for pod:", pod.Name)
				continue
			}

			var parseErr error
			addr, parseErr = fdbv1beta2.ParseProcessAddress(publicIPs[0])
			if parseErr != nil {
				return parseErr
			}
		}

		log.Println("Checking pod", pod.Name, "address", addr.MachineAddress())
		loopPod := pod
		if coordinatorAddr, ok := coordinators[addr.MachineAddress()]; ok {
			log.Println("Found coordinator for cluster", pod.Name, "address", addr.MachineAddress())
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

	// Drop the multi-region setup if present.
	newDatabaseConfiguration := cluster.Spec.DatabaseConfiguration.DeepCopy()
	// Drop the multi-region configuration.
	newDatabaseConfiguration.UsableRegions = 1
	newDatabaseConfiguration.Regions = []fdbv1beta2.Region{
		{
			DataCenters: []fdbv1beta2.DataCenter{
				{
					ID: cluster.Spec.DataCenter,
				},
			},
		},
	}
	log.Println("Update the database configuration to single region configuration")
	err = updateDatabaseConfiguration(ctx, opts.Client, cluster, *newDatabaseConfiguration)
	if err != nil {
		return err
	}

	// Pick 5 new coordinators.
	needsUpload := make([]*corev1.Pod, 0, cluster.DesiredCoordinatorCount())
	for len(newCoordinators) < cluster.DesiredCoordinatorCount() {
		log.Println("Current coordinators:", len(newCoordinators))
		candidate := candidates[len(newCoordinators)]

		var addr fdbv1beta2.ProcessAddress
		if usesDNSInClusterFile {
			dnsName := internal.GetPodDNSName(cluster, candidate.GetName())
			addr = fdbv1beta2.ProcessAddress{StringAddress: dnsName}
		} else {
			var parseErr error
			addr, parseErr = fdbv1beta2.ParseProcessAddress(candidate.Status.PodIP)
			if parseErr != nil {
				return parseErr
			}
		}

		if useTLS {
			addr.Port = 4500
			addr.Flags = map[string]bool{"tls": true}
		} else {
			addr.Port = 4501
		}

		log.Println("Adding new coordinator:", addr.String())
		newCoordinators = append(newCoordinators, addr)
		needsUpload = append(needsUpload, candidate)
	}

	// If at least one coordinator needs to get the files uploaded, we perform the download and upload for the coordinators.
	if len(needsUpload) > 0 {
		// Copy the coordinator state from one of the running coordinators to your local machine:
		coordinatorFiles := []string{"coordination-0.fdq", "coordination-1.fdq"}
		tmpCoordinatorFiles := make([]string, 2)
		tmpDir := os.TempDir()
		for idx, coordinatorFile := range coordinatorFiles {
			tmpCoordinatorFiles[idx] = path.Join(tmpDir, coordinatorFile)
		}

		log.Println("tmpCoordinatorFiles", tmpCoordinatorFiles, "checking the location of the coordination-0.fdq in Pod", runningCoordinator.Name)
		stdout, stderr, err := kubeHelper.ExecuteCommandOnPod(context.Background(), opts.Client, opts.Config, runningCoordinator, fdbv1beta2.MainContainerName, "find /var/fdb/data/ -type f -name 'coordination-0.fdq' -print -quit | head -n 1", false)
		if err != nil {
			log.Println(stderr)
			return err
		}

		lines := strings.Split(stdout, "\n")
		if len(lines) == 0 {
			return fmt.Errorf("no coordination file found in %s", runningCoordinator.Name)
		}

		dataDir := path.Dir(strings.TrimSpace(lines[0]))
		log.Println("dataDir:", dataDir)
		for idx, coordinatorFile := range coordinatorFiles {
			err = downloadCoordinatorFile(ctx, opts.Client, opts.Config, runningCoordinator, path.Join(dataDir, coordinatorFile), tmpCoordinatorFiles[idx])
			if err != nil {
				return err
			}
		}

		for _, target := range needsUpload {
			targetDataDir := getDataDir(dataDir, target, cluster)

			for idx, coordinatorFile := range coordinatorFiles {
				err = uploadCoordinatorFile(ctx, opts.Client, opts.Config, target, tmpCoordinatorFiles[idx], path.Join(targetDataDir, coordinatorFile))
				if err != nil {
					return err
				}
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
	log.Println("new connection string:", newCS)
	err = updateConnectionString(ctx, opts.Client, cluster, newCS)
	if err != nil {
		return err
	}

	// Wait ~1 min until the `ConfigMap` is synced to all Pods, you can check the `/var/dynamic-conf/fdb.cluster` inside a Pod if you are unsure.
	time.Sleep(2 * time.Minute)

	// If the split image is used we have to update the copied files by making a POST request against the sidecar API.
	// In the unified image, this step is not required as the dynamic files are directly mounted in the main container.
	// We are not deleting the Pods as the operator is set to skip the reconciliation and therefore the deleted Pods
	// would not be recreated.
	if !cluster.UseUnifiedImage() {
		log.Println("The cluster uses the split image, the plugin will update the copied files")
		for _, pod := range pods.Items {
			loopPod := pod

			command := []string{"/bin/bash", "-c"}

			var curlStr strings.Builder
			curlStr.WriteString("curl -X POST")
			if internal.PodHasSidecarTLS(&loopPod) {
				curlStr.WriteString(" --cacert ${FDB_TLS_CA_FILE} --cert ${FDB_TLS_CERTIFICATE_FILE} --key ${FDB_TLS_KEY_FILE} -k https://")
			} else {
				curlStr.WriteString(" http://")
			}

			curlStr.WriteString(loopPod.Status.PodIP)
			curlStr.WriteString(":8080/copy_files > /dev/null")

			command = append(command, curlStr.String())

			err = kubeHelper.ExecuteCommandRaw(ctx, opts.Client, opts.Config, runningCoordinator.Namespace, runningCoordinator.Name, fdbv1beta2.MainContainerName, command, nil, opts.Stdout, opts.Stderr, false)
			if err != nil {
				return err
			}
		}
	}

	log.Println("Killing fdbserver processes")
	// Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).
	err = restartFdbserverInCluster(ctx, opts.Client, opts.Config, cluster)
	if err != nil {
		return err
	}

	// Wait until all fdbservers have started again.
	time.Sleep(1 * time.Minute)

	command := []string{"fdbcli", "--exec", fmt.Sprintf("force_recovery_with_data_loss %s", cluster.Spec.DataCenter)}
	// Now you can exec into a container and use `fdbcli` to connect to the cluster.
	// If you use a multi-region cluster you have to issue `force_recovery_with_data_loss`
	log.Println("Triggering force recovery with command:", command)
	err = kubeHelper.ExecuteCommandRaw(ctx, opts.Client, opts.Config, runningCoordinator.Namespace, runningCoordinator.Name, fdbv1beta2.MainContainerName, command, nil, opts.Stdout, opts.Stderr, false)
	if err != nil {
		return err
	}

	// Now you can set `spec.Skip = false` to let the operator take over again.
	// Skip the cluster, make sure the operator is not taking any action on the cluster.
	err = setSkipReconciliation(ctx, opts.Client, cluster, false)
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

func downloadCoordinatorFile(ctx context.Context, kubeClient client.Client, config *rest.Config, pod *corev1.Pod, src string, dst string) error {
	tmpCoordinatorFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}

	defer func() {
		_ = tmpCoordinatorFile.Close()
	}()

	log.Println("Download files, target:", dst, "source", src, "pod", pod.Name, "Namespace", pod.Namespace)
	err = kubeHelper.DownloadFile(ctx, kubeClient, config, pod, fdbv1beta2.MainContainerName, src, tmpCoordinatorFile)
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

func uploadCoordinatorFile(ctx context.Context, kubeClient client.Client, config *rest.Config, pod *corev1.Pod, src string, dst string) error {
	tmpCoordinatorFile, err := os.OpenFile(src, os.O_RDONLY, 0600)
	if err != nil {
		return err
	}

	defer func() {
		_ = tmpCoordinatorFile.Close()
	}()

	log.Println("Upload files, target:", dst, "source", src, "pod", pod.Name, "Namespace", pod.Namespace)

	return kubeHelper.UploadFile(ctx, kubeClient, config, pod, fdbv1beta2.MainContainerName, tmpCoordinatorFile, dst)
}

// restartFdbserverInCluster will try to restart all fdbserver processes inside all the pods of the cluster. If the restart fails, it will be retried again two more times.
func restartFdbserverInCluster(ctx context.Context, kubeClient client.Client, config *rest.Config, cluster *fdbv1beta2.FoundationDBCluster) error {
	pods, err := getRunningPodsForCluster(ctx, kubeClient, cluster)
	if err != nil {
		return err
	}

	// Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).
	retryRestart := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		var stderr string
		_, _, err = kubeHelper.ExecuteCommand(context.Background(), kubeClient, config, pod.Namespace, pod.Name, fdbv1beta2.MainContainerName, "pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true", false)
		if err != nil {
			// If the pod doesn't exist anymore ignore the error. The pod will have the new configuration when recreated again.
			if k8serrors.IsNotFound(err) {
				continue
			}

			time.Sleep(1 * time.Second)
			log.Println("error restarting process in pod", pod.Name, "got error", err.Error(), "will be directly retried, stderr:", stderr)
			_, stderr, err = kubeHelper.ExecuteCommand(context.Background(), kubeClient, config, pod.Namespace, pod.Name, fdbv1beta2.MainContainerName, "pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true", false)
			if err != nil {
				log.Println("error restarting process in pod", pod.Name, "got error", err.Error(), "will be retried later, stderr:", stderr)
				retryRestart = append(retryRestart, pod)
			}
		}
	}

	if len(retryRestart) == 0 {
		return nil
	}

	// If we have more than one pod where we failed to restart the fdbserver processes, wait ten seconds before trying again.
	time.Sleep(10 * time.Second)
	log.Println("Failed to restart the fdbserver processes in", len(retryRestart), "pods, will be retried now.")

	for _, pod := range retryRestart {
		// Pod is marked for deletion, so we can skip it here.
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}

		_, _, err = kubeHelper.ExecuteCommand(context.Background(), kubeClient, config, pod.Namespace, pod.Name, fdbv1beta2.MainContainerName, "pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true", false)
		if err != nil {
			// If the pod doesn't exist anymore ignore the error. The pod will have the new configuration when recreated again.
			if k8serrors.IsNotFound(err) {
				continue
			}

			return err
		}
	}

	return nil
}

func checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable(ctx context.Context, kubeClient client.Client, config *rest.Config, cluster *fdbv1beta2.FoundationDBCluster) error {
	pods, err := getRunningPodsForCluster(ctx, kubeClient, cluster)
	if err != nil {
		return err
	}

	clientPod, err := kubeHelper.PickRandomPod(pods)
	if err != nil {
		return err
	}

	log.Println("Getting the status from:", clientPod.Name)
	status, err := getStatus(ctx, kubeClient, config, clientPod)
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
