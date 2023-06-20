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

import (
	"math"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

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
func DoStorageServerFaultDomainCheckOnStatus(status *fdbv1beta2.FoundationDBStatus) bool {
	// Storage server related check.
	for _, tracker := range status.Cluster.Data.TeamTrackers {
		if !tracker.State.Healthy {
			return false
		}
	}

	return true
}

// DoLogServerFaultDomainCheckOnStatus does a log server related fault domain check over the given status object.
func DoLogServerFaultDomainCheckOnStatus(status *fdbv1beta2.FoundationDBStatus) bool {
	// Log server related check.
	for _, log := range status.Cluster.Logs {
		// @todo do we need to do this check only for the current log server set? Revisit this issue later.
		if (log.LogReplicationFactor != 0 && log.LogFaultTolerance+1 != log.LogReplicationFactor) ||
			(log.RemoteLogReplicationFactor != 0 && log.RemoteLogFaultTolerance+1 != log.RemoteLogReplicationFactor) ||
			(log.SatelliteLogReplicationFactor != 0 && log.SatelliteLogFaultTolerance+1 != log.SatelliteLogReplicationFactor) {
			return false
		}
	}

	return true
}

// DoCoordinatorFaultDomainCheckOnStatus does a coordinator related fault domain check over the given status object.
// @note an empty function for now. We will revisit this later.
func DoCoordinatorFaultDomainCheckOnStatus(status *fdbv1beta2.FoundationDBStatus) bool {
	// @todo decide if we need to do coordinator related check.
	return true
}
