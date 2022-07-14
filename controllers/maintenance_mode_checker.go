/*
 * maintenance_mode_.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// updatePods provides a reconciliation step for recreating pods with new pod
// specs.
type maintenanceModeChecker struct{}

var podUpCheckDelayInSeconds = 60

// reconcile runs the reconciler's work.
func (maintenanceModeChecker) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "maintenanceModeChecker")

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r.Client)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	maintenanceZone, err := adminClient.GetMaintenanceZone()
	if err != nil {
		return &requeue{curError: err}
	}
	// Cluster is not in maintenance mode
	if maintenanceZone == "" {
		if cluster.Status.MaintenanceModeInfo.ZoneId != "" {
			cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{}
			err = r.Status().Update(ctx, cluster)
			if err != nil {
				return &requeue{curError: err}
			}
		}
		return nil
	}
	// FDB Cluster is in maintenance mode but not due to this operator actions
	if maintenanceZone != cluster.Status.MaintenanceModeInfo.ZoneId {
		if cluster.Status.MaintenanceModeInfo.ZoneId != "" {
			cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{}
			err = r.Status().Update(ctx, cluster)
			if err != nil {
				return &requeue{curError: err}
			}
		}
		return nil
	}
	logger.Info("Cluster in maintenance mode", "zone", maintenanceZone)
	// FDB Cluster is in maintenance mode due to this operator actions
	if cluster.Status.MaintenanceModeInfo.StartTimestamp.Add(time.Duration(podUpCheckDelayInSeconds) * time.Second).After(time.Now()) {
		return &requeue{message: "Waiting for delay to expire for checking if pods under maintenance mode have been bounced", delayedRequeue: true}
	}
	status, err := adminClient.GetStatus()
	if err != nil {
		return &requeue{curError: err}
	}
	processGroupsToCheck := make(map[string]struct{})
	for _, id := range cluster.Status.MaintenanceModeInfo.ProcessGroups {
		processGroupsToCheck[id] = struct{}{}
	}
	for _, process := range status.Cluster.Processes {
		delete(processGroupsToCheck, process.Locality["instance_id"])
	}
	// Some of the pods are not yet up
	if len(processGroupsToCheck) != 0 {
		return &requeue{message: fmt.Sprintf("Waiting for all proceeses in zone %s to be up", maintenanceZone), delayedRequeue: true}
	}
	// All the pods for this zone under maintenance are up
	logger.Info("Switching off maintenance mode", "zone", maintenanceZone)
	err = adminClient.ResetMaintenanceMode()
	if err != nil {
		return &requeue{curError: err}
	}
	cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{}
	err = r.Status().Update(ctx, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	return nil
}
