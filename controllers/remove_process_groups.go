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
	ctx "context"
	"fmt"
	"net"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// removeProcessGroups provides a reconciliation step for removing process groups as part of a
// shrink or replacement.
type removeProcessGroups struct{}

// reconcile runs the reconciler's work.
func (u removeProcessGroups) reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *requeue {
	remainingMap, err := r.getRemainingMap(cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	allExcluded, processGroupsToRemove := r.getProcessGroupsToRemove(cluster, remainingMap)
	// If no process groups are marked to remove we have to check if all process groups are excluded.
	if len(processGroupsToRemove) == 0 {
		if !allExcluded {
			return &requeue{message: "Reconciliation needs to exclude more processes"}
		}
		return nil
	}

	// We don't use the "cached" of the cluster status from the CRD to minimize the window between data loss (e.g. a node
	// or a set of Pods is not reachable anymore). We still end up with the risk to actually query the FDB cluster and after that
	// query the cluster gets into a degraded state.
	// We could be smarter here and only block removals that target stateful processes by e.g. filtering those out of the
	// processGroupsToRemove slice.
	if cluster.GetEnforceFullReplicationForDeletion() {
		adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
		if err != nil {
			return &requeue{curError: err}
		}
		defer adminClient.Close()

		hasDesiredFaultTolerance, err := internal.HasDesiredFaultTolerance(adminClient, cluster)
		if err != nil {
			return &requeue{curError: err}
		}

		if !hasDesiredFaultTolerance {
			return &requeue{
				message: "Removals cannot proceed because cluster has degraded fault tolerance",
				delay:   30 * time.Second,
			}
		}
	}

	removedProcessGroups := r.removeProcessGroups(context, cluster, processGroupsToRemove)
	err = includeInstance(r, context, cluster, removedProcessGroups)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}

func removeProcessGroup(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceID string) error {
	instanceListOptions := internal.GetSinglePodListOptions(cluster, instanceID)
	instances, err := r.PodLifecycleManager.GetPods(r, cluster, context, instanceListOptions...)
	if err != nil {
		return err
	}

	if len(instances) == 1 {
		err = r.PodLifecycleManager.DeletePod(r, context, instances[0])

		if err != nil {
			return err
		}
	} else if len(instances) > 0 {
		return fmt.Errorf("multiple pods found for cluster %s, processGroup %s", cluster.Name, instanceID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, instanceListOptions...)
	if err != nil {
		return err
	}
	if len(pvcs.Items) == 1 {
		err = r.Delete(context, &pvcs.Items[0])
		if err != nil {
			return err
		}
	} else if len(pvcs.Items) > 0 {
		return fmt.Errorf("multiple PVCs found for cluster %s, processGroup %s", cluster.Name, instanceID)
	}

	services := &corev1.ServiceList{}
	err = r.List(context, services, instanceListOptions...)
	if err != nil {
		return err
	}
	if len(services.Items) == 1 {
		err = r.Delete(context, &services.Items[0])
		if err != nil {
			return err
		}
	} else if len(services.Items) > 0 {
		return fmt.Errorf("multiple services found for cluster %s, processGroup %s", cluster.Name, instanceID)
	}

	return nil
}

func confirmRemoval(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, processGroupID string) (bool, bool, error) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	canBeIncluded := true
	instanceListOptions := internal.GetSinglePodListOptions(cluster, processGroupID)

	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, instanceListOptions...)
	if err != nil {
		return false, false, err
	}

	if len(pods) == 1 {
		// If the Pod is already in a terminating state we don't have to care for it
		if pods[0] != nil && pods[0].ObjectMeta.DeletionTimestamp == nil {
			logger.Info("Waiting for instance to get torn down", "processGroupID", processGroupID, "pod", pods[0].Name)
			return false, false, nil
		}
		// Pod is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(pods) > 0 {
		return false, false, fmt.Errorf("multiple pods found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, instanceListOptions...)
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
	} else if len(pvcs.Items) > 0 {
		return false, canBeIncluded, fmt.Errorf("multiple PVCs found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	services := &corev1.ServiceList{}
	err = r.List(context, services, instanceListOptions...)
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
	} else if len(services.Items) > 0 {
		return false, canBeIncluded, fmt.Errorf("multiple services found for cluster %s, processGroupID %s", cluster.Name, processGroupID)
	}

	return true, canBeIncluded, nil
}

func includeInstance(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, removedProcessGroups map[string]bool) error {
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	addresses := make([]fdbtypes.ProcessAddress, 0)

	hasStatusUpdate := false

	processGroups := make([]*fdbtypes.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove && removedProcessGroups[processGroup.ProcessGroupID] {
			for _, pAddr := range processGroup.Addresses {
				addresses = append(addresses, fdbtypes.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
			}
			hasStatusUpdate = true
		} else {
			processGroups = append(processGroups, processGroup)
		}
	}

	if len(addresses) > 0 {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))

		err = adminClient.IncludeInstances(addresses)
		if err != nil {
			return err
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		err := r.clearPendingRemovalsFromSpec(context, cluster)
		if err != nil {
			return err
		}
	}

	if hasStatusUpdate {
		cluster.Status.ProcessGroups = processGroups
		err := r.Status().Update(context, cluster)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *FoundationDBClusterReconciler) getRemainingMap(cluster *fdbtypes.FoundationDBCluster) (map[string]bool, error) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return map[string]bool{}, err
	}
	defer adminClient.Close()

	addresses := make([]fdbtypes.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.Remove || processGroup.ExclusionSkipped {
			continue
		}

		if len(processGroup.Addresses) == 0 {
			logger.Info("Getting remaining removals to check for exclusion", "processGroupID", processGroup.ProcessGroupID, "reason", "missing address")
			continue
		}

		for _, pAddr := range processGroup.Addresses {
			addresses = append(addresses, fdbtypes.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
		}
	}

	var remaining []fdbtypes.ProcessAddress
	if len(addresses) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return map[string]bool{}, err
		}
	}

	if len(remaining) > 0 {
		logger.Info("Exclusions to complete", "remainingServers", remaining)
	}

	remainingMap := make(map[string]bool, len(remaining))
	for _, address := range addresses {
		remainingMap[address.String()] = false
	}
	for _, address := range remaining {
		remainingMap[address.String()] = true
	}

	return remainingMap, nil
}

func (r *FoundationDBClusterReconciler) getProcessGroupsToRemove(cluster *fdbtypes.FoundationDBCluster, remainingMap map[string]bool) (bool, []string) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	var cordSet map[string]struct{}
	allExcluded := true
	processGroupsToRemove := make([]string, 0, len(cluster.Status.ProcessGroups))

	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.Remove {
			continue
		}

		// Only query FDB if we have a pending removal otherwise don't query FDB
		if len(cordSet) == 0 {
			var err error
			cordSet, err = r.getCoordinatorSet(cluster)

			if err != nil {
				logger.Error(err, "Fetching coordinator set for removal")
				return false, []string{}
			}
		}

		excluded, err := processGroup.IsExcluded(remainingMap)
		if !excluded || err != nil {
			logger.Info("Incomplete exclusion still present in removeProcessGroups step", "processGroupID", processGroup.ProcessGroupID, "error", err)
			allExcluded = false
			continue
		}

		if _, ok := cordSet[processGroup.ProcessGroupID]; ok {
			logger.Info("Block removal of Coordinator", "processGroupID", processGroup.ProcessGroupID)
			allExcluded = false
			continue
		}

		logger.Info("Marking exclusion complete", "processGroupID", processGroup.ProcessGroupID, "addresses", processGroup.Addresses)
		processGroup.Excluded = true
		processGroupsToRemove = append(processGroupsToRemove, processGroup.ProcessGroupID)
	}

	return allExcluded, processGroupsToRemove
}

func (r *FoundationDBClusterReconciler) removeProcessGroups(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, processGroupsToRemove []string) map[string]bool {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeProcessGroups")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "RemovingProcesses", fmt.Sprintf("Removing pods: %v", processGroupsToRemove))

	removedProcessGroups := make(map[string]bool)
	for _, id := range processGroupsToRemove {
		err := removeProcessGroup(r, context, cluster, id)
		if err != nil {
			logger.Error(err, "Error during remove Pod", "processGroupID", id)
			continue
		}

		removed, include, err := confirmRemoval(r, context, cluster, id)
		if err != nil {
			logger.Error(err, "Error during confirm Pod removal", "processGroupID", id)
			continue
		}

		if removed {
			// Pods that are stuck in terminating shouldn't block reconciliation but we also
			// don't want to include them since they have an unknown state.
			removedProcessGroups[id] = include
			continue
		}

	}

	return removedProcessGroups
}
