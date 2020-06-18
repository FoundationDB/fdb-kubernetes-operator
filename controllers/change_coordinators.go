/*
 * change_coordinators.go
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
	"errors"
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ChangeCoordinators provides a reconciliation step for choosing new
// coordinators.
type ChangeCoordinators struct{}

// Reconcile runs the reconciler's work.
func (c ChangeCoordinators) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if !cluster.Status.Configured {
		return true, nil
	}

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	connectionString, err := adminClient.GetConnectionString()
	if err != nil {
		return false, err
	}

	removals := cluster.Status.PendingRemovals
	if removals == nil {
		removals = make(map[string]fdbtypes.PendingRemovalState)
	}

	if connectionString != cluster.Status.ConnectionString {
		log.Info("Updating out-of-date connection string", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "UpdatingConnectionString", fmt.Sprintf("Setting connection string to %s", connectionString))
		cluster.Status.ConnectionString = connectionString
		err = r.Status().Update(context, cluster)

		if err != nil {
			return false, err
		}
	}

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	if len(coordinatorStatus) == 0 {
		return false, errors.New("Unable to get coordinator status")
	}

	allAddressesValid := true

	for _, process := range status.Cluster.Processes {
		_, isCoordinator := coordinatorStatus[process.Address]

		_, pendingRemoval := removals[process.Locality["instance_id"]]

		if isCoordinator && !process.Excluded && !pendingRemoval {
			coordinatorStatus[process.Address] = true
		}

		address, err := fdbtypes.ParseProcessAddress(process.Address)
		if err != nil {
			return false, err
		}

		if address.Flags["tls"] != cluster.Spec.MainContainer.EnableTLS {
			allAddressesValid = false
		}
	}

	needsChange := len(coordinatorStatus) != cluster.DesiredCoordinatorCount()
	for _, healthy := range coordinatorStatus {
		needsChange = needsChange || !healthy
	}

	if needsChange {
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
			r.Recorder.Event(cluster, "Normal", "LockAcquisitionFailed", "Lock required before changing coordinators")
			return false, nil
		}

		if !allAddressesValid {
			log.Info("Deferring coordinator change", "namespace", cluster.Namespace, "cluster", cluster.Name)
			r.Recorder.Event(cluster, "Normal", "DeferringCoordinatorChange", "Deferring coordinator change until all processes have consistent address TLS settings")
			return true, nil
		}

		log.Info("Changing coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing new coordinators")
		coordinatorCount := cluster.DesiredCoordinatorCount()
		coordinators := make([]string, 0, coordinatorCount)
		for _, process := range status.Cluster.Processes {
			_, pendingRemoval := removals[process.Locality["fdb-instance-id"]]
			eligible := !process.Excluded && isStateful(process.ProcessClass) && !pendingRemoval
			if eligible {
				coordinators = append(coordinators, process.Address)
			}
			if len(coordinators) >= coordinatorCount {
				break
			}
		}

		if len(coordinators) < coordinatorCount {
			return false, errors.New("Unable to recruit new coordinators")
		}

		connectionString, err := adminClient.ChangeCoordinators(coordinators)
		if err != nil {
			return false, err
		}
		cluster.Status.ConnectionString = connectionString
		err = r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ChangeCoordinators) RequeueAfter() time.Duration {
	return 0
}
