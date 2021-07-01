/*
 * update_config_map.go
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

	"k8s.io/apimachinery/pkg/api/equality"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateConfigMap provides a reconciliation step for updating the dynamic config
// for a cluster.
type UpdateConfigMap struct{}

// Reconcile runs the reconciler's work.
func (u UpdateConfigMap) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	configMap, err := GetConfigMap(cluster)
	if err != nil {
		return &Requeue{Error: err}
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		err = r.Create(context, configMap)
		if err != nil {
			return &Requeue{Error: err}
		}
		return nil
	} else if err != nil {
		return &Requeue{Error: err}
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
			return &Requeue{Error: err}
		}
	}

	return nil
}
