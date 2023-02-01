/*
 * bounce_processes.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/restarts"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

// bounceProcesses provides a reconciliation step for bouncing fdbserver
// processes.
type bounceProcesses struct{}

// reconcile runs the reconciler's work.
func (bounceProcesses) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	if !pointer.BoolDeref(cluster.Spec.AutomationOptions.KillProcesses, true) {
		return nil
	}

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "bounceProcesses")
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	minimumUptime, addressMap, err := internal.GetMinimumUptimeAndAddressMap(cluster, status, r.EnableRecoveryState)
	if err != nil {
		return &requeue{curError: err}
	}

	// In the case of version compatible upgrades we have to check if some processes are already running with the new
	// desired version e.g. because they were restarted by an event outside of the control of the operator.
	var upgradedProcesses int
	if cluster.VersionCompatibleUpgradeInProgress() {
		for _, process := range status.Cluster.Processes {
			dcID := process.Locality[fdbv1beta2.FDBLocalityDCIDKey]
			// Ignore processes that are not managed by this operator instance
			if dcID != cluster.Spec.DataCenter {
				continue
			}

			if process.Version == cluster.Spec.Version {
				upgradedProcesses++
			}
		}
	}

	addresses, req := getProcessesReadyForRestart(logger, cluster, addressMap, upgradedProcesses)
	if req != nil {
		return req
	}

	if len(addresses) == 0 {
		return nil
	}

	if minimumUptime < float64(cluster.GetMinimumUptimeSecondsForBounce()) {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsBounce",
			fmt.Sprintf("Spec require a bounce of some processes, but the cluster has only been up for %f seconds", minimumUptime))
		cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			logger.Error(err, "Error updating cluster status")
		}

		// Retry after we waited the minimum uptime
		return &requeue{
			message: "Cluster needs to stabilize before bouncing",
			delay:   time.Second * time.Duration(cluster.GetMinimumUptimeSecondsForBounce()-int(minimumUptime)),
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

	upgrading := cluster.Status.RunningVersion != cluster.Spec.Version

	if useLocks && upgrading {
		processGroupIDs := make([]string, 0, len(cluster.Status.ProcessGroups))
		for _, processGroup := range cluster.Status.ProcessGroups {
			processGroupIDs = append(processGroupIDs, processGroup.ProcessGroupID)
		}
		err = lockClient.AddPendingUpgrades(version, processGroupIDs)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	hasLock, err := r.takeLock(cluster, fmt.Sprintf("bouncing processes: %v", addresses))
	if !hasLock {
		return &requeue{curError: err}
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

	filteredAddresses, removedAddresses := filterIgnoredProcessGroups(cluster, addresses)
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
func getProcessesReadyForRestart(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, addressMap map[string][]fdbv1beta2.ProcessAddress, upgradedProcesses int) ([]fdbv1beta2.ProcessAddress, *requeue) {
	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
	allSynced := true
	var missingAddress []string

	filterConditions := restarts.GetFilterConditions(cluster)
	var missingProcesses int
	for _, processGroup := range cluster.Status.ProcessGroups {
		if cluster.SkipProcessGroup(processGroup) || processGroup.IsMarkedForRemoval() {
			continue
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

		if !processGroup.MatchesConditions(filterConditions) {
			logger.Info("ignore process group with non matching conditions", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if addressMap[processGroup.ProcessGroupID] == nil {
			missingAddress = append(missingAddress, processGroup.ProcessGroupID)
			continue
		}

		addresses = append(addresses, addressMap[processGroup.ProcessGroupID]...)

		if processGroup.GetConditionTime(fdbv1beta2.IncorrectConfigMap) != nil {
			allSynced = false
			logger.Info("Waiting for dynamic Pod config update", "processGroupID", processGroup.ProcessGroupID)
		}
	}

	if len(missingAddress) > 0 {
		return nil, &requeue{message: fmt.Sprintf("could not find address for processes: %s", missingAddress), delayedRequeue: true}
	}

	if !allSynced {
		return nil, &requeue{message: "Waiting for config map to sync to all pods", delayedRequeue: true}
	}

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
	if cluster.IsBeingUpgraded() && (counts.Total()-missingProcesses-upgradedProcesses) != len(addresses) {
		return nil, &requeue{
			message:        fmt.Sprintf("expected %d processes, got %d processes ready to restart", counts.Total(), len(addresses)),
			delayedRequeue: true,
		}
	}

	return addresses, nil
}

// getAddressesForUpgrade checks that all processes in a cluster are ready to be
// upgraded and returns the full list of addresses.
func getAddressesForUpgrade(logger logr.Logger, r *FoundationDBClusterReconciler, databaseStatus *fdbv1beta2.FoundationDBStatus, lockClient fdbadminclient.LockClient, cluster *fdbv1beta2.FoundationDBCluster, version fdbv1beta2.Version) ([]fdbv1beta2.ProcessAddress, *requeue) {
	pendingUpgrades, err := lockClient.GetPendingUpgrades(version)
	if err != nil {
		return nil, &requeue{curError: err}
	}

	if !internal.HasDesiredFaultToleranceFromStatus(logger, databaseStatus, cluster) {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpgradeRequeued", "Database is unavailable or doesn't have expected fault tolerance")
		return nil, &requeue{message: "Deferring upgrade until database is available or expected fault tolerance is met"}
	}

	notReadyProcesses := make([]string, 0)
	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(databaseStatus.Cluster.Processes))
	for _, process := range databaseStatus.Cluster.Processes {
		processID := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if process.Version == version.String() {
			continue
		}
		if pendingUpgrades[processID] {
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

// filterIgnoredProcessGroups removes all addresses from the addresses slice that are associated with a process group that should be ignored
// during a restart.
func filterIgnoredProcessGroups(cluster *fdbv1beta2.FoundationDBCluster, addresses []fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, bool) {
	if len(cluster.Spec.Buggify.IgnoreDuringRestart) == 0 {
		return addresses, false
	}

	ignoredIDs := make(map[string]fdbv1beta2.None, len(cluster.Spec.Buggify.IgnoreDuringRestart))
	ignoredAddresses := make(map[string]fdbv1beta2.None, len(cluster.Spec.Buggify.IgnoreDuringRestart))

	for _, id := range cluster.Spec.Buggify.IgnoreDuringRestart {
		ignoredIDs[id] = fdbv1beta2.None{}
	}

	for _, processGroup := range cluster.Status.ProcessGroups {
		if _, ok := ignoredIDs[processGroup.ProcessGroupID]; !ok {
			continue
		}

		for _, address := range processGroup.Addresses {
			ignoredAddresses[address] = fdbv1beta2.None{}
		}
	}

	filteredAddresses := make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(ignoredAddresses))
	removedAddresses := false
	for _, address := range addresses {
		if _, ok := ignoredAddresses[address.MachineAddress()]; ok {
			removedAddresses = true
			continue
		}

		filteredAddresses = append(filteredAddresses, address)
	}

	return filteredAddresses, removedAddresses
}
