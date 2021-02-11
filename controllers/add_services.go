/*
 * add_services.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// AddServices provides a reconciliation step for adding services to a cluster.
type AddServices struct{}

// Reconcile runs the reconciler's work.
func (a AddServices) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	service := GetHeadlessService(cluster)
	if service != nil {
		existingService := &corev1.Service{}
		err := r.Get(context, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, existingService)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return false, err
			}
			owner := buildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
			service.ObjectMeta.OwnerReferences = owner
			err = r.Create(context, service)
			if err != nil {
				return false, err
			}
		}
	}

	if *cluster.Spec.Services.PublicIPSource == fdbtypes.PublicIPSourceService {
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.Remove {
				continue
			}

			_, idNum, err := ParseInstanceID(processGroup.ProcessGroupID)
			if err != nil {
				return false, err
			}

			serviceName, _ := getInstanceID(cluster, processGroup.ProcessClass, idNum)
			existingService := &corev1.Service{}
			err = r.Get(context, client.ObjectKey{Namespace: cluster.Namespace, Name: serviceName}, existingService)
			if err != nil {

				if !k8serrors.IsNotFound(err) {
					return false, err
				}
				service, err := GetService(cluster, processGroup.ProcessClass, idNum)
				if err != nil {
					return false, err
				}

				err = r.Create(context, service)

				if err != nil {
					return false, err
				}
			}
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (a AddServices) RequeueAfter() time.Duration {
	return 0
}

// GetHeadlessService builds a headless service for a FoundationDB cluster.
func GetHeadlessService(cluster *fdbtypes.FoundationDBCluster) *corev1.Service {
	headless := cluster.Spec.Services.Headless
	if headless == nil || !*headless {
		return nil
	}

	service := &corev1.Service{
		ObjectMeta: getObjectMetadata(cluster, nil, "", ""),
	}
	service.ObjectMeta.Name = cluster.ObjectMeta.Name
	service.Spec.ClusterIP = "None"
	service.Spec.Selector = map[string]string{FDBClusterLabel: cluster.Name}

	return service
}
