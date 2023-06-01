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
)

// maintenanceModeChecker provides a reconciliation step for clearing the maintenance mode if all the pods in the current maintenance zone are up.
type maintenanceModeChecker struct{}

// reconcile runs the reconciler's work.
func (maintenanceModeChecker) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "maintenanceModeChecker")

	if !cluster.UseMaintenaceMode() {
		return nil
	}

	if cluster.Status.MaintenanceModeInfo.ZoneID == "" {
		return nil
	}

	logger.Info("Cluster in maintenance mode", "zone", cluster.Status.MaintenanceModeInfo.ZoneID)

	var processGroupsToUpdate int
	var hasProcessGroupsInZone bool
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.FaultDomain != cluster.Status.MaintenanceModeInfo.ZoneID {
			continue
		}

		hasProcessGroupsInZone = true

		// If a process group has any conditions like IncorrectPodSpec or MissingProcess, that means the update is not
		// yet done and we have to wait until this process group is in the desired state.
		if len(processGroup.ProcessGroupConditions) > 0 {
			processGroupsToUpdate++
		}
	}

	// If none of the process groups are in the specified zone, that means a different operator or a human operator has set the maintenance mode.
	if !hasProcessGroupsInZone {
		return nil
	}

	// Some of the pods are not yet up
	if processGroupsToUpdate > 0 {
		return &requeue{message: fmt.Sprintf("Waiting for %d process groups in zone %s to be updated", processGroupsToUpdate, cluster.Status.MaintenanceModeInfo.ZoneID), delayedRequeue: true}
	}

	// All the Pods for this zone under maintenance are up
	hasLock, err := r.takeLock(cluster, "maintenance mode check")
	if !hasLock {
		return &requeue{curError: err}
	}

	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	logger.Info("Switching off maintenance mode", "zone", cluster.Status.MaintenanceModeInfo.ZoneID)
	err = adminClient.ResetMaintenanceMode()
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}
	cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{}
	err = r.updateOrApply(ctx, cluster)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	return nil
}
