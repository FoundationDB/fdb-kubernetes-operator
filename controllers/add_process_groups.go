/*
 * add_process_groups.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// addProcessGroups provides a reconciliation step for adding new pods to a cluster.
type addProcessGroups struct{}

// reconcile runs the reconciler's work.
func (a addProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &requeue{curError: err}
	}
	desiredCounts := desiredCountStruct.Map()

	processCounts := make(map[fdbv1beta2.ProcessClass]int)
	processGroupIDs := make(map[fdbv1beta2.ProcessClass]map[int]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroupID := processGroup.ProcessGroupID
		_, num, err := podmanager.ParseProcessGroupID(processGroupID)
		if err != nil {
			return &requeue{curError: err}
		}

		class := processGroup.ProcessClass
		if processGroupIDs[class] == nil {
			processGroupIDs[class] = make(map[int]bool)
		}

		processGroupIDs[class][num] = true

		if !processGroup.IsMarkedForRemoval() {
			processCounts[class]++
		}
	}

	hasNewProcessGroups := false
	for _, processClass := range fdbv1beta2.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		if desiredCount < 0 {
			desiredCount = 0
		}
		newCount := desiredCount - processCounts[processClass]
		if newCount <= 0 {
			continue
		}
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))
		idNum := 1

		if processGroupIDs[processClass] == nil {
			processGroupIDs[processClass] = make(map[int]bool)
		}

		for i := 0; i < newCount; i++ {
			for idNum > 0 {
				_, processGroupID := internal.GetProcessGroupID(cluster, processClass, idNum)

				if !cluster.ProcessGroupIsBeingRemoved(processGroupID) && !processGroupIDs[processClass][idNum] {
					break
				}

				idNum++
			}
			_, processGroupID := internal.GetProcessGroupID(cluster, processClass, idNum)
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, processClass, nil))

			idNum++
		}
		hasNewProcessGroups = true
	}

	if hasNewProcessGroups {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
