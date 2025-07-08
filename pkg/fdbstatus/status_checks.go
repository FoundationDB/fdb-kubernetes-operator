/*
 * status_checks.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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
	"math"
	"strconv"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/go-logr/logr"
)

const (
	maximumDataLag = 60.0
	// maximumQueueByteSize is per default 250 MB
	maximumQueueByteSize = 250 * 1024 * 1024
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

// forbiddenConfigurationChangeStatusMessages represents messages that could be part of the machine-readable status. Those messages can represent
// different error cases that have occurred when fetching the machine-readable status. A list of possible messages can be
// found here: https://github.com/apple/foundationdb/blob/main/documentation/sphinx/source/mr-status.rst?plain=1#L68-L97
// and here: https://apple.github.io/foundationdb/mr-status.html#message-components.
// If any of those messages are present in the status, the operator will not allow configuration changes.
var forbiddenConfigurationChangeStatusMessages = map[string]fdbv1beta2.None{
	"unreadable_configuration": {},
}

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
func getRemainingAndExcludedFromStatus(
	logger logr.Logger,
	status *fdbv1beta2.FoundationDBStatus,
	addresses []fdbv1beta2.ProcessAddress,
) exclusionStatus {
	logger.V(1).Info("Verify if exclusions are done", "addresses", addresses)
	notExcludedAddresses := map[string]fdbv1beta2.None{}
	fullyExcludedAddresses := map[string]int{}
	visitedAddresses := map[string]int{}

	// If there are more than 1 active generations we can not handout any information about excluded processes based on
	// the cluster status information as only the latest log processes will have the log process role. If we don't check
	// for the active generations we have the risk to remove a log process that still has mutations on it that must be
	// popped.
	err := DefaultSafetyChecks(status, 1, "check exclusion status")
	if err != nil {
		logger.Info(
			"Skipping exclusion check as there are issues with the machine-readable status",
			"error",
			err.Error(),
		)
		return exclusionStatus{
			inProgress:      nil,
			fullyExcluded:   nil,
			notExcluded:     addresses,
			missingInStatus: nil,
		}
	}

	// We have to make sure that the provided machine-readable status contains the required information, if any of the
	// forbiddenStatusMessages is present, the operator is not able to make a decision if a set of processes is fully excluded
	// or not.
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
		processAddresses := []string{
			fmt.Sprintf(
				"%s:%s",
				fdbv1beta2.FDBLocalityExclusionPrefix,
				process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
			),
			process.Address.IPAddress.String(),
		}

		// We have to verify the IP address and the locality of this process, if neither should be verified we skip any
		// further checks.
		for _, address := range processAddresses {
			if _, ok := addressesToVerify[address]; !ok {
				continue
			}

			visitedAddresses[address]++
			if !process.Excluded {
				notExcludedAddresses[address] = fdbv1beta2.None{}
				continue
			}

			if len(process.Roles) == 0 {
				logger.Info("found fully excluded process without any roles", "process", process)
				fullyExcludedAddresses[address]++
			}
		}
	}

	exclusions := exclusionStatus{
		inProgress:      make([]fdbv1beta2.ProcessAddress, 0, len(addresses)),
		fullyExcluded:   make([]fdbv1beta2.ProcessAddress, 0, len(fullyExcludedAddresses)),
		notExcluded:     make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
		missingInStatus: make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(visitedAddresses)),
	}

	for _, addr := range addresses {
		address := addr.MachineAddress()
		// If we didn't visit that address (absent in the cluster status) we assume it's safe to run the exclude command against it.
		// We have to run the exclude command against those addresses, to make sure they are not serving any roles.
		visitedCount, visited := visitedAddresses[address]
		if !visited {
			exclusions.missingInStatus = append(exclusions.missingInStatus, addr)
			continue
		}

		// Those addresses are not excluded, so it's not safe to start the exclude command to check if they are fully excluded.
		if _, ok := notExcludedAddresses[address]; ok {
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
			logger.Info(
				"found excluded addresses for machine, but not all processes are fully excluded",
				"visitedCount",
				visitedCount,
				"excludedCount",
				excludedCount,
				"address",
				address,
			)
		}

		// Those are the processes that are marked as excluded but still serve at least one role.
		exclusions.inProgress = append(exclusions.inProgress, addr)
	}

	return exclusions
}

// clusterStatusHasValidRoleInformation will check if the cluster part of the machine-readable status contains messages
// that indicate that not all role information could be fetched.
func clusterStatusHasValidRoleInformation(
	logger logr.Logger,
	status *fdbv1beta2.FoundationDBStatus,
) bool {
	for _, message := range status.Cluster.Messages {
		if _, ok := forbiddenStatusMessages[message.Name]; ok {
			logger.Info(
				"Skipping exclusion check as the machine-readable status includes a message that indicates an potential incomplete status",
				"messages",
				status.Cluster.Messages,
				"forbiddenStatusMessages",
				forbiddenStatusMessages,
			)
			return false
		}
	}

	return true
}

// CanSafelyRemoveFromStatus checks whether it is safe to remove processes from the cluster, based on the provided status.
//
// The list returned by this method will be the addresses that are *not* safe to remove.
func CanSafelyRemoveFromStatus(
	logger logr.Logger,
	client fdbadminclient.AdminClient,
	addresses []fdbv1beta2.ProcessAddress,
	status *fdbv1beta2.FoundationDBStatus,
) ([]fdbv1beta2.ProcessAddress, error) {
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
			notSafeToDelete = append(notSafeToDelete, exclusions.missingInStatus...)
		}
	}

	// Verify that all processes that are assumed to be fully excluded based on the machine-readable status are actually
	// not serving any roles by running the exclude command again. If those processes are actually fully excluded and are not
	// serving any roles, the exclude command should terminate quickly, otherwise we will hit a timeout, and we know that
	// not all processes are fully excluded. This is meant to be an additional safeguard if the machine-readable status
	// returns the wrong signals.
	if len(exclusions.fullyExcluded) > 0 {
		// When we hit a timeout error here we know that at least one of the fullyExcluded is still not fully excluded.
		return notSafeToDelete, client.ExcludeProcesses(exclusions.fullyExcluded)
	}

	// All processes that are either not yet marked as excluded or still serving at least one role, cannot be removed safely.
	return notSafeToDelete, nil
}

// GetExclusions gets a list of the addresses currently excluded from the database, based on the provided status.
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
func GetMinimumUptimeAndAddressMap(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	recoveryStateEnabled bool,
) (float64, map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress, error) {
	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		return 0, nil, err
	}

	addressMap := make(
		map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress,
		len(status.Cluster.Processes),
	)

	minimumUptime := math.Inf(1)
	if runningVersion.SupportsRecoveryState() && recoveryStateEnabled {
		minimumUptime = status.Cluster.RecoveryState.SecondsSinceLastRecovered
	}

	for _, process := range status.Cluster.Processes {
		// Ignore tester processes for this check
		if process.ProcessClass == fdbv1beta2.ProcessClassTest {
			continue
		}
		// We have seen cases where a process is still reported, only with the role and the class but missing the localities.
		// in this case we want to ignore this process as it seems like the process is miss behaving.
		processGroupID, ok := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if !ok {
			logger.Info("Ignoring process with missing localities", "address", process.Address)
			continue
		}

		addressMap[fdbv1beta2.ProcessGroupID(processGroupID)] = append(
			addressMap[fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])],
			process.Address,
		)

		if process.Excluded {
			continue
		}

		// Ignore cases where the uptime seconds is exactly 0.0, this would mean that the process was exactly restarted at the time the FoundationDB cluster status
		// was queried. In most cases this only reflects an issue with the process or the status.
		if process.UptimeSeconds == 0.0 {
			continue
		}

		// If the SecondsSinceLastRecovered is higher than the process uptime, we want to use the process uptime, e.g. in
		// cases where only storage processes are restarted.
		if process.UptimeSeconds < minimumUptime {
			logger.V(1).
				Info("Process uptime is less than the last recovery", "processGroupID", process.Address, "minimumUptime", minimumUptime, "process.UptimeSeconds", process.UptimeSeconds)
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

	minimumRequiredReplicas := fdbv1beta2.MinimumFaultDomains(
		status.Cluster.DatabaseConfiguration.RedundancyMode,
	)
	for _, tracker := range status.Cluster.Data.TeamTrackers {
		region := "primary"
		if !tracker.Primary {
			region = "remote"
		}

		if !tracker.State.Healthy {
			return fmt.Errorf("team tracker in %s is in unhealthy state", region)
		}

		if tracker.State.MinReplicasRemaining < minimumRequiredReplicas {
			return fmt.Errorf(
				"team tracker in %s has %d replicas left but we require more than %d",
				region,
				tracker.State.MinReplicasRemaining,
				minimumRequiredReplicas,
			)
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
				return fmt.Errorf(
					"primary log fault tolerance is not satisfied, replication factor: %d, current fault tolerance: %d",
					log.LogReplicationFactor,
					log.LogFaultTolerance,
				)
			}
		}

		if log.RemoteLogReplicationFactor != 0 {
			if log.RemoteLogFaultTolerance+1 != log.RemoteLogReplicationFactor {
				return fmt.Errorf(
					"remote log fault tolerance is not satisfied, replication factor: %d, current fault tolerance: %d",
					log.RemoteLogReplicationFactor,
					log.RemoteLogFaultTolerance,
				)
			}
		}

		if log.SatelliteLogReplicationFactor != 0 {
			if log.SatelliteLogFaultTolerance+1 != log.SatelliteLogReplicationFactor {
				return fmt.Errorf(
					"satellite log fault tolerance is not satisfied, replication factor: %d, current fault tolerance: %d",
					log.SatelliteLogReplicationFactor,
					log.SatelliteLogFaultTolerance,
				)
			}
		}
	}

	return nil
}

// DoCoordinatorFaultDomainCheckOnStatus does a coordinator related fault domain check over the given status object.
func DoCoordinatorFaultDomainCheckOnStatus(status *fdbv1beta2.FoundationDBStatus) error {
	notReachable := make([]string, 0, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		if coordinator.Reachable {
			continue
		}

		notReachable = append(notReachable, coordinator.Address.String())
	}

	if len(notReachable) > 0 {
		return fmt.Errorf(
			"not all coordinators are reachable, unreachable coordinators: %s",
			strings.Join(notReachable, ","),
		)
	}

	// If this is the case the statements above should already catch the unreachable coordinators and printout a more
	// detailed message.
	if !status.Client.Coordinators.QuorumReachable {
		return fmt.Errorf("quorum of coordinators is not reachable")
	}

	return nil
}

// DoFaultDomainChecksOnStatus does the specified fault domain check(s) over the given status object.
// @note this is a wrapper over the above fault domain related functions.
func DoFaultDomainChecksOnStatus(
	status *fdbv1beta2.FoundationDBStatus,
	storageServerCheck bool,
	logServerCheck bool,
	coordinatorCheck bool,
) error {
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

// HasDesiredFaultToleranceFromStatus checks if the cluster has the desired fault tolerance based on the provided status.
func HasDesiredFaultToleranceFromStatus(
	log logr.Logger,
	status *fdbv1beta2.FoundationDBStatus,
	cluster *fdbv1beta2.FoundationDBCluster,
) bool {
	if !status.Client.DatabaseStatus.Available {
		log.Info("Cluster is not available",
			"namespace", cluster.Namespace,
			"cluster", cluster.Name)

		return false
	}

	// Should we also add a method to check the different process classes? Currently the degraded log fault tolerance
	// will block the removal of a storage process.
	err := DoStorageServerFaultDomainCheckOnStatus(status)
	if err != nil {
		log.Info("Fault domain check for storage subsystem failed", "error", err)
		return false
	}

	err = DoLogServerFaultDomainCheckOnStatus(status)
	if err != nil {
		log.Info("Fault domain check for log servers failed", "error", err)
		return false
	}

	err = DoCoordinatorFaultDomainCheckOnStatus(status)
	if err != nil {
		log.Info("Fault domain check for coordinator servers failed", "error", err)
		return false
	}

	return true
}

// DefaultSafetyChecks performs a set of default safety checks, e.g. it checks if the cluster is available from the
// client perspective and it checks that there are not too many active generations.
func DefaultSafetyChecks(
	status *fdbv1beta2.FoundationDBStatus,
	maximumActiveGenerations int,
	action string,
) error {
	// If there are more than 10 active generations we should not allow the cluster to bounce processes as this could
	// cause another recovery increasing the active generations. In general the active generations should be at 1 during
	// normal operations.
	if status.Cluster.RecoveryState.ActiveGenerations > maximumActiveGenerations {
		return fmt.Errorf(
			"cluster has %d active generations, but only %d active generations are allowed to safely %s",
			status.Cluster.RecoveryState.ActiveGenerations,
			maximumActiveGenerations,
			action,
		)
	}

	// If the database is unavailable we shouldn't perform any action on the cluster.
	if !status.Client.DatabaseStatus.Available {
		return fmt.Errorf("cluster is unavailable, cannot %s", action)
	}

	return nil
}

// CanSafelyBounceProcesses returns nil when it is safe to do a bounce on the cluster or returns an error with more information
// why it's not safe to bounce processes in the cluster.
func CanSafelyBounceProcesses(
	currentUptime float64,
	minimumUptime float64,
	status *fdbv1beta2.FoundationDBStatus,
) error {
	err := DefaultSafetyChecks(status, 10, "bounce processes")
	if err != nil {
		return err
	}

	// If the current uptime of the cluster is below the minimum uptime we should not allow to bounce processes. This is
	// a safeguard to reduce the risk of repeated bounces in a short timeframe.
	if currentUptime < minimumUptime {
		return fmt.Errorf(
			"cluster has only been up for %.2f seconds, but must be up for %.2f seconds to safely bounce",
			currentUptime,
			minimumUptime,
		)
	}

	// If the machine-readable status reports that a clean bounce is not possible, we shouldn't perform a bounce. This
	// value will be false if the cluster is not fully recovered:
	// https://github.com/apple/foundationdb/blob/7.3.29/fdbserver/Status.actor.cpp#L437-L448
	if status.Cluster.BounceImpact.CanCleanBounce != nil &&
		!*status.Cluster.BounceImpact.CanCleanBounce {
		return fmt.Errorf(
			"cannot perform a clean bounce based on cluster status, current recovery state: %s",
			status.Cluster.RecoveryState.Name,
		)
	}

	return nil
}

// CanSafelyExcludeProcesses currently performs the DefaultSafetyChecks. In the future this check might be extended to
// perform more specific checks.
// Deprecated: Make use of CanSafelyExcludeOrIncludeProcesses
func CanSafelyExcludeProcesses(status *fdbv1beta2.FoundationDBStatus) error {
	return DefaultSafetyChecks(status, 10, "exclude processes")
}

func canSafelyExcludeOrIncludeProcesses(
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	inclusion bool,
	minRecoverySeconds float64,
) error {
	action := "exclude processes"
	if inclusion {
		action = "include processes"
	}

	err := DefaultSafetyChecks(status, 10, action)
	if err != nil {
		return err
	}

	version, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		return err
	}

	if version.SupportsRecoveryState() {
		// We want to make sure that the cluster is recovered for some time. This should protect the cluster from
		// getting into a bad state as a result of frequent inclusions/exclusions.
		if status.Cluster.RecoveryState.SecondsSinceLastRecovered < minRecoverySeconds {
			return fmt.Errorf(
				"cannot: %s, clusters last recovery was %0.2f seconds ago, wait until the last recovery was %0.0f seconds ago",
				action,
				status.Cluster.RecoveryState.SecondsSinceLastRecovered,
				minRecoverySeconds,
			)
		}
	}

	// In the case of inclusions we also want to make sure we only change the list of excluded server if the cluster is
	// in a good shape, otherwise the CC might crash: https://github.com/apple/foundationdb/blob/release-7.1/fdbserver/ClusterRecovery.actor.cpp#L575-L579
	if inclusion && !recoveryStateAllowsInclusion(status) {
		return fmt.Errorf(
			"cannot: %s, cluster recovery state is %s, but it must be \"fully_recovered\" or \"all_logs_recruited\"",
			action,
			status.Cluster.RecoveryState.Name,
		)
	}

	return nil
}

// See: https://github.com/apple/foundationdb/blob/release-7.1/fdbserver/ClusterRecovery.actor.cpp#L575-L579
func recoveryStateAllowsInclusion(status *fdbv1beta2.FoundationDBStatus) bool {
	return status.Cluster.RecoveryState.Name == "fully_recovered" ||
		status.Cluster.RecoveryState.Name == "all_logs_recruited"
}

// CanSafelyExcludeProcessesWithRecoveryState currently performs the DefaultSafetyChecks and makes sure that the last recovery was at least `minRecoverySeconds` seconds ago.
// In the future this check might be extended to perform more specific checks.
func CanSafelyExcludeProcessesWithRecoveryState(
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	minRecoverySeconds float64,
) error {
	return canSafelyExcludeOrIncludeProcesses(cluster, status, false, minRecoverySeconds)
}

// CanSafelyIncludeProcesses currently performs the DefaultSafetyChecks and makes sure that the last recovery was at least `minRecoverySeconds` seconds ago.
// In the future this check might be extended to perform more specific checks.
func CanSafelyIncludeProcesses(
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	minRecoverySeconds float64,
) error {
	return canSafelyExcludeOrIncludeProcesses(cluster, status, true, minRecoverySeconds)
}

// ConfigurationChangeAllowed will return an error if the configuration change is assumed to be unsafe. If no error
// is returned the configuration change can be applied.
func ConfigurationChangeAllowed(
	status *fdbv1beta2.FoundationDBStatus,
	useRecoveryState bool,
) error {
	err := DefaultSafetyChecks(status, 10, "change configuration")
	if err != nil {
		return err
	}

	// Check the health of the data distribution before allowing configuration changes.
	if !status.Cluster.Data.State.Healthy {
		return fmt.Errorf("data distribution is not healthy: %s", status.Cluster.Data.State.Name)
	}

	// Check if the cluster status messages contain any messages that provide a signal to assume it's not safe
	// to change the configuration.
	for _, message := range status.Cluster.Messages {
		if _, ok := forbiddenConfigurationChangeStatusMessages[message.Name]; ok {
			return fmt.Errorf("status contains error message: %s", message.Name)
		}
	}

	// We want to wait at least 60 seconds between configuration changes that trigger a recovery, otherwise we might
	// issue too frequent configuration changes.
	if useRecoveryState && status.Cluster.RecoveryState.SecondsSinceLastRecovered < 60.0 {
		return fmt.Errorf(
			"clusters last recovery was %0.2f seconds ago, wait until the last recovery was 60 seconds ago",
			status.Cluster.RecoveryState.SecondsSinceLastRecovered,
		)
	}

	return CheckQosStatus(status)
}

// CheckQosStatus will perform checks on the QoS related metrics from the machine-readable status to validate if certain
// changes are safe.
// Check https://apple.github.io/foundationdb/administration.html#machine-readable-status for some additional background
// information.
func CheckQosStatus(status *fdbv1beta2.FoundationDBStatus) error {
	// We picked this value from the FoundationDB implementation, this is the default threshold to report a process as lagging.
	if status.Cluster.Qos.WorstDataLagStorageServer.Seconds > maximumDataLag {
		return fmt.Errorf(
			"worst data lag is too high, current worst data lag in seconds: %0.2f, maximum allowed lag:  %0.2f",
			status.Cluster.Qos.WorstDataLagStorageServer.Seconds,
			maximumDataLag,
		)
	}

	if status.Cluster.Qos.WorstDurabilityLagStorageServer.Seconds > maximumDataLag {
		return fmt.Errorf(
			"worst durability lag is too high, current worst durability lag in seconds: %0.2f, maximum allowed lag:  %0.2f",
			status.Cluster.Qos.WorstDurabilityLagStorageServer.Seconds,
			maximumDataLag,
		)
	}

	if status.Cluster.Qos.WorstQueueBytesLogServer > maximumQueueByteSize {
		return fmt.Errorf(
			"worst queue bytes for log server is too high, current worst queue bytes: %s, maximum allowed queue bytes: %s",
			PrettyPrintBytes(status.Cluster.Qos.WorstQueueBytesLogServer),
			PrettyPrintBytes(maximumQueueByteSize),
		)
	}

	if status.Cluster.Qos.WorstQueueBytesStorageServer > maximumQueueByteSize {
		return fmt.Errorf(
			"worst queue bytes for storage server is too high, current worst queue bytes: %s, maximum allowed queue bytes: %s",
			PrettyPrintBytes(status.Cluster.Qos.WorstQueueBytesStorageServer),
			PrettyPrintBytes(maximumQueueByteSize),
		)
	}

	return nil
}

// GetExcludedLocalitiesFromStatus returns the excluded localities based on the machine-readable status. If the provided status is empty a new status is fetched.
func GetExcludedLocalitiesFromStatus(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	getAdminClient func(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.AdminClient, error),
) (map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, error) {
	exclusions := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
	if cluster.UseLocalitiesForExclusion() {
		if status == nil {
			adminClient, err := getAdminClient(logger, cluster)
			if err != nil {
				return exclusions, err
			}

			status, err = adminClient.GetStatus()
			if err != nil {
				return exclusions, err
			}
		}

		prefix := fdbv1beta2.FDBLocalityExclusionPrefix + ":"
		for _, excludedServer := range status.Cluster.DatabaseConfiguration.ExcludedServers {
			if excludedServer.Locality == "" {
				continue
			}

			processGroupID, found := strings.CutPrefix(excludedServer.Locality, prefix)
			if !found {
				continue
			}
			exclusions[fdbv1beta2.ProcessGroupID(processGroupID)] = fdbv1beta2.None{}
		}
	}

	return exclusions, nil
}

// CanSafelyRemoveMaintenanceMode will return true if the maintenance mode can be disabled.
func CanSafelyRemoveMaintenanceMode(status *fdbv1beta2.FoundationDBStatus) error {
	err := DefaultSafetyChecks(status, 10, "remove maintenance mode")
	if err != nil {
		return err
	}

	return CheckQosStatus(status)
}

// PrettyPrintBytes will return a string that represents the storedBytes in a human-readable format.
func PrettyPrintBytes(bytes int64) string {
	units := []string{"", "Ki", "Mi", "Gi", "Ti", "Pi"}

	currentBytes := float64(bytes)
	for _, unit := range units {
		if currentBytes < 1024 {
			return fmt.Sprintf("%3.2f%s", currentBytes, unit)
		}

		currentBytes /= 1024
	}

	// Fallback will be to printout the bytes.
	return strconv.FormatInt(bytes, 10)
}

// ClusterIsConfigured will return true if the cluster is configured based on the machine-readable status of the status of
// the FoundationDBCLuster resource.
func ClusterIsConfigured(
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
) bool {
	// If we saw at least once that the cluster was configured, we assume that the cluster is always configured.
	return cluster.Status.Configured ||
		status.Client.DatabaseStatus.Available &&
			status.Cluster.Layers.Error != "configurationMissing"
}
