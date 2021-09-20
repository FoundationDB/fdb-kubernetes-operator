/*
 * replace_misconfigured_pods.go
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

	"k8s.io/apimachinery/pkg/api/resource"

	"k8s.io/apimachinery/pkg/api/equality"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// ReplaceMisconfiguredPods identifies processes that need to be replaced in
// order to bring up new processes with different configuration.
type ReplaceMisconfiguredPods struct{}

// Reconcile runs the reconciler's work.
func (c ReplaceMisconfiguredPods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	hasNewRemovals := false

	processGroups := make(map[string]*fdbtypes.ProcessGroupStatus)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroups[processGroup.ProcessGroupID] = processGroup
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err := r.List(context, pvcs, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, pvc := range pvcs.Items {
		instanceID := internal.GetProcessGroupIDFromMeta(cluster, pvc.ObjectMeta)
		processGroupStatus := processGroups[instanceID]
		if processGroupStatus == nil {
			return &Requeue{Error: fmt.Errorf("unknown PVC %s in replace_misconfigured_pods", instanceID)}
		}

		if processGroupStatus.Remove {
			continue
		}

		needsNewRemoval, err := instanceNeedsRemovalForPVC(cluster, pvc)
		if err != nil {
			return &Requeue{Error: err}
		}
		if needsNewRemoval {
			instances, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetSinglePodListOptions(cluster, instanceID)...)
			if err != nil {
				return &Requeue{Error: err}
			}

			if len(instances) > 0 {
				processGroupStatus.Remove = true
				hasNewRemovals = true
			}
		}
	}

	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, pod := range pods {
		processGroupStatus := processGroups[GetProcessGroupID(cluster, pod)]
		needsRemoval, err := instanceNeedsRemoval(cluster, pod, processGroupStatus)
		if err != nil {
			return &Requeue{Error: err}
		}

		if needsRemoval {
			processGroupStatus.Remove = true
			hasNewRemovals = true
		}
	}

	if hasNewRemovals {
		err = r.Status().Update(context, cluster)
		if err != nil {
			return &Requeue{Error: err}
		}

		return &Requeue{Message: "Removals have been updated in the cluster status"}
	}

	return nil
}

func instanceNeedsRemovalForPVC(cluster *fdbtypes.FoundationDBCluster, pvc corev1.PersistentVolumeClaim) (bool, error) {
	instanceID := internal.GetInstanceIDFromMeta(cluster, pvc.ObjectMeta)
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "pvc", pvc.Name, "processGroupID", instanceID, "reconciler", "ReplaceMisconfiguredPods")

	ownedByCluster := !cluster.ShouldFilterOnOwnerReferences()
	if !ownedByCluster {
		for _, ownerReference := range pvc.OwnerReferences {
			if ownerReference.UID == cluster.UID {
				ownedByCluster = true
				break
			}
		}
	}
	if !ownedByCluster {
		logger.Info("Ignoring PVC that is not owned by the cluster")
		return false, nil
	}
	instanceID := internal.GetProcessGroupIDFromMeta(pvc.ObjectMeta)

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "pvc", pvc.Name, "processGroupID", instanceID, "reconciler", "ReplaceMisconfiguredPods")

	_, idNum, err := ParseProcessGroupID(instanceID)
	if err != nil {
		return false, err
	}
	processClass := internal.GetProcessClassFromMeta(cluster, pvc.ObjectMeta)
	desiredPVC, err := internal.GetPvc(cluster, processClass, idNum)
	if err != nil {
		return false, err
	}
	pvcHash, err := internal.GetJSONHash(desiredPVC.Spec)
	if err != nil {
		return false, err
	}

	if pvc.Annotations[fdbtypes.LastSpecKey] != pvcHash {
		logger.Info("Replace instance",
			"reason", fmt.Sprintf("PVC spec has changed from %s to %s", pvcHash, pvc.Annotations[fdbtypes.LastSpecKey]))
		return true, nil
	}
	if pvc.Name != desiredPVC.Name {
		logger.Info("Replace instance",
			"reason", fmt.Sprintf("PVC name has changed from %s to %s", desiredPVC.Name, pvc.Name))
		return true, nil
	}
	return false, nil
}

func instanceNeedsRemoval(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod, processGroupStatus *fdbtypes.ProcessGroupStatus) (bool, error) {
	if pod == nil {
		return false, nil
	}

	instanceID := GetProcessGroupID(cluster, pod)

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", instanceID, "reconciler", "ReplaceMisconfiguredPods")

	if processGroupStatus == nil {
		return false, fmt.Errorf("unknown instance %s in replace_misconfigured_pods", instanceID)
	}

	if processGroupStatus.Remove {
		return false, nil
	}

	_, idNum, err := ParseProcessGroupID(instanceID)
	if err != nil {
		return false, err
	}

	processClass, err := GetProcessClass(cluster, pod)
	if err != nil {
		return false, err
	}

	_, desiredInstanceID := internal.GetInstanceID(cluster, processClass, idNum)
	if instanceID != desiredInstanceID {
		logger.Info("Replace instance",
			"reason", fmt.Sprintf("expect instanceID: %s", desiredInstanceID))
		return true, nil
	}

	ipSource, err := GetPublicIPSource(pod)
	if err != nil {
		return false, err
	}
	if ipSource != cluster.GetPublicIPSource() {
		logger.Info("Replace instance",
			"reason", fmt.Sprintf("publicIP source has changed from %s to %s", ipSource, cluster.GetPublicIPSource()))
		return true, nil
	}

	if processClass == fdbtypes.ProcessClassStorage {
		// Replace the instance if the storage servers differ
		storageServersPerPod, err := internal.GetStorageServersPerPodForPod(pod)
		if err != nil {
			return false, err
		}

		if storageServersPerPod != cluster.GetStorageServersPerPod() {
			logger.Info("Replace instance",
				"reason", fmt.Sprintf("storageServersPerPod has changed from %d to %d", storageServersPerPod, cluster.GetStorageServersPerPod()))
			return true, nil
		}
	}

	expectedNodeSelector := cluster.GetProcessSettings(processClass).PodTemplate.Spec.NodeSelector
	if !equality.Semantic.DeepEqual(pod.Spec.NodeSelector, expectedNodeSelector) {
		logger.Info("Replace instance",
			"reason", fmt.Sprintf("nodeSelector has changed from %s to %s", pod.Spec.NodeSelector, expectedNodeSelector))
		return true, nil
	}

	if cluster.Spec.UpdatePodsByReplacement {
		specHash, err := internal.GetPodSpecHash(cluster, processClass, idNum, nil)
		if err != nil {
			return false, err
		}

		if pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey] != specHash {
			logger.Info("Replace instance",
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey]))
			return true, nil
		}
	}

	if cluster.Spec.ReplaceInstancesWhenResourcesChange != nil && *cluster.Spec.ReplaceInstancesWhenResourcesChange {
		desiredSpec, err := internal.GetPodSpec(cluster, processClass, idNum)
		if err != nil {
			return false, err
		}

		if resourcesNeedsReplacement(desiredSpec.Containers, pod.Spec.Containers) {
			logger.Info("Replace instance",
				"reason", "Resource requests have changed")
			return true, nil
		}

		if resourcesNeedsReplacement(desiredSpec.InitContainers, pod.Spec.InitContainers) {
			logger.Info("Replace instance",
				"reason", "Resource requests have changed")
			return true, nil
		}
	}

	return false, nil
}

func resourcesNeedsReplacement(desired []corev1.Container, current []corev1.Container) bool {
	// We only care about requests since limits are ignored during scheduling
	desiredCPURequests, desiredMemoryRequests := getCPUandMemoryRequests(desired)
	currentCPURequests, currentMemoryRequests := getCPUandMemoryRequests(current)

	return desiredCPURequests.Cmp(*currentCPURequests) == 1 || desiredMemoryRequests.Cmp(*currentMemoryRequests) == 1
}

func getCPUandMemoryRequests(containers []corev1.Container) (*resource.Quantity, *resource.Quantity) {
	cpuRequests := &resource.Quantity{}
	memoryRequests := &resource.Quantity{}

	for _, container := range containers {
		cpu := container.Resources.Requests.Cpu()

		if cpu != nil {
			cpuRequests.Add(*cpu)
		}

		memory := container.Resources.Requests.Memory()

		if memory != nil {
			memoryRequests.Add(*memory)
		}
	}

	return cpuRequests, memoryRequests
}
