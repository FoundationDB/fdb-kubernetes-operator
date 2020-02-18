/*
 * choose_removals.go
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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ChooseRemovals chooses which processes will be removed during a shrink.
type ChooseRemovals struct{}

// Reconcile runs the reconciler's work.
func (c ChooseRemovals) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	hasNewRemovals := false

	var removals = cluster.Spec.PendingRemovals

	if removals == nil {
		removals = make(map[string]string)
	}

	instancesToRemove := make(map[string]bool)

	for _, instanceID := range cluster.Spec.InstancesToRemove {
		instancesToRemove[instanceID] = true
		instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, client.InNamespace(cluster.Namespace), client.MatchingLabels(map[string]string{"fdb-instance-id": instanceID}))
		if err != nil {
			return false, err
		}
		for _, instance := range instances {
			if removals[instance.Metadata.Name] == "" {
				if instance.Pod != nil {
					var ip string
					if r.PodIPProvider == nil {
						ip = instance.Pod.Status.PodIP
					} else {
						ip = r.PodIPProvider(instance.Pod)
					}
					removals[instance.Metadata.Name] = ip
					hasNewRemovals = true
				}
			}
		}
	}

	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return false, err
	}
	desiredCounts := desiredCountStruct.Map()

	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, processClass, "")...)
		if err != nil {
			return false, err
		}

		if desiredCount < 0 {
			desiredCount = 0
		}

		removedCount := currentCounts[processClass] - desiredCount
		if removedCount > 0 {
			err = sortInstancesByID(instances)
			if err != nil {
				return false, err
			}

			r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing %d %s processes", removedCount, processClass))

			removalsChosen := 0
			for indexOfPod := 0; indexOfPod < len(instances) && removalsChosen < removedCount; indexOfPod++ {
				instance := instances[len(instances)-1-indexOfPod]
				instanceID := instance.GetInstanceID()
				if !instancesToRemove[instanceID] {
					podClient, err := r.getPodClient(context, cluster, instance)
					if err != nil {
						return false, err
					}
					removals[instance.Metadata.Name] = podClient.GetPodIP()
					instancesToRemove[instanceID] = true
					cluster.Spec.InstancesToRemove = append(cluster.Spec.InstancesToRemove, instanceID)
					removalsChosen++
				}
			}
			hasNewRemovals = true
			cluster.Status.ProcessCounts.IncreaseCount(processClass, -1*removalsChosen)
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		for podName := range cluster.Spec.PendingRemovals {
			if removals[podName] == "" {
				instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, client.InNamespace(cluster.Namespace), client.MatchingField("metadata.name", podName))
				if err != nil {
					return false, err
				}
				if len(instances) == 0 {
					delete(removals, podName)
					hasNewRemovals = true
				} else {
					pod := instances[0].Pod
					if pod == nil {
						return false, MissingPodError(instances[0], cluster)
					}

					var ip string
					if r.PodIPProvider == nil {
						ip = pod.Status.PodIP
					} else {
						ip = r.PodIPProvider(pod)
					}

					if ip != "" {
						removals[podName] = ip
						hasNewRemovals = true
					}
				}
			}
		}
	}

	if hasNewRemovals {
		cluster.Spec.PendingRemovals = removals
		err := r.Update(context, cluster)
		if err != nil {
			return false, err
		}
	}
	return !hasNewRemovals, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ChooseRemovals) RequeueAfter() time.Duration {
	return 0
}
