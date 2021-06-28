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

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// UpdatePods provides a reconciliation step for recreating pods with new pod
// specs.
type UpdatePods struct{}

// Reconcile runs the reconciler's work.
func (u UpdatePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	updates := make(map[string][]FdbInstance)

	removals := make(map[string]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			removals[processGroup.ProcessGroupID] = true
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

		if instance.Pod.DeletionTimestamp != nil && !cluster.InstanceIsBeingRemoved(instanceID) {
			return &Requeue{Message: "Cluster has pod that is pending deletion", Delay: podSchedulingDelayDuration}
		}

		_, idNum, err := ParseInstanceID(instanceID)
		if err != nil {
			return &Requeue{Error: err}
		}

		specHash, err := GetPodSpecHash(cluster, instance.GetProcessClass(), idNum, nil)
		if err != nil {
			return &Requeue{Error: err}
		}

		if instance.Metadata.Annotations[fdbtypes.LastSpecKey] != specHash {
			log.Info("Update Pod",
				"namespace", cluster.Namespace,
				"name", cluster.Name,
				"processGroupID", instanceID,
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, instance.Metadata.Annotations[fdbtypes.LastSpecKey]))

			podClient, message := r.getPodClient(cluster, instance)
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
				updates[zone] = make([]FdbInstance, 0)
			}
			updates[zone] = append(updates[zone], instance)
		}
	}

	if len(updates) > 0 {
		if cluster.Spec.UpdatePodsByReplacement {
			log.Info("Requeuing reconciliation to replace pods", "namespace", cluster.Namespace, "cluster", cluster.Name)
			return &Requeue{Message: "Requeueing reconciliation to replace pods"}
		}

		var enabled = cluster.Spec.AutomationOptions.DeletePods
		if enabled != nil && !*enabled {
			r.Recorder.Event(cluster, corev1.EventTypeNormal,
				"NeedsPodsDeletion", "Spec require deleting some pods, but deleting pods is disabled")
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return &Requeue{Message: "Pod deletion is disabled"}
		}
	}

	for zone, zoneInstances := range updates {
		ready, err := r.PodLifecycleManager.CanDeletePods(r, context, cluster)
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

		log.Info("Deleting pods", "namespace", cluster.Namespace, "cluster", cluster.Name, "zone", zone, "count", len(zoneInstances))
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))

		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, zoneInstances, false)
		if err != nil {
			return &Requeue{Error: err}
		}

		return &Requeue{Message: "Pods need to be recreated"}
	}
	return nil
}
