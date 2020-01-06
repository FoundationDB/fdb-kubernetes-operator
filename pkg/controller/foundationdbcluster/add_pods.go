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

package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"reflect"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// AddPods provides a reconciliation step for adding new pods to a cluster.
type AddPods struct{}

func (a AddPods) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()

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

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}

	instanceIDs := make(map[string]map[int]bool)
	for _, instance := range instances {
		_, num, err := ParseInstanceID(instance.Metadata.Labels["fdb-instance-id"])
		if err != nil {
			return false, err
		}

		class := instance.Metadata.Labels["fdb-process-class"]
		if instanceIDs[class] == nil {
			instanceIDs[class] = make(map[int]bool)
		}

		if instance.Pod != nil && instance.Pod.DeletionTimestamp != nil {
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
			r.List(context, getPodListOptions(cluster, processClass, ""), pvcs)
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
					matchingInstances, err := r.PodLifecycleManager.GetInstances(
						r, cluster, context,
						getPodListOptions(cluster, processClass, pvc.Labels["fdb-instance-id"]),
					)
					if err != nil {
						return false, err
					}
					podName := pvc.Name[0 : len(pvc.Name)-5]
					_, pendingRemoval := cluster.Spec.PendingRemovals[podName]
					if len(matchingInstances) == 0 && !pendingRemoval {
						reusablePvcs[index] = true
					}
				}
			}

			addedCount := 0
			for index := range reusablePvcs {
				if newCount <= 0 {
					break
				}
				instanceID := pvcs.Items[index].Labels["fdb-instance-id"]
				_, idNum, err := ParseInstanceID(instanceID)
				if err != nil {
					return false, err
				}

				pod, err := GetPod(context, cluster, processClass, idNum, r)
				if err != nil {
					return false, err
				}
				if pod.Labels["fdb-instance-id"] != instanceID {
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
					owner, err := buildOwnerReference(context, cluster, r)
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

func (a AddPods) RequeueAfter() time.Duration {
	return 0
}
