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
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// updateSidecarVersions provides a reconciliation step for upgrading the
// sidecar.
type updateSidecarVersions struct{}

// reconcile runs the reconciler's work.
func (updateSidecarVersions) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	// We don't need to upgrade the sidecar if no upgrade is in progress, we can skip any further work here.
	if !cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		return nil
	}

	var upgraded int
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			logger.V(1).Info("Ignore process group that is stuck terminating",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		// If a Pod is not found ignore it for now.
		if err != nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if internal.GetImageType(pod) != cluster.DesiredImageType() {
			logger.V(1).Info("Ignore process group with the wrong image type",
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
			if container.Name == fdbv1beta2.SidecarContainerName && container.Image != image {
				logger.Info("Upgrading sidecar", "processGroupID", podmanager.GetProcessGroupID(cluster, pod), "oldImage", container.Image, "newImage", image)
				err = r.PodLifecycleManager.UpdateImageVersion(logr.NewContext(ctx, logger), r, cluster, pod, containerIndex, image)
				if err != nil {
					return &requeue{curError: err}
				}
				upgraded++
			}
		}
	}

	if upgraded > 0 {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "SidecarUpgraded", fmt.Sprintf("New version: %s, number of sidecars upgraded: %d", cluster.Spec.Version, upgraded))
	}

	return nil
}
