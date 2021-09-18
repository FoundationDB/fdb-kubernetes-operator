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

package controllers

import (
	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodLifecycleManager provides an abstraction around Pod management to allow
// using intermediary controllers that will manage the Pod lifecycle.
type PodLifecycleManager interface {
	// GetInstances lists the instances in the cluster
	GetInstances(client.Client, *fdbtypes.FoundationDBCluster, ctx.Context, ...client.ListOption) ([]FdbInstance, error)

	// CreateInstance creates a new instance based on a pod definition
	CreateInstance(client.Client, ctx.Context, *corev1.Pod) error

	// DeleteInstance shuts down an instance
	DeleteInstance(client.Client, ctx.Context, FdbInstance) error

	// CanDeletePods checks whether it is safe to delete pods.
	CanDeletePods(AdminClient, ctx.Context, *fdbtypes.FoundationDBCluster) (bool, error)

	// UpdatePods updates a list of pods to match the latest specs.
	UpdatePods(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, []FdbInstance, bool) error

	// UpdateImageVersion updates a container's image.
	UpdateImageVersion(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance, int, string) error

	// UpdateMetadata updates an instance's metadata.
	UpdateMetadata(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance) error

	// InstanceIsUpdated determines whether an instance is up to date.
	//
	// This does not need to check the metadata or the pod spec hash. This only
	// needs to check aspects of the rollout that are not available in the
	// instance metadata.
	InstanceIsUpdated(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance) (bool, error)
}

// StandardPodLifecycleManager provides an implementation of PodLifecycleManager
// that directly creates pods.
type StandardPodLifecycleManager struct{}

// GetInstances returns a list of instances for FDB pods that have been
// created.
func (manager StandardPodLifecycleManager) GetInstances(r client.Client, cluster *fdbtypes.FoundationDBCluster, context ctx.Context, options ...client.ListOption) ([]FdbInstance, error) {
	pods := &corev1.PodList{}
	err := r.List(context, pods, options...)
	if err != nil {
		return nil, err
	}
	instances := make([]FdbInstance, 0, len(pods.Items))
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
			instances = append(instances, newFdbInstance(pod))
		}
	}

	return instances, nil
}

// CreateInstance creates a new instance based on a pod definition
func (manager StandardPodLifecycleManager) CreateInstance(r client.Client, context ctx.Context, pod *corev1.Pod) error {
	return r.Create(context, pod)
}

// DeleteInstance shuts down an instance
func (manager StandardPodLifecycleManager) DeleteInstance(r client.Client, context ctx.Context, instance FdbInstance) error {
	return r.Delete(context, instance.Pod)
}

// CanDeletePods checks whether it is safe to delete pods.
func (manager StandardPodLifecycleManager) CanDeletePods(adminClient AdminClient, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	return hasDesiredFaultTolerance(adminClient, cluster)
}

// UpdatePods updates a list of pods to match the latest specs.
func (manager StandardPodLifecycleManager) UpdatePods(r client.Client, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instances []FdbInstance, unsafe bool) error {
	for _, instance := range instances {
		err := r.Delete(context, instance.Pod)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateImageVersion updates a container's image.
func (manager StandardPodLifecycleManager) UpdateImageVersion(r client.Client, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, containerIndex int, image string) error {
	instance.Pod.Spec.Containers[containerIndex].Image = image
	return r.Update(context, instance.Pod)
}

// UpdateMetadata updates an instance's metadata.
func (manager StandardPodLifecycleManager) UpdateMetadata(r client.Client, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) error {
	instance.Pod.ObjectMeta = *instance.Metadata
	return r.Update(context, instance.Pod)
}

// InstanceIsUpdated determines whether an instance is up to date.
//
// This does not need to check the metadata or the pod spec hash. This only
// needs to check aspects of the rollout that are not available in the
// instance metadata.
func (manager StandardPodLifecycleManager) InstanceIsUpdated(client.Client, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance) (bool, error) {
	return true, nil
}
