/*
 * replacements.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package replacements

import (
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/pointer"
)

// ReplaceMisconfiguredProcessGroups checks if the cluster has any misconfigured process groups that must be replaced.
func ReplaceMisconfiguredProcessGroups(log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, pvcMap map[string]corev1.PersistentVolumeClaim, podMap map[string]*corev1.Pod) (bool, error) {
	hasReplacements := false

	maxReplacements := getMaxReplacements(cluster, cluster.GetMaxConcurrentReplacements())
	for _, processGroup := range cluster.Status.ProcessGroups {
		if maxReplacements <= 0 {
			log.Info("Early abort, reached limit of concurrent replacements")
			break
		}

		if processGroup.IsMarkedForRemoval() {
			continue
		}

		pvc, hasPVC := pvcMap[processGroup.ProcessGroupID]
		pod, hasPod := podMap[processGroup.ProcessGroupID]

		if hasPVC {
			needsPVCRemoval, err := processGroupNeedsRemovalForPVC(cluster, pvc, log)
			if err != nil {
				return hasReplacements, err
			}

			if needsPVCRemoval && hasPod {
				processGroup.MarkForRemoval()
				hasReplacements = true
				maxReplacements--
				continue
			}
		} else if processGroup.ProcessClass.IsStateful() {
			log.V(1).Info("Could not find PVC for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
		}

		if !hasPod || pod == nil {
			log.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		needsRemoval, err := processGroupNeedsRemoval(cluster, pod, processGroup, log)
		if err != nil {
			return hasReplacements, err
		}

		if needsRemoval {
			processGroup.MarkForRemoval()
			hasReplacements = true
			maxReplacements--
		}
	}

	return hasReplacements, nil
}

func processGroupNeedsRemovalForPVC(cluster *fdbv1beta2.FoundationDBCluster, pvc corev1.PersistentVolumeClaim, log logr.Logger) (bool, error) {
	processGroupID := internal.GetProcessGroupIDFromMeta(cluster, pvc.ObjectMeta)
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "pvc", pvc.Name, "processGroupID", processGroupID, "reconciler", "replaceMisconfiguredProcessGroups")

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

	_, idNum, err := internal.ParseProcessGroupID(processGroupID)
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

	if pvc.Annotations[fdbv1beta2.LastSpecKey] != pvcHash {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("PVC spec has changed from %s to %s", pvcHash, pvc.Annotations[fdbv1beta2.LastSpecKey]))
		return true, nil
	}
	if pvc.Name != desiredPVC.Name {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("PVC name has changed from %s to %s", desiredPVC.Name, pvc.Name))
		return true, nil
	}

	return false, nil
}

func processGroupNeedsRemoval(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, processGroupStatus *fdbv1beta2.ProcessGroupStatus, log logr.Logger) (bool, error) {
	if pod == nil {
		return false, nil
	}

	processGroupID := internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta)

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", processGroupID, "reconciler", "replaceMisconfiguredProcessGroups")

	if processGroupStatus == nil {
		return false, fmt.Errorf("unknown process group %s in replace_misconfigured_pods", processGroupID)
	}

	if processGroupStatus.IsMarkedForRemoval() {
		return false, nil
	}

	_, idNum, err := internal.ParseProcessGroupID(processGroupID)
	if err != nil {
		return false, err
	}

	processClass := internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta)
	_, desiredProcessGroupID := internal.GetProcessGroupID(cluster, processClass, idNum)
	if processGroupID != desiredProcessGroupID {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("expect process group ID: %s", desiredProcessGroupID))
		return true, nil
	}

	ipSource, err := internal.GetPublicIPSource(pod)
	if err != nil {
		return false, err
	}
	if ipSource != cluster.GetPublicIPSource() {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("publicIP source has changed from %s to %s", ipSource, cluster.GetPublicIPSource()))
		return true, nil
	}

	if processClass == fdbv1beta2.ProcessClassStorage {
		// Replace the process group if the storage servers differ
		storageServersPerPod, err := internal.GetStorageServersPerPodForPod(pod)
		if err != nil {
			return false, err
		}

		if storageServersPerPod != cluster.GetStorageServersPerPod() {
			logger.Info("Replace process group",
				"reason", fmt.Sprintf("storageServersPerPod has changed from %d to %d", storageServersPerPod, cluster.GetStorageServersPerPod()))
			return true, nil
		}
	}

	// When three data hall redundancy is enabled. The node selecctor is not provided by the spec.
	// But configured by the operator. Thus we can skip this step
	if cluster.Spec.DatabaseConfiguration.RedundancyMode != fdbv1beta2.RedundancyModeThreeDataHall {
		expectedNodeSelector := cluster.GetProcessSettings(processClass).PodTemplate.Spec.NodeSelector
		if !equality.Semantic.DeepEqual(pod.Spec.NodeSelector, expectedNodeSelector) {
			specHash, err := internal.GetPodSpecHash(cluster, processClass, idNum, nil, processGroupStatus.LocalityDataHall)
			if err != nil {
				return false, err
			}

			if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] != specHash {
				logger.Info("Replace process group",
					"reason", fmt.Sprintf("nodeSelector has changed from %s to %s", pod.Spec.NodeSelector, expectedNodeSelector))
				return true, nil
			}
		}
	}
	//TODO(manuel.fontan) for three data hall if Localities Node Selector changes we should replace the affected Pods.

	if cluster.NeedsReplacement(processGroupStatus) {
		specHash, err := internal.GetPodSpecHash(cluster, processClass, idNum, nil, processGroupStatus.LocalityDataHall)
		if err != nil {
			return false, err
		}

		if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] != specHash {
			logger.Info("Replace process group",
				"reason", fmt.Sprintf("specHash has changed from %s to %s", specHash, pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey]))
			return true, nil
		}
	}

	if pointer.BoolDeref(cluster.Spec.ReplaceInstancesWhenResourcesChange, false) {
		desiredSpec, err := internal.GetPodSpec(cluster, processClass, idNum, processGroupStatus.LocalityDataHall)
		if err != nil {
			return false, err
		}

		if resourcesNeedsReplacement(desiredSpec.Containers, pod.Spec.Containers) {
			logger.Info("Replace process group",
				"reason", "Resource requests have changed")
			return true, nil
		}

		if resourcesNeedsReplacement(desiredSpec.InitContainers, pod.Spec.InitContainers) {
			logger.Info("Replace process group",
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
