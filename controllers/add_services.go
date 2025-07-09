/*
 * add_services.go
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

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// addServices provides a reconciliation step for adding services to a cluster.
type addServices struct{}

// reconcile runs the reconciler's work.
func (a addServices) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	_ *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	headlessService := internal.GetHeadlessService(cluster)
	if headlessService != nil {
		existingService := &corev1.Service{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
			existingService,
		)
		if err == nil {
			err = updateService(ctx, logger, cluster, r, existingService, headlessService)
			if err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}
		} else {
			if !k8serrors.IsNotFound(err) {
				return &requeue{curError: err}
			}
			owner := internal.BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
			headlessService.ObjectMeta.OwnerReferences = owner
			logger.V(1).Info("Creating service", "name", headlessService.Name)
			err = r.Create(ctx, headlessService)
			if err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}
		}
	}

	if cluster.GetPublicIPSource() == fdbv1beta2.PublicIPSourceService {
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.IsMarkedForRemoval() && processGroup.IsExcluded() {
				continue
			}

			service, err := internal.GetService(cluster, processGroup)
			if err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}

			existingService := &corev1.Service{}
			err = r.Get(
				ctx,
				client.ObjectKey{Namespace: cluster.Namespace, Name: service.Name},
				existingService,
			)
			if err == nil {
				err = updateService(ctx, logger, cluster, r, existingService, service)
				if err != nil {
					return &requeue{curError: err, delayedRequeue: true}
				}
			} else if k8serrors.IsNotFound(err) {
				logger.V(1).Info("Creating service", "name", service.Name)
				err = r.Create(ctx, service)
				if err != nil {
					return &requeue{curError: err, delayedRequeue: true}
				}
			} else {
				return &requeue{curError: err, delayedRequeue: true}
			}
		}
	}

	return nil
}

// requiresRecreation returns true if the cluster supports podIPFamily as IPv6 and the existing service does not have
// IPv6 in the IPFamilies.
func requiresRecreation(
	cluster *fdbv1beta2.FoundationDBCluster,
	existingService *corev1.Service,
) bool {
	return cluster.IsPodIPFamily6() &&
		(existingService.Spec.IPFamilies == nil || existingService.Spec.IPFamilies[0] != corev1.IPv6Protocol)
}

// recreateService removes the existing service and create a new service.
func recreateService(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	currentService *corev1.Service,
	newService *corev1.Service,
	logger logr.Logger,
) error {
	logger.V(1).Info("Recreating service", "name", newService.Name)
	err := r.Delete(ctx, currentService)
	if err != nil {
		return err
	}

	return r.Create(ctx, newService)
}

// updateServices updates selected safe fields on a service based on a new
// service definition.
func updateService(
	ctx context.Context,
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	r *FoundationDBClusterReconciler,
	currentService *corev1.Service,
	newService *corev1.Service,
) error {
	if requiresRecreation(cluster, currentService) {
		return recreateService(ctx, r, currentService, newService, logger)
	}
	originalSpec := currentService.Spec.DeepCopy()

	currentService.Spec.Selector = newService.Spec.Selector

	needsUpdate := !equality.Semantic.DeepEqual(currentService.Spec, *originalSpec)
	metadata := currentService.ObjectMeta
	if internal.MergeLabels(&metadata, newService.ObjectMeta) {
		needsUpdate = true
	}
	if internal.MergeAnnotations(&metadata, newService.ObjectMeta) {
		needsUpdate = true
	}
	if needsUpdate {
		currentService.ObjectMeta = metadata
		logger.Info("Updating service")
		return r.Update(ctx, currentService)
	}

	return nil
}
