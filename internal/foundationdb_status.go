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
	"fmt"
	"math"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// GetCoordinatorsFromStatus gets the current coordinators from the status.
// The returning set will contain all processes by their process group ID.
func GetCoordinatorsFromStatus(status *fdbv1beta2.FoundationDBStatus) map[string]struct{} {
	coordinators := make(map[string]struct{})

	for _, pInfo := range status.Cluster.Processes {
		for _, roleInfo := range pInfo.Roles {
			if roleInfo.Role != string(fdbv1beta2.ProcessRoleCoordinator) {
				continue
			}

			// We don't have to check for duplicates here, if the process group ID is already
			// set we just overwrite it.
			coordinators[pInfo.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]] = struct{}{}
			break
		}
	}

	return coordinators
}

// GetMinimumUptimeAndAddressMap returns address map of the processes included the the foundationdb status. The minimum
// uptime will be either secondsSinceLastRecovered if the recovery state is supported and enabled otherwise we will
// take the minimum uptime of all processes.
func GetMinimumUptimeAndAddressMap(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, recoveryStateEnabled bool) (float64, map[string][]fdbv1beta2.ProcessAddress, error) {
	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		return 0, nil, err
	}

	useRecoveryState := runningVersion.SupportsRecoveryState() && recoveryStateEnabled

	addressMap := make(map[string][]fdbv1beta2.ProcessAddress, len(status.Cluster.Processes))

	minimumUptime := math.Inf(1)
	if useRecoveryState {
		minimumUptime = status.Cluster.RecoveryState.SecondsSinceLastRecovered
	}

	for _, process := range status.Cluster.Processes {
		addressMap[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]] = append(addressMap[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]], process.Address)

		if useRecoveryState || process.Excluded {
			continue
		}

		if process.UptimeSeconds < minimumUptime {
			minimumUptime = process.UptimeSeconds
		}
	}

	return minimumUptime, addressMap, nil
}

// RemoveWarningsInJSON removes any warning messages that might appear in the status output from the fdbcli and returns
// the JSON output without the warning message.
func RemoveWarningsInJSON(jsonString string) ([]byte, error) {
	idx := strings.Index(jsonString, "{")
	if idx == -1 {
		return nil, fmt.Errorf("the JSON string doesn't contain a starting '{'")
	}

	return []byte(strings.TrimSpace(jsonString[idx:])), nil
}

// GetUnsupportedClients will return all clients that are not supporting the specified protocolVersion. Clients that
// have the same machine address as a fdbserver process will be ignored. The assumption here is that no other processes
// are co-located to the fdbserver process. We have to ignore those clients since FDB 7.1 added all fdbserver processes
// as clients and there is no other way to distinguish them and this would result in the operator being stuck in this step.
// See: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1468
func GetUnsupportedClients(status *fdbv1beta2.FoundationDBStatus, protocolVersion string) ([]string, error) {
	fdbserverProcesses := map[string]fdbv1beta2.None{}
	for _, process := range status.Cluster.Processes {
		fdbserverProcesses[process.Address.MachineAddress()] = fdbv1beta2.None{}
	}

	var unsupportedClients []string
	for _, versionInfo := range status.Cluster.Clients.SupportedVersions {
		if versionInfo.ProtocolVersion == "Unknown" {
			continue
		}

		if versionInfo.ProtocolVersion != protocolVersion {
			for _, client := range versionInfo.MaxProtocolClients {
				address, err := fdbv1beta2.ParseProcessAddress(client.Address)
				if err != nil {
					return nil, err
				}

				if _, ok := fdbserverProcesses[address.MachineAddress()]; ok {
					continue
				}

				unsupportedClients = append(unsupportedClients, client.Description())
			}
		}
	}

	return unsupportedClients, nil
}
