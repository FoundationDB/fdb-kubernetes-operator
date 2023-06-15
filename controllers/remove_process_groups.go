/*
 * remove_process_groups.go
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
	"net"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/buggify"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/removals"
	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// removeProcessGroups provides a reconciliation step for removing process groups as part of a
// shrink or replacement.
type removeProcessGroups struct{}

// reconcile runs the reconciler's work.
func (u removeProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	remainingMap, err := removals.GetRemainingMap(logger, adminClient, cluster, status)
	if err != nil {
		return &requeue{curError: err}
	}

	allExcluded, newExclusions, processGroupsToRemove := r.getProcessGroupsToRemove(cluster, remainingMap)
	// If no process groups are marked to remove we have to check if all process groups are excluded.
	if len(processGroupsToRemove) == 0 {
		if !allExcluded {
			return &requeue{message: "Reconciliation needs to exclude more processes"}
		}
		return nil
	}

	// Update the cluster to reflect the new exclusions in our status
	if newExclusions {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	// Ensure we only remove process groups that are not blocked to be removed by the buggify config.
	processGroupsToRemove = buggify.FilterBlockedRemovals(cluster, processGroupsToRemove)
	// If all of the process groups are filtered out we can stop doing the next steps.
	if len(processGroupsToRemove) == 0 {
		return nil
	}

	// We don't use the "cached" of the cluster status from the CRD to minimize the window between data loss (e.g. a node
	// or a set of Pods is not reachable anymore). We still end up with the risk to actually query the FDB cluster and after that
	// query the cluster gets into a degraded state.
	// We could be smarter here and only block removals that target stateful processes by e.g. filtering those out of the
	// processGroupsToRemove slice.
	hasDesiredFaultTolerance := internal.HasDesiredFaultToleranceFromStatus(logger, status, cluster)
	if !hasDesiredFaultTolerance {
		return &requeue{
			message: "Removals cannot proceed because cluster has degraded fault tolerance",
			delay:   30 * time.Second,
		}
	}

	// In addition to that we should add the same logic as in the exclude step
	// to ensure we never exclude/remove more process groups than desired.
	zonedRemovals, lastDeletion, err := removals.GetZonedRemovals(status, processGroupsToRemove)
	if err != nil {
		return &requeue{curError: err}
	}

	// If the operator is allowed to delete all process groups at the same time we don't enforce any safety checks.
	if cluster.GetRemovalMode() != fdbv1beta2.PodUpdateModeAll {
		// To ensure we are not deletion zones faster than Kubernetes actually removes Pods we are adding a wait time
		// if we have resources in the terminating state. We will only block if the terminating state was recently (in the
		// last minute).
		waitTime, allowed := removals.RemovalAllowed(lastDeletion, time.Now().Unix(), cluster.GetWaitBetweenRemovalsSeconds())
		if !allowed {
			return &requeue{message: fmt.Sprintf("not allowed to remove process groups, waiting: %v", waitTime), delay: time.Duration(waitTime) * time.Second}
		}
	}

	zone, zoneRemovals, err := removals.GetProcessGroupsToRemove(cluster.GetRemovalMode(), zonedRemovals)
	if err != nil {
		return &requeue{curError: err}
	}

	logger.Info("Removing process groups", "zone", zone, "count", len(zoneRemovals), "deletionMode", cluster.GetRemovalMode())

	// This will return a map of the newly removed ProcessGroups and the ProcessGroups with the ResourcesTerminating condition
	removedProcessGroups := r.removeProcessGroups(ctx, cluster, zoneRemovals, zonedRemovals[removals.TerminatingZone])

	err = includeProcessGroup(ctx, r, cluster, removedProcessGroups)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}

func removeProcessGroup(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, processGroupID fdbv1beta2.ProcessGroupID) error {
	listOptions := internal.GetSinglePodListOptions(cluster, processGroupID)
	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, listOptions...)
	if err != nil {
		return err
	}

	if len(pods) == 1 && pods[0].DeletionTimestamp.IsZero() {
		err = r.PodLifecycleManager.DeletePod(ctx, r, pods[0])
		if err != nil {
			return err
		}
	} else if len(pods) > 1 {
		return fmt.Errorf("multiple pods found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, pvcs, listOptions...)
	if err != nil {
		return err
	}
	if len(pvcs.Items) == 1 && pvcs.Items[0].DeletionTimestamp.IsZero() {
		logr.FromContextOrDiscard(ctx).V(1).Info("Deleting pvc", "name", pvcs.Items[0].Name)
		err = r.Delete(ctx, &pvcs.Items[0])
		if err != nil {
			return err
		}
	} else if len(pvcs.Items) > 1 {
		return fmt.Errorf("multiple PVCs found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	services := &corev1.ServiceList{}
	err = r.List(ctx, services, listOptions...)
	if err != nil {
		return err
	}
	if len(services.Items) == 1 && services.Items[0].DeletionTimestamp.IsZero() {
		logr.FromContextOrDiscard(ctx).V(1).Info("Deleting service", "name", services.Items[0].Name)
		err = r.Delete(ctx, &services.Items[0])
		if err != nil {
			return err
		}
	} else if len(services.Items) > 1 {
		return fmt.Errorf("multiple services found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	return nil
}

func confirmRemoval(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, processGroupID fdbv1beta2.ProcessGroupID) (bool, bool, error) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	canBeIncluded := true
	listOptions := internal.GetSinglePodListOptions(cluster, processGroupID)

	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, listOptions...)
	if err != nil {
		return false, false, err
	}

	if len(pods) == 1 {
		// If the Pod is already in a terminating state we don't have to care for it
		if pods[0] != nil && pods[0].ObjectMeta.DeletionTimestamp == nil {
			logger.Info("Waiting for process group to get torn down", "processGroupID", processGroupID, "pod", pods[0].Name)
			return false, false, nil
		}
		// Pod is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(pods) > 1 {
		return false, false, fmt.Errorf("multiple pods found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, pvcs, listOptions...)
	if err != nil {
		return false, canBeIncluded, err
	}

	if len(pvcs.Items) == 1 {
		if pvcs.Items[0].DeletionTimestamp == nil {
			logger.Info("Waiting for volume claim to get torn down", "processGroupID", processGroupID, "pvc", pvcs.Items[0].Name)
			return false, canBeIncluded, nil
		}
		// PVC is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(pvcs.Items) > 1 {
		return false, canBeIncluded, fmt.Errorf("multiple PVCs found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	services := &corev1.ServiceList{}
	err = r.List(ctx, services, listOptions...)
	if err != nil {
		return false, canBeIncluded, err
	}

	if len(services.Items) == 1 {
		if services.Items[0].DeletionTimestamp == nil {
			logger.Info("Waiting for service to get torn down", "processGroupID", processGroupID, "service", services.Items[0].Name)
			return false, canBeIncluded, nil
		}
		// service is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(services.Items) > 1 {
		return false, canBeIncluded, fmt.Errorf("multiple services found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	return true, canBeIncluded, nil
}

func includeProcessGroup(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, removedProcessGroups map[fdbv1beta2.ProcessGroupID]bool) error {
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	fdbProcessesToInclude := getProcessesToInclude(cluster, removedProcessGroups)
	if len(fdbProcessesToInclude) > 0 {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "IncludingProcesses", fmt.Sprintf("Including removed processes: %v", fdbProcessesToInclude))

		err = adminClient.IncludeProcesses(fdbProcessesToInclude)
		if err != nil {
			return err
		}

		err := r.updateOrApply(ctx, cluster)
		if err != nil {
			return err
		}
	}

	return nil
}

func getProcessesToInclude(cluster *fdbv1beta2.FoundationDBCluster, removedProcessGroups map[fdbv1beta2.ProcessGroupID]bool) []fdbv1beta2.ProcessAddress {
	fdbProcessesToInclude := make([]fdbv1beta2.ProcessAddress, 0)

	if len(removedProcessGroups) == 0 {
		return fdbProcessesToInclude
	}

	idx := 0
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() && removedProcessGroups[processGroup.ProcessGroupID] {
			if cluster.UseLocalitiesForExclusion() {
				fdbProcessesToInclude = append(fdbProcessesToInclude, fdbv1beta2.ProcessAddress{StringAddress: processGroup.GetExclusionString()})
			}
			for _, pAddr := range processGroup.Addresses {
				fdbProcessesToInclude = append(fdbProcessesToInclude, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
			}
			continue
		}
		cluster.Status.ProcessGroups[idx] = processGroup
		idx++
	}

	// Remove the trailing duplicates.
	cluster.Status.ProcessGroups = cluster.Status.ProcessGroups[:idx]

	return fdbProcessesToInclude
}

func (r *FoundationDBClusterReconciler) getProcessGroupsToRemove(cluster *fdbv1beta2.FoundationDBCluster, remainingMap map[string]bool) (bool, bool, []*fdbv1beta2.ProcessGroupStatus) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	var cordSet map[string]fdbv1beta2.None
	allExcluded := true
	newExclusions := false
	processGroupsToRemove := make([]*fdbv1beta2.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))

	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.IsMarkedForRemoval() {
			continue
		}

		// Only query FDB if we have a pending removal otherwise don't query FDB
		if len(cordSet) == 0 {
			var err error
			cordSet, err = r.getCoordinatorSet(cluster)

			if err != nil {
				logger.Error(err, "Fetching coordinator set for removal")
				return false, false, nil
			}
		}

		if _, ok := cordSet[string(processGroup.ProcessGroupID)]; ok {
			logger.Info("Block removal of Coordinator", "processGroupID", processGroup.ProcessGroupID)
			allExcluded = false
			continue
		}

		// ProcessGroup is already marked as excluded we can add it to the processGroupsToRemove and skip further
		// checks.
		if processGroup.IsExcluded() {
			processGroupsToRemove = append(processGroupsToRemove, processGroup)
			continue
		}

		excluded, err := processGroup.AllAddressesExcluded(remainingMap)
		if !excluded || err != nil {
			logger.Info("Incomplete exclusion still present in removeProcessGroups step", "processGroupID", processGroup.ProcessGroupID, "error", err)
			allExcluded = false
			continue
		}

		logger.Info("Marking exclusion complete", "processGroupID", processGroup.ProcessGroupID, "addresses", processGroup.Addresses)
		processGroup.SetExclude()
		processGroupsToRemove = append(processGroupsToRemove, processGroup)
		newExclusions = true
	}

	return allExcluded, newExclusions, processGroupsToRemove
}

func (r *FoundationDBClusterReconciler) removeProcessGroups(ctx context.Context, cluster *fdbv1beta2.FoundationDBCluster, processGroupsToRemove []fdbv1beta2.ProcessGroupID, terminatingProcessGroups []fdbv1beta2.ProcessGroupID) map[fdbv1beta2.ProcessGroupID]bool {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "RemovingProcesses", fmt.Sprintf("Removing pods: %v", processGroupsToRemove))

	processGroups := append(processGroupsToRemove, terminatingProcessGroups...)

	for _, id := range processGroups {
		err := removeProcessGroup(logr.NewContext(ctx, logger), r, cluster, id)
		if err != nil {
			logger.Error(err, "Error during remove process group", "processGroupID", id)
			continue
		}
	}

	removedProcessGroups := make(map[fdbv1beta2.ProcessGroupID]bool)
	// We have to check if the currently removed process groups are completely removed.
	// In addition, we have to check if one of the terminating process groups has been cleaned up.
	for _, id := range processGroups {
		removed, include, err := confirmRemoval(ctx, r, cluster, id)
		if err != nil {
			logger.Error(err, "Error during confirm process group removal", "processGroupID", id)
			continue
		}

		if removed {
			// Pods that are stuck in terminating shouldn't block reconciliation, but we also
			// don't want to include them since they have an unknown state.
			removedProcessGroups[id] = include
			continue
		}
	}

	return removedProcessGroups
}
