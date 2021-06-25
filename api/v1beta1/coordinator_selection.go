/*
 * coordinator_selection.go
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

package v1beta1

import "math"

// CoordinatorSelectionSetting defines the process class and the priority of it.
// A higher priority means that the process class is preferred over another.
type CoordinatorSelectionSetting struct {
	ProcessClass ProcessClass `json:"processClass,omitempty"`
	Priority     int          `json:"priority,omitempty"`
}

// IsEligibleAsCandidate checks if the given process has the right process class to be considered a valid coordinator
func (cluster *FoundationDBCluster) IsEligibleAsCandidate(pClass ProcessClass) bool {
	for _, setting := range cluster.Spec.CoordinatorSelection {
		if pClass == setting.ProcessClass {
			return true
		}
	}

	return pClass.IsStateful()
}

// GetClassPriority returns the priority for a class. This will be used to sort the processes for coordinator selection
func (cluster *FoundationDBCluster) GetClassPriority(pClass ProcessClass) int {
	for _, setting := range cluster.Spec.CoordinatorSelection {
		if pClass == setting.ProcessClass {
			return setting.Priority
		}
	}

	return math.MinInt64
}
