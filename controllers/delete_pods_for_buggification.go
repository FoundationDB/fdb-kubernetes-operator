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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// DeletePodsForBuggification provides a reconciliation step for recreating
// pods with new pod specs when buggifying the config.
type DeletePodsForBuggification struct{}

// Reconcile runs the reconciler's work.
func (d DeletePodsForBuggification) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	updates := make([]FdbInstance, 0)

	removals := make(map[string]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			removals[processGroup.ProcessGroupID] = true
		}
	}

	crashLoopPods := make(map[string]bool, len(cluster.Spec.Buggify.CrashLoop))
	crashLoopAll := false
	for _, instanceID := range cluster.Spec.Buggify.CrashLoop {
		if instanceID == "*" {
			crashLoopAll = true
		} else {
			crashLoopPods[instanceID] = true
		}
	}

	for _, instance := range instances {
		if instance.Pod == nil {
			continue
		}

		instanceID := instance.GetInstanceID()
		_, pendingRemoval := removals[instanceID]
		if pendingRemoval {
			continue
		}

		inCrashLoop := false
		for _, container := range instance.Pod.Spec.Containers {
			if container.Name == "foundationdb" && len(container.Args) > 0 {
				inCrashLoop = container.Args[0] == "crash-loop"
			}
		}

		shouldCrashLoop := crashLoopAll || crashLoopPods[instanceID]

		if shouldCrashLoop != inCrashLoop {
			log.Info("Deleting pod for buggification", "instanceID", instanceID, "shouldCrashLoop", shouldCrashLoop, "inCrashLoop", inCrashLoop)
			updates = append(updates, instance)
		}
	}

	if len(updates) > 0 {
		log.Info("Deleting pods", "namespace", cluster.Namespace, "cluster", cluster.Name, "count", len(updates))
		r.Recorder.Event(cluster, "Normal", "UpdatingPods", "Recreating pods for buggification")

		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, updates, true)
		if err != nil {
			return &Requeue{Error: err}
		}

		return &Requeue{Message: "Pods need to be recreated"}
	}
	return nil
}
