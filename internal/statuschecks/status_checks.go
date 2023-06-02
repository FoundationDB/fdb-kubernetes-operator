/*
 * status_checks.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package statuschecks

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
)

// StatusContextKey will be used as a key in a context to pass down the cached status.
type StatusContextKey struct{}

// exclusionStatus represents the current status of processes that should be excluded.
// This can include processes that are currently in the progress of being excluded (data movement),
// processes that are fully excluded and don't serve any roles and processes that are not marked for
// exclusion.
type exclusionStatus struct {
	// inProgress contains all addresses that are excluded in the cluster and the exclude command can be used to verify if it's safe to remove this address.
	inProgress []fdbv1beta2.ProcessAddress
	// fullyExcluded contains all addresses that are excluded and don't have any roles assigned, this is a sign that the process is "fully" excluded and safe to remove.
	fullyExcluded []fdbv1beta2.ProcessAddress
	// notExcluded contains all addresses that are part of the input list and are not marked as excluded in the cluster, those addresses are not safe to remove.
	notExcluded []fdbv1beta2.ProcessAddress
	// missingInStatus contains all addresses that are part of the input list but are not appearing in the cluster status json.
	missingInStatus []fdbv1beta2.ProcessAddress
}

// getRemainingAndExcludedFromStatus checks which processes of the input address list are excluded in the cluster and which are not.
func getRemainingAndExcludedFromStatus(status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress) exclusionStatus {
	notExcludedAddresses := map[string]fdbv1beta2.None{}
	fullyExcludedAddresses := map[string]fdbv1beta2.None{}
	visitedAddresses := map[string]fdbv1beta2.None{}

	// If there are more than 1 active generations we can not handout any information about excluded processes based on
	// the cluster status information as only the latest log processes will have the log process role. If we don't check
	// for the active generations we have the risk to remove a log process that still has mutations on it that must be
	// popped.
	if status.Cluster.RecoveryState.ActiveGenerations > 1 {
		return exclusionStatus{
			inProgress:      nil,
			fullyExcluded:   nil,
			notExcluded:     addresses,
			missingInStatus: nil,
		}
	}

	for _, addr := range addresses {
		notExcludedAddresses[addr.MachineAddress()] = fdbv1beta2.None{}
	}

	// Check in the status output which processes are already marked for exclusion in the cluster
	for _, process := range status.Cluster.Processes {
		if _, ok := notExcludedAddresses[process.Address.MachineAddress()]; !ok {
			continue
		}

		visitedAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
		if !process.Excluded {
			continue
		}

		if len(process.Roles) == 0 {
			fullyExcludedAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
		}

		delete(notExcludedAddresses, process.Address.MachineAddress())
	}

	exclusions := exclusionStatus{
		inProgress:      make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(notExcludedAddresses)-len(fullyExcludedAddresses)),
		fullyExcluded:   make([]fdbv1beta2.ProcessAddress, 0, len(fullyExcludedAddresses)),
		notExcluded:     make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
		missingInStatus: make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
	}

	for _, addr := range addresses {
		// If we didn't visit that address (absent in the cluster status) we assume it's safe to run the exclude command against it.
		// We have to run the exclude command against those addresses, to make sure they are not serving any roles.
		if _, ok := visitedAddresses[addr.MachineAddress()]; !ok {
			exclusions.missingInStatus = append(exclusions.missingInStatus, addr)
			continue
		}

		// Those addresses are not excluded, so it's not safe to start the exclude command to check if they are fully excluded.
		if _, ok := notExcludedAddresses[addr.MachineAddress()]; ok {
			exclusions.notExcluded = append(exclusions.notExcluded, addr)
			continue
		}

		// Those are the processes that are marked as excluded and are not serving any roles. It's safe to delete Pods
		// that host those processes.
		if _, ok := fullyExcludedAddresses[addr.MachineAddress()]; ok {
			exclusions.fullyExcluded = append(exclusions.fullyExcluded, addr)
			continue
		}

		// Those are the processes that are marked as excluded but still serve at least one role.
		exclusions.inProgress = append(exclusions.inProgress, addr)
	}

	return exclusions
}

// CanSafelyRemoveFromStatus checks whether it is safe to remove processes from the cluster, based on the provided status.
//
// The list returned by this method will be the addresses that are *not* safe to remove.
func CanSafelyRemoveFromStatus(logger logr.Logger, client fdbadminclient.AdminClient, addresses []fdbv1beta2.ProcessAddress, status *fdbv1beta2.FoundationDBStatus) ([]fdbv1beta2.ProcessAddress, error) {
	exclusions := getRemainingAndExcludedFromStatus(status, addresses)
	logger.Info("Filtering excluded processes",
		"inProgress", exclusions.inProgress,
		"fullyExcluded", exclusions.fullyExcluded,
		"notExcluded", exclusions.notExcluded,
		"missingInStatus", exclusions.missingInStatus)

	notSafeToDelete := append(exclusions.notExcluded, exclusions.inProgress...)
	// When we have at least one process that is missing in the status, we have to issue the exclude command to make sure, that those
	// missing processes can be removed or not.
	if len(exclusions.missingInStatus) > 0 {
		err := client.ExcludeProcesses(exclusions.missingInStatus)
		// When we hit a timeout error here we know that at least one of the missingInStatus is still not fully excluded for safety
		// we just return the whole slice and don't do any further distinction. We have to return all addresses that are not excluded
		// and are still in progress, but we don't want to return an error to block further actions on the successfully excluded
		// addresses.
		if err != nil {
			if internal.IsTimeoutError(err) {
				return append(exclusions.notExcluded, exclusions.missingInStatus...), nil
			}

			return nil, err
		}
	}

	// All processes that are either not yet marked as excluded or still serving at least one role, cannot be removed safely.
	return notSafeToDelete, nil
}

// GetExclusions gets a list of the addresses currently excluded from the
// database, based on the provided status.
func GetExclusions(status *fdbv1beta2.FoundationDBStatus) ([]fdbv1beta2.ProcessAddress, error) {
	excludedServers := status.Cluster.DatabaseConfiguration.ExcludedServers
	exclusions := make([]fdbv1beta2.ProcessAddress, 0, len(excludedServers))
	for _, excludedServer := range excludedServers {
		if excludedServer.Address != "" {
			pAddr, err := fdbv1beta2.ParseProcessAddress(excludedServer.Address)
			if err != nil {
				return nil, err
			}
			exclusions = append(exclusions, pAddr)
		} else {
			exclusions = append(exclusions, fdbv1beta2.ProcessAddress{StringAddress: excludedServer.Locality})
		}
	}

	return exclusions, nil
}
