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

package fdbstatus

import (
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	"math"
)

// forbiddenStatusMessages represents messages that could be part of the machine-readable status. Those messages can represent
// different error cases that have occurred when fetching the machine-readable status. A list of possible messages can be
// found here: https://github.com/apple/foundationdb/blob/main/documentation/sphinx/source/mr-status.rst?plain=1#L68-L97
// and here: https://apple.github.io/foundationdb/mr-status.html#message-components.
// We don't want to block the exclusion check for all messages, as some messages also indicate client issues or issues
// with a specific transaction priority.
var forbiddenStatusMessages = map[string]fdbv1beta2.None{
	"unreadable_configuration": {},
	"full_replication_timeout": {},
	"storage_servers_error":    {},
	"log_servers_error":        {},
}

// StatusContextKey will be used as a key in a context to pass down the cached status.
type StatusContextKey struct{}

// exclusionStatus represents the current status of processes that should be excluded.
// This can include processes that are currently in the progress of being excluded (data movement),
// processes that are fully excluded and don't serve any roles and processes that are not marked for
// exclusion.
type exclusionStatus struct {
	// inProgress containms all addresses that are excluded in the cluster and the exclude command can be used to verify if it's safe to remove this address.
	inProgress []fdbv1beta2.ProcessAddress
	// fullyExcluded contains all addresses that are excluded and don't have any roles assigned, this is a sign that the process is "fully" excluded and safe to remove.
	fullyExcluded []fdbv1beta2.ProcessAddress
	// notExcluded contains all addresses that are part of the input list and are not marked as excluded in the cluster, those addresses are not safe to remove.
	notExcluded []fdbv1beta2.ProcessAddress
	// missingInStatus contains all addresses that are part of the input list but are not appearing in the cluster status json.
	missingInStatus []fdbv1beta2.ProcessAddress
}

// getRemainingAndExcludedFromStatus checks which processes of the input address list are excluded in the cluster and which are not.
func getRemainingAndExcludedFromStatus(logger logr.Logger, status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress) exclusionStatus {
	notExcludedAddresses := map[string]fdbv1beta2.None{}
	fullyExcludedAddresses := map[string]int{}
	visitedAddresses := map[string]int{}

	// If there are more than 1 active generations we can not handout any information about excluded processes based on
	// the cluster status information as only the latest log processes will have the log process role. If we don't check
	// for the active generations we have the risk to remove a log process that still has mutations on it that must be
	// popped.
	if status.Cluster.RecoveryState.ActiveGenerations > 1 {
		logger.Info("Skipping exclusion check as there are multiple active generations", "activeGenerations", status.Cluster.RecoveryState.ActiveGenerations)
		return exclusionStatus{
			inProgress:      nil,
			fullyExcluded:   nil,
			notExcluded:     addresses,
			missingInStatus: nil,
		}
	}

	// If the database is unavailable the status might contain the processes but with missing role information. In this
	// case we should not perform any validation on the machine-readable status as this information might not be correct.
	// We don't want to run the exclude command to check for the status of the exclusions as this might result in a recovery
	// of the cluster or brings the cluster into a worse state.
	if !status.Client.DatabaseStatus.Available {
		logger.Info("Skipping exclusion check as the database is unavailable")
		return exclusionStatus{
			inProgress:      nil,
			fullyExcluded:   nil,
			notExcluded:     addresses,
			missingInStatus: nil,
		}
	}

	// ...
	if !clusterStatusHasValidRoleInformation(logger, status) {
		return exclusionStatus{
			inProgress:      nil,
			fullyExcluded:   nil,
			notExcluded:     addresses,
			missingInStatus: nil,
		}
	}

	addressesToVerify := map[string]fdbv1beta2.None{}
	for _, addr := range addresses {
		addressesToVerify[addr.MachineAddress()] = fdbv1beta2.None{}
	}

	// Check in the status output which processes are already marked for exclusion in the cluster
	for _, process := range status.Cluster.Processes {
		if _, ok := addressesToVerify[process.Address.MachineAddress()]; !ok {
			continue
		}

		visitedAddresses[process.Address.MachineAddress()]++
		if !process.Excluded {
			notExcludedAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
			continue
		}

		if len(process.Roles) == 0 {
			fullyExcludedAddresses[process.Address.MachineAddress()]++
		}
	}

	exclusions := exclusionStatus{
		inProgress:      make([]fdbv1beta2.ProcessAddress, 0, len(addresses)),
		fullyExcluded:   make([]fdbv1beta2.ProcessAddress, 0, len(fullyExcludedAddresses)),
		notExcluded:     make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
		missingInStatus: make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(visitedAddresses)),
	}

	for _, addr := range addresses {
		machine := addr.MachineAddress()
		// If we didn't visit that address (absent in the cluster status) we assume it's safe to run the exclude command against it.
		// We have to run the exclude command against those addresses, to make sure they are not serving any roles.
		visitedCount, visited := visitedAddresses[machine]
		if !visited {
			exclusions.missingInStatus = append(exclusions.missingInStatus, addr)
			continue
		}

		// Those addresses are not excluded, so it's not safe to start the exclude command to check if they are fully excluded.
		if _, ok := notExcludedAddresses[machine]; ok {
			exclusions.notExcluded = append(exclusions.notExcluded, addr)
			continue
		}

		// Those are the processes that are marked as excluded and are not serving any roles. It's safe to delete Pods
		// that host those processes.
		excludedCount, ok := fullyExcludedAddresses[addr.MachineAddress()]
		if ok {
			// We have to make sure that we have visited as many processes as we have seen fully excluded. Otherwise we might
			// return a wrong signal if more than one process is used per Pod. In this case we have to wait for all processes
			// to be fully excluded.
			if visitedCount == excludedCount {
				exclusions.fullyExcluded = append(exclusions.fullyExcluded, addr)
				continue
			}
			logger.Info("found excluded addresses for machine, but not all processes are fully excluded", "visitedCount", visitedCount, "excludedCount", excludedCount, "machine", machine)
		}

		// Those are the processes that are marked as excluded but still serve at least one role.
		exclusions.inProgress = append(exclusions.inProgress, addr)
	}

	return exclusions
}

// clusterStatusHasValidRoleInformation will check if the cluster part of the machine-readable status contains messages
// that indicate that not all role information could be fetched.
func clusterStatusHasValidRoleInformation(logger logr.Logger, status *fdbv1beta2.FoundationDBStatus) bool {
	for _, message := range status.Cluster.Messages {
		if _, ok := forbiddenStatusMessages[message.Name]; ok {
			logger.Info("Skipping exclusion check as the machine-readable status includes a message that indicates an potential incomplete status",
				"messages", status.Cluster.Messages,
				"forbiddenStatusMessages", forbiddenStatusMessages)
			return false
		}
	}

	return true
}

// CanSafelyRemoveFromStatus checks whether it is safe to remove processes from the cluster, based on the provided status.
//
// The list returned by this method will be the addresses that are *not* safe to remove.
func CanSafelyRemoveFromStatus(logger logr.Logger, client fdbadminclient.AdminClient, addresses []fdbv1beta2.ProcessAddress, status *fdbv1beta2.FoundationDBStatus) ([]fdbv1beta2.ProcessAddress, error) {
	exclusions := getRemainingAndExcludedFromStatus(logger, status, addresses)
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

// GetCoordinatorsFromStatus gets the current coordinators from the status.
// The returning set will contain all processes by their process group ID.
func GetCoordinatorsFromStatus(status *fdbv1beta2.FoundationDBStatus) map[string]fdbv1beta2.None {
	coordinators := make(map[string]fdbv1beta2.None)

	for _, pInfo := range status.Cluster.Processes {
		for _, roleInfo := range pInfo.Roles {
			if roleInfo.Role != string(fdbv1beta2.ProcessRoleCoordinator) {
				continue
			}

			// We don't have to check for duplicates here, if the process group ID is already
			// set we just overwrite it.
			coordinators[pInfo.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]] = fdbv1beta2.None{}
			break
		}
	}

	return coordinators
}

// GetMinimumUptimeAndAddressMap returns address map of the processes included the the foundationdb status. The minimum
// uptime will be either secondsSinceLastRecovered if the recovery state is supported and enabled otherwise we will
// take the minimum uptime of all processes.
func GetMinimumUptimeAndAddressMap(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, recoveryStateEnabled bool) (float64, map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress, error) {
	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		return 0, nil, err
	}

	useRecoveryState := runningVersion.SupportsRecoveryState() && recoveryStateEnabled

	addressMap := make(map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress, len(status.Cluster.Processes))

	minimumUptime := math.Inf(1)
	if useRecoveryState {
		minimumUptime = status.Cluster.RecoveryState.SecondsSinceLastRecovered
	}

	for _, process := range status.Cluster.Processes {
		// We have seen cases where a process is still reported, only with the role and the class but missing the localities.
		// in this case we want to ignore this process as it seems like the process is miss behaving.
		processGroupID, ok := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if !ok {
			logger.Info("Ignoring process with missing localities", "address", process.Address)
			continue
		}

		addressMap[fdbv1beta2.ProcessGroupID(processGroupID)] = append(addressMap[fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])], process.Address)

		if useRecoveryState || process.Excluded {
			continue
		}

		// Ignore cases where the uptime seconds is exactly 0.0, this would mean that the process was exactly restarted at the time the FoundationDB cluster status
		// was queried. In most cases this only reflects an issue with the process or the status.
		if process.UptimeSeconds == 0.0 {
			continue
		}

		if process.UptimeSeconds < minimumUptime {
			minimumUptime = process.UptimeSeconds
		}
	}

	return minimumUptime, addressMap, nil
}

// DoStorageServerFaultDomainCheckOnStatus does a storage server related fault domain check over the given status object.
func DoStorageServerFaultDomainCheckOnStatus(status *fdbv1beta2.FoundationDBStatus) error {
	if len(status.Cluster.Data.TeamTrackers) == 0 {
		return fmt.Errorf("no team trackers specified in status")
	}

	for _, tracker := range status.Cluster.Data.TeamTrackers {
		if !tracker.State.Healthy {
			region := "primary"
			if !tracker.Primary {
				region = "remote"
			}

			return fmt.Errorf("team tracker in %s is in unhealthy state", region)
		}
	}

	return nil
}

// DoLogServerFaultDomainCheckOnStatus does a log server related fault domain check over the given status object.
func DoLogServerFaultDomainCheckOnStatus(status *fdbv1beta2.FoundationDBStatus) error {
	if len(status.Cluster.Logs) == 0 {
		return fmt.Errorf("no log information specified in status")
	}

	for _, log := range status.Cluster.Logs {
		// @todo do we need to do this check only for the current log server set? Revisit this issue later.
		if log.LogReplicationFactor != 0 {
			if log.LogFaultTolerance+1 != log.LogReplicationFactor {
				return fmt.Errorf("primary log fault tolerance is not satisfied, replication factor: %d, current fault tolerance: %d", log.LogReplicationFactor, log.LogFaultTolerance)
			}
		}

		if log.RemoteLogReplicationFactor != 0 {
			if log.RemoteLogFaultTolerance+1 != log.RemoteLogReplicationFactor {
				return fmt.Errorf("remote log fault tolerance is not satisfied, replication factor: %d, current fault tolerance: %d", log.RemoteLogReplicationFactor, log.RemoteLogFaultTolerance)
			}
		}

		if log.SatelliteLogReplicationFactor != 0 {
			if log.SatelliteLogFaultTolerance+1 != log.SatelliteLogReplicationFactor {
				return fmt.Errorf("satellite log fault tolerance is not satisfied, replication factor: %d, current fault tolerance: %d", log.SatelliteLogReplicationFactor, log.SatelliteLogFaultTolerance)
			}
		}
	}

	return nil
}

// DoCoordinatorFaultDomainCheckOnStatus does a coordinator related fault domain check over the given status object.
// @note an empty function for now. We will revisit this later.
func DoCoordinatorFaultDomainCheckOnStatus(_ *fdbv1beta2.FoundationDBStatus) error {
	// TODO: decide if we need to do coordinator related check.
	return nil
}

// DoFaultDomainChecksOnStatus does the specified fault domain check(s) over the given status object.
// @note this is a wrapper over the above fault domain related functions.
func DoFaultDomainChecksOnStatus(status *fdbv1beta2.FoundationDBStatus, storageServerCheck bool, logServerCheck bool, coordinatorCheck bool) error {
	if storageServerCheck {
		err := DoStorageServerFaultDomainCheckOnStatus(status)
		if err != nil {
			return err
		}
	}

	if logServerCheck {
		err := DoLogServerFaultDomainCheckOnStatus(status)
		if err != nil {
			return err
		}
	}

	if coordinatorCheck {
		return DoCoordinatorFaultDomainCheckOnStatus(status)
	}

	return nil
}

func hasDesiredFaultTolerance(expectedFaultTolerance int, maxZoneFailuresWithoutLosingData int, maxZoneFailuresWithoutLosingAvailability int) bool {
	// Only if both max zone failures for availability and data loss are greater or equal to the expected fault tolerance we know that we meet
	// our fault tolerance requirements.
	return maxZoneFailuresWithoutLosingData >= expectedFaultTolerance && maxZoneFailuresWithoutLosingAvailability >= expectedFaultTolerance
}

// HasDesiredFaultToleranceFromStatus checks if the cluster has the desired fault tolerance based on the provided status.
func HasDesiredFaultToleranceFromStatus(log logr.Logger, status *fdbv1beta2.FoundationDBStatus, cluster *fdbv1beta2.FoundationDBCluster) bool {
	if !status.Client.DatabaseStatus.Available {
		log.Info("Cluster is not available",
			"namespace", cluster.Namespace,
			"cluster", cluster.Name)

		return false
	}

	expectedFaultTolerance := cluster.DesiredFaultTolerance()
	log.Info("Check desired fault tolerance",
		"expectedFaultTolerance", expectedFaultTolerance,
		"maxZoneFailuresWithoutLosingData", status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData,
		"maxZoneFailuresWithoutLosingAvailability", status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability)

	return hasDesiredFaultTolerance(
		expectedFaultTolerance,
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData,
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability)
}
