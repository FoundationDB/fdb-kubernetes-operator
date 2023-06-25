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

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// addServices provides a reconciliation step for adding services to a cluster.
type addServices struct{}

// reconcile runs the reconciler's work.
func (a addServices) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	service := internal.GetHeadlessService(cluster)
	if service != nil {
		existingService := &corev1.Service{}
		err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, existingService)
		if err == nil {
			// Update the existing service
			err = updateService(ctx, logger, r, existingService, service)
			if err != nil {
				return &requeue{curError: err}
			}
		} else {
			if !k8serrors.IsNotFound(err) {
				return &requeue{curError: err}
			}
			owner := internal.BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
			service.ObjectMeta.OwnerReferences = owner
			logger.V(1).Info("Creating service", "name", service.Name)
			err = r.Create(ctx, service)
			if err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}
		}
	}

	if cluster.GetPublicIPSource() == fdbv1beta2.PublicIPSourceService {
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.IsMarkedForRemoval() {
				continue
			}

			idNum, err := processGroup.ProcessGroupID.GetIDNumber()
			if err != nil {
				return &requeue{curError: err}
			}

			serviceName, _ := internal.GetProcessGroupID(cluster, processGroup.ProcessClass, idNum)
			service, err := internal.GetService(cluster, processGroup.ProcessClass, idNum)
			if err != nil {
				return &requeue{curError: err}
			}

			existingService := &corev1.Service{}
			err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: serviceName}, existingService)
			if err == nil {
				// Update the existing service
				err = updateService(ctx, logger, r, existingService, service)
				if err != nil {
					return &requeue{curError: err}
				}
			} else if k8serrors.IsNotFound(err) {
				logger.V(1).Info("Creating service", "name", service.Name)
				err = r.Create(ctx, service)
				if err != nil {
					if internal.IsQuotaExceeded(err) {
						return &requeue{curError: err, delayedRequeue: true}
					}

					return &requeue{curError: err}
				}
			} else {
				return &requeue{curError: err}
			}
		}
	}

	return nil
}

// updateServices updates selected safe fields on a service based on a new
// service definition.
func updateService(ctx context.Context, logger logr.Logger, r *FoundationDBClusterReconciler, currentService *corev1.Service, newService *corev1.Service) error {
	originalSpec := currentService.Spec.DeepCopy()

	currentService.Spec.Selector = newService.Spec.Selector

	needsUpdate := !equality.Semantic.DeepEqual(currentService.Spec, *originalSpec)
	metadata := currentService.ObjectMeta
	if mergeLabelsInMetadata(&metadata, newService.ObjectMeta) {
		needsUpdate = true
	}
	if mergeAnnotations(&metadata, newService.ObjectMeta) {
		needsUpdate = true
	}
	if needsUpdate {
		currentService.ObjectMeta = metadata
		logger.Info("Updating service")
		return r.Update(ctx, currentService)
	}
	return nil
}
