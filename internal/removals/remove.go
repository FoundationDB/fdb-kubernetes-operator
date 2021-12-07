/*
 * remove.go
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

package removals

import (
	"fmt"
	"net"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// GetProcessGroupsToRemove returns a list of process groups to be removed based on the removal mode.
func GetProcessGroupsToRemove(removalMode fdbtypes.DeletionMode, removals map[string][]string) (string, []string, error) {
	if removalMode == fdbtypes.DeletionModeAll {
		var deletions []string

		for _, zoneProcesses := range removals {
			deletions = append(deletions, zoneProcesses...)
		}

		return "cluster", deletions, nil
	}

	if removalMode == fdbtypes.DeletionModeProcessGroup {
		for _, zoneProcesses := range removals {
			if len(zoneProcesses) < 1 {
				continue
			}

			// Fetch the first process group and delete it
			return zoneProcesses[0], []string{zoneProcesses[0]}, nil
		}
	}

	if removalMode == fdbtypes.DeletionModeZone {
		for zoneName, zoneProcesses := range removals {
			// Fetch the first zone and stop
			return zoneName, zoneProcesses, nil
		}
	}

	return "", nil, fmt.Errorf("unknown deletion mode: \"%s\"", removalMode)
}

// GetZonedRemovals returns a map with the zone as key and a list of process groups IDs to be removed.
// If the process group has not an associated process in the cluster status the zone will be "UNKNOWN"
func GetZonedRemovals(status *fdbtypes.FoundationDBStatus, processGroupsToRemove []string) (map[string][]string, error) {
	// Convert the process list into a map with the process group ID as
	// key.
	processInfo := map[string]fdbtypes.FoundationDBStatusProcessInfo{}
	for _, p := range status.Cluster.Processes {
		processInfo[p.Locality[fdbtypes.FDBLocalityInstanceIDKey]] = p
	}

	zoneMap := map[string][]string{}
	for _, pg := range processGroupsToRemove {
		p, ok := processInfo[pg]
		if !ok {
			zoneMap["UNKNOWN"] = append(zoneMap["UNKNOWN"], pg)
			continue
		}

		zone := p.Locality[fdbtypes.FDBLocalityZoneIDKey]
		zoneMap[zone] = append(zoneMap[zone], pg)
	}

	return zoneMap, nil
}

// GetRemainingMap returns a map that indicates if a process group is fully excluded in the cluster.
func GetRemainingMap(logger logr.Logger, adminClient fdbadminclient.AdminClient, cluster *fdbtypes.FoundationDBCluster) (map[string]bool, error) {
	var err error

	addresses := make([]fdbtypes.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.Remove || processGroup.ExclusionSkipped {
			continue
		}

		if len(processGroup.Addresses) == 0 {
			logger.Info("Getting remaining removals to check for exclusion", "processGroupID", processGroup.ProcessGroupID, "reason", "missing address")
			continue
		}

		for _, pAddr := range processGroup.Addresses {
			addresses = append(addresses, fdbtypes.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
		}
	}

	var remaining []fdbtypes.ProcessAddress
	if len(addresses) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return map[string]bool{}, err
		}
	}

	if len(remaining) > 0 {
		logger.Info("Exclusions to complete", "remainingServers", remaining)
	}

	remainingMap := make(map[string]bool, len(remaining))
	for _, address := range addresses {
		remainingMap[address.String()] = false
	}
	for _, address := range remaining {
		remainingMap[address.String()] = true
	}

	return remainingMap, nil
}
