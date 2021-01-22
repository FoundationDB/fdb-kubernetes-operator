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

	addresses := make([]string, 0, len(cluster.Status.ProcessGroups))

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove && !processGroup.ExclusionSkipped {
			if len(processGroup.Addresses) == 0 {
				return false, fmt.Errorf("Cannot check the exclusion state of instance %s, which has no IP address", processGroup.ProcessGroupID)
			}

			addresses = append(addresses, processGroup.Addresses...)
		}
	}

	if len(addresses) == 0 {
		return true, nil
	}

	remaining, err := adminClient.CanSafelyRemove(addresses)
	if err != nil {
		return false, err
	}

	if len(remaining) > 0 {
		log.Info("Waiting for exclusions to complete", "namespace", cluster.Namespace, "cluster", cluster.Name, "remainingServers", remaining)
		return false, nil
	}

	hasStatusUpdates := false

	remainingMap := make(map[string]bool, len(remaining))
	for _, address := range addresses {
		remainingMap[address] = false
	}
	for _, address := range remaining {
		remainingMap[address] = true
	}
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove && !processGroup.ExclusionSkipped {
			excluded := true
			for _, address := range processGroup.Addresses {
				isRemaining, isPresent := remainingMap[address]
				if !isPresent || isRemaining {
					log.Info("Process missing in exclusion results", "namespace", cluster.Namespace, "name", cluster.Name, "instance", processGroup.ProcessGroupID, "address", address)
					excluded = false
				}
			}
			if excluded {
				log.Info("Marking exclusion complete", "namespace", cluster.Namespace, "name", cluster.Name, "instance", processGroup.ProcessGroupID, "addresses", processGroup.Addresses)
				processGroup.Excluded = true
				hasStatusUpdates = true
			}
		}
	}

	if hasStatusUpdates {
		err = r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ConfirmExclusionCompletion) RequeueAfter() time.Duration {
	return 0
}
