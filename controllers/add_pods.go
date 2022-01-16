/*
 * add_pods.go
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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// addPods provides a reconciliation step for adding new pods to a cluster.
type addPods struct{}

// reconcile runs the reconciler's work.
func (a addPods) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
	configMap, err := internal.GetConfigMap(cluster)
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "addPods")
	if err != nil {
		return &requeue{curError: err}
	}
	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		logger.Info("Creating config map", "name", configMap.Name)
		err = r.Create(ctx, configMap)
		if err != nil {
			return &requeue{curError: err}
		}
	} else if err != nil {
		return &requeue{curError: err}
	}

	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}

	podMap := internal.CreatePodMap(cluster, pods)

	for _, processGroup := range cluster.Status.ProcessGroups {
		_, podExists := podMap[processGroup.ProcessGroupID]
		if !podExists && !processGroup.Remove {
			_, idNum, err := podmanager.ParseProcessGroupID(processGroup.ProcessGroupID)
			if err != nil {
				return &requeue{curError: err}
			}

			pod, err := internal.GetPod(cluster, processGroup.ProcessClass, idNum)
			if err != nil {
				r.Recorder.Event(cluster, corev1.EventTypeWarning, "GetPod", fmt.Sprintf("failed to get the PodSpec for %s/%d with error: %s", processGroup.ProcessClass, idNum, err))
				return &requeue{curError: err}
			}

			serverPerPod, err := internal.GetStorageServersPerPodForPod(pod)
			if err != nil {
				return &requeue{curError: err}
			}

			imageType := internal.GetImageType(pod)

			configMapHash, err := internal.GetDynamicConfHash(configMap, processGroup.ProcessClass, imageType, serverPerPod)
			if err != nil {
				return &requeue{curError: err}
			}

			pod.ObjectMeta.Annotations[fdbtypes.LastConfigMapKey] = configMapHash

			if *cluster.Spec.Routing.PublicIPSource == fdbtypes.PublicIPSourceService {
				service := &corev1.Service{}
				err = r.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, service)
				if err != nil {
					return &requeue{curError: err}
				}
				ip := service.Spec.ClusterIP
				if ip == "" {
					logger.Info("Service does not have an IP address", "processGroupID", processGroup.ProcessGroupID)
					return &requeue{message: fmt.Sprintf("Service %s does not have an IP address", service.Name)}
				}
				pod.Annotations[fdbtypes.PublicIPAnnotation] = ip
			}

			err = r.PodLifecycleManager.CreatePod(ctx, r, pod)
			if err != nil {
				return &requeue{curError: err}
			}
		}
	}

	return nil
}
