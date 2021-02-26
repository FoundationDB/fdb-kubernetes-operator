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
)

// ChooseRemovals chooses which processes will be removed during a shrink.
type ChooseRemovals struct{}

// Reconcile runs the reconciler's work.
func (c ChooseRemovals) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	hasNewRemovals := false

	var removals = make(map[string]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			removals[processGroup.ProcessGroupID] = true
		}
	}

	currentCounts := fdbtypes.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true).Map()
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return false, err
	}
	desiredCounts := desiredCountStruct.Map()

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}
	localityMap := make(map[string]localityInfo)
	for _, process := range status.Cluster.Processes {
		id := process.Locality[FDBLocalityInstanceIDKey]
		localityMap[id] = localityInfo{ID: id, Address: process.Address, LocalityData: process.Locality}
	}

	remainingProcessMap := make(map[string]bool, len(cluster.Status.ProcessGroups))

	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]

		removedCount := currentCounts[processClass] - desiredCount

		processClassLocality := make([]localityInfo, 0, currentCounts[processClass])

		for _, processGroup := range cluster.Status.ProcessGroupsByProcessClass(processClass) {
			if processGroup.Remove {
				removedCount--
			} else {
				locality, present := localityMap[processGroup.ProcessGroupID]
				if present {
					processClassLocality = append(processClassLocality, locality)
				}
			}
		}

		if removedCount > 0 {
			r.Recorder.Event(cluster, "Normal", "ShrinkingProcesses", fmt.Sprintf("Removing %d %s processes", removedCount, processClass))

			remainingProcesses, err := chooseDistributedProcesses(processClassLocality, desiredCount, processSelectionConstraint{})
			if err != nil {
				return false, err
			}

			log.Info("Chose remaining processes after shrink", "desiredCount", desiredCount, "options", processClassLocality, "selected", remainingProcesses)

			for _, localityInfo := range remainingProcesses {
				remainingProcessMap[localityInfo.ID] = true
			}

			hasNewRemovals = true
		} else {
			for _, localityInfo := range processClassLocality {
				remainingProcessMap[localityInfo.ID] = true
			}
		}
	}

	if hasNewRemovals {
		for _, processGroup := range cluster.Status.ProcessGroups {
			if !remainingProcessMap[processGroup.ProcessGroupID] {
				processGroup.Remove = true
			}
		}
		err := r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ChooseRemovals) RequeueAfter() time.Duration {
	return 0
}
