/*
 * choose_removals.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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
	"context"
	"fmt"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/locality"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// chooseRemovals chooses which processes will be removed during a shrink.
type chooseRemovals struct{}

// reconcile runs the reconciler's work.
func (c chooseRemovals) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "chooseRemovals")
	hasNewRemovals := false

	var removals = make(map[fdbv1beta2.ProcessGroupID]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			removals[processGroup.ProcessGroupID] = true
		}
	}

	currentCounts := fdbv1beta2.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true).Map()
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &requeue{curError: err}
	}
	desiredCounts := desiredCountStruct.Map()

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()
	status, err := adminClient.GetStatus()
	if err != nil {
		return &requeue{curError: err}
	}
	localityMap := make(map[string]locality.Info)
	for _, process := range status.Cluster.Processes {
		id := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		localityMap[id] = locality.Info{ID: id, Address: process.Address, LocalityData: process.Locality}
	}

	remainingProcessMap := make(map[string]bool, len(cluster.Status.ProcessGroups))

	for _, processClass := range fdbv1beta2.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		removedCount := currentCounts[processClass] - desiredCount
		processClassLocality := make([]locality.Info, 0, currentCounts[processClass])

		for _, processGroup := range cluster.Status.ProcessGroupsByProcessClass(processClass) {
			if processGroup.IsMarkedForRemoval() {
				removedCount--
			} else {
				locality, present := localityMap[string(processGroup.ProcessGroupID)]
				if present {
					processClassLocality = append(processClassLocality, locality)
				}
			}
		}

		if removedCount > 0 {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "ShrinkingProcesses", fmt.Sprintf("Removing %d %s processes", removedCount, processClass))

			remainingProcesses, err := locality.ChooseDistributedProcesses(cluster, processClassLocality, desiredCount, locality.ProcessSelectionConstraint{})
			if err != nil {
				return &requeue{curError: err}
			}

			logger.Info("Chose remaining processes after shrink",
				"desiredCount", desiredCount,
				"options", processClassLocality,
				"selected", remainingProcesses)

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
			if !remainingProcessMap[string(processGroup.ProcessGroupID)] {
				processGroup.MarkForRemoval()
			}
		}
		err := r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
