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
	ctx "context"
	"fmt"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newAnalyzeCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze if the given clusters have any issues",
		Long:  "Analyze if the given clusters have any issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Root().Flags().GetBool("force")
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

			if flagNoColor {
				color.NoColor = true
			}

			if len(args) == 0 && !allClusters {
				return cmd.Help()
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

			var clusters []string
			if allClusters {
				var clusterList fdbtypes.FoundationDBClusterList
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
			for _, cluster := range clusters {
				err := analyzeCluster(cmd, kubeClient, cluster, namespace, autoFix, force)
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
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().Bool("auto-fix", false, "defines if the analyze tasks should try to fix the found issues (e.g. replace Pods).")
	cmd.Flags().Bool("all-clusters", false, "defines all clusters in the given namespace should be analyzed.")
	// We might want to move this into the root cmd if we need this in multiple places
	cmd.Flags().Bool("no-color", false, "Disable color output.")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func printStatement(cmd *cobra.Command, line string, err bool) {
	if err {
		color.Set(color.FgRed)
		cmd.PrintErrf("✖ %s\n", line)
		color.Unset()
		return
	}

	color.Set(color.FgGreen)
	cmd.Printf("✔ %s\n", line)
	color.Unset()
}

// TODO (johscheuer): check if we have enough resources.
func replaceFailedInstances(cluster *fdbtypes.FoundationDBCluster, instanceIDs []string) {
	for _, instanceID := range instanceIDs {
		markedForRemoval := false
		for _, removal := range cluster.Spec.InstancesToRemove {
			if removal != instanceID {
				continue
			}

			markedForRemoval = true
		}

		for _, removal := range cluster.Spec.InstancesToRemoveWithoutExclusion {
			if removal != instanceID {
				continue
			}

			markedForRemoval = true
		}

		if !markedForRemoval {
			cluster.Spec.InstancesToRemove = append(cluster.Spec.InstancesToRemove, instanceID)
		}
	}
}

func analyzeCluster(cmd *cobra.Command, kubeClient client.Client, clusterName string, namespace string, autoFix bool, force bool) error {
	foundIssues := false
	var cluster fdbtypes.FoundationDBCluster
	err := kubeClient.Get(ctx.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return err
	}

	cmd.Printf("Checking cluster: %s/%s\n", namespace, clusterName)
	patch := client.MergeFrom(cluster.DeepCopy())

	// 1. Check if the cluster is available
	if cluster.Status.Health.Available {
		printStatement(cmd, "Cluster is available", false)
	} else {
		foundIssues = true
		printStatement(cmd, "Cluster is not available", true)
	}

	if cluster.Status.Health.FullReplication {
		printStatement(cmd, "Cluster is fully replicated", false)
	} else {
		foundIssues = true
		printStatement(cmd, "Cluster is not fully replicated", true)
	}

	// 2. Check if reconciled
	if cluster.Status.Generations.Reconciled == cluster.ObjectMeta.Generation {
		printStatement(cmd, "Cluster is reconciled", false)
	} else {
		foundIssues = true
		printStatement(cmd, "Cluster is not reconciled", true)
	}

	// We could add here more fields from cluster.Status.Generations and check if they are present.
	var replaceInstances []string
	// 3. Check for issues in processGroupID
	processGroupIssue := false
	for _, processGroup := range cluster.Status.ProcessGroups {
		if len(processGroup.ProcessGroupConditions) == 0 {
			continue
		}

		// Skip if the processGroup should be removed
		// or should we check for how long they are marked as removed e.g. stuck in removal?
		if processGroup.Remove {
			statement := fmt.Sprintf("ProcessGroup: %s is marked for removal, excluded state: %t", processGroup.ProcessGroupID, processGroup.Excluded)
			printStatement(cmd, statement, true)
			continue
		}

		processGroupIssue = true
		for _, condition := range processGroup.ProcessGroupConditions {
			statement := fmt.Sprintf("ProcessGroup: %s has the following condition: %s since %s", processGroup.ProcessGroupID, condition.ProcessGroupConditionType, time.Unix(condition.Timestamp, 0).String())
			printStatement(cmd, statement, true)
		}

		needsReplacement, _ := processGroup.NeedsReplacement(0)
		if needsReplacement && autoFix {
			replaceInstances = append(replaceInstances, processGroup.ProcessGroupID)
		}
	}

	if !processGroupIssue {
		printStatement(cmd, "ProcessGroups are all in ready condition", false)
	} else {
		foundIssues = true
	}

	// 4. Check if all Pods are up and running
	pods, err := getPodsForCluster(kubeClient, clusterName, namespace)
	if err != nil {
		return err
	}

	podIssue := false
	var killPods []corev1.Pod
	if len(pods.Items) == 0 {
		foundIssues = true
		printStatement(cmd, "Found no Pods for this cluster", true)
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s has unexpected Phase %s with Reason: %s", namespace, pod.Name, pod.Status.Phase, pod.Status.Reason)
			printStatement(cmd, statement, true)
		}

		deletePod := false
		for _, container := range pod.Status.ContainerStatuses {
			if container.Ready {
				continue
			}

			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s has an unready container: %s", namespace, pod.Name, container.Name)
			printStatement(cmd, statement, true)

			// Replace the Pod if the container is unready for more then 30 minutes
			if container.State.Terminated != nil && container.State.Terminated.ExitCode != 0 && container.State.Terminated.FinishedAt.Add(30*time.Minute).Before(time.Now()) {
				deletePod = true
			}
		}

		if deletePod && autoFix {
			killPods = append(killPods, pod)
		}

		if pod.DeletionTimestamp != nil && pod.DeletionTimestamp.Add(5*time.Minute).Before(time.Now()) {
			podIssue = true
			statement := fmt.Sprintf("Pod %s/%s has been stuck in terminating since %s", namespace, pod.Name, pod.DeletionTimestamp)
			printStatement(cmd, statement, true)

			// The instance should be delete so we can safely replace it
			if autoFix {
				replaceInstances = append(replaceInstances, pod.Labels[fdbtypes.FDBInstanceIDLabel])
			}
		}
	}

	if !podIssue {
		printStatement(cmd, "Pods are all running and available", false)
	} else {
		foundIssues = true
	}

	// TODO: add more checks using the FDB status directly e.g. error message or something else or overloaded or matching versions
	// We could add more auto fixes in the future.
	if autoFix {
		confirmed := false
		if !force && len(replaceInstances) > 0 {
			confirmed = confirmAction(fmt.Sprintf("Replace instances %v in cluster %s/%s", replaceInstances, namespace, clusterName))
		}

		if force || confirmed {
			cmd.Printf("Replacing the following instances: %s", replaceInstances)
			replaceFailedInstances(&cluster, replaceInstances)
			err := kubeClient.Patch(ctx.Background(), &cluster, patch)
			if err != nil {
				return err
			}
		}

		pods := filterDeletePods(replaceInstances, killPods)
		if !force && len(pods) > 0 {
			confirmed = confirmAction(fmt.Sprintf("Delete Pods %v in cluster %s/%s", killPods, namespace, clusterName))
		}

		if force || confirmed {
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

func filterDeletePods(replaceInstances []string, killPods []corev1.Pod) []corev1.Pod {
	res := make([]corev1.Pod, 0, len(killPods))

	for _, killPod := range killPods {
		contained := false
		for _, replaceInstance := range replaceInstances {
			if replaceInstance == killPod.Name {
				contained = true
			}
		}

		if !contained {
			res = append(res, killPod)
		}
	}

	return res
}
