/*
 * add_pods.go
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
	"reflect"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// AddPods provides a reconciliation step for adding new pods to a cluster.
type AddPods struct{}

// Reconcile runs the reconciler's work.
func (a AddPods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return false, err
	}
	desiredCounts := desiredCountStruct.Map()

	if reflect.DeepEqual(currentCounts, desiredCounts) {
		return true, nil
	}

	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return false, err
	}
	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		err = r.Create(context, configMap)
		if err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	instanceIDs := make(map[string]map[int]bool)
	for _, instance := range instances {
		instanceID := instance.GetInstanceID()
		_, num, err := ParseInstanceID(instanceID)
		if err != nil {
			return false, err
		}

		class := instance.GetProcessClass()
		if instanceIDs[class] == nil {
			instanceIDs[class] = make(map[int]bool)
		}

		if instance.Pod != nil && instance.Pod.DeletionTimestamp != nil && !cluster.InstanceIsBeingRemoved(instanceID) {
			return false, ReconciliationNotReadyError{message: "Cluster has pod that is pending deletion", retryable: true}
		}

		instanceIDs[class][num] = true
	}

	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		if desiredCount < 0 {
			desiredCount = 0
		}
		newCount := desiredCount - currentCounts[processClass]
		if newCount > 0 {
			r.Recorder.Event(cluster, "Normal", "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))

			pvcs := &corev1.PersistentVolumeClaimList{}
			r.List(context, pvcs, getPodListOptions(cluster, processClass, "")...)
			reusablePvcs := make(map[int]bool, len(pvcs.Items))

			for index, pvc := range pvcs.Items {
				ownedByCluster := false
				for _, ownerReference := range pvc.OwnerReferences {
					if ownerReference.UID == cluster.UID {
						ownedByCluster = true
						break
					}
				}

				if ownedByCluster && pvc.ObjectMeta.DeletionTimestamp == nil {
					instanceID := GetInstanceIDFromMeta(pvc.ObjectMeta)

					matchingInstances, err := r.PodLifecycleManager.GetInstances(
						r, cluster, context,
						getPodListOptions(cluster, processClass, instanceID)...,
					)
					if err != nil {
						return false, err
					}
					if len(matchingInstances) == 0 && !cluster.InstanceIsBeingRemoved(instanceID) {
						reusablePvcs[index] = true
					}
				}
			}

			addedCount := 0
			for index := range reusablePvcs {
				if newCount <= 0 {
					break
				}
				instanceID := GetInstanceIDFromMeta(pvcs.Items[index].ObjectMeta)
				_, idNum, err := ParseInstanceID(instanceID)
				if err != nil {
					return false, err
				}

				pod, err := GetPod(context, cluster, processClass, idNum, r)
				if err != nil {
					return false, err
				}
				if GetInstanceIDFromMeta(pod.ObjectMeta) != instanceID {
					return false, fmt.Errorf("Failed to create new pod to match PVC %s", pvcs.Items[index].Name)
				}

				err = r.PodLifecycleManager.CreateInstance(r, context, pod)
				if err != nil {
					return false, err
				}

				if instanceIDs[processClass] == nil {
					instanceIDs[processClass] = make(map[int]bool)
				}
				instanceIDs[processClass][idNum] = true

				addedCount++
				newCount--
			}

			idNum := 1

			if instanceIDs[processClass] == nil {
				instanceIDs[processClass] = make(map[int]bool)
			}

			for i := 0; i < newCount; i++ {
				for idNum > 0 {
					if !instanceIDs[processClass][idNum] {
						break
					}
					idNum++
				}

				pvc, err := GetPvc(cluster, processClass, idNum)
				if err != nil {
					return false, err
				}

				if pvc != nil {
					owner, err := buildOwnerReferenceForCluster(context, cluster, r)
					if err != nil {
						return false, err
					}
					pvc.ObjectMeta.OwnerReferences = owner

					err = r.Create(context, pvc)
					if err != nil {
						return false, err
					}
				}

				pod, err := GetPod(context, cluster, processClass, idNum, r)
				if err != nil {
					return false, err
				}
				err = r.PodLifecycleManager.CreateInstance(r, context, pod)
				if err != nil {
					return false, err
				}

				addedCount++
				idNum++
			}
			cluster.Status.ProcessCounts.IncreaseCount(processClass, addedCount)
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (a AddPods) RequeueAfter() time.Duration {
	return 0
}
