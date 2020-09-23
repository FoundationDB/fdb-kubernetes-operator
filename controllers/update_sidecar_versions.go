/*
 * update_sidecar_versions.go
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
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// UpdateSidecarVersions provides a reconciliation step for upgrading the
// sidecar.
type UpdateSidecarVersions struct {
}

// Reconcile runs the reconciler's work.
func (u UpdateSidecarVersions) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}
	upgraded := false
	for _, instance := range instances {
		if instance.Pod == nil {
			return false, MissingPodError(instance, cluster)
		}

		settings := cluster.GetProcessSettings(instance.GetProcessClass())

		image := cluster.Spec.SidecarContainer.ImageName
		if settings.PodTemplate != nil {
			for _, container := range settings.PodTemplate.Spec.Containers {
				if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != "" {
					image = container.Image
				}
			}
		}
		if image == "" {
			image = "foundationdb/foundationdb-kubernetes-sidecar"
		}
		image = fmt.Sprintf("%s:%s", image, cluster.GetFullSidecarVersion(false))

		for containerIndex, container := range instance.Pod.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != image {
				log.Info("Upgrading sidecar", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instance.Pod.Name, "oldImage", container.Image, "newImage", image)
				err = r.PodLifecycleManager.UpdateImageVersion(r, context, cluster, instance, containerIndex, image)
				if err != nil {
					return false, err
				}
				upgraded = true
			}
		}
	}
	if upgraded {
		r.Recorder.Event(cluster, "Normal", "SidecarUpgraded", fmt.Sprintf("New version: %s", cluster.Spec.Version))
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdateSidecarVersions) RequeueAfter() time.Duration {
	return 0
}
