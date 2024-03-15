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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/locality"
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// changeCoordinators provides a reconciliation step for choosing new
// coordinators.
type changeCoordinators struct{}

// reconcile runs the reconciler's work.
func (c changeCoordinators) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	if !cluster.Status.Configured {
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

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address.String()] = false
	}

	hasValidCoordinators, allAddressesValid, err := locality.CheckCoordinatorValidity(logger, cluster, status, coordinatorStatus)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	if hasValidCoordinators {
		return nil
	}

	if !allAddressesValid {
		logger.Info("Deferring coordinator change")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "DeferringCoordinatorChange", "Deferring coordinator change until all processes have consistent address TLS settings")
		return nil
	}

	hasLock, err := r.takeLock(logger, cluster, "changing coordinators")
	if !hasLock {
		return &requeue{curError: err, delayedRequeue: true}
	}

	defer func() {
		lockErr := r.releaseLock(logger, cluster)
		if lockErr != nil {
			logger.Error(lockErr, "could not release lock")
		}
	}()

	logger.Info("Changing coordinators")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing new coordinators")

	coordinators, err := selectCoordinators(logger, cluster, status)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	coordinatorAddresses := make([]fdbv1beta2.ProcessAddress, len(coordinators))
	for index, process := range coordinators {
		coordinatorAddresses[index] = getCoordinatorAddress(cluster, process)
	}

	logger.Info("Final coordinators candidates", "coordinators", coordinatorAddresses)
	connectionString, err := adminClient.ChangeCoordinators(coordinatorAddresses)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}
	cluster.Status.ConnectionString = connectionString
	err = r.updateOrApply(ctx, cluster)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	return nil
}

// selectCandidates is a helper for Reconcile that picks non-excluded, not-being-removed class-matching process groups.
func selectCandidates(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) ([]locality.Info, error) {
	candidates := make([]locality.Info, 0, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		if process.Excluded || process.UnderMaintenance {
			continue
		}

		if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
			continue
		}

		// Ignore processes with missing locality, see: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1254
		if len(process.Locality) == 0 {
			continue
		}

		// If the cluster should be using DNS in the cluster file we should make sure the locality is set.
		if cluster.UseDNSInClusterFile() {
			_, ok := process.Locality[fdbv1beta2.FDBLocalityDNSNameKey]
			if !ok {
				continue
			}
		}

		if cluster.ProcessGroupIsBeingRemoved(fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])) {
			continue
		}

		currentLocality, err := locality.InfoForProcess(process, cluster.Spec.MainContainer.EnableTLS)
		if err != nil {
			return candidates, err
		}

		candidates = append(candidates, currentLocality)
	}

	return candidates, nil
}

func selectCoordinators(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) ([]locality.Info, error) {
	var err error
	coordinatorCount := cluster.DesiredCoordinatorCount()

	candidates, err := selectCandidates(cluster, status)
	if err != nil {
		return []locality.Info{}, err
	}

	coordinators, err := locality.ChooseDistributedProcesses(cluster, candidates, coordinatorCount, locality.ProcessSelectionConstraint{
		HardLimits: locality.GetHardLimits(cluster),
	})

	logger.Info("Current coordinators", "coordinators", coordinators, "error", err)
	if err != nil {
		return candidates, err
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range coordinators {
		coordinatorStatus[getCoordinatorAddress(cluster, coordinator).String()] = false
	}

	hasValidCoordinators, allAddressesValid, err := locality.CheckCoordinatorValidity(logger, cluster, status, coordinatorStatus)
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

func getCoordinatorAddress(cluster *fdbv1beta2.FoundationDBCluster, locality locality.Info) fdbv1beta2.ProcessAddress {
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
