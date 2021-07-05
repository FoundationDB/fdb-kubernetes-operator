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
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ReplaceFailedPods identifies processes that have failed and need to be
// replaced.
type ReplaceFailedPods struct{}

// Reconcile runs the reconciler's work.
func (c ReplaceFailedPods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	if chooseNewRemovals(cluster) {
		err := r.Status().Update(context, cluster)
		if err != nil {
			return &Requeue{Error: err}
		}

		return &Requeue{Message: "Removals have been updated in the cluster status"}
	}

	return nil
}

// chooseNewRemovals flags failed processes for removal and returns an indicator
// of whether any processes were thus flagged.
func chooseNewRemovals(cluster *fdbtypes.FoundationDBCluster) bool {
	if !*cluster.Spec.AutomationOptions.Replacements.Enabled {
		return false
	}

	// The maximum number of removals will be the defined number in the cluster spec
	// minus all currently ongoing removals e.g. process groups marked fro removal but
	// not fully excluded.
	removalCount := 0
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.Remove && !processGroupStatus.Excluded {
			// If we already have a removal in-flight, we should not try
			// replacing more failed pods.
			removalCount++
		}
	}
	maxReplacements := cluster.GetMaxConcurrentReplacements() - removalCount

	hasReplacement := false
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if maxReplacements <= 0 {
			return hasReplacement
		}

		needsReplacement, missingTime := processGroupStatus.NeedsReplacement(*cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds)
		if needsReplacement && *cluster.Spec.AutomationOptions.Replacements.Enabled {
			if len(processGroupStatus.Addresses) == 0 {
				log.Info(
					"Ignore failed process group with missing address",
					"namespace", cluster.Namespace,
					"cluster", cluster.Name,
					"processGroupID", processGroupStatus.ProcessGroupID,
					"failureTime", time.Unix(missingTime, 0).UTC().String())
				continue
			}

			log.Info("Replace instance",
				"namespace", cluster.Namespace,
				"name", cluster.Name,
				"processGroupID", processGroupStatus.ProcessGroupID,
				"reason", fmt.Sprintf("automatic replacement detected failure time: %s", time.Unix(missingTime, 0).UTC().String()))

			processGroupStatus.Remove = true
			hasReplacement = true
			maxReplacements--
		}
	}

	return hasReplacement
}
