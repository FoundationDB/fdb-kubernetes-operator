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
	"context"
	"reflect"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"k8s.io/apimachinery/pkg/api/equality"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateConfigMap provides a reconciliation step for updating the dynamic config
// for a cluster.
type updateConfigMap struct{}

// reconcile runs the reconciler's work.
func (u updateConfigMap) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	logger := log.WithValues("namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name, "reconciler", "UpdateConfigMap")
	existing := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		logger.Info("Creating config map")
		err = r.Create(ctx, configMap)
		if err != nil {
			return &requeue{curError: err}
		}
		return nil
	} else if err != nil {
		return &requeue{curError: err}
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
		logger.Info("Updating config map")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConfigMap", "")
		existing.Data = configMap.Data
		err = r.Update(ctx, existing)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
