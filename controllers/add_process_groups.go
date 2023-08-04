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
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// addProcessGroups provides a reconciliation step for adding new pods to a cluster.
type addProcessGroups struct{}

// reconcile runs the reconciler's work.
func (a addProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &requeue{curError: err}
	}
	desiredCounts := desiredCountStruct.Map()
	processCounts, processGroupIDs, err := cluster.GetCurrentProcessGroupsAndProcessCounts()
	if err != nil {
		return &requeue{curError: err}
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

		logger.Info("Adding new Process Groups", "processClass", processClass, "newCount", newCount)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))
		idNum := 1

		for i := 0; i < newCount; i++ {
			var processGroupID fdbv1beta2.ProcessGroupID
			processGroupID, idNum = cluster.GetNextProcessGroupID(processClass, processGroupIDs[processClass], idNum)
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, processClass, nil))
			// Increase the idNum here, since we just added a Process Group with this ID number.
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
