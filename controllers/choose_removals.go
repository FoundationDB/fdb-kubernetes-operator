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

package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ChooseRemovals chooses which processes will be removed during a shrink.
type ChooseRemovals struct{}

func (c ChooseRemovals) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	hasNewRemovals := false

	var removals = cluster.Spec.PendingRemovals
	if removals == nil {
		removals = make(map[string]string)
	}

	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()
	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, processClass, ""))
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
			for indexOfPod := 0; indexOfPod < removedCount; indexOfPod++ {
				instance := instances[len(instances)-1-indexOfPod]
				podClient, err := r.getPodClient(context, cluster, instance)
				if err != nil {
					return false, err
				}
				removals[instance.Metadata.Name] = podClient.GetPodIP()
			}
			hasNewRemovals = true
			cluster.Status.ProcessCounts.IncreaseCount(processClass, -1*removedCount)
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		for podName := range cluster.Spec.PendingRemovals {
			if removals[podName] == "" {
				instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, client.InNamespace(cluster.Namespace).MatchingField("metadata.name", podName))
				if err != nil {
					return false, err
				}
				if len(instances) == 0 {
					delete(removals, podName)
				} else {
					podClient, err := r.getPodClient(context, cluster, instances[0])
					if err != nil {
						return false, err
					}
					removals[podName] = podClient.GetPodIP()
				}
				hasNewRemovals = true
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

func (c ChooseRemovals) RequeueAfter() time.Duration {
	return 0
}
