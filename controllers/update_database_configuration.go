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
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"

	"github.com/go-logr/logr"

	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// updateDatabaseConfiguration provides a reconciliation step for changing the
// database configuration.
type updateDatabaseConfiguration struct{}

// reconcile runs the reconciler's work.
func (u updateDatabaseConfiguration) reconcile(_ context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
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

	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	if initialConfig || !equality.Semantic.DeepEqual(desiredConfiguration, currentConfiguration) {
		var nextConfiguration fdbv1beta2.DatabaseConfiguration
		if initialConfig {
			nextConfiguration = desiredConfiguration
		} else {
			nextConfiguration = currentConfiguration.GetNextConfigurationChange(desiredConfiguration)
		}
		configurationString, _ := nextConfiguration.GetConfigurationString(cluster.Spec.Version)

		if !initialConfig {
			err = fdbstatus.ConfigurationChangeAllowed(status, runningVersion.SupportsRecoveryState() && r.EnableRecoveryState)
			if err != nil {
				logger.Info("Changing current configuration is not safe", "error", err, "current configuration", currentConfiguration, "desired configuration", desiredConfiguration)
				r.Recorder.Event(cluster, corev1.EventTypeNormal, "NeedsConfigurationChange",
					fmt.Sprintf("Spec require configuration change to `%s`, but configuration change is not safe: %s", configurationString, err.Error()))
				return &requeue{message: "Configuration change is not safe, retry later", delayedRequeue: true, delay: 10 * time.Second}
			}
		}

		if !initialConfig {
			hasLock, err := r.takeLock(logger, cluster,
				fmt.Sprintf("reconfiguring the database to `%s`", configurationString))
			if !hasLock {
				return &requeue{curError: err, delayedRequeue: true}
			}

			defer func() {
				lockErr := r.releaseLock(logger, cluster)
				if lockErr != nil {
					logger.Error(lockErr, "could not release lock")
				}
			}()
		}

		logger.Info("Configuring database", "current configuration", currentConfiguration, "desired configuration", desiredConfiguration)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(nextConfiguration, initialConfig, cluster.Spec.Version)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		if initialConfig {
			return &requeue{message: "Requeuing for fetching the initial configuration from FDB cluster", delay: 1 * time.Second}
		}

		logger.Info("Configured database", "initialConfig", initialConfig)
		if !equality.Semantic.DeepEqual(nextConfiguration, desiredConfiguration) {
			return &requeue{message: "Requeuing for next stage of database configuration change", delayedRequeue: true}
		}
	}

	return nil
}
