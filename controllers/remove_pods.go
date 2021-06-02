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
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// RemovePods provides a reconciliation step for removing pods as part of a
// shrink or replacement.
type RemovePods struct{}

// Reconcile runs the reconciler's work.
func (u RemovePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	remainingMap, err := r.getRemainingMap(cluster)
	if err != nil {
		return false, err
	}

	allExcluded, processGroupsToRemove := getProcessGroupsToRemove(cluster, remainingMap)
	// If no process groups are marked to remove we have to check if all process groups are excluded.
	if len(processGroupsToRemove) == 0 {
		return allExcluded, nil
	}

	allRemoved, removedProcessGroups := r.removeProcessGroups(context, cluster, processGroupsToRemove)
	err = includeInstance(r, context, cluster, removedProcessGroups)
	if err != nil {
		return false, err
	}

	return allRemoved && allExcluded, nil
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
		return fmt.Errorf("multiple pods found for cluster %s, instance ID %s", cluster.Name, instanceID)
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
		return fmt.Errorf("multiple PVCs found for cluster %s, instance ID %s", cluster.Name, instanceID)
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
		return fmt.Errorf("multiple services found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	return nil
}

func confirmPodRemoval(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceID string) (bool, bool, error) {
	canBeIncluded := true
	instanceListOptions := getSinglePodListOptions(cluster, instanceID)

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions...)
	if err != nil {
		return false, false, err
	}

	if len(instances) == 1 {
		// If the Pod is already in a terminating state we don't have to care for it
		if instances[0].Metadata != nil && instances[0].Metadata.DeletionTimestamp == nil {
			log.Info("Waiting for instance to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "pod", instances[0].Metadata.Name)
			return false, false, nil
		}
		// Pod is in terminating state so we don't want to block but we also don't want to include it
		canBeIncluded = false
	} else if len(instances) > 0 {
		return false, false, fmt.Errorf("multiple pods found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, instanceListOptions...)
	if err != nil {
		return false, canBeIncluded, err
	}
	if len(pvcs.Items) == 1 {
		log.Info("Waiting for volume claim to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "pvc", pvcs.Items[0].Name)
		return false, canBeIncluded, nil
	} else if len(pvcs.Items) > 0 {
		return false, canBeIncluded, fmt.Errorf("multiple PVCs found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	services := &corev1.ServiceList{}
	err = r.List(context, services, instanceListOptions...)
	if err != nil {
		return false, canBeIncluded, err
	}
	if len(services.Items) == 1 {
		log.Info("Waiting for service to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "service", services.Items[0].Name)
		return false, canBeIncluded, nil
	} else if len(services.Items) > 0 {
		return false, canBeIncluded, fmt.Errorf("multiple services found for cluster %s, instance ID %s", cluster.Name, instanceID)
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
			// TODO (johscheuer): do we really want to skip further actions?
			return map[string]bool{}, fmt.Errorf("cannot check the exclusion state of instance %s, which has no IP address", processGroup.ProcessGroupID)
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

func getProcessGroupsToRemove(cluster *fdbtypes.FoundationDBCluster, remainingMap map[string]bool) (bool, []string) {
	allExcluded := true
	processGroupsToRemove := make([]string, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.Remove {
			continue
		}

		excluded, err := processGroup.IsExcluded(remainingMap)
		if !excluded || err != nil {
			log.Info("Incomplete exclusion still present in RemovePods step", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroup", processGroup.ProcessGroupID, "error", err)
			allExcluded = false
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

		removed, include, err := confirmPodRemoval(r, context, cluster, id)
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

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u RemovePods) RequeueAfter() time.Duration {
	return 0
}
