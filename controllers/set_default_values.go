/*
 * set_default_values.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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
)

// SetDefaultValues provides a reconciliation step for setting default values in
// the cluster spec.
type SetDefaultValues struct {
}

// Reconcile runs the reconciler's work.
func (s SetDefaultValues) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	changed := false
	if cluster.Spec.RedundancyMode == "" {
		cluster.Spec.RedundancyMode = "double"
		changed = true
	}
	if cluster.Spec.StorageEngine == "" {
		cluster.Spec.StorageEngine = "ssd"
		changed = true
	}
	if cluster.Spec.UsableRegions == 0 {
		cluster.Spec.UsableRegions = 1
	}
	if cluster.Spec.RunningVersion == "" {
		cluster.Spec.RunningVersion = cluster.Spec.Version
		changed = true
	}
	if changed {
		err := r.Update(context, cluster)
		return false, err
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (s SetDefaultValues) RequeueAfter() time.Duration {
	return 0
}
