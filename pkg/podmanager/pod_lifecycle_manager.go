/*
 * pod_lifecycle_manager.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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

package podmanager

import (
	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodLifecycleManager provides an abstraction around Pod management to allow
// using intermediary controllers that will manage the Pod lifecycle.
type PodLifecycleManager interface {
	// GetPods lists the instances in the cluster
	GetPods(client.Client, *fdbtypes.FoundationDBCluster, ctx.Context, ...client.ListOption) ([]*corev1.Pod, error)

	// CreatePods creates a new instance based on a pod definition
	CreatePod(client.Client, ctx.Context, *corev1.Pod) error

	// DeletePods shuts down an instance
	DeletePod(client.Client, ctx.Context, *corev1.Pod) error

	// CanDeletePods checks whether it is safe to delete pods.
	CanDeletePods(fdbadminclient.AdminClient, ctx.Context, *fdbtypes.FoundationDBCluster) (bool, error)

	// UpdatePods updates a list of pods to match the latest specs.
	UpdatePods(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, []*corev1.Pod, bool) error

	// UpdateImageVersion updates a container's image.
	UpdateImageVersion(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, *corev1.Pod, int, string) error

	// UpdateMetadata updates an instance's metadata.
	UpdateMetadata(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, *corev1.Pod) error

	// PodIsUpdated determines whether an instance is up to date.
	//
	// This does not need to check the metadata or the pod spec hash. This only
	// needs to check aspects of the rollout that are not available in the
	// instance metadata.
	PodIsUpdated(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, *corev1.Pod) (bool, error)
}

// StandardPodLifecycleManager provides an implementation of PodLifecycleManager
// that directly creates pods.
type StandardPodLifecycleManager struct{}

// GetPods returns a list of Pods for FDB pods that have been
// created.
func (manager StandardPodLifecycleManager) GetPods(r client.Client, cluster *fdbtypes.FoundationDBCluster, context ctx.Context, options ...client.ListOption) ([]*corev1.Pod, error) {
	pods := &corev1.PodList{}
	err := r.List(context, pods, options...)
	if err != nil {
		return nil, err
	}
	resPods := make([]*corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		ownedByCluster := !cluster.ShouldFilterOnOwnerReferences()
		if !ownedByCluster {
			for _, reference := range pod.ObjectMeta.OwnerReferences {
				if reference.UID == cluster.UID {
					ownedByCluster = true
					break
				}
			}
		}

		if ownedByCluster {
			resPod := pod
			resPods = append(resPods, &resPod)
		}
	}

	return resPods, nil
}

// CreatePod creates a new Pod based on a Pod definition
func (manager StandardPodLifecycleManager) CreatePod(r client.Client, context ctx.Context, pod *corev1.Pod) error {
	return r.Create(context, pod)
}

// DeletePod shuts down a Pod
func (manager StandardPodLifecycleManager) DeletePod(r client.Client, context ctx.Context, pod *corev1.Pod) error {
	return r.Delete(context, pod)
}

// CanDeletePods checks whether it is safe to delete Pods.
func (manager StandardPodLifecycleManager) CanDeletePods(adminClient fdbadminclient.AdminClient, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	return internal.HasDesiredFaultTolerance(adminClient, cluster)
}

// UpdatePods updates a list of Pods to match the latest specs.
func (manager StandardPodLifecycleManager) UpdatePods(r client.Client, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, pods []*corev1.Pod, unsafe bool) error {
	for _, pod := range pods {
		err := r.Delete(context, pod)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateImageVersion updates a Pod container's image.
func (manager StandardPodLifecycleManager) UpdateImageVersion(r client.Client, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod, containerIndex int, image string) error {
	pod.Spec.Containers[containerIndex].Image = image
	return r.Update(context, pod)
}

// UpdateMetadata updates an Pod's metadata.
func (manager StandardPodLifecycleManager) UpdateMetadata(r client.Client, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) error {
	return r.Update(context, pod)
}

// PodIsUpdated determines whether a Pod is up to date.
//
// This does not need to check the metadata or the pod spec hash. This only
// needs to check aspects of the rollout that are not available in the
// PodIsUpdated metadata.
func (manager StandardPodLifecycleManager) PodIsUpdated(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, *corev1.Pod) (bool, error) {
	return true, nil
}

// GetPodSpec provides an external interface for the internal GetPodSpec method
// This is necessary for compatibility reasons.
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.PodSpec, error) {
	return internal.GetPodSpec(cluster, processClass, idNum)
}
