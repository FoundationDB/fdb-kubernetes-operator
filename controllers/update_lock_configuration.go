/*
 * update_lock_configuration.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// updateLockConfiguration reconciles the state of the locking system in the
// database with the cluster configuration.
type updateLockConfiguration struct{}

// reconcile runs the reconciler's work.
func (updateLockConfiguration) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus) *requeue {
	if len(cluster.Spec.LockOptions.DenyList) == 0 || !cluster.ShouldUseLocks() || !cluster.Status.Configured {
		return nil
	}

	lockClient, err := r.getLockClient(cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	err = lockClient.UpdateDenyList(cluster.Spec.LockOptions.DenyList)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}
