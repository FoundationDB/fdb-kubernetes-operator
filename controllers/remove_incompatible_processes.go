/*
 * remove_incompatible_processes.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
)

// removeIncompatibleProcesses is a reconciler that will restart incompatible fdbserver processes, this can happen
// during an upgrade when the kill command doesn't reach all processes, see: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1281
type removeIncompatibleProcesses struct{}

// reconcile runs the reconciler's work.
func (removeIncompatibleProcesses) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "removeIncompatibleProcesses")
	err := processIncompatibleProcesses(ctx, r, logger, cluster)

	if err != nil {
		return &requeue{curError: err, delay: 15 * time.Second, delayedRequeue: true}
	}

	return nil
}

func processIncompatibleProcesses(ctx context.Context, r *FoundationDBClusterReconciler, logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) error {
	if !r.EnableRestartIncompatibleProcesses {
		logger.Info("skipping disabled subreconciler")
		return nil
	}

	if !cluster.Status.Configured {
		logger.Info("waiting for cluster to be configured")
		return nil
	}

	if cluster.IsBeingUpgraded() {
		logger.Info("waiting for cluster to be upgraded before checking for incompatible connections")
		return nil
	}

	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return err
	}

	podMap := internal.CreatePodMap(cluster, pods)

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r.Client)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		// If we hit a timeout issue we don't want to block any further steps.
		if internal.IsTimeoutError(err) {
			return nil
		}
		return err
	}

	if len(status.Cluster.IncompatibleConnections) == 0 {
		return nil
	}

	// Wait until the cluster is running for the minimum uptime before looking for incompatible processes.
	minimumUptime, _, err := internal.GetMinimumUptimeAndAddressMap(cluster, status, r.EnableRecoveryState)
	if err != nil {
		return err
	}

	if minimumUptime < float64(cluster.GetMinimumUptimeSecondsForBounce()) {
		logger.V(1).Info("Skipping reconciler and waiting until cluster is for up minimum uptime")
		return nil
	}

	// Ensure the cluster is running at fault tolerance before recreating Pods.
	hasDesiredFaultTolerance := internal.HasDesiredFaultToleranceFromStatus(logger, status, cluster)
	if !hasDesiredFaultTolerance {
		logger.V(1).Info("Skipping reconciler and waiting until cluster has desired fault tolerance")
		return nil
	}

	logger.Info("incompatible connections", "incompatibleConnections", status.Cluster.IncompatibleConnections)
	incompatibleConnections := parseIncompatibleConnections(logger, status.Cluster.IncompatibleConnections)

	incompatiblePods := make([]*corev1.Pod, 0, len(incompatibleConnections))
	for _, processGroup := range cluster.Status.ProcessGroups {
		pod, ok := podMap[processGroup.ProcessGroupID]
		if !ok || pod == nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if pod.DeletionTimestamp != nil {
			logger.V(1).Info("Skipping Pod that is already marked for deletion",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if isIncompatible(incompatibleConnections, processGroup) {
			logger.Info("recreate Pod for process group with incompatible version", "processGroupID", processGroup.ProcessGroupID, "address", processGroup.Addresses)
			incompatiblePods = append(incompatiblePods, pod)
		}
	}

	// Do an unsafe update of the Pods since they are not reachable anyway
	return r.PodLifecycleManager.UpdatePods(ctx, r, cluster, incompatiblePods, true)
}

// parseIncompatibleConnections parses the incompatible connections string slice to a map
func parseIncompatibleConnections(logger logr.Logger, incompatibleConnections []string) map[string]fdbv1beta2.None {
	result := make(map[string]fdbv1beta2.None)
	for _, incompatibleAddress := range incompatibleConnections {
		address, err := fdbv1beta2.ParseProcessAddress(incompatibleAddress)
		if err != nil {
			logger.Error(err, "could not parse address in incompatible connections", "address", incompatibleAddress)
			continue
		}

		if address.Port == 0 {
			logger.V(1).Info("Ignore incompatible connection with port 0", "address", address)
			continue
		}

		result[address.IPAddress.String()] = fdbv1beta2.None{}
	}

	return result
}

// isIncompatible checks if the process group is in the list of incompatible connections.
func isIncompatible(incompatibleConnections map[string]fdbv1beta2.None, processGroup *fdbv1beta2.ProcessGroupStatus) bool {
	for _, address := range processGroup.Addresses {
		if _, ok := incompatibleConnections[address]; ok {
			return true
		}
	}

	return false
}
