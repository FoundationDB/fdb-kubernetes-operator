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
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// addProcessGroups provides a reconciliation step for adding new pods to a cluster.
type addProcessGroups struct{}

// reconcile runs the reconciler's work.
func (a addProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &requeue{curError: err}
	}

	desiredCounts := desiredCountStruct.Map()
	processCounts, processGroupIDs, err := cluster.GetCurrentProcessGroupsAndProcessCounts()
	if err != nil {
		return &requeue{curError: err}
	}

	// Fetch the excluded localities from the provided machine-readable status. If the status is not available, e.g. because
	// the cluster is unavailable, return an empty map and continue with adding new process groups if required.
	exclusions, getLocalitiesErr := fdbstatus.GetExcludedLocalitiesFromStatus(logger, cluster, status, r.getAdminClient)
	if getLocalitiesErr != nil {
		logger.Error(err, "Error getting exclusion list")
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

		_, ok := processGroupIDs[processClass]
		if !ok {
			processGroupIDs[processClass] = map[int]bool{}
		}

		hasNewProcessGroups = true
		logger.Info("Adding new Process Groups", "processClass", processClass, "newCount", newCount, "desiredCount", desiredCount, "currentCount", processCounts[processClass])
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))
		for i := 0; i < newCount; i++ {
			processGroupID := cluster.GetNextRandomProcessGroupIDWithExclusions(processClass, processGroupIDs[processClass], exclusions)
			logger.Info("Adding new Process Group to cluster", "processClass", processClass, "processGroupID", processGroupID, "exclusions", exclusions)
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, processClass, nil))
		}
	}

	if hasNewProcessGroups {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	if getLocalitiesErr != nil {
		return &requeue{curError: getLocalitiesErr, delayedRequeue: true}
	}

	return nil
}
