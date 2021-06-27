/*
 * remove_pods.go
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// RemovePods provides a reconciliation step for removing pods as part of a
// shrink or replacement.
type RemovePods struct{}

// Reconcile runs the reconciler's work.
func (u RemovePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	remainingMap, err := r.getRemainingMap(cluster)
	if err != nil {
		return &Requeue{Error: err}
	}

	allExcluded, processGroupsToRemove := r.getProcessGroupsToRemove(cluster, remainingMap)
	// If no process groups are marked to remove we have to check if all process groups are excluded.
	if len(processGroupsToRemove) == 0 {
		if !allExcluded {
			return &Requeue{Message: "Reconciliation needs to exclude more processes"}
		}
		return nil
	}

	allRemoved, removedProcessGroups := r.removeProcessGroups(context, cluster, processGroupsToRemove)
	err = includeInstance(r, context, cluster, removedProcessGroups)
	if err != nil {
		return &Requeue{Error: err}
	}

	if !allRemoved || !allExcluded {
		return &Requeue{Message: "Reconciliation needs to remove more processes"}
	}
	return nil
}

func removePod(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceID string) error {
	instanceListOptions := getSinglePodListOptions(cluster, instanceID)
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions...)
	if err != nil {
		return err
	}

	if len(instances) == 1 {
		err = r.PodLifecycleManager.DeleteInstance(r, context, instances[0])

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

func confirmRemoval(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceID string) (bool, bool, error) {
	canBeIncluded := true
	instanceListOptions := getSinglePodListOptions(cluster, instanceID)

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions...)
	if err != nil {
		return false, false, err
	}

	if len(instances) == 1 {
		// If the Pod is already in a terminating state we don't have to care for it
		if instances[0].Metadata != nil && instances[0].Metadata.DeletionTimestamp == nil {
			log.Info("Waiting for instance to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", instanceID, "pod", instances[0].Metadata.Name)
			return false, false, nil
		}
		// Pod is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(instances) > 0 {
		return false, false, fmt.Errorf("multiple pods found for cluster %s, processGroup %s", cluster.Name, instanceID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, instanceListOptions...)
	if err != nil {
		return false, canBeIncluded, err
	}

	if len(pvcs.Items) == 1 {
		if pvcs.Items[0].DeletionTimestamp == nil {
			log.Info("Waiting for volume claim to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", instanceID, "pvc", pvcs.Items[0].Name)
			return false, canBeIncluded, nil
		}
		// PVC is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(pvcs.Items) > 0 {
		return false, canBeIncluded, fmt.Errorf("multiple PVCs found for cluster %s, processGroup %s", cluster.Name, instanceID)
	}

	services := &corev1.ServiceList{}
	err = r.List(context, services, instanceListOptions...)
	if err != nil {
		return false, canBeIncluded, err
	}

	if len(services.Items) == 1 {
		if services.Items[0].DeletionTimestamp == nil {
			log.Info("Waiting for service to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", instanceID, "service", services.Items[0].Name)
			return false, canBeIncluded, nil
		}
		// service is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(services.Items) > 0 {
		return false, canBeIncluded, fmt.Errorf("multiple services found for cluster %s, processGroup %s", cluster.Name, instanceID)
	}

	return true, canBeIncluded, nil
}

func includeInstance(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, removedProcessGroups map[string]bool) error {
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	addresses := make([]string, 0)

	hasStatusUpdate := false

	processGroups := make([]*fdbtypes.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove && removedProcessGroups[processGroup.ProcessGroupID] {
			addresses = append(addresses, processGroup.Addresses...)
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
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return map[string]bool{}, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.Remove || processGroup.ExclusionSkipped {
			continue
		}

		if len(processGroup.Addresses) == 0 {
			log.Info("Getting remaining removals to check for exclusion", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", processGroup.ProcessGroupID, "reason", "missing address")
			continue
		}

		addresses = append(addresses, processGroup.Addresses...)
	}

	var remaining []string
	if len(addresses) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return map[string]bool{}, err
		}
	}

	if len(remaining) > 0 {
		log.Info("Exclusions to complete", "namespace", cluster.Namespace, "cluster", cluster.Name, "remainingServers", remaining)
	}

	remainingMap := make(map[string]bool, len(remaining))
	for _, address := range addresses {
		remainingMap[address] = false
	}
	for _, address := range remaining {
		remainingMap[address] = true
	}

	return remainingMap, nil
}

func (r *FoundationDBClusterReconciler) getCoordinatorSet(cluster *fdbtypes.FoundationDBCluster) (map[string]internal.None, error) {
	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		log.Error(err, "Fetching coordinator set for removal", "namespace", cluster.Namespace, "cluster", cluster.Name)
		return map[string]internal.None{}, err
	}
	defer adminClient.Close()

	return adminClient.GetCoordinatorSet()
}

func (r *FoundationDBClusterReconciler) getProcessGroupsToRemove(cluster *fdbtypes.FoundationDBCluster, remainingMap map[string]bool) (bool, []string) {
	var cordSet map[string]internal.None
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
				return false, []string{}
			}
		}

		excluded, err := processGroup.IsExcluded(remainingMap)
		if !excluded || err != nil {
			log.Info("Incomplete exclusion still present in RemovePods step", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", processGroup.ProcessGroupID, "error", err)
			allExcluded = false
			continue
		}

		if _, ok := cordSet[processGroup.ProcessGroupID]; ok {
			log.Info("Block removal of Coordinator", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", processGroup.ProcessGroupID)
			allExcluded = false
			continue
		}

		log.Info("Marking exclusion complete", "namespace", cluster.Namespace, "name", cluster.Name, "processGroup", processGroup.ProcessGroupID, "addresses", processGroup.Addresses)
		processGroup.Excluded = true
		processGroupsToRemove = append(processGroupsToRemove, processGroup.ProcessGroupID)
	}

	return allExcluded, processGroupsToRemove
}

func (r *FoundationDBClusterReconciler) removeProcessGroups(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, processGroupsToRemove []string) (bool, map[string]bool) {
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "RemovingProcesses", fmt.Sprintf("Removing pods: %v", processGroupsToRemove))

	removedProcessGroups := make(map[string]bool)
	allRemoved := true
	for _, id := range processGroupsToRemove {
		err := removePod(r, context, cluster, id)
		if err != nil {
			allRemoved = false
			log.Error(err, "Error during remove Pod", "namespace", cluster.Namespace, "name", cluster.Name, "processGroup", id)
			continue
		}

		removed, include, err := confirmRemoval(r, context, cluster, id)
		if err != nil {
			allRemoved = false
			log.Error(err, "Error during confirm Pod removal", "namespace", cluster.Namespace, "name", cluster.Name, "processGroup", id)
			continue
		}

		if removed {
			// Pods that are stuck in terminating shouldn't block reconciliation but we also
			// don't want to include them since they have an unknown state.
			removedProcessGroups[id] = include
			continue
		}

		allRemoved = false
	}

	return allRemoved, removedProcessGroups
}
