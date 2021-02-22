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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

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

	if connectionString != cluster.Status.ConnectionString {
		log.Info("Updating out-of-date connection string", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConnectionString", fmt.Sprintf("Setting connection string to %s", connectionString))
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

	hasValidCoordinators, allAddressesValid, err := checkCoordinatorValidity(cluster, status)
	if err != nil {
		return false, err
	}

	if !hasValidCoordinators {
		hasLock, err := r.takeLock(cluster, "changing coordinators")
		if !hasLock {
			return false, err
		}

		if !allAddressesValid {
			log.Info("Deferring coordinator change", "namespace", cluster.Namespace, "cluster", cluster.Name)
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "DeferringCoordinatorChange", "Deferring coordinator change until all processes have consistent address TLS settings")
			return true, nil
		}

		log.Info("Changing coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing new coordinators")

		coordinatorCount := cluster.DesiredCoordinatorCount()
		candidates := make([]localityInfo, 0, len(status.Cluster.Processes))
		chooseCoordinators := func(candidates []localityInfo) ([]localityInfo, error) {
			return chooseDistributedProcesses(candidates, coordinatorCount, processSelectionConstraint{
				HardLimits: map[string]int{fdbtypes.FDBLocalityZoneIDKey: 1},
			})
		}
		// Use all stateful pods if needed, but only storage if possible.
		candidates = selectCandidates(cluster, status, candidates, fdbtypes.ProcessClassStorage)
		coordinators, err := chooseCoordinators(candidates)
		if err != nil {
			// Add in tLogs as candidates
			candidates = selectCandidates(cluster, status, candidates, fdbtypes.ProcessClassLog)
			coordinators, err = chooseCoordinators(candidates)
			if err != nil {
				// Add in transaction roles too
				candidates = selectCandidates(cluster, status, candidates, fdbtypes.ProcessClassTransaction)
				coordinators, err = chooseCoordinators(candidates)
				if err != nil {
					return false, err
				}
			}
		}

		coordinatorAddresses := make([]string, len(coordinators))
		for index, process := range coordinators {
			coordinatorAddresses[index] = process.Address
		}

		connectionString, err := adminClient.ChangeCoordinators(coordinatorAddresses)
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

// selectCandidates is a helper for Reconcile that picks non-excluded, not-being-removed class-matching instances.
func selectCandidates(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus, candidates []localityInfo, class fdbtypes.ProcessClass) []localityInfo {
	for _, process := range status.Cluster.Processes {
		eligible := !process.Excluded && process.ProcessClass == class && !cluster.InstanceIsBeingRemoved(process.Locality[fdbtypes.FDBInstanceIDLabel])
		if eligible {
			candidates = append(candidates, localityInfoForProcess(process))
		}
	}
	return candidates
}
