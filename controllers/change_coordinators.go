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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// changeCoordinators provides a reconciliation step for choosing new
// coordinators.
type changeCoordinators struct{}

// reconcile runs the reconciler's work.
func (c changeCoordinators) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "changeCoordinators")

	if !cluster.Status.Configured {
		return nil
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	connectionString, err := adminClient.GetConnectionString()
	if err != nil {
		return &requeue{curError: err}
	}

	if connectionString != cluster.Status.ConnectionString {
		logger.Info("Updating out-of-date connection string")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConnectionString", fmt.Sprintf("Setting connection string to %s", connectionString))
		cluster.Status.ConnectionString = connectionString
		err = r.Status().Update(ctx, cluster)

		if err != nil {
			return &requeue{curError: err}
		}
	}

	status, err := adminClient.GetStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address.String()] = false
	}

	hasValidCoordinators, allAddressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
	if err != nil {
		return &requeue{curError: err}
	}

	if hasValidCoordinators {
		return nil
	}

	if !allAddressesValid {
		logger.Info("Deferring coordinator change")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "DeferringCoordinatorChange", "Deferring coordinator change until all processes have consistent address TLS settings")
		return nil
	}

	hasLock, err := r.takeLock(cluster, "changing coordinators")
	if !hasLock {
		return &requeue{curError: err}
	}

	logger.Info("Changing coordinators")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing new coordinators")

	coordinators, err := selectCoordinators(cluster, status)
	if err != nil {
		return &requeue{curError: err}
	}

	coordinatorAddresses := make([]fdbv1beta2.ProcessAddress, len(coordinators))
	for index, process := range coordinators {
		coordinatorAddresses[index] = getCoordinatorAddress(cluster, process)
	}

	logger.Info("Final coordinators candidates", "coordinators", coordinatorAddresses)
	connectionString, err = adminClient.ChangeCoordinators(coordinatorAddresses)
	if err != nil {
		return &requeue{curError: err}
	}
	cluster.Status.ConnectionString = connectionString
	err = r.Status().Update(ctx, cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}

// selectCandidates is a helper for Reconcile that picks non-excluded, not-being-removed class-matching process groups.
func selectCandidates(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) ([]localityInfo, error) {
	candidates := make([]localityInfo, 0, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		if process.Excluded {
			continue
		}

		if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
			continue
		}

		if cluster.ProcessGroupIsBeingRemoved(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]) {
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

func selectCoordinators(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) ([]localityInfo, error) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "changeCoordinators")
	var err error
	coordinatorCount := cluster.DesiredCoordinatorCount()

	candidates, err := selectCandidates(cluster, status)
	if err != nil {
		return []localityInfo{}, nil
	}

	coordinators, err := chooseDistributedProcesses(cluster, candidates, coordinatorCount, processSelectionConstraint{
		HardLimits: getHardLimits(cluster),
	})

	logger.Info("Current coordinators", "coordinators", coordinators)
	if err != nil {
		return candidates, err
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range coordinators {
		coordinatorStatus[getCoordinatorAddress(cluster, coordinator).String()] = false
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

func getCoordinatorAddress(cluster *fdbv1beta2.FoundationDBCluster, locality localityInfo) fdbv1beta2.ProcessAddress {
	dnsName := locality.LocalityData[fdbv1beta2.FDBLocalityDNSNameKey]

	address := locality.Address

	if cluster.UseDNSInClusterFile() && dnsName != "" {
		return fdbv1beta2.ProcessAddress{
			StringAddress: dnsName,
			Port:          address.Port,
			Flags:         address.Flags,
		}
	}
	return address
}
