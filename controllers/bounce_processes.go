/*
 * bounce_processes.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2024 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/buggify"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/restarts"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

// bounceProcesses provides a reconciliation step for bouncing fdbserver
// processes.
type bounceProcesses struct{}

// reconcile runs the reconciler's work.
func (bounceProcesses) reconcile(_ context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	if !pointer.BoolDeref(cluster.Spec.AutomationOptions.KillProcesses, true) {
		return nil
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	currentMinimumUptime, addressMap, err := fdbstatus.GetMinimumUptimeAndAddressMap(logger, cluster, status, r.EnableRecoveryState)
	if err != nil {
		return &requeue{curError: err}
	}

	addresses, req := getProcessesReadyForRestart(logger, cluster, addressMap)
	if req != nil {
		return req
	}

	// Only perform the check if the cluster controller must be restarted if the cluster was up long enough. This is an
	// additional safety guard to reduce the risk of successive restarts in cases where unidirectional partitions occur.
	if currentMinimumUptime > r.MinimumRequiredUptimeCCBounce.Seconds() {
		// Check if the status contains unreachable tester processes. In this case the cluster controller must be restarted.
		// Otherwise the status will contain a message with "status_incomplete" and "unreachable_processes". Those messages
		// could block further actions like the check if a process is exclude and doesn't serve any roles.
		clusterControllerAddress := checkIfClusterControllerNeedsRestart(logger, cluster, status)
		if clusterControllerAddress != nil {
			logger.Info("found unreachable tester processes in status which requires a cluster controller restart")
			// Adding the same address twice is not a problem for the kill command, so we can just append the returned address.
			addresses = append(addresses, *clusterControllerAddress)
		}
	}

	if len(addresses) == 0 {
		return nil
	}

	logger.V(1).Info("processes that can be restarted", "addresses", addresses)

	// Check if the cluster can safely bounce processes.
	err = fdbstatus.CanSafelyBounceProcesses(currentMinimumUptime, float64(cluster.GetMinimumUptimeSecondsForBounce()), status)
	if err != nil {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsBounce", err.Error())
		// Retry after we waited the minimum uptime or at least 15 seconds.
		delayTime := cluster.GetMinimumUptimeSecondsForBounce() - int(currentMinimumUptime)
		if delayTime < 15 {
			delayTime = 15
		}

		return &requeue{
			message: err.Error(),
			delay:   time.Second * time.Duration(delayTime),
		}
	}

	var lockClient fdbadminclient.LockClient
	useLocks := cluster.ShouldUseLocks()
	if useLocks {
		lockClient, err = r.getLockClient(cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}
	version, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return &requeue{curError: err}
	}

	upgrading := cluster.IsBeingUpgradedWithVersionIncompatibleVersion()
	if useLocks && upgrading {
		processGroupIDs := make([]fdbv1beta2.ProcessGroupID, 0, len(cluster.Status.ProcessGroups))
		for _, processGroup := range cluster.Status.ProcessGroups {
			processGroupIDs = append(processGroupIDs, processGroup.ProcessGroupID)
		}

		logger.V(1).Info("adding processes to the pending upgrades", "processGroupIDs", processGroupIDs)
		err = lockClient.AddPendingUpgrades(version, processGroupIDs)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	hasLock, err := r.takeLock(logger, cluster, fmt.Sprintf("bouncing processes: %v", addresses))
	if err != nil {
		return &requeue{curError: err}
	}

	if hasLock {
		defer func() {
			lockErr := r.releaseLock(logger, cluster)
			if lockErr != nil {
				logger.Error(lockErr, "could not release lock")
			}
		}()
	}

	if useLocks && upgrading {
		var req *requeue
		addresses, req = getAddressesForUpgrade(logger, r, status, lockClient, cluster, version)
		if req != nil {
			return req
		}
		if addresses == nil {
			return &requeue{curError: fmt.Errorf("unknown error when getting addresses that are ready for upgrade")}
		}
	}

	filteredAddresses, removedAddresses := buggify.FilterIgnoredProcessGroups(cluster, addresses, status)
	if removedAddresses {
		addresses = filteredAddresses
	}

	if len(filteredAddresses) == 0 {
		return nil
	}

	logger.Info("Bouncing processes", "addresses", addresses, "upgrading", upgrading)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "BouncingProcesses", fmt.Sprintf("Bouncing processes: %v", addresses))
	err = adminClient.KillProcesses(addresses)
	if err != nil {
		return &requeue{curError: err}
	}

	// If the cluster was upgraded we will requeue and let the update_status command set the correct version.
	// Updating the version in this method has the drawback that we upgrade the version independent of the success
	// of the kill command. The kill command is not reliable, which means that some kill request might not be
	// delivered and the return value will still not contain any error.
	if upgrading {
		return &requeue{message: "fetch latest status after upgrade"}
	}

	return nil
}

// getProcessesReadyForRestart returns a slice of process addresses that can be restarted. If addresses are missing or not all processes
// have the latest configuration this method will return a requeue struct with more details.
func getProcessesReadyForRestart(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, addressMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, *requeue) {
	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
	allSynced := true
	versionIncompatibleUpgrade := cluster.IsBeingUpgradedWithVersionIncompatibleVersion()
	var missingAddress []fdbv1beta2.ProcessGroupID

	filterConditions := restarts.GetFilterConditions(cluster)
	var missingProcesses int
	var markedForRemoval int
	for _, processGroup := range cluster.Status.ProcessGroups {
		// Ignore tester processes in this reconciler as tester processes cannot be restarted with the kill command.
		if processGroup.ProcessClass == fdbv1beta2.ProcessClassTest {
			continue
		}

		// Skip process groups that are stuck in terminating. Such a case could represent a kubelet in an unavailable/stuck
		// state.
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			continue
		}

		// We have to count the marked for removal processes in addition to the other process groups. The reason for this
		// is that a process groups that is marked for removal will require the operator to spin up an additional process group.
		// This means as long as we have at least one process group that is marked for removal we have more process groups
		// than the cluster.GetProcessCountsWithDefaults() will return. The total number of process groups will be the result
		// of cluster.GetProcessCountsWithDefaults() + the number of process groups marked for removal.
		if processGroup.IsMarkedForRemoval() {
			markedForRemoval++
			// If we do a version incompatible upgrade we want to add the excluded processes to the list of processes
			// that should be restarted, to make sure we restart all processes in the cluster.
			if versionIncompatibleUpgrade && processGroup.IsExcluded() {
				logger.Info("adding process group that is marked for exclusion to list of restarted processes", "processGroupID", processGroup.ProcessGroupID)
				addresses = append(addresses, addressMap[processGroup.ProcessGroupID]...)
				continue
			}
		}

		// Ignore processes that are missing for more than 30 seconds e.mg. if the process is network partitioned.
		// This is required since the update status will not update the SidecarUnreachable setting if a process is
		// missing in the status.
		if missingTime := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses); missingTime != nil {
			if time.Unix(*missingTime, 0).Add(cluster.GetIgnoreMissingProcessesSeconds()).Before(time.Now()) {
				logger.Info("ignore process group with missing process", "processGroupID", processGroup.ProcessGroupID)
				missingProcesses++
				continue
			}
		}

		// If a Pod is stuck in pending we have to ignore it, as the processes hosted by this Pod will not be running.
		if cluster.SkipProcessGroup(processGroup) {
			logger.Info("ignore process group with Pod stuck in pending", "processGroupID", processGroup.ProcessGroupID)
			missingProcesses++
			continue
		}

		// If any of the processes that should not be skipped are not having an updated ConfigMap, we should be waiting
		// for the config to be propagated.
		if processGroup.GetConditionTime(fdbv1beta2.IncorrectConfigMap) != nil {
			allSynced = false
			logger.Info("Waiting for dynamic Pod config update", "processGroupID", processGroup.ProcessGroupID)
		}

		if !processGroup.MatchesConditions(filterConditions) {
			logger.V(1).Info("ignore process group with non matching conditions", "processGroupID", processGroup.ProcessGroupID, "expectedConditions", filterConditions, "currentConditions", processGroup.ProcessGroupConditions)
			continue
		}

		if addressMap[processGroup.ProcessGroupID] == nil {
			missingAddress = append(missingAddress, processGroup.ProcessGroupID)
			continue
		}

		addresses = append(addresses, addressMap[processGroup.ProcessGroupID]...)
	}

	if len(missingAddress) > 0 {
		return nil, &requeue{message: fmt.Sprintf("could not find address for processes: %s", missingAddress), delayedRequeue: true}
	}

	if !allSynced {
		return nil, &requeue{message: "Waiting for config map to sync to all pods", delayedRequeue: true}
	}

	// Only if the cluster is upgraded with an incompatible version we have to make sure that all processes are ready to be restarted.
	// In the case of a patch upgrade we will be recreating the Pods anyway without this bounce step.
	if cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		counts, err := cluster.GetProcessCountsWithDefaults()
		if err != nil {
			return nil, &requeue{
				curError:       err,
				delayedRequeue: true,
			}
		}

		// If we upgrade the cluster wait until all processes are ready for the restart. We don't want to block the restart
		// if some processes are already upgraded e.g. in the case of version compatible upgrades and we also don't want to
		// block the restart command if a process is missing longer than the specified GetIgnoreMissingProcessesSeconds.
		// Those checks should ensure we only run the restart command if all processes that have to be restarted and are connected
		// to cluster are ready to be restarted.
		expectedProcesses := counts.Total() - missingProcesses + markedForRemoval
		// If more than one storage server per Pod is running we have to account for this. In this case we have to add the
		// additional storage processes.
		if cluster.Spec.StorageServersPerPod > 1 {
			expectedProcesses += counts.Storage * (cluster.Spec.StorageServersPerPod - 1)
		}

		if cluster.Spec.LogServersPerPod > 1 {
			expectedProcesses += counts.Log * (cluster.Spec.LogServersPerPod - 1)
			expectedProcesses += counts.Transaction * (cluster.Spec.LogServersPerPod - 1)
		}

		// If not all processes are ready to restart we will block the upgrade and delay it.
		if expectedProcesses > len(addresses) {
			logger.Info("delay bounce as not all processes are ready to be bounced for upgrade", "expectedProcesses", expectedProcesses, "addresses", len(addresses))
			return nil, &requeue{
				message:        fmt.Sprintf("expected %d processes, got %d processes ready to restart", expectedProcesses, len(addresses)),
				delay:          5 * time.Second,
				delayedRequeue: true,
			}
		}
	}

	return addresses, nil
}

// getAddressesForUpgrade checks that all processes in a cluster are ready to be
// upgraded and returns the full list of addresses.
func getAddressesForUpgrade(logger logr.Logger, r *FoundationDBClusterReconciler, status *fdbv1beta2.FoundationDBStatus, lockClient fdbadminclient.LockClient, cluster *fdbv1beta2.FoundationDBCluster, version fdbv1beta2.Version) ([]fdbv1beta2.ProcessAddress, *requeue) {
	pendingUpgrades, err := lockClient.GetPendingUpgrades(version)
	if err != nil {
		return nil, &requeue{curError: err}
	}

	// We don't want to check for fault tolerance here to make sure the operator is able to restart processes if some
	// processes where restarted before the operator issued the cluster wide restart. For version incompatible upgrades
	// that would mean that the processes restarted earlier are not part of the cluster anymore leading to a fault tolerance
	// drop.
	if !status.Client.DatabaseStatus.Available {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpgradeRequeued", "Database is unavailable")
		return nil, &requeue{message: "Deferring upgrade until database is available"}
	}

	notReadyProcesses := make([]string, 0)
	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		if cluster.Spec.DataCenter != process.Locality[fdbv1beta2.FDBLocalityDCIDKey] {
			continue
		}
		processID := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if process.Version == version.String() {
			continue
		}
		if pendingUpgrades[fdbv1beta2.ProcessGroupID(processID)] {
			addresses = append(addresses, process.Address)
		} else {
			notReadyProcesses = append(notReadyProcesses, processID)
		}
	}

	if len(notReadyProcesses) > 0 {
		logger.Info("Deferring upgrade until all processes are ready to be upgraded", "remainingProcesses", notReadyProcesses)
		message := fmt.Sprintf("Waiting for processes to be updated: %v", notReadyProcesses)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpgradeRequeued", message)
		return nil, &requeue{message: message}
	}
	err = lockClient.ClearPendingUpgrades()
	if err != nil {
		return nil, &requeue{curError: err}
	}

	return addresses, nil
}

// checkIfClusterControllerNeedsRestart checks if the cluster controller must be restarted. There are a few cases where
// this might be required. One case is when at least on tester process is running in the cluster and that tester process
// fails. Currently this leads to the cluster controller reporting unreachable processes and the status incomplete message.
// Having those messages in the cluster's machine-readable status could block some operations of the operator and the
// solution to that is to restart the cluster controller process. If the FDB version supports to remove the old tester
// worker automatically this step will return no processes to be restarted.
func checkIfClusterControllerNeedsRestart(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) *fdbv1beta2.ProcessAddress {
	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		logger.Error(err, "could not parse running version in checkIfClusterControllerNeedsRestart")
		return nil
	}

	// If the cluster controller automatically removes the dead tester processes, the operator can skip any further work.
	if runningVersion.AutomaticallyRemovesDeadTesterProcesses() {
		return nil
	}

	// If the status contains no cluster messages we can skip further check.
	if len(status.Cluster.Messages) == 0 {
		return nil
	}

	var containsUnreachableProcessesMessage, containsStatusIncompleteMessage bool
	var unreachableProcesses []fdbv1beta2.FoundationDBUnreachableProcess

	for _, message := range status.Cluster.Messages {
		if message.Name == "status_incomplete" {
			containsStatusIncompleteMessage = true
			continue
		}

		if message.Name == "unreachable_processes" {
			containsUnreachableProcessesMessage = true
			unreachableProcesses = message.UnreachableProcesses
			continue
		}
	}

	// If no unreachable process message and no status incomplete message is present, we can skip further checks.
	if !containsUnreachableProcessesMessage && !containsStatusIncompleteMessage || len(unreachableProcesses) == 0 {
		return nil
	}

	// Convert the slice of unreachable processes into a set for faster access.
	unreachableProcessesSet := map[string]fdbv1beta2.None{}
	for _, unreachableProcess := range unreachableProcesses {
		unreachableProcessesSet[unreachableProcess.Address] = fdbv1beta2.None{}
	}

	var clusterControllerAddress fdbv1beta2.ProcessAddress
	var foundClusterController bool
	var unreachableProcessContainsTester bool
	// We have to validate if at least one tester process is unreachable. In this case we have to restart the cluster
	// controller. This will cause a recovery and the missing tester process will be removed from the list of unreachable
	// processes.
	for _, process := range status.Cluster.Processes {
		if process.ProcessClass == fdbv1beta2.ProcessClassTest {
			if _, ok := unreachableProcessesSet[process.Address.String()]; ok {
				unreachableProcessContainsTester = true
			}
			continue
		}

		if foundClusterController {
			continue
		}

		for _, role := range process.Roles {
			if fdbv1beta2.ProcessRole(role.Role) == fdbv1beta2.ProcessRoleClusterController {
				clusterControllerAddress = process.Address
				foundClusterController = true
				continue
			}
		}
	}

	// Only return the cluster controller address if at least one tester process was part of the unreachable processes list.
	if unreachableProcessContainsTester {
		return &clusterControllerAddress
	}

	return nil
}
