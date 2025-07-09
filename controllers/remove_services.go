/*
 * remove_services.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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
	if cluster.NeedsHeadlessService() {
		return nil
	}

	existingService := &corev1.Service{}
	err := r.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
		existingService,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return &requeue{curError: err}
	}

	logger.V(1).Info("Deleting service", "name", existingService.Name)
	err = r.Delete(ctx, existingService)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}
