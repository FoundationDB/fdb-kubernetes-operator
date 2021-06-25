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

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
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

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	hasValidCoordinators, allAddressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
	if err != nil {
		return false, err
	}

	if hasValidCoordinators {
		return true, nil
	}

	if !allAddressesValid {
		log.Info("Deferring coordinator change", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "DeferringCoordinatorChange", "Deferring coordinator change until all processes have consistent address TLS settings")
		return true, nil
	}

	hasLock, err := r.takeLock(cluster, "changing coordinators")
	if !hasLock {
		return false, err
	}

	log.Info("Changing coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing new coordinators")

	coordinators, err := selectCoordinators(cluster, status)
	if err != nil {
		return false, err
	}

	coordinatorAddresses := make([]string, len(coordinators))
	for index, process := range coordinators {
		coordinatorAddresses[index] = process.Address
	}

	log.Info("Final coordinators candidates", "namespace", cluster.Namespace, "cluster", cluster.Name, "coordinators", coordinatorAddresses)
	connectionString, err = adminClient.ChangeCoordinators(coordinatorAddresses)
	if err != nil {
		return false, err
	}
	cluster.Status.ConnectionString = connectionString
	err = r.Status().Update(context, cluster)
	if err != nil {
		return false, err
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (c ChangeCoordinators) RequeueAfter() time.Duration {
	return 0
}

// selectCandidates is a helper for Reconcile that picks non-excluded, not-being-removed class-matching instances.
func selectCandidates(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus) ([]localityInfo, error) {
	candidates := make([]localityInfo, 0, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		if process.Excluded {
			continue
		}

		if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
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

func selectCoordinators(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus) ([]localityInfo, error) {
	var err error
	coordinatorCount := cluster.DesiredCoordinatorCount()

	candidates, err := selectCandidates(cluster, status)
	if err != nil {
		return []localityInfo{}, nil
	}

	coordinators, err := chooseDistributedProcesses(cluster, candidates, coordinatorCount, processSelectionConstraint{
		HardLimits: getHardLimits(cluster),
	})

	log.Info("Current coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name, "coordinators", coordinators)
	if err != nil {
		return candidates, err
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
