/*
 * update_sidecar_versions.go
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
	"fmt"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// updateSidecarVersions provides a reconciliation step for upgrading the
// sidecar.
type updateSidecarVersions struct{}

// reconcile runs the reconciler's work.
func (updateSidecarVersions) reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updateSidecarVersions")
	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}
	upgraded := false

	podMap := internal.CreatePodMap(cluster, pods)
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			logger.V(1).Info("Ignore process group marked for removal",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, ok := podMap[processGroup.ProcessGroupID]
		if !ok || pod == nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		processClass, err := podmanager.GetProcessClass(cluster, pod)
		if err != nil {
			return &requeue{curError: err}
		}

		image, err := internal.GetSidecarImage(cluster, processClass)
		if err != nil {
			return &requeue{curError: err}
		}

		for containerIndex, container := range pod.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != image {
				logger.Info("Upgrading sidecar", "processGroupID", podmanager.GetProcessGroupID(cluster, pod), "oldImage", container.Image, "newImage", image)
				err = r.PodLifecycleManager.UpdateImageVersion(r, context, cluster, pod, containerIndex, image)
				if err != nil {
					return &requeue{curError: err}
				}
				upgraded = true
			}
		}
	}

	if upgraded {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "SidecarUpgraded", fmt.Sprintf("New version: %s", cluster.Spec.Version))
	}

	return nil
}
