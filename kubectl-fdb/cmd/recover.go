/*
 * recover.go
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
	"errors"
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
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	utilexec "k8s.io/client-go/util/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RecoveryOpts struct to pass down all args to the actual runner.
type RecoveryOpts struct {
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
	// excludedCoordinators defines the coordinators that should be skipped during the recovery effort.
	excludedCoordinators []string
}

func newRecoverCmd(streams genericiooptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Subcommand to recover a cluster if a majority of coordinators is lost permanently",
		Long:  "Subcommand to recover a cluster if a majority of coordinators is lost permanently",
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
		Example: `
# Recover the multi-region cluster "sample-cluster-1" in the current Namespace
kubectl fdb recover multi-region sample-cluster-1

# Recover the multi-region cluster "sample-cluster-1" in the "testing" Namespace
kubectl fdb recover multi-region -n testing sample-cluster-1

# Recover the single-dc cluster "sample-cluster-1" in the current Namespace
kubectl fdb recover single-dc sample-cluster-1

# Recover the single-dc cluster "sample-cluster-1" in the "testing" Namespace
kubectl fdb recover single-dc -n testing sample-cluster-1
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.AddCommand(newRecoverMultiRegionClusterCmd(streams))
	cmd.AddCommand(newRecoverSingleDCClusterCmd(streams))
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
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

func downloadCoordinatorFile(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	pod *corev1.Pod,
	src string,
	dst string,
) error {
	tmpCoordinatorFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}

	defer func() {
		_ = tmpCoordinatorFile.Close()
	}()

	log.Println(
		"Download files, target:",
		dst,
		"source",
		src,
		"pod",
		pod.Name,
		"Namespace",
		pod.Namespace,
	)
	err = kubeHelper.DownloadFile(
		ctx,
		kubeClient,
		config,
		pod,
		fdbv1beta2.MainContainerName,
		src,
		tmpCoordinatorFile,
	)
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

func uploadCoordinatorFile(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	pod *corev1.Pod,
	src string,
	dst string,
) error {
	tmpCoordinatorFile, err := os.OpenFile(src, os.O_RDONLY, 0600)
	if err != nil {
		return err
	}

	defer func() {
		_ = tmpCoordinatorFile.Close()
	}()

	log.Println(
		"Upload files, target:",
		dst,
		"source",
		src,
		"pod",
		pod.Name,
		"Namespace",
		pod.Namespace,
	)

	return kubeHelper.UploadFile(
		ctx,
		kubeClient,
		config,
		pod,
		fdbv1beta2.MainContainerName,
		tmpCoordinatorFile,
		dst,
	)
}

// sigkillExitCode is the exit code reported when the exec session was killed with SIGKILL (128 + 9).
const sigkillExitCode = 137

// isContainerRestartError returns true if the error indicates that the container was killed or restarted while the
// command was running. Killing the fdbserver processes can take down the container (and the exec session with it),
// so those errors mean the restart worked as intended.
func isContainerRestartError(err error) bool {
	if err == nil {
		return false
	}

	var exitErr utilexec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitStatus() == sigkillExitCode {
		return true
	}

	return strings.Contains(err.Error(), "container not found") ||
		strings.Contains(err.Error(), "unable to upgrade connection")
}

// describeContainerState returns a human readable description of the state of the main container in the provided Pod.
// This includes the current state, the restart count and the last termination state, which helps to identify
// containers that are crash-looping.
func describeContainerState(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != fdbv1beta2.MainContainerName {
			continue
		}

		var current string
		switch {
		case status.State.Running != nil:
			current = fmt.Sprintf("running since %s", status.State.Running.StartedAt)
		case status.State.Waiting != nil:
			current = fmt.Sprintf(
				"waiting, reason: %s, message: %s",
				status.State.Waiting.Reason,
				status.State.Waiting.Message,
			)
		case status.State.Terminated != nil:
			current = fmt.Sprintf(
				"terminated, reason: %s, exit code: %d",
				status.State.Terminated.Reason,
				status.State.Terminated.ExitCode,
			)
		default:
			current = "unknown"
		}

		description := fmt.Sprintf(
			"phase: %s, container %s: %s, ready: %t, restart count: %d",
			pod.Status.Phase,
			status.Name,
			current,
			status.Ready,
			status.RestartCount,
		)
		if status.LastTerminationState.Terminated != nil {
			description += fmt.Sprintf(
				", last termination: reason: %s, exit code: %d, finished at: %s",
				status.LastTerminationState.Terminated.Reason,
				status.LastTerminationState.Terminated.ExitCode,
				status.LastTerminationState.Terminated.FinishedAt,
			)
		}

		return description
	}

	return fmt.Sprintf("phase: %s, no status found for container %s", pod.Status.Phase, fdbv1beta2.MainContainerName)
}

// logContainerStateOnRestartError logs the current state of the Pod if the error indicates a container restart. This
// helps to understand whether the container is running again or is crash-looping.
func logContainerStateOnRestartError(ctx context.Context, kubeClient client.Client, pod corev1.Pod, err error) {
	if !isContainerRestartError(err) {
		return
	}

	currentPod := &corev1.Pod{}
	getErr := kubeClient.Get(ctx, client.ObjectKeyFromObject(&pod), currentPod)
	if getErr != nil {
		log.Println("could not fetch pod", pod.Name, "to log the container state:", getErr.Error())
		return
	}

	log.Println("state of pod", pod.Name, "after container restart error:", describeContainerState(currentPod))
}

// restartFdbserverInCluster will try to restart all fdbserver processes inside all the pods of the cluster. If the restart fails, it will be retried again two more times.
func restartFdbserverInCluster(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	cluster *fdbv1beta2.FoundationDBCluster,
) error {
	pods, err := getRunningPodsForCluster(ctx, kubeClient, cluster)
	if err != nil {
		return err
	}

	// Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).
	retryRestart := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		var stderr string
		_, stderr, err = kubeHelper.ExecuteCommand(
			context.Background(),
			kubeClient,
			config,
			pod.Namespace,
			pod.Name,
			fdbv1beta2.MainContainerName,
			"pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true",
			false,
		)
		if err != nil {
			// If the pod doesn't exist anymore ignore the error. The pod will have the new configuration when recreated again.
			if k8serrors.IsNotFound(err) {
				continue
			}

			time.Sleep(1 * time.Second)
			logContainerStateOnRestartError(ctx, kubeClient, pod, err)
			log.Println(
				"error restarting process in pod",
				pod.Name,
				"got error",
				err.Error(),
				"will be directly retried, stderr:",
				stderr,
			)
			_, stderr, err = kubeHelper.ExecuteCommand(
				context.Background(),
				kubeClient,
				config,
				pod.Namespace,
				pod.Name,
				fdbv1beta2.MainContainerName,
				"pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true",
				false,
			)
			if err != nil {
				logContainerStateOnRestartError(ctx, kubeClient, pod, err)
				log.Println(
					"error restarting process in pod",
					pod.Name,
					"got error",
					err.Error(),
					"will be retried later, stderr:",
					stderr,
				)
				retryRestart = append(retryRestart, pod)
			}
		}
	}

	if len(retryRestart) == 0 {
		return nil
	}

	// If we have more than one pod where we failed to restart the fdbserver processes, wait ten seconds before trying again.
	time.Sleep(10 * time.Second)
	log.Println(
		"Failed to restart the fdbserver processes in",
		len(retryRestart),
		"pods, will be retried now.",
	)

	for _, pod := range retryRestart {
		// Pod is marked for deletion, so we can skip it here.
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}

		_, _, err = kubeHelper.ExecuteCommand(
			context.Background(),
			kubeClient,
			config,
			pod.Namespace,
			pod.Name,
			fdbv1beta2.MainContainerName,
			"pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true",
			false,
		)
		if err != nil {
			// If the pod doesn't exist anymore ignore the error. The pod will have the new configuration when recreated again.
			if k8serrors.IsNotFound(err) {
				continue
			}

			// The container was killed or restarted, this is expected when the fdbserver processes are killed.
			if isContainerRestartError(err) {
				logContainerStateOnRestartError(ctx, kubeClient, pod, err)
				log.Println(
					"ignoring error for pod",
					pod.Name,
					"as the container was restarted:",
					err.Error(),
				)
				continue
			}

			return err
		}
	}

	return nil
}

// checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable checks if the majority of the coordinators are
// unreachable and if the cluster is unavailable. This is a safeguard to reduce the risk of running the recovery
// commands against a healthy cluster.
func checkIfClusterIsUnavailableAndMajorityOfCoordinatorsAreUnreachable(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	cluster *fdbv1beta2.FoundationDBCluster,
) error {
	pods, err := getRunningPodsForCluster(ctx, kubeClient, cluster)
	if err != nil {
		return err
	}

	clientPod, err := kubeHelper.PickRandomPod(pods)
	if err != nil {
		return err
	}

	log.Println("Getting the status from:", clientPod.Name)
	for range 5 {
		err = getStatusAndCheckIfClusterShouldBeRecovered(ctx, kubeClient, config, clientPod)
		if err == nil {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	return err
}

func getStatusAndCheckIfClusterShouldBeRecovered(ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	clientPod *corev1.Pod) error {
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
