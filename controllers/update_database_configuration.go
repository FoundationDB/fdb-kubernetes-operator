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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/pointer"

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
	var currentConfiguration fdbtypes.DatabaseConfiguration

	status, err := adminClient.GetStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	initialConfig := !cluster.Status.Configured
	dataState := status.Cluster.Data.State

	if !(initialConfig || status.Client.DatabaseStatus.Available) {
		logger.Info("Skipping database configuration change because database is unavailable")
		return nil
	}

	currentConfiguration = status.Cluster.DatabaseConfiguration.NormalizeConfigurationWithSeparatedProxies(cluster.Spec.Version, cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured())
	// We have to reset the excluded servers here otherwise we will trigger a reconfiguration if one or more servers
	// are excluded.
	currentConfiguration.ExcludedServers = nil
	cluster.ClearMissingVersionFlags(&currentConfiguration)

	if initialConfig || !equality.Semantic.DeepEqual(desiredConfiguration, currentConfiguration) {
		var nextConfiguration fdbtypes.DatabaseConfiguration
		if initialConfig {
			nextConfiguration = desiredConfiguration
		} else {
			nextConfiguration = currentConfiguration.GetNextConfigurationChange(desiredConfiguration)
		}
		configurationString, _ := nextConfiguration.GetConfigurationString(cluster.Spec.Version)

		if !(initialConfig || dataState.Healthy) {
			logger.Info("Waiting for data distribution to be healthy", "stateName", dataState.Name, "stateDescription", dataState.Description)
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but data distribution is not fully healthy: %s (%s)", configurationString, dataState.Name, dataState.Description))
			return nil
		}

		if !pointer.BoolDeref(cluster.Spec.AutomationOptions.ConfigureDatabase, true) {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but configuration changes are disabled", configurationString))
			cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
			err = r.updateOrApply(ctx, cluster)
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

		logger.Info("Configuring database", "current configuration", currentConfiguration, "desired configuration", desiredConfiguration)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(nextConfiguration, initialConfig, cluster.Spec.Version)
		if err != nil {
			return &requeue{curError: err}
		}
		if initialConfig {
			cluster.Status.Configured = true
			err = r.updateOrApply(ctx, cluster)
			if err != nil {
				return &requeue{curError: err}
			}
			return nil
		}
		logger.Info("Configured database")

		if !equality.Semantic.DeepEqual(nextConfiguration, desiredConfiguration) {
			return &requeue{message: "Requeuing for next stage of database configuration change"}
		}
	}

	return nil
}
