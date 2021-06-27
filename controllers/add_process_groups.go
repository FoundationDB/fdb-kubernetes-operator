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
	ctx "context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// AddProcessGroups provides a reconciliation step for adding new pods to a cluster.
type AddProcessGroups struct{}

// Reconcile runs the reconciler's work.
func (a AddProcessGroups) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &Requeue{Error: err}
	}
	desiredCounts := desiredCountStruct.Map()

	processCounts := make(map[fdbtypes.ProcessClass]int)
	processGroupIDs := make(map[fdbtypes.ProcessClass]map[int]bool)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroupID := processGroup.ProcessGroupID
		_, num, err := ParseInstanceID(processGroupID)
		if err != nil {
			return &Requeue{Error: err}
		}

		class := processGroup.ProcessClass
		if processGroupIDs[class] == nil {
			processGroupIDs[class] = make(map[int]bool)
		}

		processGroupIDs[class][num] = true

		if !processGroup.Remove {
			processCounts[class]++
		}
	}

	hasNewProcessGroups := false
	for _, processClass := range fdbtypes.ProcessClasses {
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
				_, processGroupID := getInstanceID(cluster, processClass, idNum)

				if !cluster.InstanceIsBeingRemoved(processGroupID) && !processGroupIDs[processClass][idNum] {
					break
				}

				idNum++
			}
			_, processGroupID := getInstanceID(cluster, processClass, idNum)
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbtypes.NewProcessGroupStatus(processGroupID, processClass, nil))

			idNum++
		}
		hasNewProcessGroups = true
	}

	if hasNewProcessGroups {
		err = r.Status().Update(context, cluster)
		if err != nil {
			return &Requeue{Error: err}
		}
	}

	return nil
}
