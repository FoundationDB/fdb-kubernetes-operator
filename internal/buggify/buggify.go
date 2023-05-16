/*
 * buggify.go
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

package buggify

import fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

// FilterBlockedRemovals will remove all matching process groups from the processGroupsToRemove slice that are defined in
// the BlockRemoval.
func FilterBlockedRemovals(cluster *fdbv1beta2.FoundationDBCluster, processGroupsToRemove []*fdbv1beta2.ProcessGroupStatus) []*fdbv1beta2.ProcessGroupStatus {
	if len(cluster.Spec.Buggify.BlockRemoval) == 0 {
		return processGroupsToRemove
	}

	// processGroupsToRemove
	blockedProcessGroups := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(cluster.Spec.Buggify.BlockRemoval))
	for _, blockedProcessGroup := range cluster.Spec.Buggify.BlockRemoval {
		blockedProcessGroups[blockedProcessGroup] = fdbv1beta2.None{}
	}

	filteredList := make([]*fdbv1beta2.ProcessGroupStatus, 0, len(processGroupsToRemove))
	for _, processGroup := range processGroupsToRemove {
		if _, ok := blockedProcessGroups[processGroup.ProcessGroupID]; ok {
			continue
		}

		filteredList = append(filteredList, processGroup)
	}

	return filteredList
}

// FilterIgnoredProcessGroups removes all addresses from the addresses slice that are associated with a process group that should be ignored
// during a restart.
func FilterIgnoredProcessGroups(cluster *fdbv1beta2.FoundationDBCluster, addresses []fdbv1beta2.ProcessAddress, status *fdbv1beta2.FoundationDBStatus) ([]fdbv1beta2.ProcessAddress, bool) {
	if len(cluster.Spec.Buggify.IgnoreDuringRestart) == 0 {
		return addresses, false
	}

	ignoredIDs := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(cluster.Spec.Buggify.IgnoreDuringRestart))
	ignoredAddresses := make(map[string]fdbv1beta2.None, len(cluster.Spec.Buggify.IgnoreDuringRestart))

	for _, id := range cluster.Spec.Buggify.IgnoreDuringRestart {
		ignoredIDs[id] = fdbv1beta2.None{}
	}

	for _, process := range status.Cluster.Processes {
		processGroupId, ok := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if !ok {
			continue
		}

		if _, ok := ignoredIDs[fdbv1beta2.ProcessGroupID(processGroupId)]; !ok {
			continue
		}

		ignoredAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
	}

	filteredAddresses := make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(ignoredAddresses))
	removedAddresses := false
	for _, address := range addresses {
		if _, ok := ignoredAddresses[address.MachineAddress()]; ok {
			removedAddresses = true
			continue
		}

		filteredAddresses = append(filteredAddresses, address)
	}

	return filteredAddresses, removedAddresses
}
