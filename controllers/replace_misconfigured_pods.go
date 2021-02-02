/*
 * replace_misconfigured_pods.go
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

	"k8s.io/apimachinery/pkg/api/equality"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// ReplaceMisconfiguredPods identifies processes that need to be replaced in
// order to bring up new processes with different configuration.
type ReplaceMisconfiguredPods struct{}

// Reconcile runs the reconciler's work.
func (c ReplaceMisconfiguredPods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	hasNewRemovals := false

	processGroups := make(map[string]*fdbtypes.ProcessGroupStatus)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroups[processGroup.ProcessGroupID] = processGroup
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err := r.List(context, pvcs, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	for _, pvc := range pvcs.Items {
		ownedByCluster := false
		for _, ownerReference := range pvc.OwnerReferences {
			if ownerReference.UID == cluster.UID {
				ownedByCluster = true
				break
			}
		}

		if !ownedByCluster {
			continue
		}

		instanceID := GetInstanceIDFromMeta(pvc.ObjectMeta)
		processGroupStatus := processGroups[instanceID]
		if processGroupStatus == nil {
			return false, fmt.Errorf("unknown PVC %s in replace_misconfigured_pods", instanceID)
		}
		if processGroupStatus.Remove {
			continue
		}

		_, idNum, err := ParseInstanceID(instanceID)
		if err != nil {
			return false, err
		}

		processClass := GetProcessClassFromMeta(pvc.ObjectMeta)
		desiredPVC, err := GetPvc(cluster, processClass, idNum)
		if err != nil {
			return false, err
		}
		pvcHash, err := GetJSONHash(desiredPVC.Spec)
		if err != nil {
			return false, err
		}

		if pvc.Annotations[LastSpecKey] != pvcHash {
			instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getSinglePodListOptions(cluster, instanceID)...)
			if err != nil {
				return false, err
			}
			if len(instances) > 0 {
				processGroupStatus.Remove = true
				hasNewRemovals = true
			}
		}
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	for _, instance := range instances {
		processGroupStatus := processGroups[instance.GetInstanceID()]
		needsRemoval, err := instanceNeedsRemoval(cluster, instance, processGroupStatus)
		if err != nil {
			return false, err
		}

		if needsRemoval {
			processGroupStatus.Remove = true
			hasNewRemovals = true
		}
	}

	if hasNewRemovals {
		err = r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}

		return false, nil
	}

	return true, nil
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
		return true, nil
	}

	if instance.GetPublicIPSource() != cluster.GetPublicIPSource() {
		return true, nil
	}

	if instance.GetProcessClass() == fdbtypes.ProcessClassStorage {
		// Replace the instance if the storage servers differ
		storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
		if err != nil {
			return false, err
		}

		if storageServersPerPod != cluster.GetStorageServersPerPod() {
			return true, nil
		}
	}

	expectedNodeSelector := cluster.GetProcessSettings(instance.GetProcessClass()).PodTemplate.Spec.NodeSelector
	if !equality.Semantic.DeepEqual(instance.Pod.Spec.NodeSelector, expectedNodeSelector) {
		return true, nil
	}

	if cluster.Spec.UpdatePodsByReplacement {
		specHash, err := GetPodSpecHash(cluster, instance.GetProcessClass(), idNum, nil)
		if err != nil {
			return false, err
		}

		return instance.Metadata.Annotations[LastSpecKey] != specHash, nil
	}

	return false, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ReplaceMisconfiguredPods) RequeueAfter() time.Duration {
	return 0
}
