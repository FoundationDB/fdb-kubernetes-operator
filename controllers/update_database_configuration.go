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

	"github.com/go-logr/logr"

	"k8s.io/utils/pointer"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// updateDatabaseConfiguration provides a reconciliation step for changing the
// database configuration.
type updateDatabaseConfiguration struct{}

// reconcile runs the reconciler's work.
func (u updateDatabaseConfiguration) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus, logger logr.Logger) *requeue {
	if !pointer.BoolDeref(cluster.Spec.AutomationOptions.ConfigureDatabase, true) {
		return nil
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}
	defer adminClient.Close()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	initialConfig := !cluster.Status.Configured
	if !(initialConfig || status.Client.DatabaseStatus.Available) {
		logger.Info("Skipping database configuration change because database is unavailable")
		return nil
	}

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	desiredConfiguration.RoleCounts.Storage = 0
	currentConfiguration := status.Cluster.DatabaseConfiguration.NormalizeConfigurationWithSeparatedProxies(cluster.Spec.Version, cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured())
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

		dataState := status.Cluster.Data.State
		if !(initialConfig || dataState.Healthy) {
			logger.Info("Waiting for data distribution to be healthy", "stateName", dataState.Name, "stateDescription", dataState.Description)
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but data distribution is not fully healthy: %s (%s)", configurationString, dataState.Name, dataState.Description))
			return nil
		}

		if !initialConfig {
			hasLock, err := r.takeLock(logger, cluster,
				fmt.Sprintf("reconfiguring the database to `%s`", configurationString))
			if !hasLock {
				return &requeue{curError: err, delayedRequeue: true}
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
			return nil
		}
		logger.Info("Configured database")

		if !equality.Semantic.DeepEqual(nextConfiguration, desiredConfiguration) {
			return &requeue{message: "Requeuing for next stage of database configuration change", delayedRequeue: true}
		}
	}

	return nil
}
