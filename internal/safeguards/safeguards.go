/*
 * safeguards.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package safeguards

import (
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/go-logr/logr"
	"math"
)

// The fraction of processes that must be present in order to start a new
// exclusion.
var missingProcessThreshold = 0.8

// CanExcludeNewProcesses validates if new processes can be excluded.
func CanExcludeNewProcesses(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass) (bool, []string) {
	// Block excludes on missing processes not marked for removal
	missingProcesses := make([]string, 0)
	validProcesses := make([]string, 0)

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() || processGroup.ProcessClass != processClass {
			continue
		}

		if processGroup.GetConditionTime(fdbv1beta2.MissingProcesses) != nil ||
			processGroup.GetConditionTime(fdbv1beta2.MissingPod) != nil {
			missingProcesses = append(missingProcesses, processGroup.ProcessGroupID)
			logger.Info("Missing processes", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		validProcesses = append(validProcesses, processGroup.ProcessGroupID)
	}

	desiredProcesses, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		logger.Error(err, "Error calculating process counts")
		return false, missingProcesses
	}
	desiredCount := desiredProcesses.Map()[processClass]

	if len(validProcesses) < desiredCount-1 && len(validProcesses) < int(math.Ceil(float64(desiredCount)*missingProcessThreshold)) {
		return false, missingProcesses
	}

	return true, nil
}

// HasEnoughProcessesToUpgrade checks if enough processes are ready to be upgraded. The logic is currently a simple heuristic
// which checks if at least 90% of the desired processes are pending the upgrade. Processes that are stuck in a terminating
// state will be ignored for the calculation.
func HasEnoughProcessesToUpgrade(processGroupIDs []string, processGroups []*fdbv1beta2.ProcessGroupStatus, desiredProcesses fdbv1beta2.ProcessCounts) error {
	// TODO (johscheuer): Expose this fraction and make it configurable.
	desiredProcessCounts := int(math.Ceil(float64(desiredProcesses.Total()) * 0.9))

	var terminatingProcesses int
	for _, processGroup := range processGroups {
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) == nil {
			continue
		}

		terminatingProcesses++
	}

	processesForUpgrade := len(processGroupIDs) - terminatingProcesses
	if processesForUpgrade < desiredProcessCounts {
		return fmt.Errorf("expected to have %d process groups for performing the upgrade, currently only %d process groups are available", desiredProcessCounts, processesForUpgrade)
	}

	return nil
}
