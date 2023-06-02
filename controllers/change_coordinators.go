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
func (c changeCoordinators) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "changeCoordinators")

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

	if status.Cluster.ConnectionString != cluster.Status.ConnectionString {
		logger.Info("Updating out-of-date connection string")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConnectionString", fmt.Sprintf("Setting connection string to %s", status.Cluster.ConnectionString))
		cluster.Status.ConnectionString = status.Cluster.ConnectionString
		err = r.updateOrApply(ctx, cluster)

		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
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

	hasLock, err := r.takeLock(cluster, "changing coordinators")
	if !hasLock {
		return &requeue{curError: err, delayedRequeue: true}
	}

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

// TODO move them into separate package?
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

	logger.Info("Current coordinators", "coordinators", coordinators)
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
