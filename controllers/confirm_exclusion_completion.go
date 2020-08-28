/*
 * confirm_exclusion_completion.go
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
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ConfirmExclusionCompletion provides a reconciliation step for checking
// whether exclusions have completed.
type ConfirmExclusionCompletion struct{}

// Reconcile runs the reconciler's work.
func (c ConfirmExclusionCompletion) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Status.PendingRemovals))

	for instanceID, state := range cluster.Status.PendingRemovals {
		if !state.ExclusionComplete {
			if state.Address == "" {
				return false, fmt.Errorf("Cannot check the exclusion state of instance %s, which has no IP address", instanceID)
			}
			address := cluster.GetFullAddress(state.Address)
			addresses = append(addresses, address)
		}
	}

	remaining := addresses
	for len(remaining) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return false, err
		}
		if len(remaining) != len(addresses) {
			remainingMap := make(map[string]bool, len(remaining))
			for _, address := range remaining {
				remainingMap[address] = true
			}
			for id, state := range cluster.Status.PendingRemovals {
				if !remainingMap[state.Address] {
					newState := state
					newState.ExclusionComplete = true
					cluster.Status.PendingRemovals[id] = newState
				}
			}
			err = r.updatePendingRemovals(context, cluster)
			if err != nil {
				return false, err
			}
		}
		if len(remaining) > 0 {
			log.Info("Waiting for exclusions to complete", "namespace", cluster.Namespace, "cluster", cluster.Name, "remainingServers", remaining)
			time.Sleep(time.Second)
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ConfirmExclusionCompletion) RequeueAfter() time.Duration {
	return 0
}
