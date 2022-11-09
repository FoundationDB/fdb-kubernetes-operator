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
	"github.com/FoundationDB/fdb-kubernetes-operator/internal/safeguards"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
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

	addresses, req := getProcessesReadyForRestart(ctx, logger, r, cluster, addressMap)
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
			message:        "Cluster needs to stabilize before bouncing",
			delay:          time.Second * time.Duration(cluster.GetMinimumUptimeSecondsForBounce()-int(minimumUptime)),
			delayedRequeue: true,
		}
	}

	var lockClient fdbadminclient.LockClient
	useLocks := cluster.ShouldUseLocks()
	if useLocks {
		lockClient, err = r.getLockClient(cluster)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}
	version, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	upgrading := cluster.Status.RunningVersion != cluster.Spec.Version

	if useLocks && upgrading {
		// Ignore all Process groups that are not reachable and therefore will not get any ConfigMap updates. Otherwise
		// a single Pod might block the whole upgrade. The assumption here is that the Pod will be recreated with the
		// new version in the removeIncompatibleProcesses reconciler. This can still be an issue in the current
		// implementation since the AddPendingUpgrades call will only add new pending process groups but never remove
		// "old" ones (if the process is missing/not part of the FoundationDB status the process will be ignored). Once
		// we move to a more centralized upgrade process this problem will be solved.
		processGroupIDs := fdbv1beta2.FilterByConditions(cluster.Status.ProcessGroups, map[fdbv1beta2.ProcessGroupConditionType]bool{
			fdbv1beta2.SidecarUnreachable: false,
		}, false)

		desiredProcesses, err := cluster.GetProcessCountsWithDefaults()
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		err = safeguards.HasEnoughProcessesToUpgrade(processGroupIDs, cluster.Status.ProcessGroups, desiredProcesses)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		err = lockClient.AddPendingUpgrades(version, processGroupIDs)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	hasLock, err := r.takeLock(cluster, fmt.Sprintf("bouncing processes: %v", addresses))
	if !hasLock {
		return &requeue{curError: err, delayedRequeue: true}
	}

	if useLocks && upgrading {
		var req *requeue
		addresses, req = getAddressesForUpgrade(logger, r, status, lockClient, cluster, version)
		if req != nil {
			return req
		}
		if addresses == nil {
			return &requeue{curError: fmt.Errorf("unknown error when getting addresses that are ready for upgrade"), delayedRequeue: true}
		}
	}

	logger.Info("Bouncing processes", "addresses", addresses, "upgrading", upgrading)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "BouncingProcesses", fmt.Sprintf("Bouncing processes: %v", addresses))
	err = adminClient.KillProcesses(addresses)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
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
func getProcessesReadyForRestart(ctx context.Context, logger logr.Logger, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, addressMap map[string][]fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, *requeue) {
	processesToBounce := fdbv1beta2.FilterByConditions(cluster.Status.ProcessGroups, map[fdbv1beta2.ProcessGroupConditionType]bool{
		fdbv1beta2.IncorrectCommandLine: true,
		fdbv1beta2.IncorrectPodSpec:     false,
		fdbv1beta2.SidecarUnreachable:   false, // ignore all Process groups that are not reachable and therefore will not get any ConfigMap updates.
	}, true)

	addresses := make([]fdbv1beta2.ProcessAddress, 0, len(processesToBounce))
	allSynced := true
	var missingAddress []string

	for _, process := range processesToBounce {
		processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, process)
		if cluster.SkipProcessGroup(processGroup) {
			continue
		}

		// Ignore processes that are missing for more than 30 seconds e.g. if the process is network partitioned.
		// This is required since the update status will not update the SidecarUnreachable setting if a process is
		// missing in the status.
		if missingTime := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses); missingTime != nil {
			if time.Unix(*missingTime, 0).Add(cluster.GetIgnoreMissingProcessesSeconds()).Before(time.Now()) {
				continue
			}
		}

		if addressMap[process] == nil {
			missingAddress = append(missingAddress, process)
			continue
		}

		addresses = append(addresses, addressMap[process]...)

		processGroupID := podmanager.GetProcessGroupIDFromProcessID(process)
		pod, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetSinglePodListOptions(cluster, processGroupID)...)
		if err != nil {
			return nil, &requeue{curError: err}
		}
		if len(pod) == 0 {
			return nil, &requeue{message: fmt.Sprintf("No pod defined for process group ID: \"%s\"", processGroupID), delay: podSchedulingDelayDuration}
		}

		synced, err := r.updatePodDynamicConf(cluster, pod[0])
		if !synced {
			allSynced = false
			logger.Info("Update dynamic Pod config", "processGroupID", processGroupID, "synced", synced, "error", err)
		}
	}

	if len(missingAddress) > 0 {
		return nil, &requeue{curError: fmt.Errorf("could not find address for processes: %s", missingAddress), delayedRequeue: true}
	}

	if !allSynced {
		return nil, &requeue{message: "Waiting for config map to sync to all pods", delayedRequeue: true}
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

	if !databaseStatus.Client.DatabaseStatus.Available {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpgradeRequeued", "Database is unavailable")
		return nil, &requeue{message: "Deferring upgrade until database is available"}
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
