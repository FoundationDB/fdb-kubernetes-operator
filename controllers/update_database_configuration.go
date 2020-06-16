/*
 * update_database_configuration.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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
	"reflect"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateDatabaseConfiguration provides a reconciliation step for changing the
// database configuration.
type UpdateDatabaseConfiguration struct{}

// Reconcile runs the reconciler's work.
func (u UpdateDatabaseConfiguration) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)

	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	desiredConfiguration.RoleCounts.Storage = 0
	needsChange := false
	var currentConfiguration fdbtypes.DatabaseConfiguration

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}

	initialConfig := !cluster.Status.Configured

	healthy := initialConfig || status.Client.DatabaseStatus.Healthy
	available := initialConfig || status.Client.DatabaseStatus.Available

	if !available {
		log.Info("Skipping database configuration change because database is unavailable", "namespace", cluster.Namespace, "cluster", cluster.Name)
		return true, nil
	}

	currentConfiguration = status.Cluster.DatabaseConfiguration.NormalizeConfiguration()
	desiredConfiguration.FillInDefaultVersionFlags(currentConfiguration)
	needsChange = initialConfig || !reflect.DeepEqual(desiredConfiguration, currentConfiguration)

	if needsChange {
		var nextConfiguration fdbtypes.DatabaseConfiguration
		if initialConfig {
			nextConfiguration = desiredConfiguration
		} else {
			nextConfiguration = currentConfiguration.GetNextConfigurationChange(desiredConfiguration)
		}
		configurationString, _ := nextConfiguration.GetConfigurationString()
		var enabled = cluster.Spec.AutomationOptions.ConfigureDatabase

		if !healthy {
			log.Info("Waiting for database to be healthy", "namespace", cluster.Namespace, "cluster", cluster.Name)
			r.Recorder.Event(cluster, "Normal", "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but cluster is not healthy", configurationString))
			return false, nil
		}
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return false, err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but configuration changes are disabled", configurationString))
			cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Database configuration changes are disabled"}
		}

		if !initialConfig {
			lockClient, err := r.getLockClient(cluster)
			if err != nil {
				return false, err
			}

			hasLock, err := lockClient.TakeLock()
			if err != nil {
				return false, err
			}
			if !hasLock {
				log.Info("Failed to get lock", "namespace", cluster.Namespace, "cluster", cluster.Name)
				r.Recorder.Event(cluster, "Normal", "LockAcquisitionFailed", "Lock required before reconfiguring the database")
				return false, nil
			}
		}

		log.Info("Configuring database", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(nextConfiguration, initialConfig)
		if err != nil {
			return false, err
		}
		if initialConfig {
			cluster.Status.Configured = true
			err = r.Status().Update(context, cluster)
			return err != nil, err
		}
		log.Info("Configured database", "namespace", cluster.Namespace, "cluster", cluster.Name)

		if !reflect.DeepEqual(nextConfiguration, desiredConfiguration) {
			log.Info("Requeuing for next stage of database configuration change", "namespace", cluster.Namespace, "cluster", cluster.Name)
			return false, nil
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdateDatabaseConfiguration) RequeueAfter() time.Duration {
	return 0
}
