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
	ctx "context"
	"reflect"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// UpdateLabels provides a reconciliation step for updating the labels on pods.
type UpdateLabels struct{}

// Reconcile runs the reconciler's work.
func (u UpdateLabels) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	pods, err := r.PodLifecycleManager.GetInstances(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, pod := range pods {
		if pod == nil {
			continue
		}

		processClass, err := GetProcessClass(pod)
		if err != nil {
			return &Requeue{Error: err}
		}

		metadata := internal.GetPodMetadata(cluster, processClass, GetProcessGroupID(pod), "")
		if metadata.Annotations == nil {
			metadata.Annotations = make(map[string]string)
		}

		if !podMetadataCorrect(metadata, pod) {
			err = r.PodLifecycleManager.UpdateMetadata(r, context, cluster, pod)
			if err != nil {
				return &Requeue{Error: err}
			}
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}
	for _, pvc := range pvcs.Items {
		processClass := internal.GetProcessClassFromMeta(pvc.ObjectMeta)
		instanceID := internal.GetInstanceIDFromMeta(pvc.ObjectMeta)

		metadata := internal.GetPvcMetadata(cluster, processClass, instanceID)
		if metadata.Annotations == nil {
			metadata.Annotations = make(map[string]string, 1)
		}
		metadata.Annotations[fdbtypes.LastSpecKey] = pvc.ObjectMeta.Annotations[fdbtypes.LastSpecKey]

		metadataCorrect := true
		if !reflect.DeepEqual(pvc.ObjectMeta.Labels, metadata.Labels) {
			pvc.Labels = metadata.Labels
			metadataCorrect = false
		}

		if mergeAnnotations(&pvc.ObjectMeta, metadata) {
			metadataCorrect = false
		}

		if !metadataCorrect {
			err = r.Update(context, &pvc)
			if err != nil {
				return &Requeue{Error: err}
			}
		}
	}

	return nil
}

func podMetadataCorrect(metadata metav1.ObjectMeta, pod *corev1.Pod) bool {
	metadataCorrect := true
	metadata.Annotations[fdbtypes.LastSpecKey] = pod.ObjectMeta.Annotations[fdbtypes.LastSpecKey]

	if mergeLabelsInMetadata(&pod.ObjectMeta, metadata) {
		metadataCorrect = false
	}

	if mergeAnnotations(&pod.ObjectMeta, metadata) {
		metadataCorrect = false
	}

	return metadataCorrect
}
