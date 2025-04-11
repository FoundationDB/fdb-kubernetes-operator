/*
 * coordination.go
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

package coordination

import (
	"fmt"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/restarts"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/go-logr/logr"
)

// IgnoreMissingProcessDuration defines the duration a Process Group must have the MissingProcess condition to be
// ignored in the exclusion check and let the exclusions potentially move forward.
// We should consider to make this configurable in the long term.
const IgnoreMissingProcessDuration = 5 * time.Minute

// Ensure that the WaitTimeError implements the error interface.
var _ error = (*WaitTimeError)(nil)

// WaitTimeError represent and error when the last pending process groups was added earlier than the wait time allows.
type WaitTimeError struct {
	// timeSinceLastPendingWasAdded represents the time since the last pending process group was added.
	timeSinceLastPendingWasAdded time.Duration
	// waitTime represents the minimum time the operator should be waiting since a pending process group was added.
	waitTime time.Duration
}

// Error returns the error string for this error.
func (err WaitTimeError) Error() string {
	return fmt.Sprintf("last pending process group was added: %s ago, wait time for pending additions is: %s", err.timeSinceLastPendingWasAdded.String(), err.waitTime.String())
}

// GetWaitTime returns the difference between the wait time and the time since the last pending process group was added.
// The result can be used to delay the reconcile queue.
func (err WaitTimeError) GetWaitTime() time.Duration {
	return err.waitTime - err.timeSinceLastPendingWasAdded
}

// AllProcessesReady will return all the process groups that are in the pending and ready list. If the time since the
// last update was made is earlier than the wait time, a WaitTimeError will be returned.
func AllProcessesReady(logger logr.Logger, pendingProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, readyProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, waitTime time.Duration) error {
	notReadyProcesses, _, err := getNotReadyProcesses(logger, pendingProcessGroups, readyProcessGroups, waitTime)
	if err != nil {
		return err
	}

	if len(notReadyProcesses) > 0 {
		return fmt.Errorf("not all processes are ready: %v", strings.Join(notReadyProcesses, ","))
	}

	if len(pendingProcessGroups) != len(readyProcessGroups) {
		return fmt.Errorf("not all processes are ready: mismatch in %d pending processes and %d ready processes", len(pendingProcessGroups), len(readyProcessGroups))
	}

	return nil
}

func getNotReadyProcesses(logger logr.Logger, pendingProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, readyProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, waitTime time.Duration) ([]string, time.Time, error) {
	timestampLastAdded := time.Time{}

	notReadyProcesses := make([]string, 0, max(len(pendingProcessGroups)-len(readyProcessGroups), 0))
	for pending, timestamp := range pendingProcessGroups {
		// Tester processes are not managed over the global coordination.
		if pending.GetProcessClass() == fdbv1beta2.ProcessClassTest {
			continue
		}

		if timestamp.After(timestampLastAdded) {
			timestampLastAdded = timestamp
		}

		readyTimestamp, isReady := readyProcessGroups[pending]
		if isReady {
			if readyTimestamp.After(timestampLastAdded) {
				timestampLastAdded = readyTimestamp
			}

			continue
		}

		notReadyProcesses = append(notReadyProcesses, string(pending))
		logger.Info("found process group in pending list but not in ready list", "processGroupID", pending)
	}

	// Check if the last addition was longer ago than the wait time duration.
	if time.Since(timestampLastAdded) < waitTime {
		return notReadyProcesses, timestampLastAdded, WaitTimeError{
			timeSinceLastPendingWasAdded: time.Since(timestampLastAdded),
			waitTime:                     waitTime,
		}
	}

	return notReadyProcesses, timestampLastAdded, nil
}

// AllProcessesReadyForExclusion will return all the process groups that are in the pending and ready list. If the time since the
// last update was made is earlier than the wait time, a WaitTimeError will be returned. It implements a similar logic to the
// AllProcessesReady with some modifications for the exclude reconciler to ensure that the excludes can mode forward.
func AllProcessesReadyForExclusion(logger logr.Logger, pendingProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, readyProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, waitTime time.Duration) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	notReadyProcesses, timestampLastAdded, err := getNotReadyProcesses(logger, pendingProcessGroups, readyProcessGroups, waitTime)
	if err != nil {
		return nil, err
	}

	// In case that not all processes are ready we have to check when the last time an update to the pending and
	// ready list has happened.
	if len(notReadyProcesses) > 0 {
		// If there was a change in the last 5 minutes (or longer), we will return an error message and wait for the processes
		// to get  ready.
		if time.Since(timestampLastAdded) < max(5*waitTime, IgnoreMissingProcessDuration) {
			return nil, fmt.Errorf("not all processes are ready: %v", strings.Join(notReadyProcesses, ","))
		}

		// In case that we already waited for 5 minutes, start the exclusion on the processes that are marked to be ready.
		// Otherwise, we might be blocking forever, e.g. in cases where the quota is limited.
		return readyProcessGroups, nil
	}

	if len(pendingProcessGroups) != len(readyProcessGroups) {
		return nil, fmt.Errorf("not all processes are ready: mismatch in %d pending processes and %d ready processes", len(pendingProcessGroups), len(readyProcessGroups))
	}

	return readyProcessGroups, nil
}

// GetAddressesFromStatus will return the process addresses for the provided processGroups based on the provided machine-readable status.
func GetAddressesFromStatus(logger logr.Logger, status *fdbv1beta2.FoundationDBStatus, processGroups map[fdbv1beta2.ProcessGroupID]time.Time, includeLocalities bool, includeAddresses bool) []fdbv1beta2.ProcessAddress {
	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(status.Cluster.Processes))
	visited := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(processGroups))
	for _, process := range status.Cluster.Processes {
		// Ignore any tester processes as those are not managed by the global coordination system.
		if process.ProcessClass == fdbv1beta2.ProcessClassTest {
			continue
		}

		processID, ok := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if !ok {
			logger.Info("Ignore process with missing locality field", "address", process.Address.String())
			continue
		}

		if _, ok := processGroups[fdbv1beta2.ProcessGroupID(processID)]; ok {
			visited[fdbv1beta2.ProcessGroupID(processID)] = fdbv1beta2.None{}
			if includeLocalities {
				addresses = append(addresses, fdbv1beta2.ProcessAddress{
					StringAddress: fmt.Sprintf("%s:%s", fdbv1beta2.FDBLocalityExclusionPrefix, processID),
				})
			}

			if includeAddresses {
				addresses = append(addresses, process.Address)
			}
		}
	}

	// If some processes are not part of the machine-readable status print it out.
	if len(visited) != len(processGroups) {
		missing := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
		for processGroupID := range processGroups {
			if _, ok := visited[processGroupID]; ok {
				continue
			}

			// Since the locality is static (process group ID) we can use it here.
			if includeAddresses {
				addresses = append(addresses, fdbv1beta2.ProcessAddress{
					StringAddress: fmt.Sprintf("%s:%s", fdbv1beta2.FDBLocalityExclusionPrefix, processGroupID),
				})
			}

			missing[processGroupID] = fdbv1beta2.None{}
		}

		logger.Info("Not all requested process groups are part of the machine-readable status", "missingProcessGroups", missing)
	}

	return addresses
}

// UpdateGlobalCoordinationState will update the state for global synchronization. If the synchronization mode is local,
// this method will skip all work.
func UpdateGlobalCoordinationState(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, adminClient fdbadminclient.AdminClient) error {
	// If the synchronization mode is local (default) skip all work. If the mode is changed from global to local
	// the human operator must clean up.
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeLocal {
		return nil
	}

	// Keep track of all the visited process groups, we use this to remove entries from process groups that no longer
	// exists.
	visited := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}

	// Read all data from the lists to get the current state. If a prefix is provided to the get methods, only
	// process groups with the additional sub path will be returned.
	pendingForExclusion, err := adminClient.GetPendingForExclusion(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	pendingForRestart, err := adminClient.GetPendingForRestart(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	pendingForRemoval, err := adminClient.GetPendingForRemoval(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	pendingForInclusion, err := adminClient.GetPendingForInclusion(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	readyForRestart, err := adminClient.GetReadyForRestart(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	readyForExclusion, err := adminClient.GetReadyForExclusion(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	readyForInclusion, err := adminClient.GetReadyForInclusion(cluster.Spec.ProcessGroupIDPrefix)
	if err != nil {
		return err
	}

	// UpdateAction can be "delete" or "add". If the action is "add" the entry will be added, if
	// the action is "delete" the entry will be deleted.
	updatesPendingForExclusion := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	updatesPendingForInclusion := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	updatesPendingForRestart := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	updatesPendingForRemoval := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	updatesReadyForInclusion := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	updatesReadyForRestart := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	updatesReadyForExclusion := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}

	// Iterate over all process groups to generate the expected state.
	for _, processGroup := range cluster.Status.ProcessGroups {
		// Tester processes are not managed over the global coordination.
		if processGroup.ProcessClass == fdbv1beta2.ProcessClassTest {
			continue
		}

		// Keep track of the visited process group to remove entries from removed process groups.
		visited[processGroup.ProcessGroupID] = fdbv1beta2.None{}
		if processGroup.IsMarkedForRemoval() {
			if _, ok := pendingForRemoval[processGroup.ProcessGroupID]; !ok {
				updatesPendingForRemoval[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
			}

			// Only add the process group if the exclusion is not done yet.
			if !processGroup.IsExcluded() {
				if _, ok := pendingForExclusion[processGroup.ProcessGroupID]; !ok {
					updatesPendingForExclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
				}

				if _, ok := pendingForInclusion[processGroup.ProcessGroupID]; !ok {
					updatesPendingForInclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
				}
			} else {
				// Check if the process group is present in pendingForExclusion or readyForExclusion.
				// If so, add them to the set to remove those entries as the process is already excluded.
				if _, ok := pendingForExclusion[processGroup.ProcessGroupID]; ok {
					updatesPendingForExclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionDelete
				}

				if _, ok := readyForExclusion[processGroup.ProcessGroupID]; ok {
					updatesReadyForExclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionDelete
				}

				// Ensure the process is added to the pending for inclusion list.
				if _, ok := pendingForInclusion[processGroup.ProcessGroupID]; !ok {
					updatesPendingForInclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
				}

				if processGroup.ExclusionSkipped {
					if _, ok := readyForInclusion[processGroup.ProcessGroupID]; !ok {
						updatesReadyForInclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
					}
				}
			}

			continue
		}

		// If the process groups is missing long enough to be ignored, ensure that it's removed from the pending
		// and the ready list.
		if processGroup.GetConditionTime(fdbv1beta2.IncorrectCommandLine) != nil && !restarts.ShouldBeIgnoredBecauseMissing(logger, cluster, processGroup) {
			// Check if the process group is present in pendingForRestart.
			// If not add it to the according set.
			if _, ok := pendingForRestart[processGroup.ProcessGroupID]; !ok {
				updatesPendingForRestart[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
			}
		} else {
			// Check if the process group is present in pendingForRestart or readyForRestart.
			// If so, add them to the set to remove those entries as the process has the correct command line.
			if _, ok := pendingForRestart[processGroup.ProcessGroupID]; ok {
				logger.Info("Removing from pendingForRestart", "processGroupID", processGroup.ProcessGroupID)
				updatesPendingForRestart[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionDelete
			}

			if _, ok := readyForRestart[processGroup.ProcessGroupID]; ok {
				logger.Info("Removing from readyForRestart", "processGroupID", processGroup.ProcessGroupID)
				updatesReadyForRestart[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionDelete
			}
		}
	}

	// Iterate over all the sets and mark all entries that are associated with a removed process group to be
	// removed.
	addUnvisitedProcessGroupsToBeRemoved(pendingForExclusion, updatesPendingForExclusion, visited)
	addUnvisitedProcessGroupsToBeRemoved(pendingForRestart, updatesPendingForRestart, visited)
	addUnvisitedProcessGroupsToBeRemoved(pendingForRemoval, updatesPendingForRemoval, visited)
	addUnvisitedProcessGroupsToBeRemoved(pendingForInclusion, updatesPendingForInclusion, visited)
	addUnvisitedProcessGroupsToBeRemoved(readyForRestart, updatesReadyForRestart, visited)
	addUnvisitedProcessGroupsToBeRemoved(readyForExclusion, updatesReadyForExclusion, visited)
	addUnvisitedProcessGroupsToBeRemoved(readyForInclusion, updatesReadyForInclusion, visited)

	// Update all the fields that have changes.
	err = adminClient.UpdatePendingForExclusion(updatesPendingForExclusion)
	if err != nil {
		return err
	}

	err = adminClient.UpdatePendingForRestart(updatesPendingForRestart)
	if err != nil {
		return err
	}

	err = adminClient.UpdatePendingForRemoval(updatesPendingForRemoval)
	if err != nil {
		return err
	}

	err = adminClient.UpdatePendingForInclusion(updatesPendingForInclusion)
	if err != nil {
		return err
	}

	err = adminClient.UpdateReadyForRestart(updatesReadyForRestart)
	if err != nil {
		return err
	}

	err = adminClient.UpdateReadyForExclusion(updatesReadyForExclusion)
	if err != nil {
		return err
	}

	return adminClient.UpdateReadyForInclusion(updatesReadyForInclusion)
}

// addUnvisitedProcessGroupsToBeRemoved will add all the updates that were not visited to the updates as being deleted.
// This will ensure that removed process groups will be removed from the set.
func addUnvisitedProcessGroupsToBeRemoved(pendingSet map[fdbv1beta2.ProcessGroupID]time.Time, updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction, visited map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None) {
	for processGroupID := range pendingSet {
		// If the process group was not visited the process group was removed and all the
		// associated entries should be removed too.
		if _, ok := visited[processGroupID]; !ok {
			updates[processGroupID] = fdbv1beta2.UpdateActionDelete
		}
	}
}
