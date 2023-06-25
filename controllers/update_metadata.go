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

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// updateMetadata provides a reconciliation step for updating the metadata on Pods.
type updateMetadata struct{}

// reconcile runs the reconciler's work.
func (updateMetadata) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	// TODO(johscheuer): Remove the use of the pvc map and directly make a get request.
	pvcs := &corev1.PersistentVolumeClaimList{}
	err := r.List(ctx, pvcs, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}
	pvcMap := internal.CreatePVCMap(cluster, pvcs)

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			logger.V(1).Info("Ignore process group marked for removal",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		if err == nil {
			metadata := internal.GetPodMetadata(cluster, processGroup.ProcessClass, processGroup.ProcessGroupID, "")
			if metadata.Annotations == nil {
				metadata.Annotations = make(map[string]string, 1)
			}

			if pod.Spec.NodeName != "" {
				metadata.Annotations[fdbv1beta2.NodeAnnotation] = pod.Spec.NodeName
			}

			if !metadataCorrect(metadata, &pod.ObjectMeta) {
				err := r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod)
				if err != nil {
					return &requeue{curError: err}
				}
			}
		} else {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
		}

		// We can skip all stateless processes because they won't have a PVC attached.
		if !processGroup.ProcessClass.IsStateful() {
			continue
		}

		pvc, ok := pvcMap[processGroup.ProcessGroupID]
		if !ok {
			logger.V(1).Info("Could not find PVC for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		metadata := internal.GetPvcMetadata(cluster, processGroup.ProcessClass, processGroup.ProcessGroupID)
		if metadata.Annotations == nil {
			metadata.Annotations = make(map[string]string, 1)
		}

		if !metadataCorrect(metadata, &pvc.ObjectMeta) {
			err = r.Update(ctx, &pvc)
			if err != nil {
				return &requeue{curError: err}
			}
		}
	}

	return nil
}

func metadataCorrect(desiredMetadata metav1.ObjectMeta, currentMetadata *metav1.ObjectMeta) bool {
	desiredMetadata.Annotations[fdbv1beta2.LastSpecKey] = currentMetadata.Annotations[fdbv1beta2.LastSpecKey]
	// If the annotations or labels have changed the metadata has to be updated.
	return !mergeLabelsInMetadata(currentMetadata, desiredMetadata) && !mergeAnnotations(currentMetadata, desiredMetadata)
}
