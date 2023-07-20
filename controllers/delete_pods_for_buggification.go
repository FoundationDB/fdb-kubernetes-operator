/*
 * delete_pods_for_buggification.go
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

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// deletePodsForBuggification provides a reconciliation step for recreating
// pods with new pod specs when buggifying the config.
type deletePodsForBuggification struct{}

// reconcile runs the reconciler's work.
func (d deletePodsForBuggification) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	crashLoopContainerProcessGroups := cluster.GetCrashLoopContainerProcessGroups()

	noSchedulePods := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(cluster.Spec.Buggify.NoSchedule))
	for _, processGroupID := range cluster.Spec.Buggify.NoSchedule {
		noSchedulePods[processGroupID] = fdbv1beta2.None{}
	}

	var updates []*corev1.Pod
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			logger.V(1).Info("Ignore process group stuck in terminating",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		// If a Pod is not found ignore it for now.
		if err != nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		inCrashLoop := false
		for _, container := range pod.Spec.Containers {
			if container.Name == fdbv1beta2.MainContainerName && len(container.Args) > 0 {
				inCrashLoop = inCrashLoop || container.Args[0] == "crash-loop"
			} else if container.Name == fdbv1beta2.SidecarContainerName && len(container.Args) > 0 {
				inCrashLoop = inCrashLoop || container.Args[0] == "crash-loop"
			}
		}

		shouldCrashLoop := false
		for _, targets := range crashLoopContainerProcessGroups {
			_, shouldCrashLoop = targets["*"]
			if shouldCrashLoop {
				break
			}
			_, shouldCrashLoop = targets[processGroup.ProcessGroupID]
			if shouldCrashLoop {
				break
			}
		}

		if shouldCrashLoop != inCrashLoop {
			logger.Info("Deleting pod for buggification",
				"processGroupID", processGroup.ProcessGroupID,
				"shouldCrashLoop", shouldCrashLoop,
				"inCrashLoop", inCrashLoop)
			updates = append(updates, pod)
			continue
		}

		// Recreate Pods that should be in the no schedule state
		var inNoSchedule, shouldBeNoSchedule bool
		_, shouldBeNoSchedule = noSchedulePods[processGroup.ProcessGroupID]

		// Ensure the Pod has an Affinity set before accessing it.
		if pod.Spec.Affinity == nil ||
			pod.Spec.Affinity.NodeAffinity == nil ||
			pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			// Uf the Pod should be in the noSchedule state we have to recreate the Pod.
			if shouldBeNoSchedule {
				logger.Info("Deleting pod for buggification",
					"processGroupID", processGroup.ProcessGroupID,
					"shouldBeNoSchedule", shouldBeNoSchedule,
					"inNoSchedule", inNoSchedule)
				updates = append(updates, pod)
			}
			continue
		}

		for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, expression := range term.MatchExpressions {
				if expression.Key == fdbv1beta2.NodeSelectorNoScheduleLabel {
					inNoSchedule = true
				}
			}
		}

		if inNoSchedule != shouldBeNoSchedule {
			logger.Info("Deleting pod for buggification",
				"processGroupID", processGroup.ProcessGroupID,
				"shouldBeNoSchedule", shouldBeNoSchedule,
				"inNoSchedule", inNoSchedule)
			updates = append(updates, pod)
		}
	}

	if len(updates) > 0 {
		logger.Info("Deleting pods", "count", len(updates))
		r.Recorder.Event(cluster, "Normal", "UpdatingPods", "Recreating pods for buggification")
		err := r.PodLifecycleManager.UpdatePods(logr.NewContext(ctx, logger), r, cluster, updates, true)
		if err != nil {
			return &requeue{curError: err}
		}

		return &requeue{message: "Pods need to be recreated"}
	}

	return nil
}
