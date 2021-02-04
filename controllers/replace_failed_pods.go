/*
 * replace_failed_pods.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors.
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
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ReplaceFailedPods identifies processes that have failed and need to be
// replaced.
type ReplaceFailedPods struct{}

// Reconcile runs the reconciler's work.
func (c ReplaceFailedPods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if chooseNewRemovals(cluster) {
		err := r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}

		return false, nil
	}

	return true, nil
}

// chooseNewRemovals flags failed processes for removal and returns an indicator
// of whether any processes were thus flagged.
func chooseNewRemovals(cluster *fdbtypes.FoundationDBCluster) bool {
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.Remove && !processGroupStatus.Excluded {
			// If we already have a removal in-flight, we should not try
			// replacing more failed pods.
			return false
		}
	}

	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		missingTime := processGroupStatus.GetConditionTime(fdbtypes.MissingProcesses)
		failureWindowStart := time.Now().Add(-1 * time.Duration(*cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds) * time.Second).Unix()
		needsReplacement := missingTime != nil && *missingTime < failureWindowStart && !processGroupStatus.Remove
		if needsReplacement && *cluster.Spec.AutomationOptions.Replacements.Enabled {
			log.Info("Replacing failed process group", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", processGroupStatus.ProcessGroupID, "failureTime", *missingTime)
			processGroupStatus.Remove = true
			return true
		}
	}

	return false
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ReplaceFailedPods) RequeueAfter() time.Duration {
	return 0
}
