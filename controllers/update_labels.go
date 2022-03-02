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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// updateLabels provides a reconciliation step for updating the labels on pods.
type updateLabels struct{}

// reconcile runs the reconciler's work.
func (updateLabels) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updateLabels")
	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}
	podMap := internal.CreatePodMap(cluster, pods)

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, pvcs, internal.GetPodListOptions(cluster, "", "")...)
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

		pod, ok := podMap[processGroup.ProcessGroupID]
		if ok {
			metadata := internal.GetPodMetadata(cluster, processGroup.ProcessClass, processGroup.ProcessGroupID, "")
			if metadata.Annotations == nil {
				metadata.Annotations = make(map[string]string, 1)
			}

			if !metadataCorrect(metadata, &pod.ObjectMeta) {
				err = r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod)
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
		if !ok || pod == nil {
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
	metadataCorrect := true
	desiredMetadata.Annotations[fdb.LastSpecKey] = currentMetadata.Annotations[fdb.LastSpecKey]

	if mergeLabelsInMetadata(currentMetadata, desiredMetadata) {
		metadataCorrect = false
	}

	if mergeAnnotations(currentMetadata, desiredMetadata) {
		metadataCorrect = false
	}

	return metadataCorrect
}
