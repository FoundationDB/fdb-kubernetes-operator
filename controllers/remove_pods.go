/*
 * remove_pods.go
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
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RemovePods provides a reconciliation step for removing pods as part of a
// shrink or replacement.
type RemovePods struct{}

func (u RemovePods) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if len(cluster.Spec.PendingRemovals) == 0 {
		return true, nil
	}
	r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", cluster.Spec.PendingRemovals))
	for id := range cluster.Spec.PendingRemovals {
		err := r.removePod(context, cluster, id)
		if err != nil {
			return false, err
		}
	}

	for id := range cluster.Spec.PendingRemovals {
		removed, err := r.confirmPodRemoval(context, cluster, id)
		if !removed {
			return removed, err
		}
	}

	return true, nil
}

func (r *ReconcileFoundationDBCluster) removePod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, podName string) error {
	instanceListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", podName)
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions)
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		err = r.PodLifecycleManager.DeleteInstance(r, context, instances[0])

		if err != nil {
			return err
		}
	}

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", podName))
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcListOptions, pvcs)
	if err != nil {
		return err
	}
	if len(pvcs.Items) > 0 {
		err = r.Delete(context, &pvcs.Items[0])
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) confirmPodRemoval(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceName string) (bool, error) {
	instanceListOptions := getSinglePodListOptions(cluster, instanceName)

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions)
	if err != nil {
		return false, err
	}
	if len(instances) > 0 {
		log.Info("Waiting for instance get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		return false, nil
	}

	pods := &corev1.PodList{}
	err = r.List(context, instanceListOptions, pods)
	if err != nil {
		return false, err
	}
	if len(pods.Items) > 0 {
		log.Info("Waiting for pod get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		return false, nil
	}

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", instanceName))
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcListOptions, pvcs)
	if err != nil {
		return false, err
	}
	if len(pvcs.Items) > 0 {
		log.Info("Waiting for volume claim get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "name", pvcs.Items[0].Name)
		return false, nil
	}

	return true, nil
}

func (u RemovePods) RequeueAfter() time.Duration {
	return 0
}
