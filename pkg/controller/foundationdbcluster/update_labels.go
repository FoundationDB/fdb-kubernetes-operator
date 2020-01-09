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

package foundationdbcluster

import (
	ctx "context"
	"reflect"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// UpdateLabels provides a reconciliation step for updating the labels on pods.
type UpdateLabels struct{}

func (u UpdateLabels) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}
	for _, instance := range instances {
		if instance.Pod != nil {
			processClass := instance.Metadata.Labels["fdb-process-class"]
			instanceId := instance.Metadata.Labels["fdb-instance-id"]

			metadata := getPodMetadata(cluster, processClass, instanceId, "")
			if metadata.Annotations == nil {
				metadata.Annotations = make(map[string]string)
			}
			metadata.Annotations[LastPodHashKey] = instance.Pod.ObjectMeta.Annotations[LastPodHashKey]
			metadataCorrect := true

			if !reflect.DeepEqual(instance.Pod.ObjectMeta.Labels, metadata.Labels) {
				instance.Pod.ObjectMeta.Labels = metadata.Labels
				metadataCorrect = false
			}

			if !reflect.DeepEqual(instance.Pod.ObjectMeta.Annotations, metadata.Annotations) {
				instance.Pod.ObjectMeta.Annotations = metadata.Annotations
				metadataCorrect = false
			}

			if !metadataCorrect {
				err = r.Update(context, instance.Pod)
				if err != nil {
					return false, err
				}
			}
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, getPodListOptions(cluster, "", ""), pvcs)
	if err != nil {
		return false, err
	}
	for _, pvc := range pvcs.Items {
		processClass := pvc.ObjectMeta.Labels["fdb-process-class"]
		instanceId := pvc.ObjectMeta.Labels["fdb-instance-id"]

		metadata := getPvcMetadata(cluster, processClass, instanceId)

		metadataCorrect := true
		if !reflect.DeepEqual(pvc.ObjectMeta.Labels, metadata.Labels) {
			pvc.Labels = metadata.Labels
			metadataCorrect = false
		}

		if !reflect.DeepEqual(pvc.ObjectMeta.Annotations, metadata.Annotations) {
			pvc.Annotations = metadata.Annotations
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

func (u UpdateLabels) RequeueAfter() time.Duration {
	return 0
}
