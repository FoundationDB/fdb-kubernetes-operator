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
	err := r.List(context, pvcs, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, pvc := range pvcs.Items {
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
			continue
		}

		instanceID := GetInstanceIDFromMeta(pvc.ObjectMeta)
		processGroupStatus := processGroups[instanceID]
		if processGroupStatus == nil {
			return &Requeue{Error: fmt.Errorf("unknown PVC %s in replace_misconfigured_pods", instanceID)}
		}

		if processGroupStatus.Remove {
			continue
		}

		_, idNum, err := ParseInstanceID(instanceID)
		if err != nil {
			return &Requeue{Error: err}
		}

		processClass := internal.GetProcessClassFromMeta(pvc.ObjectMeta)
		desiredPVC, err := GetPvc(cluster, processClass, idNum)
		if err != nil {
			return &Requeue{Error: err}
		}

		pvcHash, err := GetJSONHash(desiredPVC.Spec)
		if err != nil {
			return &Requeue{Error: err}
		}

		if pvc.Annotations[fdbtypes.LastSpecKey] != pvcHash {
			instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getSinglePodListOptions(cluster, instanceID)...)
			if err != nil {
				return &Requeue{Error: err}
			}

			if len(instances) > 0 {
				processGroupStatus.Remove = true
				hasNewRemovals = true

				log.Info("Replace instance",
					"namespace", cluster.Namespace,
					"name", cluster.Name,
					"processGroupID", instanceID,
					"pvc", pvc.Name,
					"reason", fmt.Sprintf("PVC spec has changed from %s to %s", pvcHash, pvc.Annotations[fdbtypes.LastSpecKey]))
			}
		}
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, instance := range instances {
		processGroupStatus := processGroups[instance.GetInstanceID()]
		needsRemoval, err := instanceNeedsRemoval(cluster, instance, processGroupStatus)
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

func instanceNeedsRemoval(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, processGroupStatus *fdbtypes.ProcessGroupStatus) (bool, error) {
	if instance.Pod == nil {
		return false, nil
	}

	instanceID := instance.GetInstanceID()

	if processGroupStatus == nil {
		return false, fmt.Errorf("unknown instance %s in replace_misconfigured_pods", instanceID)
	}

	if processGroupStatus.Remove {
		return false, nil
	}

	_, idNum, err := ParseInstanceID(instanceID)
	if err != nil {
		return false, err
	}

	_, desiredInstanceID := getInstanceID(cluster, instance.GetProcessClass(), idNum)
	if instanceID != desiredInstanceID {
		log.Info("Replace instance",
			"namespace", cluster.Namespace,
			"name", cluster.Name,
			"processGroupID", instanceID,
			"reason", fmt.Sprintf("expect instanceID: %s", desiredInstanceID))
		return true, nil
	}

	if instance.GetPublicIPSource() != cluster.GetPublicIPSource() {
		log.Info("Replace instance",
			"namespace", cluster.Namespace,
			"name", cluster.Name,
			"processGroupID", instanceID,
			"reason", fmt.Sprintf("publicIP source has changed from %s to %s", instance.GetPublicIPSource(), cluster.GetPublicIPSource()))
		return true, nil
	}

	if instance.GetProcessClass() == fdbtypes.ProcessClassStorage {
		// Replace the instance if the storage servers differ
		storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
		if err != nil {
			return false, err
		}

		if storageServersPerPod != cluster.GetStorageServersPerPod() {
			log.Info("Replace instance",
				"namespace", cluster.Namespace,
				"name", cluster.Name,
				"processGroupID", instanceID,
				"reason", fmt.Sprintf("storageServersPerPod has changed from %d to %d", storageServersPerPod, cluster.GetStorageServersPerPod()))
			return true, nil
		}
	}

	expectedNodeSelector := cluster.GetProcessSettings(instance.GetProcessClass()).PodTemplate.Spec.NodeSelector
	if !equality.Semantic.DeepEqual(instance.Pod.Spec.NodeSelector, expectedNodeSelector) {
		log.Info("Replace instance",
			"namespace", cluster.Namespace,
			"name", cluster.Name,
			"processGroupID", instanceID,
			"reason", fmt.Sprintf("nodeSelector has changed from %s to %s", instance.Pod.Spec.NodeSelector, expectedNodeSelector))
		return true, nil
	}

	if cluster.Spec.UpdatePodsByReplacement {
		specHash, err := GetPodSpecHash(cluster, instance.GetProcessClass(), idNum, nil)
		if err != nil {
			return false, err
		}

		if instance.Metadata.Annotations[fdbtypes.LastSpecKey] != specHash {
			log.Info("Replace instance",
				"namespace", cluster.Namespace,
				"name", cluster.Name,
				"processGroupID", instanceID,
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, instance.Metadata.Annotations[fdbtypes.LastSpecKey]))
			return true, nil
		}
	}

	if cluster.Spec.ReplaceInstancesWhenResourcesChange != nil && *cluster.Spec.ReplaceInstancesWhenResourcesChange {
		desiredSpec, err := GetPodSpec(cluster, instance.GetProcessClass(), idNum)
		if err != nil {
			return false, err
		}

		if resourcesNeedsReplacement(desiredSpec.Containers, instance.Pod.Spec.Containers) {
			log.Info("Replace instance",
				"namespace", cluster.Namespace,
				"name", cluster.Name,
				"processGroupID", instanceID,
				"reason", "Resource requests have changed")
			return true, nil
		}

		if resourcesNeedsReplacement(desiredSpec.InitContainers, instance.Pod.Spec.InitContainers) {
			log.Info("Replace instance",
				"namespace", cluster.Namespace,
				"name", cluster.Name,
				"processGroupID", instanceID,
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
