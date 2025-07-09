/*
 * maintenance.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2024 Apple Inc. and the FoundationDB project authors
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

package maintenance

import (
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
)

// GetMaintenanceInformation returns the information about processes that have finished, stale information in the maintenance list and processes that still must be updated.
func GetMaintenanceInformation(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	processesUnderMaintenance map[fdbv1beta2.ProcessGroupID]int64,
	staleDuration time.Duration,
	differentZoneWaitDuration time.Duration,
) ([]fdbv1beta2.ProcessGroupID, []fdbv1beta2.ProcessGroupID, []fdbv1beta2.ProcessGroupID) {
	finishedMaintenance := make([]fdbv1beta2.ProcessGroupID, 0, len(processesUnderMaintenance))
	staleMaintenanceInformation := make(
		[]fdbv1beta2.ProcessGroupID,
		0,
		len(processesUnderMaintenance),
	)
	processesToUpdate := make([]fdbv1beta2.ProcessGroupID, 0, len(processesUnderMaintenance))

	// If the provided status is empty return all processes to be updated.
	if status == nil {
		for processGroupID := range processesUnderMaintenance {
			processesToUpdate = append(processesToUpdate, processGroupID)
		}

		logger.Info("provided status is empty")
		return nil, nil, processesToUpdate
	}

	logger.Info("start evaluation", "processesUnderMaintenance", processesUnderMaintenance)

	// If no processes are in the maintenance list, we can skip further checks and we don't have to iterate over
	// all processes in the cluster.
	if len(processesUnderMaintenance) == 0 {
		return nil, nil, nil
	}

	for _, process := range status.Cluster.Processes {
		// Only storage processes are affected by the maintenance mode.
		if process.ProcessClass != fdbv1beta2.ProcessClassStorage {
			continue
		}

		processGroupID, ok := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if !ok {
			continue
		}

		// Check if the provided process is under maintenance, if not we can skip further checks.
		maintenanceStart, isUnderMaintenance := processesUnderMaintenance[fdbv1beta2.ProcessGroupID(processGroupID)]
		if !isUnderMaintenance {
			continue
		}

		// Get the start time of the processes, based on the current time and the uptime seconds reported by the process.
		startTime := time.Now().Add(-1 * time.Duration(process.UptimeSeconds) * time.Second)
		maintenanceStartTime := time.Unix(maintenanceStart, 0)

		zoneID, ok := process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
		if !ok {
			continue
		}

		logger.Info(
			"found process under maintenance",
			"processGroupID",
			processGroupID,
			"zoneID",
			zoneID,
			"currentMaintenance",
			status.Cluster.MaintenanceZone,
			"startTime",
			startTime.String(),
			"maintenanceStartTime",
			maintenanceStartTime.String(),
			"UptimeSeconds",
			process.UptimeSeconds,
		)
		// Remove the process group ID from processesUnderMaintenance as we have found a processes.
		delete(processesUnderMaintenance, fdbv1beta2.ProcessGroupID(processGroupID))

		// If the start time is after the maintenance start time, we can assume that maintenance for this specific process is done.
		if startTime.After(maintenanceStartTime) {
			finishedMaintenance = append(
				finishedMaintenance,
				fdbv1beta2.ProcessGroupID(processGroupID),
			)
			continue
		}

		// If the zones are not matching those are probably stale entries. Once they are long enough in the list of
		// entries they will be removed.
		if zoneID != string(status.Cluster.MaintenanceZone) {
			// If the entry was recently added, per default less than 5 minutes, we are adding it to the processesToUpdate
			// list, even if the zones are not matching. We are doing this to reduce the risk of the operator acting on
			// a stale version of the machine-readable status, e.g. because of CPU throttling or the operator
			// caching the machine-readable status and taking a long time to reconcile.
			durationSinceMaintenanceStarted := time.Since(maintenanceStartTime)
			if durationSinceMaintenanceStarted < differentZoneWaitDuration {
				processesToUpdate = append(
					processesToUpdate,
					fdbv1beta2.ProcessGroupID(processGroupID),
				)
			}

			// If the maintenance start time is longer ago than the defined stale duration, we can assume that this is
			// an old entry that should be cleaned up.
			if durationSinceMaintenanceStarted > staleDuration {
				staleMaintenanceInformation = append(
					staleMaintenanceInformation,
					fdbv1beta2.ProcessGroupID(processGroupID),
				)
			}

			continue
		}

		processesToUpdate = append(processesToUpdate, fdbv1beta2.ProcessGroupID(processGroupID))
	}

	// Create a map for all the storage process groups, to validate if any stale maintenance entries exist for removed
	// process groups.
	storageProcessGroups := map[fdbv1beta2.ProcessGroupID]*fdbv1beta2.ProcessGroupStatus{}
	if len(processesUnderMaintenance) > 0 {
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
				continue
			}

			storageProcessGroups[processGroup.ProcessGroupID] = processGroup
		}
	}

	// After we checked above the processes that are done with their maintenance and the processes that still must be
	// restarted we have to filter out all stale entries. We filter out those stale entries to make sure the entries
	// are eventually cleaned up.
	for processGroupID, maintenanceStart := range processesUnderMaintenance {
		// If the maintenance start time is longer ago than the defined stale duration, we can assume that this is
		// an old entry that should be cleaned up.
		if time.Since(time.Unix(maintenanceStart, 0)) > staleDuration {
			staleMaintenanceInformation = append(staleMaintenanceInformation, processGroupID)
			continue
		}

		// If the process group is managed by this operator instance and no associated process group exists, we can
		// assume that the process group was removed and the maintenance information is stale.
		if strings.HasPrefix(string(processGroupID), cluster.Spec.ProcessGroupIDPrefix) {
			logger.V(1).
				Info("found stale maintenance information for removed process group", "processGroupID", processGroupID)
			_, ok := storageProcessGroups[processGroupID]
			if !ok {
				staleMaintenanceInformation = append(staleMaintenanceInformation, processGroupID)
				continue
			}
		}

		processesToUpdate = append(processesToUpdate, processGroupID)
	}

	return finishedMaintenance, staleMaintenanceInformation, processesToUpdate
}
