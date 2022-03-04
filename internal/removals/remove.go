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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

const (
	// UnknownZone maps all process groups that are not known to the FoundationDB status.
	UnknownZone = "foundationdb.org/unknown"
	// TerminatingZone maps all process groups that have the condition ResourcesTerminating.
	TerminatingZone = "foundationdb.org/terminating"
)

// GetProcessGroupsToRemove returns a list of process groups to be removed based on the removal mode.
func GetProcessGroupsToRemove(removalMode fdbv1beta2.PodUpdateMode, removals map[string][]string) (string, []string, error) {
	if removalMode == fdbv1beta2.PodUpdateModeAll {
		var deletions []string

		for _, zoneProcesses := range removals {
			deletions = append(deletions, zoneProcesses...)
		}

		return "cluster", deletions, nil
	}

	if removalMode == fdbv1beta2.PodUpdateModeProcessGroup {
		for _, zoneProcesses := range removals {
			if len(zoneProcesses) < 1 {
				continue
			}

			// Fetch the first process group and delete it
			return zoneProcesses[0], []string{zoneProcesses[0]}, nil
		}
	}

	if removalMode == fdbv1beta2.PodUpdateModeZone {
		for zoneName, zoneProcesses := range removals {
			if zoneName == TerminatingZone {
				continue
			}
			// Fetch the first zone and stop
			return zoneName, zoneProcesses, nil
		}
		return "", nil, nil
	}

	if removalMode == fdbv1beta2.PodUpdateModeNone {
		return "None", nil, nil
	}

	return "", nil, fmt.Errorf("unknown deletion mode: \"%s\"", removalMode)
}

// GetZonedRemovals returns a map with the zone as key and a list of process groups IDs to be removed.
// If the process group has not an associated process in the cluster status the zone will be UnknownZone.
// if the process group has the ResourcesTerminating condition the zone will be TerminatingZone.
func GetZonedRemovals(status *fdb.FoundationDBStatus, processGroupsToRemove []*fdbv1beta2.ProcessGroupStatus) (map[string][]string, int64, error) {
	var lastestRemovalTimestamp int64
	// Convert the process list into a map with the process group ID as key.
	processInfo := map[string]fdb.FoundationDBStatusProcessInfo{}
	for _, p := range status.Cluster.Processes {
		processInfo[p.Locality[fdb.FDBLocalityInstanceIDKey]] = p
	}

	zoneMap := map[string][]string{}
	for _, pg := range processGroupsToRemove {
		// Using the ResourcesTerminating is not a complete precise measurement of the time when we
		// actually removed the process group, but it should be a good indicator to how long the process group is in
		// that state.
		removalTimestamp := pointer.Int64Deref(pg.GetConditionTime(fdbv1beta2.ResourcesTerminating), 0)
		if removalTimestamp > 0 {
			if removalTimestamp > lastestRemovalTimestamp {
				lastestRemovalTimestamp = removalTimestamp
			}
			zoneMap[TerminatingZone] = append(zoneMap[TerminatingZone], pg.ProcessGroupID)
			continue
		}

		p, ok := processInfo[pg.ProcessGroupID]
		if !ok {
			zoneMap[UnknownZone] = append(zoneMap[UnknownZone], pg.ProcessGroupID)
			continue
		}

		zone := p.Locality[fdb.FDBLocalityZoneIDKey]
		zoneMap[zone] = append(zoneMap[zone], pg.ProcessGroupID)
	}

	return zoneMap, lastestRemovalTimestamp, nil
}

// GetRemainingMap returns a map that indicates if a process group is fully excluded in the cluster.
func GetRemainingMap(logger logr.Logger, adminClient fdbadminclient.AdminClient, cluster *fdbv1beta2.FoundationDBCluster) (map[string]bool, error) {
	var err error

	addresses := make([]fdb.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.IsMarkedForRemoval() || processGroup.IsExcluded() {
			continue
		}

		if len(processGroup.Addresses) == 0 {
			logger.Info("Getting remaining removals to check for exclusion", "processGroupID", processGroup.ProcessGroupID, "reason", "missing address")
			continue
		}

		for _, pAddr := range processGroup.Addresses {
			addresses = append(addresses, fdb.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
		}
	}

	var remaining []fdb.ProcessAddress
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

// RemovalAllowed returns if we are allowed to remove the process group or if we have to wait to ensure a safe deletion.
func RemovalAllowed(lastDeletion int64, currentTimestamp int64, waitTime int) (int64, bool) {
	ts := currentTimestamp - int64(waitTime)
	if lastDeletion > ts {
		return lastDeletion - ts, false
	}

	return 0, true
}
