/*
 * add_pods.go
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
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// AddPods provides a reconciliation step for adding new pods to a cluster.
type AddPods struct{}

// Reconcile runs the reconciler's work.
func (a AddPods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	configMap, err := GetConfigMap(cluster)
	if err != nil {
		return false, err
	}
	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		err = r.Create(context, configMap)
		if err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	}

	configMapHash, err := GetDynamicConfHash(configMap)
	if err != nil {
		return false, err
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	instanceMap := make(map[string]FdbInstance, len(instances))
	for _, instance := range instances {
		instanceMap[instance.GetInstanceID()] = instance
	}

	for _, processGroup := range cluster.Status.ProcessGroups {
		_, instanceExists := instanceMap[processGroup.ProcessGroupID]
		if !instanceExists && !processGroup.Remove {
			_, idNum, err := ParseInstanceID(processGroup.ProcessGroupID)
			if err != nil {
				return false, err
			}

			pod, err := GetPod(cluster, processGroup.ProcessClass, idNum)
			if err != nil {
				r.Recorder.Event(cluster, "Error", "GetPod", fmt.Sprintf("failed to get the PodSpec for %s/%d with error: %s", processGroup.ProcessClass, idNum, err))
				return false, err
			}

			pod.ObjectMeta.Annotations[LastConfigMapKey] = configMapHash

			if *cluster.Spec.Services.PublicIPSource == fdbtypes.PublicIPSourceService {
				service := &corev1.Service{}
				err = r.Get(context, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, service)
				if err != nil {
					return false, err
				}
				ip := service.Spec.ClusterIP
				if ip == "" {
					log.Info("Service does not have an IP address", "namespace", cluster.Namespace, "cluster", cluster.Name, "podName", pod.Name)
					return false, nil
				}
				pod.Annotations[PublicIPAnnotation] = ip
			}

			err = r.PodLifecycleManager.CreateInstance(r, context, pod)
			if err != nil {
				return false, err
			}
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (a AddPods) RequeueAfter() time.Duration {
	return 0
}
