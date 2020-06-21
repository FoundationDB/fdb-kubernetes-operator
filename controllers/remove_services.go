/*
 * remove_services.go
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RemoveServices provides a reconciliation step for removing services from a cluster.
type RemoveServices struct{}

// Reconcile runs the reconciler's work.
func (u RemoveServices) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	service, err := GetHeadlessService(cluster)
	if service != nil {
		return true, nil
	}

	existingServices := &corev1.ServiceList{}
	err = r.List(context, existingServices, client.InNamespace(cluster.Namespace), client.MatchingField("metadata.name", cluster.Name))
	if err != nil {
		return false, err
	}

	if len(existingServices.Items) > 0 {
		err = r.Delete(context, &existingServices.Items[0])
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u RemoveServices) RequeueAfter() time.Duration {
	return 0
}
