/*
 * remove_services.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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

	"github.com/go-logr/logr"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// removeServices provides a reconciliation step for removing services from a cluster.
type removeServices struct{}

// reconcile runs the reconciler's work.
func (u removeServices) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	_ *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	serviceNamesToRemove := make([]string, 0, 2)

	if !cluster.NeedsHeadlessService() {
		serviceNamesToRemove = append(serviceNamesToRemove, cluster.Name)
	}
	if !cluster.ShouldEnableGRPC() {
		serviceNamesToRemove = append(serviceNamesToRemove, fmt.Sprintf("%s-grpc", cluster.Name))
	}

	for _, serviceName := range serviceNamesToRemove {
		existingService := &corev1.Service{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: cluster.Namespace, Name: serviceName},
			existingService,
		)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return &requeue{curError: err}
		}

		ownedByCluster := false
		for _, ownerReference := range existingService.OwnerReferences {
			if ownerReference.UID == cluster.UID {
				ownedByCluster = true
			}
		}

		if !ownedByCluster {
			logger.V(1).
				Info("Ignoring service with no matching owner reference", "name", existingService.Name)
			continue
		}

		logger.V(1).Info("Deleting service", "name", existingService.Name)
		err = r.Delete(ctx, existingService)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
