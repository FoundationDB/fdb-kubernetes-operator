/*
 * exclude_instances.go
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
)

// ExcludeInstances provides a reconciliation step for excluding instances from
// the database.
type ExcludeInstances struct{}

// Reconcile runs the reconciler's work.
func (e ExcludeInstances) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return false, err
	}

	addresses := make([]string, 0, len(cluster.Status.PendingRemovals))
	hasExclusionUpdates := false
	for id, state := range cluster.Status.PendingRemovals {
		if state.Address != "" {
			address := cluster.GetFullAddress(state.Address)
			if !state.ExclusionStarted {
				addresses = append(addresses, address)
				newState := state
				newState.ExclusionStarted = true
				cluster.Status.PendingRemovals[id] = newState
				hasExclusionUpdates = true
			}
		} else if !state.ExclusionStarted || !state.ExclusionComplete {
			newState := state
			newState.ExclusionStarted = true
			newState.ExclusionComplete = true
			cluster.Status.PendingRemovals[id] = newState
			hasExclusionUpdates = true
		}
	}

	if len(addresses) > 0 {
		r.Recorder.Event(cluster, "Normal", "ExcludingProcesses", fmt.Sprintf("Excluding %v", addresses))
		err = adminClient.ExcludeInstances(addresses)

		if hasExclusionUpdates && !version.HasNonBlockingExcludes() {
			updateErr := r.updatePendingRemovals(context, cluster)
			if updateErr != nil {
				return false, updateErr
			}
			hasExclusionUpdates = false
		}
		if err != nil {
			return false, err
		}
	}

	if hasExclusionUpdates {
		err = r.updatePendingRemovals(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (e ExcludeInstances) RequeueAfter() time.Duration {
	return 0
}
