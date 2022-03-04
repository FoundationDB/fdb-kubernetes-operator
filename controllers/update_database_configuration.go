/*
 * update_database_configuration.go
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
	"reflect"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// updateDatabaseConfiguration provides a reconciliation step for changing the
// database configuration.
type updateDatabaseConfiguration struct{}

// reconcile runs the reconciler's work.
func (u updateDatabaseConfiguration) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updateDatabaseConfiguration")
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)

	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	desiredConfiguration.RoleCounts.Storage = 0
	needsChange := false
	var currentConfiguration fdb.DatabaseConfiguration

	status, err := adminClient.GetStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	initialConfig := !cluster.Status.Configured

	available := initialConfig || status.Client.DatabaseStatus.Available
	dataState := status.Cluster.Data.State
	dataHealthy := initialConfig || dataState.Healthy

	if !available {
		logger.Info("Skipping database configuration change because database is unavailable")
		return nil
	}

	currentConfiguration = status.Cluster.DatabaseConfiguration.NormalizeConfiguration()
	cluster.ClearMissingVersionFlags(&currentConfiguration)
	needsChange = initialConfig || !reflect.DeepEqual(desiredConfiguration, currentConfiguration)

	if needsChange {
		var nextConfiguration fdb.DatabaseConfiguration
		if initialConfig {
			nextConfiguration = desiredConfiguration
		} else {
			nextConfiguration = currentConfiguration.GetNextConfigurationChange(desiredConfiguration)
		}
		configurationString, _ := nextConfiguration.GetConfigurationString()
		var enabled = cluster.Spec.AutomationOptions.ConfigureDatabase

		if !dataHealthy {
			logger.Info("Waiting for data distribution to be healthy", "stateName", dataState.Name, "stateDescription", dataState.Description)
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but data distribution is not fully healthy: %s (%s)", configurationString, dataState.Name, dataState.Description))
			return nil
		}

		if enabled != nil && !*enabled {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but configuration changes are disabled", configurationString))
			cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
			err = r.Status().Update(ctx, cluster)
			if err != nil {
				logger.Error(err, "Error updating cluster status")
			}
			return &requeue{message: "Database configuration changes are disabled"}
		}

		if !initialConfig {
			hasLock, err := r.takeLock(cluster,
				fmt.Sprintf("reconfiguring the database to `%s`", configurationString))
			if !hasLock {
				return &requeue{curError: err}
			}
		}

		logger.Info("Configuring database")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(nextConfiguration, initialConfig)
		if err != nil {
			return &requeue{curError: err}
		}
		if initialConfig {
			cluster.Status.Configured = true
			err = r.Status().Update(ctx, cluster)
			if err != nil {
				return &requeue{curError: err}
			}
			return nil
		}
		logger.Info("Configured database")

		if !reflect.DeepEqual(nextConfiguration, desiredConfiguration) {
			logger.Info("Requeuing for next stage of database configuration change")
			return &requeue{message: "Requeuing for next stage of database configuration change"}
		}
	}

	return nil
}
