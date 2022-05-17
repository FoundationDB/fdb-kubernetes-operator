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
	"context"
	"fmt"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

// updatePods provides a reconciliation step for recreating pods with new pod
// specs.
type updatePods struct{}

// reconcile runs the reconciler's work.
func (updatePods) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updatePods")

	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}

	updates := make(map[string][]*corev1.Pod)
	podMap := internal.CreatePodMap(cluster, pods)

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			logger.V(1).Info("Ignore removed Pod",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if cluster.SkipProcessGroup(processGroup) {
			logger.V(1).Info("Ignore pending Pod",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if cluster.NeedsReplacement(processGroup) {
			logger.V(1).Info("Skip process group for deletion, requires a replacement",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, ok := podMap[processGroup.ProcessGroupID]
		if !ok || pod == nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if shouldRequeueDueToTerminatingPod(pod, cluster, processGroup.ProcessGroupID) {
			return &requeue{message: "Cluster has pod that is pending deletion", delay: podSchedulingDelayDuration, delayedRequeue: true}
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

		if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] != specHash {
			logger.Info("Update Pod",
				"processGroupID", processGroup.ProcessGroupID,
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey]))

			podClient, message := r.getPodClient(cluster, pod)
			if podClient == nil {
				return &requeue{message: message, delay: podSchedulingDelayDuration}
			}

			substitutions, err := podClient.GetVariableSubstitutions()
			if err != nil {
				return &requeue{curError: err}
			}

			if substitutions == nil {
				logger.Info("Skipping pod due to missing locality information",
					"processGroupID", processGroup.ProcessGroupID)
				continue
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
		if cluster.Spec.AutomationOptions.PodUpdateStrategy == fdbv1beta2.PodUpdateStrategyReplacement {
			logger.Info("Requeuing reconciliation to replace pods")
			return &requeue{message: "Requeueing reconciliation to replace pods"}
		}

		if r.PodLifecycleManager.GetDeletionMode(cluster) == fdbv1beta2.PodUpdateModeNone {
			r.Recorder.Event(cluster, corev1.EventTypeNormal,
				"NeedsPodsDeletion", "Spec require deleting some pods, but deleting pods is disabled")
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.Status().Update(ctx, cluster)
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

	return deletePodsForUpdates(ctx, r, cluster, adminClient, updates, logger)
}

func shouldRequeueDueToTerminatingPod(pod *corev1.Pod, cluster *fdbv1beta2.FoundationDBCluster, processGroupID string) bool {
	return pod.DeletionTimestamp != nil &&
		pod.DeletionTimestamp.Add(time.Duration(cluster.GetIgnoreTerminatingPodsSeconds())*time.Second).After(time.Now()) &&
		!cluster.ProcessGroupIsBeingRemoved(processGroupID)
}

func getPodsToDelete(deletionMode fdbv1beta2.PodUpdateMode, updates map[string][]*corev1.Pod) (string, []*corev1.Pod, error) {
	if deletionMode == fdbv1beta2.PodUpdateModeAll {
		var deletions []*corev1.Pod

		for _, zoneProcesses := range updates {
			deletions = append(deletions, zoneProcesses...)
		}

		return "cluster", deletions, nil
	}

	if deletionMode == fdbv1beta2.PodUpdateModeProcessGroup {
		for _, zoneProcesses := range updates {
			if len(zoneProcesses) < 1 {
				continue
			}
			pod := zoneProcesses[0]
			// Fetch the first pod and delete it
			return pod.Name, []*corev1.Pod{pod}, nil
		}
	}

	if deletionMode == fdbv1beta2.PodUpdateModeZone {
		// Default case is zone
		for zoneName, zoneProcesses := range updates {
			// Fetch the first zone and stop
			return zoneName, zoneProcesses, nil
		}
	}

	if deletionMode == fdbv1beta2.PodUpdateModeNone {
		return "None", nil, nil
	}

	return "", nil, fmt.Errorf("unknown deletion mode: \"%s\"", deletionMode)
}

// deletePodsForUpdates will delete Pods with the specified deletion mode
func deletePodsForUpdates(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, adminClient fdbadminclient.AdminClient, updates map[string][]*corev1.Pod, logger logr.Logger) *requeue {
	deletionMode := r.PodLifecycleManager.GetDeletionMode(cluster)
	zone, deletions, err := getPodsToDelete(deletionMode, updates)
	if err != nil {
		return &requeue{curError: err}
	}

	ready, err := r.PodLifecycleManager.CanDeletePods(logr.NewContext(ctx, logger), adminClient, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	if !ready {
		return &requeue{message: "Reconciliation requires deleting pods, but deletion is currently not safe", delay: podSchedulingDelayDuration}
	}

	// Only lock the cluster if we are not running in the delete "All" mode.
	// Otherwise we want to delete all Pods and don't require a lock to sync with other clusters.
	if deletionMode != fdbv1beta2.PodUpdateModeAll {
		hasLock, err := r.takeLock(cluster, "updating pods")
		if !hasLock {
			return &requeue{curError: err}
		}
	}

	logger.Info("Deleting pods", "zone", zone, "count", len(deletions), "deletionMode", string(cluster.Spec.AutomationOptions.DeletionMode))
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))

	err = r.PodLifecycleManager.UpdatePods(logr.NewContext(ctx, logger), r, cluster, deletions, false)
	if err != nil {
		return &requeue{curError: err}
	}

	return &requeue{message: "Pods need to be recreated", delayedRequeue: true}
}
