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
	ctx "context"

	corev1 "k8s.io/api/core/v1"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// DeletePodsForBuggification provides a reconciliation step for recreating
// pods with new pod specs when buggifying the config.
type DeletePodsForBuggification struct{}

// Reconcile runs the reconciler's work.
func (d DeletePodsForBuggification) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "DeletePodsForBuggification")
	pods, err := r.PodLifecycleManager.GetInstances(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	updates := make([]*corev1.Pod, 0)

	removals := make(map[string]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			removals[processGroup.ProcessGroupID] = true
		}
	}

	crashLoopPods := make(map[string]bool, len(cluster.Spec.Buggify.CrashLoop))
	crashLoopAll := false
	for _, processGroupID := range cluster.Spec.Buggify.CrashLoop {
		if processGroupID == "*" {
			crashLoopAll = true
		} else {
			crashLoopPods[processGroupID] = true
		}
	}

	for _, pod := range pods {
		if pod == nil {
			continue
		}

		instanceID := GetProcessGroupID(pod)
		_, pendingRemoval := removals[instanceID]
		if pendingRemoval {
			continue
		}

		inCrashLoop := false
		for _, container := range pod.Spec.Containers {
			if container.Name == "foundationdb" && len(container.Args) > 0 {
				inCrashLoop = container.Args[0] == "crash-loop"
			}
		}

		shouldCrashLoop := crashLoopAll || crashLoopPods[instanceID]

		if shouldCrashLoop != inCrashLoop {
			logger.Info("Deleting pod for buggification",
				"processGroupID", instanceID,
				"shouldCrashLoop", shouldCrashLoop,
				"inCrashLoop", inCrashLoop)
			updates = append(updates, pod)
		}
	}

	if len(updates) > 0 {
		logger.Info("Deleting pods", "count", len(updates))
		r.Recorder.Event(cluster, "Normal", "UpdatingPods", "Recreating pods for buggification")

		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, updates, true)
		if err != nil {
			return &Requeue{Error: err}
		}

		return &Requeue{Message: "Pods need to be recreated"}
	}
	return nil
}
