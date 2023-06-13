/*
 * update_pods.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2023 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/statuschecks"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

// updatePods provides a reconciliation step for recreating pods with new pod
// specs.
type updatePods struct{}

// reconcile runs the reconciler's work.
func (updatePods) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updatePods")

	updates, err := getPodsToUpdate(ctx, logger, r, cluster)
	if err != nil {
		return &requeue{curError: err, delay: podSchedulingDelayDuration, delayedRequeue: true}
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
			err = r.updateOrApply(ctx, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}
			return &requeue{message: "Pod deletion is disabled"}
		}
	}

	if len(updates) == 0 {
		return nil
	}

	return deletePodsForUpdates(ctx, r, cluster, updates, logger, status)
}

// processGroupIsUnavailable returns true if the process group is unavailable.
func processGroupIsUnavailable(processGroupStatus *fdbv1beta2.ProcessGroupStatus) bool {
	// If the Process Group has Pods is pending state, we count it as unavailable.
	if processGroupStatus.GetConditionTime(fdbv1beta2.PodPending) != nil {
		return true
	}
	// If the Process Group is missing processes, we count it as unavailable.
	if processGroupStatus.GetConditionTime(fdbv1beta2.MissingProcesses) != nil {
		return true
	}
	// If the Pod is running with failed containers, we count it as unavailable.
	if processGroupStatus.GetConditionTime(fdbv1beta2.PodFailing) != nil {
		return true
	}
	return false
}

// getFaultDomainsWithUnavailablePods returns a map of fault domains with unavailable Pods. The map has the fault domain as key and the value is not used.
func getFaultDomainsWithUnavailablePods(ctx context.Context, logger logr.Logger, reconciler *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) map[fdbv1beta2.FaultDomain]fdbv1beta2.None {
	faultDomainsWithUnavailablePods := make(map[fdbv1beta2.FaultDomain]fdbv1beta2.None)

	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.ProcessClass.IsStateful() {
			continue
		}

		if processGroupIsUnavailable(processGroup) {
			faultDomainsWithUnavailablePods[processGroup.FaultDomain] = fdbv1beta2.None{}
			continue
		}
		pod, err := reconciler.PodLifecycleManager.GetPod(ctx, reconciler, cluster, processGroup.GetPodName(cluster))
		if err != nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID, "error", err)
			faultDomainsWithUnavailablePods[processGroup.FaultDomain] = fdbv1beta2.None{}
			continue
		}
		// If the Pod is marked for deletion, we count it as unavailable.
		if pod.DeletionTimestamp != nil {
			faultDomainsWithUnavailablePods[processGroup.FaultDomain] = fdbv1beta2.None{}
			continue
		}
	}

	return faultDomainsWithUnavailablePods
}

// getPodsToUpdate returns a map of Zone to Pods mapping. The map has the fault domain as key and all Pods in that fault domain will be present as a slice of *corev1.Pod.
func getPodsToUpdate(ctx context.Context, logger logr.Logger, reconciler *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) (map[string][]*corev1.Pod, error) {
	updates := make(map[string][]*corev1.Pod)

	faultDomainsWithUnavailablePods := getFaultDomainsWithUnavailablePods(ctx, logger, reconciler, cluster)
	maxZonesWithUnavailablePods := cluster.GetMaxZonesWithUnavailablePods()

	// When number of zones with unavailable Pods exceeds the maxZonesWithUnavailablePods skip updates.
	if len(faultDomainsWithUnavailablePods) > maxZonesWithUnavailablePods {
		logger.V(1).Info("Skip process groups for update, the number of zones with unavailable Pods exceeds the limit",
			"maxZonesWithUnavailablePods", maxZonesWithUnavailablePods,
			"faultDomainsWithUnavailablePods", faultDomainsWithUnavailablePods)
		return updates, nil
	}

	for _, processGroup := range cluster.Status.ProcessGroups {
		// When the number of zones with unavailable Pods equals the maxZonesWithUnavailablePods
		// skip the process group if it does not belong to a zone with unavailable Pods.
		if len(faultDomainsWithUnavailablePods) == maxZonesWithUnavailablePods {
			if _, ok := faultDomainsWithUnavailablePods[processGroup.FaultDomain]; !ok {
				logger.V(1).Info("Skip process group for update, the number zones with unavailable Pods equals the limit but the process group does not belong to a zone with unavailable Pods",
					"processGroupID", processGroup.ProcessGroupID,
					"maxZonesWithUnavailablePods", maxZonesWithUnavailablePods,
					"faultDomain", processGroup.FaultDomain,
					"faultDomainsWithUnavailablePods", faultDomainsWithUnavailablePods)
				continue
			}
		}

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

		pod, err := reconciler.PodLifecycleManager.GetPod(ctx, reconciler, cluster, processGroup.GetPodName(cluster))
		// If a Pod is not found ignore it for now.
		if err != nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if shouldRequeueDueToTerminatingPod(pod, cluster, processGroup.ProcessGroupID) {
			return nil, fmt.Errorf("cluster has Pod %s that is pending deletion", pod.Name)
		}

		_, idNum, err := podmanager.ParseProcessGroupID(processGroup.ProcessGroupID)
		if err != nil {
			logger.Info("Skipping Pod due to error parsing Process Group ID",
				"processGroupID", processGroup.ProcessGroupID,
				"error", err.Error())
			continue
		}

		processClass, err := podmanager.GetProcessClass(cluster, pod)
		if err != nil {
			logger.Info("Skipping Pod due to error fetching process class",
				"processGroupID", processGroup.ProcessGroupID,
				"error", err.Error())
			continue
		}

		specHash, err := internal.GetPodSpecHash(cluster, processClass, idNum, nil)
		if err != nil {
			logger.Info("Skipping Pod due to error generating spec hash",
				"processGroupID", processGroup.ProcessGroupID,
				"error", err.Error())
			continue
		}

		// The Pod is updated, so we can continue.
		if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] == specHash {
			continue
		}

		logger.Info("Update Pod",
			"processGroupID", processGroup.ProcessGroupID,
			"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey]))

		podClient, message := reconciler.getPodClient(cluster, pod)
		if podClient == nil {
			logger.Info("Skipping Pod due to missing Pod client information",
				"processGroupID", processGroup.ProcessGroupID,
				"message", message)
			continue
		}

		substitutions, err := podClient.GetVariableSubstitutions()
		if err != nil {
			logger.Info("Skipping Pod due to missing variable substitutions",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if substitutions == nil {
			logger.Info("Skipping Pod due to missing locality information",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		zone := substitutions["FDB_ZONE_ID"]
		if reconciler.InSimulation {
			zone = "simulation"
		}

		if updates[zone] == nil {
			updates[zone] = make([]*corev1.Pod, 0)
		}
		updates[zone] = append(updates[zone], pod)
	}

	return updates, nil
}

func shouldRequeueDueToTerminatingPod(pod *corev1.Pod, cluster *fdbv1beta2.FoundationDBCluster, processGroupID fdbv1beta2.ProcessGroupID) bool {
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

	// TODO(jscheuermann): If a maintenance zone is already set, we should favour Pods in this zone to be updated.
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
func deletePodsForUpdates(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, updates map[string][]*corev1.Pod, logger logr.Logger, status *fdbv1beta2.FoundationDBStatus) *requeue {
	deletionMode := r.PodLifecycleManager.GetDeletionMode(cluster)
	zone, deletions, err := getPodsToDelete(deletionMode, updates)
	if err != nil {
		return &requeue{curError: err}
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r.Client)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}
	defer adminClient.Close()

	newContext := logr.NewContext(ctx, logger)
	if status != nil {
		newContext = context.WithValue(ctx, statuschecks.StatusContextKey{}, status)
	}

	ready, err := r.PodLifecycleManager.CanDeletePods(newContext, adminClient, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	if !ready {
		return &requeue{message: "Reconciliation requires deleting pods, but deletion is currently not safe", delay: podSchedulingDelayDuration}
	}

	// Only lock the cluster if we are not running in the delete "All" mode.
	// Otherwise, we want to delete all Pods and don't require a lock to sync with other clusters.
	if deletionMode != fdbv1beta2.PodUpdateModeAll {
		hasLock, err := r.takeLock(cluster, "updating pods")
		if !hasLock {
			return &requeue{curError: err}
		}
	}

	// TODO(jscheuermann): If a maintenance zone is already set, we should allow to delete Pods in this zone.
	if deletionMode == fdbv1beta2.PodUpdateModeZone && cluster.UseMaintenaceMode() {
		logger.Info("Setting maintenance mode", "zone", zone)

		err = adminClient.SetMaintenanceZone(zone, cluster.GetMaintenaceModeTimeoutSeconds())
		if err != nil {
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
