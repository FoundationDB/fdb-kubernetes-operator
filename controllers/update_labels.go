/*
 * update_labels.go
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
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// UpdateLabels provides a reconciliation step for updating the labels on pods.
type UpdateLabels struct{}

// Reconcile runs the reconciler's work.
func (u UpdateLabels) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}
	for _, instance := range instances {
		if instance.Pod != nil {
			processClass := instance.GetProcessClass()
			instanceID := instance.GetInstanceID()

			metadata := getPodMetadata(cluster, processClass, instanceID, "")
			if metadata.Annotations == nil {
				metadata.Annotations = make(map[string]string)
			}

			if !podMetadataCorrect(metadata, &instance) {
				err = r.PodLifecycleManager.UpdateMetadata(r, context, cluster, instance)
				if err != nil {
					return false, err
				}
			}
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}
	for _, pvc := range pvcs.Items {
		processClass := GetProcessClassFromMeta(pvc.ObjectMeta)
		instanceID := GetInstanceIDFromMeta(pvc.ObjectMeta)

		metadata := getPvcMetadata(cluster, processClass, instanceID)
		if metadata.Annotations == nil {
			metadata.Annotations = make(map[string]string, 1)
		}
		metadata.Annotations[LastSpecKey] = pvc.ObjectMeta.Annotations[LastSpecKey]

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
				return false, err
			}
		}
	}

	return true, nil
}

func podMetadataCorrect(metadata metav1.ObjectMeta, instance *FdbInstance) bool {
	metadataCorrect := true
	metadata.Annotations[LastSpecKey] = instance.Metadata.Annotations[LastSpecKey]

	if mergeLabelsInMetadata(instance.Metadata, metadata) {
		metadataCorrect = false
	}

	if mergeAnnotations(instance.Metadata, metadata) {
		metadataCorrect = false
	}

	return metadataCorrect
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdateLabels) RequeueAfter() time.Duration {
	return 0
}
