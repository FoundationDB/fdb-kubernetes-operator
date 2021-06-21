/*
 * foundationdb_status.go
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

package internal

import fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

// GetCoordinatorsFromStatus gets the current coordinators from the status.
// The returning set will contain all processes by their process group ID.
func GetCoordinatorsFromStatus(status *fdbtypes.FoundationDBStatus) map[string]None {
	coordinators := make(map[string]None)

	for _, pInfo := range status.Cluster.Processes {
		for _, roleInfo := range pInfo.Roles {
			if roleInfo.Role != string(fdbtypes.ProcessRoleCoordinator) {
				continue
			}

			// We don't have to check for duplicates here, if the process group ID is already
			// set we just overwrite it.
			coordinators[pInfo.Locality[fdbtypes.FDBLocalityInstanceIDKey]] = None{}
			break
		}
	}

	return coordinators
}
