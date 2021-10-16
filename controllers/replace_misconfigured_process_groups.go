/*
 * replace_misconfigured_process_groups.go
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
	ctx "context"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/replacements"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// replaceMisconfiguredProcessGroups identifies processes that need to be replaced in
// order to bring up new processes with different configuration.
type replaceMisconfiguredProcessGroups struct{}

// reconcile runs the reconciler's work.
func (c replaceMisconfiguredProcessGroups) reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "replaceMisconfiguredProcessGroups")

	pvcs := &corev1.PersistentVolumeClaimList{}
	err := r.List(context, pvcs, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}

	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}

	hasReplacements, err := replacements.HasReplacements(logger, cluster, internal.CreatePVCMap(cluster, pvcs), internal.CreatePodMap(cluster, pods))
	if err != nil {
		return &requeue{curError: err}
	}

	if hasReplacements {
		err = r.Status().Update(context, cluster)
		if err != nil {
			return &requeue{curError: err}
		}

		log.Info("Removals have been updated in the cluster status")
	}

	return nil
}
