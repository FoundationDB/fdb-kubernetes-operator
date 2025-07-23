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

	"k8s.io/utils/ptr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// updateDatabaseConfiguration provides a reconciliation step for changing the
// database configuration.
type updateDatabaseConfiguration struct{}

// reconcile runs the reconciler's work.
func (u updateDatabaseConfiguration) reconcile(
	_ context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	if !ptr.Deref(cluster.Spec.AutomationOptions.ConfigureDatabase, true) {
		return nil
	}

	adminClient, err := r.getAdminClient(logger, cluster)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}
	defer func() {
		_ = adminClient.Close()
	}()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	clusterIsConfigured := fdbstatus.ClusterIsConfigured(cluster, status)
	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	currentConfiguration := status.Cluster.DatabaseConfiguration.NormalizeConfiguration(cluster)

	if !clusterIsConfigured ||
		!equality.Semantic.DeepEqual(desiredConfiguration, currentConfiguration) {
		var nextConfiguration fdbv1beta2.DatabaseConfiguration
		if clusterIsConfigured {
			nextConfiguration = currentConfiguration.GetNextConfigurationChange(
				desiredConfiguration,
			)
		} else {
			nextConfiguration = desiredConfiguration
		}
		configurationString, configErr := nextConfiguration.GetConfigurationString()
		if configErr != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		// If the cluster is configured run additional safety checks before performing the database configuration change.
		if clusterIsConfigured {
			runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
			if err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}

			err = fdbstatus.ConfigurationChangeAllowed(
				status,
				runningVersion.SupportsRecoveryState() && r.EnableRecoveryState,
			)
			if err != nil {
				logger.Info(
					"Changing current configuration is not safe",
					"error",
					err,
					"current configuration",
					currentConfiguration,
					"desired configuration",
					desiredConfiguration,
				)
				r.Recorder.Event(
					cluster,
					corev1.EventTypeNormal,
					"NeedsConfigurationChange",
					fmt.Sprintf(
						"Spec requires configuration change to `%s`, but configuration change is not safe: %s",
						configurationString,
						err.Error(),
					),
				)
				return &requeue{
					message: fmt.Sprintf(
						"Configuration change is not safe: %s, will retry",
						err.Error(),
					),
					delayedRequeue: true,
					delay:          10 * time.Second,
				}
			}

			err = r.takeLock(logger, cluster,
				fmt.Sprintf("reconfiguring the database to `%s`", configurationString))
			if err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}
		}

		logger.Info(
			"Configuring database",
			"current configuration",
			currentConfiguration,
			"desired configuration",
			desiredConfiguration,
		)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(nextConfiguration, !clusterIsConfigured)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		logger.Info("Configured database", "clusterIsConfigured", clusterIsConfigured)
		if !equality.Semantic.DeepEqual(nextConfiguration, desiredConfiguration) {
			return &requeue{
				message:        "Requeuing for next stage of database configuration change",
				delay:          30 * time.Second,
				delayedRequeue: true,
			}
		}
	}

	return nil
}
