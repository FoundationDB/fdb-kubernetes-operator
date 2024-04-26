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

// nodeTaintReplacementsAllowed returns true if replacements based on the NodeTaintReplacing are allowed.
// This is the case if the taint feature is enabled in the operator and not more than 10% of the clusters
// process group have that condition. The 10% is a rough estimate that might not work properly for smaller
// clusters. The idea is to prevent the operator from replacing any process groups if all or a huge number of
// process groups have the NodeTaintReplacing condition, e.g. because a component is tainting too many nodes.
// The operator will only replace automatically as many process groups as configured with the MaxConcurrentReplacements
// setting. This function only offers an additional safeguard to reduce the risk of too many replacements when too many
// nodes are tainted as that will probably signal an issue in the Kubernetes cluster.
func nodeTaintReplacementsAllowed(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) (bool, error) {
	if cluster.IsTaintFeatureDisabled() {
		return false, nil
	}

	faultDomains := map[fdbv1beta2.FaultDomain]fdbv1beta2.None{}
	faultDomainsWithTaint := map[fdbv1beta2.FaultDomain]fdbv1beta2.None{}
	for _, processGroup := range cluster.Status.ProcessGroups {
		// Ignore process groups with empty fault domains.
		if processGroup.FaultDomain == "" {
			continue
		}

		faultDomains[processGroup.FaultDomain] = fdbv1beta2.None{}
		// If the fault domain is already present in faultDomainWithTaint we can skip further work.
		_, ok := faultDomainsWithTaint[processGroup.FaultDomain]
		if ok {
			continue
		}

		if conditionTime := processGroup.GetConditionTime(fdbv1beta2.NodeTaintReplacing); conditionTime != nil {
			faultDomainsWithTaint[processGroup.FaultDomain] = fdbv1beta2.None{}
		}
	}

	maxAllowed, err := cluster.GetMaxFaultDomainsWithTaintedProcessGroups(len(faultDomains))
	if err != nil {
		return false, err
	}

	allowed := maxAllowed >= len(faultDomainsWithTaint)
	logger.V(1).Info("node taint replacements allowed information", "faultDomainWithTaint", len(faultDomainsWithTaint), "faultDomains", len(faultDomains), "maxAllowed", maxAllowed, "allowed", allowed)

	return allowed, nil
}

// ReplaceFailedProcessGroups flags failed processes groups for removal. The first return value will indicate if any
// new Process Group was removed and the second return value will indicate if there are more Process Groups that
// needs a replacement, but the operator is not allowed to replace those as the limit is reached.
func ReplaceFailedProcessGroups(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, hasDesiredFaultTolerance bool) (bool, bool) {
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
	failureDetectionTimeSeconds := cluster.GetFailureDetectionTimeSeconds()
	taintReplacementTimeSeconds := cluster.GetTaintReplacementTimeSeconds()
	// If the operator should not replace any process groups because of the NodeTaintReplacing condition, we simply set
	// the replacement time to max int.
	taintReplacementsAllowed, err := nodeTaintReplacementsAllowed(logger, cluster)
	if err != nil {
		logger.Error(err, "could not detect if replacements for taints is allowed, will default to false")
	}

	if !taintReplacementsAllowed {
		// We set the taintReplacementTimeSeconds to be ~1 year. That means a process group must have that condition for
		// over a year before it gets replaced.
		taintReplacementTimeSeconds = int((8760 * time.Hour).Seconds())
	}

	for _, processGroup := range cluster.Status.ProcessGroups {
		// If a process group is already marked for removal we can skip it here.
		if processGroup.IsMarkedForRemoval() {
			continue
		}

		if processGroup.IsUnderMaintenance(status.Cluster.MaintenanceZone) {
			logger.Info(
				"Skip process group that is in maintenance zone",
				"processGroupID", processGroup.ProcessGroupID,
				"maintenance zone", processGroup.FaultDomain)
			continue
		}

		failureCondition, failureTime := processGroup.NeedsReplacement(failureDetectionTimeSeconds, taintReplacementTimeSeconds)
		if failureTime == 0 {
			continue
		}

		// If the process is crash looping we can ignore it.
		if _, ok := ignore[processGroup.ProcessGroupID]; ok {
			continue
		}

		skipExclusion := false
		// Only if localities are not used for exclusions we should be skipping the exclusion.
		// Skipping the exclusion could lead to a race condition, which can be prevented if
		// we are able to exclude by locality.
		// see: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1890
		if len(processGroup.Addresses) == 0 && !localitiesUsedForExclusion {
			if !hasDesiredFaultTolerance {
				logger.Info(
					"Skip process group with missing address",
					"processGroupID", processGroup.ProcessGroupID,
					"failureTime", time.Unix(failureTime, 0).UTC().String())
				continue
			}

			// Since the process groups doesn't contain any addresses we have to skip exclusion.
			// The assumption here is that this is safe since we assume that the process group was never scheduled onto any node
			// otherwise the process group should have an address associated.
			skipExclusion = true
			logger.Info(
				"Replace process group with missing address",
				"processGroupID", processGroup.ProcessGroupID,
				"failureTime", time.Unix(failureTime, 0).UTC().String())
		}

		// We are not allowed to replace additional process groups.
		if !removalAllowed(cluster, maxReplacements, faultDomainsWithReplacements, processGroup.FaultDomain) {
			// If there are more processes that should be replaced but we hit the replace limit, we want to make sure
			// the controller queues another reconciliation to eventually replace this failed process group.
			hasMoreFailedProcesses = true
			logger.Info("Detected replace process group but cannot replace it because we hit the replacement limit",
				"processGroupID", processGroup.ProcessGroupID,
				"failureCondition", failureCondition,
				"faultDomain", processGroup.FaultDomain,
				"reason", fmt.Sprintf("automatic replacement detected failure time: %s", time.Unix(failureTime, 0).UTC().String()))
			continue
		}

		logger.Info("Replace process group",
			"processGroupID", processGroup.ProcessGroupID,
			"failureCondition", failureCondition,
			"faultDomain", processGroup.FaultDomain,
			"reason", fmt.Sprintf("automatic replacement detected failure time: %s", time.Unix(failureTime, 0).UTC().String()))

		processGroup.MarkForRemoval()
		hasReplacement = true
		processGroup.ExclusionSkipped = skipExclusion
		maxReplacements--
		faultDomainsWithReplacements[processGroup.FaultDomain] = fdbv1beta2.None{}
	}

	return hasReplacement, hasMoreFailedProcesses
}
