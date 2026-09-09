/*
 * recover_single_dc_cluster.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newRecoverSingleDCClusterCmd(streams genericiooptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "single-dc",
		Short: "Recover a single dc cluster if a majority of coordinators is lost permanently",
		Long:  "Recover a single dc cluster if a majority of coordinators is lost permanently",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}

			if len(args) != 1 {
				return fmt.Errorf(
					"exactly one cluster name must be specified, provided args: %v",
					args,
				)
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

			excludedCoordinators, err := cmd.Flags().GetStringArray("exclude-coordinator")
			if err != nil {
				return err
			}

			if wait {
				confirmed := confirmAction(
					fmt.Sprintf(
						"WARNING:\nThe cluster: %s/%s will be force recovered.\nOnly perform those steps if you are unable to recover the coordinator pods.\nPerforming this action could lead to data loss.\n At least one coordinator must be active and running to copy the coordinator state.\n",
						namespace,
						clusterName,
					),
				)
				if !confirmed {
					return fmt.Errorf("aborted recover single-dc aciton")
				}
			}

			return RecoverSingleDCCluster(cmd.Context(),
				RecoveryOpts{
					Client:               kubeClient,
					Config:               config,
					ClusterName:          clusterName,
					Namespace:            namespace,
					Stdout:               cmd.OutOrStdout(),
					Stderr:               cmd.OutOrStderr(),
					excludedCoordinators: excludedCoordinators,
				})
		},
		Example: `
# Recover the single dc cluster "sample-cluster-1" in the current Namespace
kubectl fdb recover single-dc sample-cluster-1

# Recover the single-dc cluster "sample-cluster-1" in the "testing" Namespace
kubectl fdb recover single-dc -n testing sample-cluster-1

# Recover the single-dc cluster "sample-cluster-1" in the "testing" Namespace and excluding the coordinator on pod
# sample-cluster-1-storage-42
kubectl fdb recover single-dc -n testing sample-cluster-1 --exclude-coordinator sample-cluster-1-storage-42
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)
	cmd.Flags().
		StringArray("exclude-coordinator", []string{}, "Exclude a coordinator from the recovery process, e.g. because the coordinator pod is running but not able to reach the rest of the cluster. The provided name must match the pod name of the coordinator.")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// RecoverSingleDCCluster will forcefully recover a single-dc cluster if a majority of coordinators are lost.
// Performing this action can result in data loss.
func RecoverSingleDCCluster(ctx context.Context, opts RecoveryOpts) error {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := opts.Client.Get(
		ctx,
		client.ObjectKey{Name: opts.ClusterName, Namespace: opts.Namespace},
		cluster,
	)
	if err != nil {
		return err
	}

	err = checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable(
		ctx,
		opts.Client,
		opts.Config,
		cluster,
	)
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

	log.Println(
		"current connection string",
		lastConnectionString,
		"cluster uses DNS:",
		usesDNSInClusterFile,
	)
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
	newCoordinators := make([]fdbv1beta2.ProcessAddress, 0, cluster.DesiredCoordinatorCount())
	processCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return err
	}
	candidates := make([]*corev1.Pod, 0, processCounts.Total())

	pods, err := getRunningPodsForCluster(ctx, opts.Client, cluster)
	if err != nil {
		return err
	}

	excludedCoordinators := map[string]fdbv1beta2.None{}
	for _, excludedCoordinator := range opts.excludedCoordinators {
		excludedCoordinators[excludedCoordinator] = fdbv1beta2.None{}
	}

	// Find a running coordinator to copy the coordinator files from. Note: Running doesn't necessarily mean that the
	// coordinator is healthy from a cluster perspective, as the coordinator hosting pods could be up and running but have
	// networking issues like a network partition.
	var runningCoordinator *corev1.Pod
	for _, pod := range pods.Items {
		if _, ok := excludedCoordinators[pod.Name]; ok {
			log.Println("Skipping pod as excluded from recovery coordinator set:", pod.Name)
			continue
		}

		if pod.Status.Phase != corev1.PodRunning {
			log.Println(
				"Skipping pod as pod's phase is not running, current phase:",
				pod.Status.Phase,
			)
			continue
		}

		var addr fdbv1beta2.ProcessAddress
		if usesDNSInClusterFile {
			addr = fdbv1beta2.ProcessAddress{
				StringAddress: internal.GetPodDNSName(cluster, pod.GetName()),
			}
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

	// Pick new coordinators.
	needsUpload := make([]*corev1.Pod, 0, cluster.DesiredCoordinatorCount())
	candidateIdx := 0
	for len(newCoordinators) < cluster.DesiredCoordinatorCount() {
		if candidateIdx >= len(candidates) {
			return fmt.Errorf(
				"not enough coordinator candidates: need %d more, have %d running and %d candidates",
				cluster.DesiredCoordinatorCount()-len(
					newCoordinators,
				),
				len(newCoordinators),
				len(candidates),
			)
		}
		log.Println("Current coordinators:", len(newCoordinators))
		candidate := candidates[candidateIdx]
		candidateIdx++

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

		log.Println(
			"tmpCoordinatorFiles",
			tmpCoordinatorFiles,
			"checking the location of the coordination-0.fdq in Pod",
			runningCoordinator.Name,
		)
		stdout, stderr, err := kubeHelper.ExecuteCommandOnPod(
			context.Background(),
			opts.Client,
			opts.Config,
			runningCoordinator,
			fdbv1beta2.MainContainerName,
			"find /var/fdb/data/ -type f -name 'coordination-0.fdq' -print -quit | head -n 1",
			false,
		)
		if err != nil {
			log.Println(stderr)
			return err
		}

		trimmedStdout := strings.TrimSpace(stdout)
		if trimmedStdout == "" {
			return fmt.Errorf("no coordination file found in %s", runningCoordinator.Name)
		}

		lines := strings.Split(trimmedStdout, "\n")
		dataDir := path.Dir(strings.TrimSpace(lines[0]))
		log.Println("dataDir:", dataDir)
		for idx, coordinatorFile := range coordinatorFiles {
			err = downloadCoordinatorFile(
				ctx,
				opts.Client,
				opts.Config,
				runningCoordinator,
				path.Join(dataDir, coordinatorFile),
				tmpCoordinatorFiles[idx],
			)
			if err != nil {
				return err
			}
		}

		for _, target := range needsUpload {
			targetDataDir := getDataDir(dataDir, target, cluster)

			for idx, coordinatorFile := range coordinatorFiles {
				err = uploadCoordinatorFile(
					ctx,
					opts.Client,
					opts.Config,
					target,
					tmpCoordinatorFiles[idx],
					path.Join(targetDataDir, coordinatorFile),
				)
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
				curlStr.WriteString(
					" --cacert ${FDB_TLS_CA_FILE} --cert ${FDB_TLS_CERTIFICATE_FILE} --key ${FDB_TLS_KEY_FILE} -k https://",
				)
			} else {
				curlStr.WriteString(" http://")
			}

			curlStr.WriteString(loopPod.Status.PodIP)
			curlStr.WriteString(":8080/copy_files > /dev/null")

			command = append(command, curlStr.String())

			err = kubeHelper.ExecuteCommandRaw(
				ctx,
				opts.Client,
				opts.Config,
				runningCoordinator.Namespace,
				runningCoordinator.Name,
				fdbv1beta2.MainContainerName,
				command,
				nil,
				opts.Stdout,
				opts.Stderr,
				false,
			)
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

	// Now you can set `spec.Skip = false` to let the operator take over again.
	// Skip the cluster, make sure the operator is not taking any action on the cluster.
	err = setSkipReconciliation(ctx, opts.Client, cluster, false)
	if err != nil {
		return err
	}

	return nil
}
