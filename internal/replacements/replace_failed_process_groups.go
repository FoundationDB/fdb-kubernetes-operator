/*
 * replace_failed_process_groups.go
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

package replacements

import (
	"fmt"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/go-logr/logr"
)

// getReplacementInformation will return the maximum allowed replacements for process group based replacements and the
// fault domains that have an ongoing replacement.
func getReplacementInformation(cluster *fdbv1beta2.FoundationDBCluster, maxReplacements int) (int, map[fdbv1beta2.FaultDomain]fdbv1beta2.None) {
	faultDomains := map[fdbv1beta2.FaultDomain]fdbv1beta2.None{}
	// The maximum number of replacements will be the defined number in the cluster spec
	// minus all currently ongoing replacements e.g. process groups marked for removal but
	// not fully excluded.
	removalCount := 0
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.IsMarkedForRemoval() && !processGroupStatus.IsExcluded() {
			// Count all removals that are in-flight.
			removalCount++
			faultDomains[processGroupStatus.FaultDomain] = fdbv1beta2.None{}
		}
	}

	return maxReplacements - removalCount, faultDomains
}

// removalAllowed will return true if the removal is allowed based on the clusters automatic replacement configuration.
func removalAllowed(cluster *fdbv1beta2.FoundationDBCluster, maxReplacements int, faultDomainsWithReplacements map[fdbv1beta2.FaultDomain]fdbv1beta2.None, faultDomain fdbv1beta2.FaultDomain) bool {
	if !cluster.FaultDomainBasedReplacements() {
		// If we are here we target the replacements on a process group level
		return maxReplacements > 0
	}

	// We have to check how many fault domains currently have a replacement ongoing. If more than MaxConcurrentReplacements
	// fault domains have a replacement ongoing, we will reject any further replacement.
	maxFaultDomainsWithAReplacement := cluster.GetMaxConcurrentAutomaticReplacements()
	if len(faultDomainsWithReplacements) > maxFaultDomainsWithAReplacement {
		return false
	}

	// If the current fault domains with a replacements equals to MaxConcurrentReplacements we are only allowed
	// to approve the replacement of process groups that are in a fault domain that currently has an ongoing
	// replacement.
	if len(faultDomainsWithReplacements) == maxFaultDomainsWithAReplacement {
		_, faultDomainHasReplacements := faultDomainsWithReplacements[faultDomain]
		return faultDomainHasReplacements
	}

	// At this point we have less than MaxConcurrentReplacements fault domains with a replacement, so it's fine to
	// approve the replacement.
	return true
}

// ReplaceFailedProcessGroups flags failed processes groups for removal. The first return value will indicate if any
// new Process Group was removed and the second return value will indicate if there are more Process Groups that
// needs a replacement, but the operator is not allowed to replace those as the limit is reached.
func ReplaceFailedProcessGroups(log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, hasDesiredFaultTolerance bool) (bool, bool) {
	// Automatic replacements are disabled or set to 0, so we don't have to check anything further
	if !cluster.GetEnableAutomaticReplacements() || cluster.GetMaxConcurrentAutomaticReplacements() == 0 {
		return false, false
	}

	ignore := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
	crashLoopContainerProcessGroups := cluster.GetCrashLoopContainerProcessGroups()
	for _, targets := range crashLoopContainerProcessGroups {
		// If all Process Groups are targeted to be crash looping, we can skip any further work.
		if _, ok := targets["*"]; ok {
			return false, false
		}

		ignore = targets
	}

	maxReplacements, faultDomainsWithReplacements := getReplacementInformation(cluster, cluster.GetMaxConcurrentAutomaticReplacements())
	hasReplacement := false
	hasMoreFailedProcesses := false
	localitiesUsedForExclusion := cluster.UseLocalitiesForExclusion()

	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		// If a process group is already marked for removal we can skip it here.
		if processGroupStatus.IsMarkedForRemoval() {
			continue
		}

		if processGroupStatus.IsUnderMaintenance(status.Cluster.MaintenanceZone) {
			log.Info(
				"Skip process group that is in maintenance zone",
				"processGroupID", processGroupStatus.ProcessGroupID,
				"maintenance zone", processGroupStatus.FaultDomain)
			continue
		}

		failureCondition, failureTime := processGroupStatus.NeedsReplacement(cluster.GetFailureDetectionTimeSeconds(), cluster.GetTaintReplacementTimeSeconds())
		if failureTime == 0 {
			continue
		}

		// If the process is crash looping we can ignore it.
		if _, ok := ignore[processGroupStatus.ProcessGroupID]; ok {
			continue
		}

		skipExclusion := false
		// Only if localities are not used for exclusions we should be skipping the exclusion.
		// Skipping the exclusion could lead to a race condition, which can be prevented if
		// we are able to exclude by locality.
		// see: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1890
		if len(processGroupStatus.Addresses) == 0 && !localitiesUsedForExclusion {
			if !hasDesiredFaultTolerance {
				log.Info(
					"Skip process group with missing address",
					"processGroupID", processGroupStatus.ProcessGroupID,
					"failureTime", time.Unix(failureTime, 0).UTC().String())
				continue
			}

			// Since the process groups doesn't contain any addresses we have to skip exclusion.
			// The assumption here is that this is safe since we assume that the process group was never scheduled onto any node
			// otherwise the process group should have an address associated.
			skipExclusion = true
			log.Info(
				"Replace process group with missing address",
				"processGroupID", processGroupStatus.ProcessGroupID,
				"failureTime", time.Unix(failureTime, 0).UTC().String())
		}

		// We are not allowed to replace additional process groups.
		if !removalAllowed(cluster, maxReplacements, faultDomainsWithReplacements, processGroupStatus.FaultDomain) {
			// If there are more processes that should be replaced but we hit the replace limit, we want to make sure
			// the controller queues another reconciliation to eventually replace this failed process group.
			hasMoreFailedProcesses = true
			log.Info("Detected replace process group but cannot replace it because we hit the replacement limit",
				"processGroupID", processGroupStatus.ProcessGroupID,
				"failureCondition", failureCondition,
				"faultDomain", processGroupStatus.FaultDomain,
				"reason", fmt.Sprintf("automatic replacement detected failure time: %s", time.Unix(failureTime, 0).UTC().String()))
			continue
		}

		log.Info("Replace process group",
			"processGroupID", processGroupStatus.ProcessGroupID,
			"failureCondition", failureCondition,
			"faultDomain", processGroupStatus.FaultDomain,
			"reason", fmt.Sprintf("automatic replacement detected failure time: %s", time.Unix(failureTime, 0).UTC().String()))

		processGroupStatus.MarkForRemoval()
		hasReplacement = true
		processGroupStatus.ExclusionSkipped = skipExclusion
		maxReplacements--
		faultDomainsWithReplacements[processGroupStatus.FaultDomain] = fdbv1beta2.None{}
	}

	return hasReplacement, hasMoreFailedProcesses
}
