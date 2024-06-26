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
	"github.com/FoundationDB/fdb-kubernetes-operator/internal/coordinator"
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
	for _, coord := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coord.Address.String()] = false
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

	err = coordinator.ChangeCoordinators(logger, adminClient, cluster, status)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	err = r.updateOrApply(ctx, cluster)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	return nil
}
