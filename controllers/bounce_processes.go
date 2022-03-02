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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"
	"math"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// bounceProcesses provides a reconciliation step for bouncing fdbserver
// processes.
type bounceProcesses struct{}

// reconcile runs the reconciler's work.
func (bounceProcesses) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
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

	minimumUptime := math.Inf(1)
	addressMap := make(map[string][]fdb.ProcessAddress, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		addressMap[process.Locality["instance_id"]] = append(addressMap[process.Locality["instance_id"]], process.Address)

		if process.UptimeSeconds < minimumUptime {
			minimumUptime = process.UptimeSeconds
		}
	}

	processesToBounce := fdbtypes.FilterByConditions(cluster.Status.ProcessGroups, map[fdbtypes.ProcessGroupConditionType]bool{
		fdbtypes.IncorrectCommandLine: true,
		fdbtypes.IncorrectPodSpec:     false,
	}, true)

	addresses := make([]fdb.ProcessAddress, 0, len(processesToBounce))
	allSynced := true
	var missingAddress []string

	for _, process := range processesToBounce {
		if cluster.SkipProcessGroup(fdbtypes.FindProcessGroupByID(cluster.Status.ProcessGroups, process)) {
			continue
		}

		if addressMap[process] == nil {
			missingAddress = append(missingAddress, process)
			continue
		}

		addresses = append(addresses, addressMap[process]...)

		processGroupID := podmanager.GetProcessGroupIDFromProcessID(process)
		pod, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetSinglePodListOptions(cluster, processGroupID)...)
		if err != nil {
			return &requeue{curError: err}
		}
		if len(pod) == 0 {
			return &requeue{message: fmt.Sprintf("No pod defined for process group ID: \"%s\"", processGroupID), delay: podSchedulingDelayDuration}
		}

		synced, err := r.updatePodDynamicConf(cluster, pod[0])
		if !synced {
			allSynced = false
			logger.Info("Update dynamic Pod config", "processGroupID", processGroupID, "synced", synced, "error", err)
		}
	}

	if len(missingAddress) > 0 {
		return &requeue{curError: fmt.Errorf("could not find address for processes: %s", missingAddress)}
	}

	if !allSynced {
		return &requeue{message: "Waiting for config map to sync to all pods"}
	}

	upgrading := cluster.Status.RunningVersion != cluster.Spec.Version

	if len(addresses) > 0 {
		if !pointer.BoolDeref(cluster.Spec.AutomationOptions.KillProcesses, true) {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsBounce",
				"Spec require a bounce of some processes, but killing processes is disabled")
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.Status().Update(ctx, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}

			return &requeue{message: "Kills are disabled"}
		}

		if minimumUptime < float64(cluster.Spec.MinimumUptimeSecondsForBounce) {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsBounce",
				fmt.Sprintf("Spec require a bounce of some processes, but the cluster has only been up for %f seconds", minimumUptime))
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.Status().Update(ctx, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}

			// Retry after we waited the minimum uptime
			return &requeue{
				message: "Cluster needs to stabilize before bouncing",
				delay:   time.Second * time.Duration(cluster.Spec.MinimumUptimeSecondsForBounce-int(minimumUptime)),
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
		version, err := fdb.ParseFdbVersion(cluster.Spec.Version)
		if err != nil {
			return &requeue{curError: err}
		}

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
			addresses, req = getAddressesForUpgrade(r, adminClient, lockClient, cluster, version)
			if req != nil {
				return req
			}
			if addresses == nil {
				return &requeue{curError: fmt.Errorf("unknown error when getting addresses that are ready for upgrade")}
			}
		}

		logger.Info("Bouncing processes", "addresses", addresses, "upgrading", upgrading)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "BouncingProcesses", fmt.Sprintf("Bouncing processes: %v", addresses))
		err = adminClient.KillProcesses(addresses)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	if upgrading {
		cluster.Status.RunningVersion = cluster.Spec.Version
		err = r.Status().Update(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}

// getAddressesForUpgrade checks that all processes in a cluster are ready to be
// upgraded and returns the full list of addresses.
func getAddressesForUpgrade(r *FoundationDBClusterReconciler, adminClient fdbadminclient.AdminClient, lockClient fdbadminclient.LockClient, cluster *fdbtypes.FoundationDBCluster, version fdb.FdbVersion) ([]fdb.ProcessAddress, *requeue) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "bounceProcesses")
	pendingUpgrades, err := lockClient.GetPendingUpgrades(version)
	if err != nil {
		return nil, &requeue{curError: err}
	}

	databaseStatus, err := adminClient.GetStatus()
	if err != nil {
		return nil, &requeue{curError: err}
	}

	if !databaseStatus.Client.DatabaseStatus.Available {
		logger.Info("Deferring upgrade until database is available")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpgradeRequeued", "Database is unavailable")
		return nil, &requeue{message: "Deferring upgrade until database is available"}
	}

	notReadyProcesses := make([]string, 0)
	addresses := make([]fdb.ProcessAddress, 0, len(databaseStatus.Cluster.Processes))
	for _, process := range databaseStatus.Cluster.Processes {
		processID := process.Locality["instance_id"]
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
