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

package foundationdbcluster

import (
	ctx "context"
	"reflect"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateConfigMap provides a reconciliation step for updating the dynamic conf
// for a cluster.
type UpdateConfigMap struct{}

func (u UpdateConfigMap) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	configMap, err := GetConfigMap(context, cluster, r)
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

	if !reflect.DeepEqual(existing.Data, configMap.Data) || !reflect.DeepEqual(existing.Labels, configMap.Labels) {
		log.Info("Updating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		r.Recorder.Event(cluster, "Normal", "UpdatingConfigMap", "")
		existing.ObjectMeta.Labels = configMap.ObjectMeta.Labels
		existing.Data = configMap.Data
		err = r.Update(context, existing)
		if err != nil {
			return false, err
		}
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}

	for index := range instances {
		synced, err := r.updatePodDynamicConf(context, cluster, instances[index])
		if !synced {
			return synced, err
		}
	}

	return true, nil
}

func (u UpdateConfigMap) RequeueAfter() time.Duration {
	return time.Duration(30) * time.Second
}
