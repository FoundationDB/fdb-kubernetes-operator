/*
 * restarts.go
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

package restarts

import (
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
)

// GetFilterConditions returns the filter conditions to get the processes that should be restarted.
func GetFilterConditions(cluster *fdbv1beta2.FoundationDBCluster) map[fdbv1beta2.ProcessGroupConditionType]bool {
	if !cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		// If we don't upgrade our cluster we can ignore all process groups that are not reachable and therefore will
		// not get any ConfigMap updates.
		return map[fdbv1beta2.ProcessGroupConditionType]bool{
			fdbv1beta2.IncorrectCommandLine: true,
			fdbv1beta2.IncorrectPodSpec:     false,
			fdbv1beta2.SidecarUnreachable:   false,
			fdbv1beta2.IncorrectConfigMap:   false,
		}
	}

	// If we perform a version incompatible upgrade, we have to wait until all processes are ready to be restarted.
	// This means that all the sidecar images are updated to the new desired image that contains the new version.
	return map[fdbv1beta2.ProcessGroupConditionType]bool{
		fdbv1beta2.IncorrectCommandLine:  true,
		fdbv1beta2.IncorrectSidecarImage: false,
		fdbv1beta2.IncorrectConfigMap:    false,
	}
}

// ShouldBeIgnoredBecauseMissing checks if the provided process group should be ignored because the process group should
// be skipped or because the processes of this process group are not reporting.
func ShouldBeIgnoredBecauseMissing(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus) bool {
	// Ignore processes that are missing for more than 30 seconds e.g. if the process is network partitioned.
	// This is required since the update status will not update the SidecarUnreachable setting if a process is
	// missing in the status.
	if missingTime := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses); missingTime != nil {
		if time.Unix(*missingTime, 0).Add(cluster.GetIgnoreMissingProcessesSeconds()).Before(time.Now()) {
			logger.Info("ignore process group with missing process", "processGroupID", processGroup.ProcessGroupID)
			return true
		}
	}

	// If a Pod is stuck in pending we have to ignore it, as the processes hosted by this Pod will not be running.
	if cluster.SkipProcessGroup(processGroup) {
		logger.Info("ignore process group with Pod stuck in pending", "processGroupID", processGroup.ProcessGroupID)
		return true
	}

	return false
}
