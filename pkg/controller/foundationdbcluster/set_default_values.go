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

package foundationdbcluster

import (
	ctx "context"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SetDefaultValues provides a reconciliation step for setting default values in
// the cluster spec.
type SetDefaultValues struct {
}

func (s SetDefaultValues) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
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
	if cluster.Spec.Resources == nil {
		cluster.Spec.Resources = &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"memory": resource.MustParse("1Gi"),
				"cpu":    resource.MustParse("1"),
			},
			Requests: corev1.ResourceList{
				"memory": resource.MustParse("1Gi"),
				"cpu":    resource.MustParse("1"),
			},
		}
		changed = true
	}
	if cluster.Spec.RunningVersion == "" {
		cluster.Spec.RunningVersion = cluster.Spec.Version
		changed = true
	}
	if changed {
		err := r.Update(context, cluster)
		if err != nil {
			return false, err
		}
	}
	return !changed, nil
}

func (s SetDefaultValues) RequeueAfter() time.Duration {
	return 0
}
