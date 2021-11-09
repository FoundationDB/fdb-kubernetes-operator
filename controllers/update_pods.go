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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	"golang.org/x/net/context"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// updatePods provides a reconciliation step for recreating pods with new pod
// specs.
type updatePods struct{}

// reconcile runs the reconciler's work.
func (updatePods) reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updatePods")

	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
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
			return &requeue{message: "Cluster has pod that is pending deletion", delay: podSchedulingDelayDuration}
		}

		_, idNum, err := podmanager.ParseProcessGroupID(processGroup.ProcessGroupID)
		if err != nil {
			return &requeue{curError: err}
		}

		processClass, err := podmanager.GetProcessClass(cluster, pod)
		if err != nil {
			return &requeue{curError: err}
		}

		specHash, err := internal.GetPodSpecHash(cluster, processClass, idNum, nil)
		if err != nil {
			return &requeue{curError: err}
		}

		if pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey] != specHash {
			logger.Info("Update Pod",
				"processGroupID", processGroup.ProcessGroupID,
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey]))

			podClient, message := r.getPodClient(cluster, pod)
			if podClient == nil {
				return &requeue{message: message, delay: podSchedulingDelayDuration}
			}

			substitutions, err := podClient.GetVariableSubstitutions()
			if err != nil {
				return &requeue{curError: err}
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
			return &requeue{message: "Requeueing reconciliation to replace pods"}
		}

		if !pointer.BoolDeref(cluster.Spec.AutomationOptions.DeletePods, true) {
			r.Recorder.Event(cluster, corev1.EventTypeNormal,
				"NeedsPodsDeletion", "Spec require deleting some pods, but deleting pods is disabled")
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}
			return &requeue{message: "Pod deletion is disabled"}
		}
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r.Client)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	if len(updates) == 0 {
		return nil
	}

	return deletePodsForUpdates(context, r, cluster, adminClient, updates, logger)
}

func getPodsToDelete(deletionMode fdbtypes.DeletionMode, updates map[string][]*corev1.Pod) (string, []*corev1.Pod, error) {
	var deletions []*corev1.Pod
	var zone string

	if deletionMode == fdbtypes.DeletionModeAll {
		zone = "cluster"
		for _, zoneInstances := range updates {
			deletions = append(deletions, zoneInstances...)
		}

		return zone, deletions, nil
	}

	if deletionMode == fdbtypes.DeletionModeProcessGroup {
		for _, zoneInstances := range updates {
			if len(zoneInstances) < 1 {
				continue
			}
			pod := zoneInstances[0]
			zone = pod.Name
			deletions = append(deletions, pod)
			// Fetch the first pod and delete it
			return zone, deletions, nil
		}
	}

	if deletionMode == fdbtypes.DeletionModeZone {
		// Default case is zone
		for zoneName, zoneInstances := range updates {
			zone = zoneName
			deletions = zoneInstances
			// Fetch the first zone and stop
			return zone, deletions, nil
		}
	}

	return zone, deletions, fmt.Errorf("unknown deletion mode: \"%s\"", deletionMode)
}

// deletePodsForUpdates will delete Pods with the specified deletion mode
func deletePodsForUpdates(context context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster, adminClient fdbadminclient.AdminClient, updates map[string][]*corev1.Pod, logger logr.Logger) *requeue {
	deletioNMode := r.PodLifecycleManager.GetDeletionMode(cluster)
	zone, deletions, err := getPodsToDelete(deletioNMode, updates)
	if err != nil {
		return &requeue{curError: err}
	}

	ready, err := r.PodLifecycleManager.CanDeletePods(adminClient, context, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	if !ready {
		return &requeue{message: "Reconciliation requires deleting pods, but deletion is not currently safe", delay: podSchedulingDelayDuration}
	}

	// Only lock the cluster if we are not running in the delete "All" mode.
	// Otherwise we want to delete all Pods and don't require a lock to sync with other clusters.
	if deletioNMode != fdbtypes.DeletionModeAll {
		hasLock, err := r.takeLock(cluster, "updating pods")
		if !hasLock {
			return &requeue{curError: err}
		}
	}

	logger.Info("Deleting pods", "zone", zone, "count", len(deletions), "deletionMode", string(cluster.Spec.AutomationOptions.DeletionMode))
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))

	err = r.PodLifecycleManager.UpdatePods(r, context, cluster, deletions, false)
	if err != nil {
		return &requeue{curError: err}
	}

	return &requeue{message: "Pods need to be recreated"}
}
