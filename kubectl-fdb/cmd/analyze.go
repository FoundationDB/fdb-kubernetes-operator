/*
 * analyze.go
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
	"encoding/json"
	"fmt"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newAnalyzeCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze if the given clusters have any issues",
		Long:  "Analyze if the given clusters have any issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			autoFix, err := cmd.Flags().GetBool("auto-fix")
			if err != nil {
				return err
			}

			flagNoColor, err := cmd.Flags().GetBool("no-color")
			if err != nil {
				return err
			}

			allClusters, err := cmd.Flags().GetBool("all-clusters")
			if err != nil {
				return err
			}

			ignoreRemovals, err := cmd.Flags().GetBool("ignore-removals")
			if err != nil {
				return err
			}

			shouldAnalyzeStatus, err := cmd.Flags().GetBool("analyze-status")
			if err != nil {
				return err
			}

			ignoreConditions, err := cmd.Flags().GetStringArray("ignore-condition")
			if err != nil {
				return err
			}

			err = allConditionsValid(ignoreConditions)
			if err != nil {
				return err
			}

			if flagNoColor {
				color.NoColor = true
			}

			if len(args) == 0 && !allClusters {
				return cmd.Help()
			}

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

			clientSet, err := kubernetes.NewForConfig(config)
			if err != nil {
				return err
			}

			// TODO (jscheuermann): Don't load clusters twice if we check all clusters
			var clusters []string
			if allClusters {
				var clusterList fdbv1beta2.FoundationDBClusterList
				err := kubeClient.List(context.Background(), &clusterList, client.InNamespace(namespace))
				if err != nil {
					return err
				}

				for _, cluster := range clusterList.Items {
					clusters = append(clusters, cluster.Name)
				}
			} else {
				clusters = args
			}

			var errs []error
			for _, clusterName := range clusters {
				cluster, err := loadCluster(kubeClient, namespace, clusterName)
				if err != nil {
					errs = append(errs, fmt.Errorf("could not fetch cluster information for: %s/%s, error: %w", namespace, clusterName, err))
					continue
				}

				err = analyzeCluster(cmd, kubeClient, cluster, autoFix, wait, ignoreConditions, ignoreRemovals)
				if err != nil {
					errs = append(errs, err)
				}

				if !shouldAnalyzeStatus {
					continue
				}

				err = analyzeStatus(cmd, config, clientSet, kubeClient, cluster, autoFix)
				if err != nil {
					errs = append(errs, err)
				}
			}

			if len(errs) > 0 {
				var errMsg strings.Builder

				for _, err := range errs {
					errMsg.WriteString("\n")
					errMsg.WriteString(err.Error())
				}

				return fmt.Errorf(errMsg.String())
			}

			return nil
		},
		Example: `
# Analyze the cluster "sample-cluster-1" in the current namespace
kubectl fdb analyze sample-cluster-1

# Analyze the cluster "sample-cluster-1" in the namespace "test-namespace"
kubectl fdb -n test-namespace analyze sample-cluster-1

# Analyze the cluster "sample-cluster-1" in the current namespace and fixes issues
kubectl fdb analyze --auto-fix sample-cluster-1

# Analyze the cluster "sample-cluster-1" in the current namespace and ignore the IncorrectCommandLine and IncorrectPodSpec condition
kubectl fdb analyze --ignore-condition=IncorrectCommandLine --ignore-condition=IncorrectPodSpec sample-cluster-1

# Per default the plugin will print out how many process groups are marked for removal instead of printing out each process group.
# This can be disabled by using the ignore-removals flag to print out the details about process groups that are marked for removal.
kubectl fdb analyze --ignore-removals=false sample-cluster-1
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().Bool("auto-fix", false, "defines if the analyze tasks should try to fix the found issues (e.g. replace Pods).")
	cmd.Flags().Bool("all-clusters", false, "defines all clusters in the given namespace should be analyzed.")
	cmd.Flags().Bool("analyze-status", false, "defines if the command should also analyze the machine-readable status of the provided cluster(s).")
	// We might want to move this into the root cmd if we need this in multiple places
	cmd.Flags().Bool("no-color", false, "Disable color output.")
	cmd.Flags().StringArray("ignore-condition", nil, "specify which process group conditions should be ignored and not be printed to stdout.")
	cmd.Flags().Bool("ignore-removals", true, "specify if process groups marked for removal should be ignored.")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func allConditionsValid(conditions []string) error {
	conditionMap := map[string]fdbv1beta2.None{}

	for _, condition := range fdbv1beta2.AllProcessGroupConditionTypes() {
		conditionMap[string(condition)] = fdbv1beta2.None{}
	}

	var errString strings.Builder

	for _, condition := range conditions {
		if _, ok := conditionMap[condition]; !ok {
			errString.WriteString("unknown condition: ")
			errString.WriteString(condition)
			errString.WriteString("\n")
		}
	}

	if errString.Len() == 0 {
		return nil
	}

	return fmt.Errorf(errString.String())
}

func analyzeCluster(cmd *cobra.Command, kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, autoFix bool, wait bool, ignoreConditions []string, ignoreRemovals bool) error {
	var foundIssues bool
	if cluster == nil {
		return fmt.Errorf("error provided cluster is nil")
	}

	cmd.Printf("Checking cluster: %s/%s\n", cluster.Namespace, cluster.Name)

	// 1. Check if the cluster is available
	if cluster.Status.Health.Available {
		printStatement(cmd, "Cluster is available", goodMessage)
	} else {
		foundIssues = true
		printStatement(cmd, "Cluster is not available", errorMessage)
	}

	if cluster.Status.Health.FullReplication {
		printStatement(cmd, "Cluster is fully replicated", goodMessage)
	} else {
		foundIssues = true
		printStatement(cmd, "Cluster is not fully replicated", errorMessage)
	}

	// 2. Check if reconciled
	if cluster.Status.Generations.Reconciled == cluster.ObjectMeta.Generation {
		printStatement(cmd, "Cluster is reconciled", goodMessage)
	} else {
		foundIssues = true
		printStatement(cmd, "Cluster is not reconciled", errorMessage)
	}

	// We could add here more fields from cluster.Status.Generations and check if they are present.
	var failedProcessGroups []string
	processGroupMap := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
	removedProcessGroups := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}

	// 3. Check for issues in processGroupID
	processGroupIssue := false
	var ignoredConditions int
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroupMap[processGroup.ProcessGroupID] = fdbv1beta2.None{}

		// Skip if the processGroup should be removed
		// or should we check for how long they are marked as removed e.g. stuck in removal?
		if processGroup.IsMarkedForRemoval() {
			removedProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
			if !ignoreRemovals {
				statement := fmt.Sprintf("ProcessGroup: %s is marked for removal, excluded state: %t", processGroup.ProcessGroupID, processGroup.IsExcluded())
				printStatement(cmd, statement, warnMessage)
			}

			continue
		}

		if len(processGroup.ProcessGroupConditions) == 0 {
			continue
		}

		processGroupIssue = true
		for _, condition := range processGroup.ProcessGroupConditions {
			skip := false
			for _, ignoreCondition := range ignoreConditions {
				if condition.ProcessGroupConditionType == fdbv1beta2.ProcessGroupConditionType(ignoreCondition) {
					skip = true
					ignoredConditions++
					break
				}
			}

			if skip {
				continue
			}

			statement := fmt.Sprintf("ProcessGroup: %s has the following condition: %s since %s", processGroup.ProcessGroupID, condition.ProcessGroupConditionType, time.Unix(condition.Timestamp, 0).String())
			printStatement(cmd, statement, errorMessage)
		}

		if autoFix {
			_, failureTime := processGroup.NeedsReplacement(0, 0)
			if failureTime > 0 {
				failedProcessGroups = append(failedProcessGroups, string(processGroup.ProcessGroupID))
			}
		}
	}

	if ignoreRemovals && len(removedProcessGroups) > 0 {
		printStatement(cmd, fmt.Sprintf("Ignored %d process groups marked for removal", len(removedProcessGroups)), warnMessage)
	}

	if ignoredConditions > 0 {
		printStatement(cmd, fmt.Sprintf("Ignored %d conditions", ignoredConditions), warnMessage)
	}

	if !processGroupIssue {
		printStatement(cmd, "ProcessGroups are all in ready condition", goodMessage)
	} else {
		foundIssues = true
	}

	// 4. Check if all Pods are up and running
	pods, err := getPodsForCluster(kubeClient, cluster)
	if err != nil {
		return err
	}

	podIssue := false
	var killPods []corev1.Pod
	if len(pods.Items) == 0 {
		foundIssues = true
		printStatement(cmd, "Found no Pods for this cluster", errorMessage)
	}

	processGroupIDLabel := cluster.GetProcessGroupIDLabel()
	for _, pod := range pods.Items {
		// Skip Pods that are marked for removal those will probably be in a terminating state.
		if _, ok := removedProcessGroups[fdbv1beta2.ProcessGroupID(pod.Labels[cluster.GetProcessGroupIDLabel()])]; ok {
			continue
		}

		if pod.DeletionTimestamp != nil && pod.DeletionTimestamp.Add(5*time.Minute).Before(time.Now()) {
			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s has been stuck in terminating since %s", pod.Namespace, pod.Name, pod.DeletionTimestamp)
			printStatement(cmd, statement, errorMessage)

			// The process groups that should be deleted, so we can safely replace it
			if autoFix {
				failedProcessGroups = append(failedProcessGroups, pod.Labels[cluster.GetProcessGroupIDLabel()])
			}

			continue
		}

		if pod.Status.Phase != corev1.PodRunning {
			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s has unexpected Phase %s with Reason: %s", pod.Namespace, pod.Name, pod.Status.Phase, pod.Status.Reason)
			printStatement(cmd, statement, errorMessage)
		}

		deletePod := false
		for _, container := range pod.Status.ContainerStatuses {
			if container.Ready {
				continue
			}

			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s has an unready container: %s", pod.Namespace, pod.Name, container.Name)
			printStatement(cmd, statement, errorMessage)

			// Replace the Pod if the container is unready for more then 30 minutes
			if container.State.Terminated != nil && container.State.Terminated.ExitCode != 0 && container.State.Terminated.FinishedAt.Add(30*time.Minute).Before(time.Now()) {
				deletePod = true
			}
		}

		if deletePod && autoFix {
			killPods = append(killPods, pod)
		}

		id := fdbv1beta2.ProcessGroupID(pod.Labels[processGroupIDLabel])
		if _, ok := processGroupMap[id]; !ok {
			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s with the ID %s is not part of the cluster spec status", pod.Namespace, pod.Name, id)
			printStatement(cmd, statement, errorMessage)
		}
	}

	if !podIssue {
		printStatement(cmd, "Pods are all running and available", goodMessage)
	} else {
		foundIssues = true
	}

	// We could add more auto fixes in the future.
	if autoFix {
		confirmed := false

		if len(failedProcessGroups) > 0 {
			err := replaceProcessGroups(kubeClient, cluster.Name, failedProcessGroups, cluster.Namespace, "", true, wait, false, true)
			if err != nil {
				return err
			}
		}

		pods := filterDeletePods(failedProcessGroups, killPods)
		if wait && len(pods) > 0 {
			podNames := make([]string, 0, len(killPods))
			for _, pod := range killPods {
				podNames = append(podNames, pod.Name)
			}

			confirmed = confirmAction(fmt.Sprintf("Delete Pods %v in cluster %s/%s", strings.Join(podNames, ","), cluster.Namespace, cluster.Name))
		}

		if !wait || confirmed {
			for _, pod := range pods {
				cmd.Printf("Delete Pod: %s/%s\n", pod.Namespace, pod.Name)
				err := kubeClient.Delete(context.Background(), &pod)
				if err != nil {
					return err
				}
			}
		}
	}

	if foundIssues && !autoFix {
		return fmt.Errorf("found issues for cluster %s. Please check them", cluster.Name)
	}

	return nil
}

func filterDeletePods(replacements []string, killPods []corev1.Pod) []corev1.Pod {
	res := make([]corev1.Pod, 0, len(killPods))

	for _, killPod := range killPods {
		contained := false
		for _, replacement := range replacements {
			if replacement == killPod.Name {
				contained = true
			}
		}

		if !contained {
			res = append(res, killPod)
		}
	}

	return res
}

func getStatus(restConfig *rest.Config, clientSet *kubernetes.Clientset, pod *corev1.Pod) (*fdbv1beta2.FoundationDBStatus, error) {
	stdout, stderr, err := executeCmd(restConfig, clientSet, pod.Name, pod.Namespace, "fdbcli --exec 'status json'")
	if err != nil {
		return nil, fmt.Errorf("error getting status: %s, %w", stderr, err)
	}

	content, err := fdbstatus.RemoveWarningsInJSON(stdout.String())
	if err != nil {
		fmt.Println(stdout.String())
		return nil, err
	}

	status := &fdbv1beta2.FoundationDBStatus{}
	err = json.Unmarshal(content, status)
	if err != nil {
		return nil, err
	}

	return status, nil
}

func analyzeStatus(cmd *cobra.Command, restConfig *rest.Config, clientSet *kubernetes.Clientset, kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, autoFix bool) error {
	if cluster == nil {
		return fmt.Errorf("error provided cluster is nil")
	}

	cmd.Printf("Checking cluster: %s/%s with auto-fix: %t\n", cluster.Namespace, cluster.Name, autoFix)

	pods, err := getPodsForCluster(kubeClient, cluster)
	if err != nil {
		return err
	}

	pod, err := chooseRandomPod(pods)
	if err != nil {
		return err
	}

	var status *fdbv1beta2.FoundationDBStatus
	var tries int

	// Try to get the status for 5 times
	for tries < 5 {
		status, err = getStatus(restConfig, clientSet, pod)
		if err == nil {
			break
		}

		tries++
		printStatement(cmd, err.Error(), errorMessage)
	}

	if err != nil {
		return err
	}

	return analyzeStatusInternal(cmd, restConfig, clientSet, status, pod, autoFix, cluster.Name)
}

func analyzeStatusInternal(cmd *cobra.Command, restConfig *rest.Config, clientSet *kubernetes.Clientset, status *fdbv1beta2.FoundationDBStatus, pod *corev1.Pod, autoFix bool, clusterName string) error {
	var foundIssues bool

	processesWithError := make([]string, 0)
	for _, process := range status.Cluster.Processes {
		if len(process.Messages) == 0 {
			continue
		}

		foundIssues = true
		addr := process.Address.StringWithoutFlags()
		processesWithError = append(processesWithError, addr)
		for _, message := range process.Messages {
			printStatement(cmd, fmt.Sprintf("Process: %s with address: %s error: %s type: %s, time: %s",
				process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
				addr,
				message.Name,
				message.Type,
				time.Unix(int64(int(message.Time)), 0).String()), errorMessage)
		}
	}

	if len(processesWithError) > 0 && autoFix {
		// Restart one process by another. The restart will remove the condition, assuming the condition was only
		// intermediate.
		for _, process := range processesWithError {
			cmd.Println("Start killing", process)
			killCmd := fmt.Sprintf("kill; kill %s; sleep 5; status", process)
			_, stderr, err := executeCmd(restConfig, clientSet, pod.Name, pod.Namespace, fmt.Sprintf("fdbcli --exec '%s'", killCmd))
			if err != nil {
				return fmt.Errorf("error killing process %s status: %s, %w", process, stderr.String(), err)
			}
			time.Sleep(5 * time.Second)
		}
	}

	if foundIssues {
		return fmt.Errorf("found issues in status json for cluster %s. Please check them", clusterName)
	}

	return nil
}
