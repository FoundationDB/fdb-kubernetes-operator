/*
 * update_pods.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

// UpdatePods provides a reconciliation step for recreating pods with new pod
// specs.
type UpdatePods struct{}

// Reconcile runs the reconciler's work.
func (u UpdatePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
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
			return false, ReconciliationNotReadyError{message: "Cluster has pod that is pending deletion", retryable: true}
		}

		_, idNum, err := ParseInstanceID(instanceID)
		if err != nil {
			return false, err
		}

		specHash, err := GetPodSpecHash(cluster, instance.GetProcessClass(), idNum, nil)
		if err != nil {
			return false, err
		}

		if instance.Metadata.Annotations[LastSpecKey] != specHash {
			podClient, err := r.getPodClient(cluster, instance)
			if err != nil {
				return false, err
			}
			substitutions, err := podClient.GetVariableSubstitutions()
			if err != nil {
				return false, err
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
			return false, nil
		}

		var enabled = cluster.Spec.AutomationOptions.DeletePods
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return false, err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsPodsDeletion",
				"Spec require deleting some pods, but deleting pods is disabled")
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Pod deletion is disabled"}
		}
	}

	for zone, zoneInstances := range updates {
		ready, err := r.PodLifecycleManager.CanDeletePods(r, context, cluster)
		if err != nil {
			return false, err
		}
		if !ready {
			return false, ReconciliationNotReadyError{message: "Reconciliation requires deleting pods, but deletion is not currently safe"}
		}

		hasLock, err := r.takeLock(cluster, "updating pods")
		if !hasLock {
			return false, err
		}

		log.Info("Deleting pods", "namespace", cluster.Namespace, "cluster", cluster.Name, "zone", zone, "count", len(zoneInstances))
		r.Recorder.Event(cluster, "Normal", "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))

		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, zoneInstances)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdatePods) RequeueAfter() time.Duration {
	return 0
}
