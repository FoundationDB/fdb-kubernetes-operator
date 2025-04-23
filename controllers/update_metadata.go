/*
 * update_labels.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updateMetadata provides a reconciliation step for updating the metadata on Pods.
type updateMetadata struct{}

// reconcile runs the reconciler's work.
func (updateMetadata) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	var shouldRequeue bool
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			logger.V(1).Info("Ignore process group marked for removal",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		if err != nil {
			logger.Error(err, "Could not get Pod",
				"processGroupID", processGroup.ProcessGroupID)
			shouldRequeue = true
			continue
		}

		correct, err := internal.PodMetadataCorrect(cluster, processGroup, pod)
		if err != nil {
			logger.Error(err, "Could not get Pod metadata",
				"processGroupID", processGroup.ProcessGroupID)
			shouldRequeue = true
			continue
		}

		if !correct {
			logger.Info("update pod metadata", "processGroupID", processGroup.ProcessGroupID)
			err = r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod)
			if err != nil {
				logger.Error(err, "Could not update Pod metadata",
					"processGroupID", processGroup.ProcessGroupID)
				shouldRequeue = true
			}
		}

		// We can skip all stateless processes because they won't have a PVC attached.
		if !processGroup.ProcessClass.IsStateful() {
			continue
		}

		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: processGroup.GetPvcName(cluster)}, pvc)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.V(1).Info("Could not find PVC for process group ID",
					"processGroupID", processGroup.ProcessGroupID)
				continue
			}
			logger.Error(err, "Could not get PVC for process group ID", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		metadata := internal.GetPvcMetadata(cluster, processGroup.ProcessClass, processGroup.ProcessGroupID)
		if metadata.Annotations == nil {
			metadata.Annotations = make(map[string]string, 1)
		}

		if !internal.MetadataCorrect(metadata, &pvc.ObjectMeta) {
			logger.Info("update pvc metadata", "processGroupID", processGroup.ProcessGroupID)
			err = r.Update(ctx, pvc)
			if err != nil {
				logger.Error(err, "Could not update PVC metadata",
					"processGroupID", processGroup.ProcessGroupID)
				shouldRequeue = true
			}
		}
	}

	if shouldRequeue {
		return &requeue{message: "could not update all metadata", delayedRequeue: true}
	}

	return nil
}
