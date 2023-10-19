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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	"net"

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
func GetProcessGroupsToRemove(removalMode fdbv1beta2.PodUpdateMode, removals map[fdbv1beta2.FaultDomain][]*fdbv1beta2.ProcessGroupStatus) (fdbv1beta2.FaultDomain, []*fdbv1beta2.ProcessGroupStatus, error) {
	if removalMode == fdbv1beta2.PodUpdateModeAll {
		var deletions []*fdbv1beta2.ProcessGroupStatus

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
			return zoneProcesses[0].FaultDomain, []*fdbv1beta2.ProcessGroupStatus{zoneProcesses[0]}, nil
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
func GetZonedRemovals(processGroupsToRemove []*fdbv1beta2.ProcessGroupStatus) (map[fdbv1beta2.FaultDomain][]*fdbv1beta2.ProcessGroupStatus, int64, error) {
	var latestRemovalTimestamp int64

	zoneMap := map[fdbv1beta2.FaultDomain][]*fdbv1beta2.ProcessGroupStatus{}
	for _, processGroup := range processGroupsToRemove {
		// Using the ResourcesTerminating is not a complete precise measurement of the time when we
		// actually removed the process group, but it should be a good indicator to how long the process group is in
		// that state.
		removalTimestamp := pointer.Int64Deref(processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating), 0)
		if removalTimestamp > 0 {
			if removalTimestamp > latestRemovalTimestamp {
				latestRemovalTimestamp = removalTimestamp
			}
			zoneMap[TerminatingZone] = append(zoneMap[TerminatingZone], processGroup)
			continue
		}

		zone := processGroup.FaultDomain
		if zone == "" {
			zone = UnknownZone
		}
		zoneMap[zone] = append(zoneMap[zone], processGroup)
	}

	return zoneMap, latestRemovalTimestamp, nil
}

// GetRemainingMap returns a map that indicates if a process group is fully excluded in the cluster.
func GetRemainingMap(logger logr.Logger, adminClient fdbadminclient.AdminClient, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) (map[string]bool, error) {
	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.IsMarkedForRemoval() || processGroup.IsExcluded() {
			continue
		}

		if len(processGroup.Addresses) == 0 {
			logger.Info("Getting remaining removals to check for exclusion", "processGroupID", processGroup.ProcessGroupID, "reason", "missing address")
			continue
		}

		if cluster.UseLocalitiesForExclusion() {
			addresses = append(addresses, fdbv1beta2.ProcessAddress{StringAddress: processGroup.GetExclusionString()})
		}

		for _, pAddr := range processGroup.Addresses {
			addresses = append(addresses, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
		}
	}

	remainingMap := map[string]bool{}
	if len(addresses) == 0 {
		return remainingMap, nil
	}

	remaining, err := fdbstatus.CanSafelyRemoveFromStatus(logger, adminClient, addresses, status)
	if err != nil {
		return nil, err
	}

	if len(remaining) > 0 {
		logger.Info("Exclusions to complete", "remainingServers", remaining)
	}

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
