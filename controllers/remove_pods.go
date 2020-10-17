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

package controllers

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RemovePods provides a reconciliation step for removing pods as part of a
// shrink or replacement.
type RemovePods struct{}

// Reconcile runs the reconciler's work.
func (u RemovePods) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if len(cluster.Status.PendingRemovals) == 0 {
		return true, nil
	}
	r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", cluster.Status.PendingRemovals))
	for id, state := range cluster.Status.PendingRemovals {
		if !state.ExclusionComplete {
			log.Info("Incomplete exclusion still present in RemovePods step. Retrying reconciliation", "namespace", cluster.Namespace, "name", cluster.Name, "instance", id)
			return false, nil
		}
		if state.PodName != "" {
			err := r.removePod(context, cluster, state.PodName)
			if err != nil {
				return false, err
			}
		}
	}

	for _, state := range cluster.Status.PendingRemovals {
		if state.PodName != "" {
			removed, err := r.confirmPodRemoval(context, cluster, state.PodName)
			if !removed {
				return removed, err
			}
		}
	}

	return true, nil
}

func (r *FoundationDBClusterReconciler) removePod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, podName string) error {
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingField("metadata.name", podName))
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		err = r.PodLifecycleManager.DeleteInstance(r, context, instances[0])

		if err != nil {
			return err
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingField("metadata.name", fmt.Sprintf("%s-data", podName)))
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

func (r *FoundationDBClusterReconciler) confirmPodRemoval(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceName string) (bool, error) {
	instanceListOptions := getSinglePodListOptions(cluster, instanceName)

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, instanceListOptions...)
	if err != nil {
		return false, err
	}
	if len(instances) > 0 {
		log.Info("Waiting for instance get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		return false, nil
	}

	pods := &corev1.PodList{}
	err = r.List(context, pods, instanceListOptions...)
	if err != nil {
		return false, err
	}
	if len(pods.Items) > 0 {
		log.Info("Waiting for pod get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		return false, nil
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingField("metadata.name", fmt.Sprintf("%s-data", instanceName)))
	if err != nil {
		return false, err
	}
	if len(pvcs.Items) > 0 {
		log.Info("Waiting for volume claim get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "name", pvcs.Items[0].Name)
		return false, nil
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u RemovePods) RequeueAfter() time.Duration {
	return 0
}
