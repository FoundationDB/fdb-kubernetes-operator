/*
 * update_config_map.go
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
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateConfigMap provides a reconciliation step for updating the dynamic conf
// for a cluster.
type UpdateConfigMap struct{}

// Reconcile runs the reconciler's work.
func (u UpdateConfigMap) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	configMap, err := GetConfigMap(cluster)
	if err != nil {
		return false, err
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		err = r.Create(context, configMap)
		return err == nil, err
	} else if err != nil {
		return false, err
	}

	metadataCorrect := true
	if !reflect.DeepEqual(existing.ObjectMeta.Labels, configMap.ObjectMeta.Labels) {
		existing.ObjectMeta.Labels = configMap.ObjectMeta.Labels
		metadataCorrect = false
	}

	if mergeAnnotations(&existing.ObjectMeta, configMap.ObjectMeta) {
		metadataCorrect = false
	}

	if !equality.Semantic.DeepEqual(existing.Data, configMap.Data) || !metadataCorrect {
		log.Info("Updating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConfigMap", "")
		existing.Data = configMap.Data
		err = r.Update(context, existing)
		if err != nil {
			return false, err
		}
	}

	configMapHash, err := GetDynamicConfHash(configMap)
	if err != nil {
		return false, err
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	var errs []error
	// We try to update all instances and if we observe an error we add it to the error list.
	// TODO: should we try to update the instances concurrently?
	for index := range instances {
		instance := instances[index]
		if instance.Metadata.Annotations[LastConfigMapKey] == configMapHash {
			continue
		}

		synced, err := r.updatePodDynamicConf(cluster, instance)
		if !synced {
			log.Info("Update dynamic Pod config", "namespace", configMap.Namespace, "cluster", cluster.Name, "processGroupID", instance.GetInstanceID(), "synced", synced)
			if err != nil {
				errs = append(errs, err)
			} else {
				errs = append(errs, fmt.Errorf("processGroupID %s not synced", instance.GetInstanceID()))
			}

			continue
		}

		instance.Metadata.Annotations[LastConfigMapKey] = configMapHash
		err = r.PodLifecycleManager.UpdateMetadata(r, context, cluster, instance)
		if err != nil {
			log.Info("Update Pod metadata", "namespace", configMap.Namespace, "cluster", cluster.Name, "processGroupID", instance.GetInstanceID(), "error", err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		// If we return an error we don't requeue
		// So we just return that we can't continue but don't have an error
		return false, nil
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdateConfigMap) RequeueAfter() time.Duration {
	return 30 * time.Second
}
