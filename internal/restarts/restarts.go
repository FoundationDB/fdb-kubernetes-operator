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

import fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"

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

	// If we perform an upgrade we have to wait until all processes are
	// ready to be restarted. Therefore we can't ignore process groups with the SidecarUnreachable condition.
	// We could be less restrictive here in cases where we perform a version compatible upgrade.m
	return map[fdbv1beta2.ProcessGroupConditionType]bool{
		fdbv1beta2.IncorrectCommandLine: true,
		fdbv1beta2.IncorrectPodSpec:     false,
		fdbv1beta2.IncorrectConfigMap:   false,
	}
}
