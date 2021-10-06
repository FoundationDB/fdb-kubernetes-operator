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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// replaceFailedPods identifies processes groups that have failed and need to be
// replaced.
type replaceFailedPods struct{}

// reconcile runs the reconciler's work.
func (c replaceFailedPods) reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *requeue {
	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	if chooseNewRemovals(cluster, adminClient) {
		err := r.Status().Update(context, cluster)
		if err != nil {
			return &requeue{curError: err}
		}

		return &requeue{message: "Removals have been updated in the cluster status"}
	}

	return nil
}

// chooseNewRemovals flags failed processes groups for removal and returns an indicator
// of whether any processes were thus flagged.
func chooseNewRemovals(cluster *fdbtypes.FoundationDBCluster, adminClient fdbadminclient.AdminClient) bool {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "replaceFailedPods")
	if !*cluster.Spec.AutomationOptions.Replacements.Enabled {
		return false
	}

	// The maximum number of removals will be the defined number in the cluster spec
	// minus all currently ongoing removals e.g. process groups marked fro removal but
	// not fully excluded.
	removalCount := 0
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.Remove && (!processGroupStatus.Excluded || processGroupStatus.ExclusionSkipped) {
			// If we already have a removal in-flight, we should not try
			// replacing more failed process groups.
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
				// Only replace process groups without an address if the cluster has the desired fault tolerance
				// and is available.
				hasDesiredFaultTolerance, err := internal.HasDesiredFaultTolerance(adminClient, cluster)
				if err != nil {
					log.Error(err, "Could not fetch if cluster has desired fault tolerance")
					continue
				}

				if !hasDesiredFaultTolerance {
					log.Info(
						"Replace instance with missing address",
						"processGroupID", processGroupStatus.ProcessGroupID,
						"failureTime", time.Unix(missingTime, 0).UTC().String())
					continue
				}

				log.Info(
					"Replace instance with missing address",
					"processGroupID", processGroupStatus.ProcessGroupID,
					"failureTime", time.Unix(missingTime, 0).UTC().String())
			}

			logger.Info("Replace instance",
				"processGroupID", processGroupStatus.ProcessGroupID,
				"reason", fmt.Sprintf("automatic replacement detected failure time: %s", time.Unix(missingTime, 0).UTC().String()))

			processGroupStatus.Remove = true
			hasReplacement = true
			maxReplacements--
		}
	}

	return hasReplacement
}
