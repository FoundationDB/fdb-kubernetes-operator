/*
 * remove_pods.go
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
	corev1 "k8s.io/api/core/v1"
)

// RemovePods provides a reconciliation step for removing pods as part of a
// shrink or replacement.
type RemovePods struct{}

// Reconcile runs the reconciler's work.
func (u RemovePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	processGroupsToRemove := make([]string, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			excluded := processGroup.Excluded || processGroup.ExclusionSkipped
			if !excluded {
				log.Info("Incomplete exclusion still present in RemovePods step. Retrying reconciliation", "namespace", cluster.Namespace, "name", cluster.Name, "instance", processGroup.ProcessGroupID)
				return false, nil
			}
			processGroupsToRemove = append(processGroupsToRemove, processGroup.ProcessGroupID)
		}
	}

	if len(processGroupsToRemove) == 0 {
		return true, nil
	}

	if cluster.ShouldUseLocks() {
		hasLock, err := r.takeLock(cluster, fmt.Sprintf("Removing pods: %v", processGroupsToRemove))
		if !hasLock {
			return false, err
		}
	}

	r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", processGroupsToRemove))
	removedProcessGroups := make(map[string]bool)
	allRemoved := true
	for _, id := range processGroupsToRemove {

		err := removePod(r, context, cluster, id)
		if err != nil {
			return false, err
		}

		removed, err := confirmPodRemoval(r, context, cluster, id)
		if err != nil {
			return false, err
		}
		if removed {
			removedProcessGroups[id] = true
		} else {
			allRemoved = false
			continue
		}
	}

	err := includeInstance(r, context, cluster, removedProcessGroups)
	if err != nil {
		return false, err
	}

	return allRemoved, nil
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
		return fmt.Errorf("Multiple pods found for cluster %s, instance ID %s", cluster.Name, instanceID)
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
		return fmt.Errorf("Multiple PVCs found for cluster %s, instance ID %s", cluster.Name, instanceID)
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
		return fmt.Errorf("Multiple services found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	return nil
}

func confirmPodRemoval(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceID string) (bool, error) {
	instanceListOptions := getSinglePodListOptions(cluster, instanceID)

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions...)
	if err != nil {
		return false, err
	}
	if len(instances) == 1 {
		log.Info("Waiting for instance to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "pod", instances[0].Metadata.Name)
		return false, nil
	} else if len(instances) > 0 {
		return false, fmt.Errorf("Multiple pods found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	pods := &corev1.PodList{}
	err = r.List(context, pods, instanceListOptions...)
	if err != nil {
		return false, err
	}
	if len(pods.Items) == 1 {
		log.Info("Waiting for pod to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "pod", pods.Items[0].Name)
		return false, nil
	} else if len(pods.Items) > 0 {
		return false, fmt.Errorf("Multiple pods found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, instanceListOptions...)
	if err != nil {
		return false, err
	}
	if len(pvcs.Items) == 1 {
		log.Info("Waiting for volume claim to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "pvc", pvcs.Items[0].Name)
		return false, nil
	} else if len(pvcs.Items) > 0 {
		return false, fmt.Errorf("Multiple PVCs found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	services := &corev1.ServiceList{}
	err = r.List(context, services, instanceListOptions...)
	if err != nil {
		return false, err
	}
	if len(services.Items) == 1 {
		log.Info("Waiting for service to get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "instanceID", instanceID, "service", services.Items[0].Name)
		return false, nil
	} else if len(services.Items) > 0 {
		return false, fmt.Errorf("Multiple services found for cluster %s, instance ID %s", cluster.Name, instanceID)
	}

	return true, nil
}

func includeInstance(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, removedProcessGroups map[string]bool) error {
	adminClient, err := r.AdminClientProvider(cluster, r)
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
		r.Recorder.Event(cluster, "Normal", "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))

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

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u RemovePods) RequeueAfter() time.Duration {
	return 0
}
