/*
 * change_coordinators.go
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
	ctx "context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ChangeCoordinators provides a reconciliation step for choosing new
// coordinators.
type ChangeCoordinators struct{}

// Reconcile runs the reconciler's work.
func (c ChangeCoordinators) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	if !cluster.Status.Configured {
		return nil
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &Requeue{Error: err}
	}
	defer adminClient.Close()

	connectionString, err := adminClient.GetConnectionString()
	if err != nil {
		return &Requeue{Error: err}
	}

	if connectionString != cluster.Status.ConnectionString {
		log.Info("Updating out-of-date connection string", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConnectionString", fmt.Sprintf("Setting connection string to %s", connectionString))
		cluster.Status.ConnectionString = connectionString
		err = r.Status().Update(context, cluster)

		if err != nil {
			return &Requeue{Error: err}
		}
	}

	status, err := adminClient.GetStatus()
	if err != nil {
		return &Requeue{Error: err}
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	hasValidCoordinators, allAddressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
	if err != nil {
		return &Requeue{Error: err}
	}

	if hasValidCoordinators {
		return nil
	}

	if !allAddressesValid {
		log.Info("Deferring coordinator change", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "DeferringCoordinatorChange", "Deferring coordinator change until all processes have consistent address TLS settings")
		return nil
	}

	hasLock, err := r.takeLock(cluster, "changing coordinators")
	if !hasLock {
		return &Requeue{Error: err}
	}

	log.Info("Changing coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing new coordinators")

	coordinators, err := selectCoordinators(cluster, status)
	if err != nil {
		return &Requeue{Error: err}
	}

	coordinatorAddresses := make([]string, len(coordinators))
	for index, process := range coordinators {
		coordinatorAddresses[index] = process.Address
	}

	log.Info("Final coordinators candidates", "namespace", cluster.Namespace, "cluster", cluster.Name, "coordinators", coordinatorAddresses)
	connectionString, err = adminClient.ChangeCoordinators(coordinatorAddresses)
	if err != nil {
		return &Requeue{Error: err}
	}
	cluster.Status.ConnectionString = connectionString
	err = r.Status().Update(context, cluster)
	if err != nil {
		return &Requeue{Error: err}
	}

	return nil
}

// selectCandidates is a helper for Reconcile that picks non-excluded, not-being-removed class-matching instances.
func selectCandidates(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus, candidates []localityInfo, class fdbtypes.ProcessClass) ([]localityInfo, error) {
	for _, process := range status.Cluster.Processes {
		if process.Excluded {
			continue
		}

		if process.ProcessClass != class {
			continue
		}

		if cluster.InstanceIsBeingRemoved(process.Locality[fdbtypes.FDBLocalityInstanceIDKey]) {
			continue
		}

		locality, err := localityInfoForProcess(process, cluster.Spec.MainContainer.EnableTLS)
		if err != nil {
			return candidates, err
		}

		candidates = append(candidates, locality)
	}

	return candidates, nil
}

// selectCoordinators is not a deterministic method and can return different coordinators for the same input arguments
func selectCoordinators(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus) ([]localityInfo, error) {
	var err error
	coordinatorCount := cluster.DesiredCoordinatorCount()
	candidates := make([]localityInfo, 0, len(status.Cluster.Processes))
	chooseCoordinators := func(candidates []localityInfo) ([]localityInfo, error) {
		return chooseDistributedProcesses(candidates, coordinatorCount, processSelectionConstraint{
			HardLimits: getHardLimits(cluster),
		})
	}

	// Use all stateful pods if needed, but only storage if possible.
	candidates, err = selectCandidates(cluster, status, candidates, fdbtypes.ProcessClassStorage)
	if err != nil {
		return []localityInfo{}, nil
	}
	coordinators, err := chooseCoordinators(candidates)
	log.Info("Current coordinators added (storage) candidates", "namespace", cluster.Namespace, "cluster", cluster.Name, "coordinators", coordinators)

	if err != nil {
		// Add in tLogs as candidates
		candidates, err = selectCandidates(cluster, status, candidates, fdbtypes.ProcessClassLog)
		if err != nil {
			return []localityInfo{}, nil
		}
		log.Info("Current coordinators added (TLog) candidates", "namespace", cluster.Namespace, "cluster", cluster.Name, "coordinators", coordinators)
		coordinators, err = chooseCoordinators(candidates)
		if err != nil {
			// Add in transaction roles too
			candidates, err = selectCandidates(cluster, status, candidates, fdbtypes.ProcessClassTransaction)
			if err != nil {
				return []localityInfo{}, nil
			}
			log.Info("Current coordinators added (transaction) candidates", "namespace", cluster.Namespace, "cluster", cluster.Name, "coordinators", coordinators)
			coordinators, err = chooseCoordinators(candidates)
			if err != nil {
				return candidates, err
			}
		}
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	hasValidCoordinators, allAddressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
	if err != nil {
		return coordinators, err
	}

	if !hasValidCoordinators {
		return coordinators, fmt.Errorf("new coordinators are not valid")
	}

	if !allAddressesValid {
		return coordinators, fmt.Errorf("new coordinators contain invalid addresses")
	}

	return coordinators, nil
}
