/*
 * check_instances_to_remove.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"reflect"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckInstancesToRemove updates the pending removal state based on the
// instances to remove.
type CheckInstancesToRemove struct{}

// Reconcile runs the reconciler's work.
func (c CheckInstancesToRemove) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	var removals = cluster.Status.PendingRemovals

	if removals == nil {
		removals = make(map[string]fdbtypes.PendingRemovalState)
	}

	for _, instanceID := range cluster.Spec.InstancesToRemove {
		_, present := removals[instanceID]
		if !present {
			removals[instanceID] = fdbtypes.PendingRemovalState{}
		}
	}

	for _, instanceID := range cluster.Spec.InstancesToRemoveWithoutExclusion {
		removalState := removals[instanceID]
		if !removalState.ExclusionComplete {
			removalState.ExclusionComplete = true
			log.Info("Skipping exclusion based on instancesToRemoveWithoutExclusion", "namespace", cluster.Namespace, "name", cluster.Name, "instance", instanceID)
			removals[instanceID] = removalState
		}
	}

	var finalRemovals = make(map[string]fdbtypes.PendingRemovalState, len(removals))

	for instanceID, oldRemovalState := range removals {
		if oldRemovalState.HadInstance && oldRemovalState.ExclusionComplete {
			finalRemovals[instanceID] = oldRemovalState
		} else {
			instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, client.InNamespace(cluster.Namespace), client.MatchingLabels(map[string]string{"fdb-instance-id": instanceID}))
			if err != nil {
				return false, err
			}
			if len(instances) > 0 {
				newRemovalState := r.getPendingRemovalState(instances[0])
				newRemovalState.ExclusionComplete = oldRemovalState.ExclusionComplete
				finalRemovals[instanceID] = newRemovalState
			} else if oldRemovalState.HadInstance {
				finalRemovals[instanceID] = oldRemovalState
			}
		}
	}

	if len(finalRemovals) == 0 {
		finalRemovals = nil
	}

	if !reflect.DeepEqual(cluster.Status.PendingRemovals, finalRemovals) {
		cluster.Status.PendingRemovals = finalRemovals
		err := r.updatePendingRemovals(context, cluster)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c CheckInstancesToRemove) RequeueAfter() time.Duration {
	return 0
}
