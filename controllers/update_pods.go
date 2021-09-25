/*
 * update_pods.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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
	"fmt"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// UpdatePods provides a reconciliation step for recreating pods with new pod
// specs.
type UpdatePods struct{}

// Reconcile runs the reconciler's work.
func (u UpdatePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "UpdatePods")

	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	updates := make(map[string][]*corev1.Pod)
	podMap := internal.CreatePodMap(cluster, pods)

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			logger.V(1).Info("Ignore removed Pod",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if cluster.SkipProcessGroup(processGroup) {
			logger.V(1).Info("Ignore pending Pod",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, ok := podMap[processGroup.ProcessGroupID]
		if !ok || pod == nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if pod.DeletionTimestamp != nil && !cluster.InstanceIsBeingRemoved(processGroup.ProcessGroupID) {
			return &Requeue{Message: "Cluster has pod that is pending deletion", Delay: podSchedulingDelayDuration}
		}

		_, idNum, err := ParseProcessGroupID(processGroup.ProcessGroupID)
		if err != nil {
			return &Requeue{Error: err}
		}

		processClass, err := GetProcessClass(cluster, pod)
		if err != nil {
			return &Requeue{Error: err}
		}

		specHash, err := internal.GetPodSpecHash(cluster, processClass, idNum, nil)
		if err != nil {
			return &Requeue{Error: err}
		}

		if pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey] != specHash {
			logger.Info("Update Pod",
				"processGroupID", processGroup.ProcessGroupID,
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey]))

			podClient, message := r.getPodClient(cluster, pod)
			if podClient == nil {
				return &Requeue{Message: message, Delay: podSchedulingDelayDuration}
			}

			substitutions, err := podClient.GetVariableSubstitutions()
			if err != nil {
				return &Requeue{Error: err}
			}

			zone := substitutions["FDB_ZONE_ID"]
			if r.InSimulation {
				zone = "simulation"
			}

			if updates[zone] == nil {
				updates[zone] = make([]*corev1.Pod, 0)
			}
			updates[zone] = append(updates[zone], pod)
		}
	}

	if len(updates) > 0 {
		if cluster.Spec.UpdatePodsByReplacement {
			logger.Info("Requeuing reconciliation to replace pods")
			return &Requeue{Message: "Requeueing reconciliation to replace pods"}
		}

		var enabled = cluster.Spec.AutomationOptions.DeletePods
		if enabled != nil && !*enabled {
			r.Recorder.Event(cluster, corev1.EventTypeNormal,
				"NeedsPodsDeletion", "Spec require deleting some pods, but deleting pods is disabled")
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}
			return &Requeue{Message: "Pod deletion is disabled"}
		}
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r.Client)
	if err != nil {
		return &Requeue{Error: err}
	}
	defer adminClient.Close()

	for zone, zoneInstances := range updates {
		ready, err := r.PodLifecycleManager.CanDeletePods(adminClient, context, cluster)
		if err != nil {
			return &Requeue{Error: err}
		}
		if !ready {
			return &Requeue{Message: "Reconciliation requires deleting pods, but deletion is not currently safe", Delay: podSchedulingDelayDuration}
		}

		hasLock, err := r.takeLock(cluster, "updating pods")
		if !hasLock {
			return &Requeue{Error: err}
		}

		logger.Info("Deleting pods", "zone", zone, "count", len(zoneInstances))
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))

		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, zoneInstances, false)
		if err != nil {
			return &Requeue{Error: err}
		}

		return &Requeue{Message: "Pods need to be recreated"}
	}

	return nil
}
