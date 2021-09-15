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
	ctx "context"
	"fmt"
	"math"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// BounceProcesses provides a reconciliation step for bouncing fdbserver
// processes.
type BounceProcesses struct{}

// Reconcile runs the reconciler's work.
func (b BounceProcesses) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "BounceProcesses")
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &Requeue{Error: err}
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		return &Requeue{Error: err}
	}

	minimumUptime := math.Inf(1)
	addressMap := make(map[string][]fdbtypes.ProcessAddress, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		addressMap[process.Locality["instance_id"]] = append(addressMap[process.Locality["instance_id"]], process.Address)

		if process.UptimeSeconds < minimumUptime {
			minimumUptime = process.UptimeSeconds
		}
	}

	processesToBounce := fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, fdbtypes.IncorrectCommandLine, true)
	addresses := make([]fdbtypes.ProcessAddress, 0, len(processesToBounce))
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

		instanceID := GetProcessGroupIDFromProcessID(process)
		pod, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetSinglePodListOptions(cluster, instanceID)...)
		if err != nil {
			return &Requeue{Error: err}
		}
		if len(pod) == 0 {
			return &Requeue{Message: fmt.Sprintf("No pod defined for instance %s", instanceID), Delay: podSchedulingDelayDuration}
		}

		synced, err := r.updatePodDynamicConf(cluster, pod[0])
		if !synced {
			allSynced = false
			logger.Info("Update dynamic Pod config", "processGroupID", instanceID, "synced", synced, "error", err)
		}
	}

	if len(missingAddress) > 0 {
		return &Requeue{Error: fmt.Errorf("could not find address for processes: %s", missingAddress)}
	}

	if !allSynced {
		return &Requeue{Message: "Waiting for config map to sync to all pods"}
	}

	upgrading := cluster.Status.RunningVersion != cluster.Spec.Version

	if len(addresses) > 0 {
		var enabled = cluster.Spec.AutomationOptions.KillProcesses
		if enabled != nil && !*enabled {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsBounce",
				"Spec require a bounce of some processes, but killing processes is disabled")
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}

			return &Requeue{Message: "Kills are disabled"}
		}

		if minimumUptime < float64(cluster.Spec.MinimumUptimeSecondsForBounce) {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsBounce",
				fmt.Sprintf("Spec require a bounce of some processes, but the cluster has only been up for %f seconds", minimumUptime))
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}

			// Retry after we waited the minimum uptime
			return &Requeue{
				Message: "Cluster needs to stabilize before bouncing",
				Delay:   time.Second * time.Duration(cluster.Spec.MinimumUptimeSecondsForBounce-int(minimumUptime)),
			}
		}

		var lockClient LockClient
		useLocks := cluster.ShouldUseLocks()
		if useLocks {
			lockClient, err = r.getLockClient(cluster)
			if err != nil {
				return &Requeue{Error: err}
			}
		}
		version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
		if err != nil {
			return &Requeue{Error: err}
		}

		if useLocks && upgrading {
			processGroupIDs := make([]string, 0, len(cluster.Status.ProcessGroups))
			for _, processGroup := range cluster.Status.ProcessGroups {
				processGroupIDs = append(processGroupIDs, processGroup.ProcessGroupID)
			}
			err = lockClient.AddPendingUpgrades(version, processGroupIDs)
			if err != nil {
				return &Requeue{Error: err}
			}
		}

		hasLock, err := r.takeLock(cluster, fmt.Sprintf("bouncing processes: %v", addresses))
		if !hasLock {
			return &Requeue{Error: err}
		}

		if useLocks && upgrading {
			var requeue *Requeue
			addresses, requeue = getAddressesForUpgrade(r, adminClient, lockClient, cluster, version)
			if requeue != nil {
				return requeue
			}
			if addresses == nil {
				return &Requeue{Error: fmt.Errorf("unknown error when getting addresses that are ready for upgrade")}
			}
		}

		logger.Info("Bouncing instances", "addresses", addresses)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "BouncingInstances", fmt.Sprintf("Bouncing processes: %v", addresses))
		err = adminClient.KillInstances(addresses)
		if err != nil {
			return &Requeue{Error: err}
		}
	}

	if upgrading {
		cluster.Status.RunningVersion = cluster.Spec.Version
		err = r.Status().Update(context, cluster)
		if err != nil {
			return &Requeue{Error: err}
		}
	}

	return nil
}

// getAddressesForUpgrade checks that all processes in a cluster are ready to be
// upgraded and returns the full list of addresses.
func getAddressesForUpgrade(r *FoundationDBClusterReconciler, adminClient AdminClient, lockClient LockClient, cluster *fdbtypes.FoundationDBCluster, version fdbtypes.FdbVersion) ([]fdbtypes.ProcessAddress, *Requeue) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "BounceProcesses")
	pendingUpgrades, err := lockClient.GetPendingUpgrades(version)
	if err != nil {
		return nil, &Requeue{Error: err}
	}

	databaseStatus, err := adminClient.GetStatus()
	if err != nil {
		return nil, &Requeue{Error: err}
	}

	if !databaseStatus.Client.DatabaseStatus.Available {
		logger.Info("Deferring upgrade until database is available")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpgradeRequeued", "Database is unavailable")
		return nil, &Requeue{Message: "Deferring upgrade until database is available"}
	}

	notReadyProcesses := make([]string, 0)
	addresses := make([]fdbtypes.ProcessAddress, 0, len(databaseStatus.Cluster.Processes))
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
		return nil, &Requeue{Message: message}
	}
	err = lockClient.ClearPendingUpgrades()
	if err != nil {
		return nil, &Requeue{Error: err}
	}

	return addresses, nil
}
