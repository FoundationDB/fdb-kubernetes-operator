/*
 * maintenance_mode_checker.go
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
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal/maintenance"
	"github.com/go-logr/logr"
	"time"
)

// maintenanceModeChecker provides a reconciliation step for clearing the maintenance mode if all the processes in the current maintenance zone have been restarted.
type maintenanceModeChecker struct{}

// reconcile runs the reconciler's work.
func (maintenanceModeChecker) reconcile(_ context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	if !cluster.ResetMaintenanceMode() {
		return nil
	}

	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	// If the cluster is not available we skip any further checks.
	if !status.Client.DatabaseStatus.Available {
		return &requeue{message: "cluster is not available", delayedRequeue: true}
	}

	// Get all the processes that are currently under maintenance based on the information stored in FDB.
	processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	logger.Info("Cluster in maintenance mode", "zone", status.Cluster.MaintenanceZone, "processesUnderMaintenance", processesUnderMaintenance)

	// Get all the maintenance information from the FDB cluster.
	finishedMaintenance, staleMaintenanceInformation, processesToUpdate := maintenance.GetMaintenanceInformation(logger, status, processesUnderMaintenance, r.MaintenanceListStaleDuration, r.MaintenanceListWaitDuration)
	logger.Info("maintenance information", "finishedMaintenance", finishedMaintenance, "staleMaintenanceInformation", staleMaintenanceInformation, "processesToUpdate", processesToUpdate)

	// We can remove the information for all the finished maintenance and the stale entries.
	if len(finishedMaintenance) > 0 || len(staleMaintenanceInformation) > 0 {
		// Remove all the processes that finished maintenance and all the stale information.
		err = adminClient.RemoveProcessesUnderMaintenance(append(finishedMaintenance, staleMaintenanceInformation...))
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	// If no maintenance zone is active, we can ignore all further steps to reset the maintenance zone.
	if status.Cluster.MaintenanceZone == "" {
		return nil
	}

	// Some of the processes are not yet restarted.
	if len(processesToUpdate) > 0 {
		return &requeue{message: fmt.Sprintf("Waiting for %d processes in zone %s to be updated", len(processesToUpdate), status.Cluster.MaintenanceZone), delayedRequeue: true, delay: 5 * time.Second}
	}

	// Make sure we take a lock before we continue.
	hasLock, err := r.takeLock(logger, cluster, "maintenance mode check")
	if !hasLock {
		return &requeue{curError: err}
	}

	defer func() {
		lockErr := r.releaseLock(logger, cluster)
		if lockErr != nil {
			logger.Error(lockErr, "could not release lock")
		}
	}()

	logger.Info("Switching off maintenance mode", "zone", status.Cluster.MaintenanceZone)
	err = adminClient.ResetMaintenanceMode()
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	return nil
}
