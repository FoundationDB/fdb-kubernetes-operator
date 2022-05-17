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
	"context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodLifecycleManager provides an abstraction around Pod management to allow
// using intermediary controllers that will manage the Pod lifecycle.
type PodLifecycleManager interface {
	// GetPods lists the Pods in the cluster
	GetPods(context.Context, client.Client, *fdbv1beta2.FoundationDBCluster, ...client.ListOption) ([]*corev1.Pod, error)

	// CreatePod creates a new Pod based on a pod definition
	CreatePod(context.Context, client.Client, *corev1.Pod) error

	// DeletePod deletes a Pod
	DeletePod(context.Context, client.Client, *corev1.Pod) error

	// CanDeletePods checks whether it is safe to delete pods.
	CanDeletePods(context.Context, fdbadminclient.AdminClient, *fdbv1beta2.FoundationDBCluster) (bool, error)

	// UpdatePods updates a list of pods to match the latest specs.
	UpdatePods(context.Context, client.Client, *fdbv1beta2.FoundationDBCluster, []*corev1.Pod, bool) error

	// UpdateImageVersion updates a container's image.
	UpdateImageVersion(context.Context, client.Client, *fdbv1beta2.FoundationDBCluster, *corev1.Pod, int, string) error

	// UpdateMetadata updates a Pod's metadata.
	UpdateMetadata(context.Context, client.Client, *fdbv1beta2.FoundationDBCluster, *corev1.Pod) error

	// PodIsUpdated determines whether a Pod is up to date.
	//
	// This does not need to check the metadata or the pod spec hash. This only
	// needs to check aspects of the rollout that are not available in the
	// Pod's metadata.
	PodIsUpdated(context.Context, client.Client, *fdbv1beta2.FoundationDBCluster, *corev1.Pod) (bool, error)

	// GetDeletionMode returns the PodUpdateMode of the cluster if set or the default value.
	GetDeletionMode(*fdbv1beta2.FoundationDBCluster) fdbv1beta2.PodUpdateMode
}

// StandardPodLifecycleManager provides an implementation of PodLifecycleManager
// that directly creates pods.
type StandardPodLifecycleManager struct{}

// GetPods returns a list of Pods for FDB pods that have been
// created.
func (manager StandardPodLifecycleManager) GetPods(ctx context.Context, r client.Client, cluster *fdbv1beta2.FoundationDBCluster, options ...client.ListOption) ([]*corev1.Pod, error) {
	pods := &corev1.PodList{}
	err := r.List(ctx, pods, options...)
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
func (manager StandardPodLifecycleManager) CreatePod(ctx context.Context, r client.Client, pod *corev1.Pod) error {
	logr.FromContextOrDiscard(ctx).V(1).Info("Creating pod", "name", pod.Name)
	return r.Create(ctx, pod)
}

// DeletePod shuts down a Pod
func (manager StandardPodLifecycleManager) DeletePod(ctx context.Context, r client.Client, pod *corev1.Pod) error {
	logr.FromContextOrDiscard(ctx).V(1).Info("Deleting pod", "name", pod.Name)
	return r.Delete(ctx, pod)
}

// CanDeletePods checks whether it is safe to delete Pods.
func (manager StandardPodLifecycleManager) CanDeletePods(_ context.Context, adminClient fdbadminclient.AdminClient, cluster *fdbv1beta2.FoundationDBCluster) (bool, error) {
	return internal.HasDesiredFaultTolerance(adminClient, cluster)
}

// UpdatePods updates a list of Pods to match the latest specs.
func (manager StandardPodLifecycleManager) UpdatePods(ctx context.Context, r client.Client, _ *fdbv1beta2.FoundationDBCluster, pods []*corev1.Pod, _ bool) error {
	for _, pod := range pods {
		logr.FromContextOrDiscard(ctx).V(1).Info("Deleting pod", "name", pod.Name)
		err := r.Delete(ctx, pod)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateImageVersion updates a Pod container's image.
func (manager StandardPodLifecycleManager) UpdateImageVersion(ctx context.Context, r client.Client, _ *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, containerIndex int, image string) error {
	pod.Spec.Containers[containerIndex].Image = image
	return r.Update(ctx, pod)
}

// UpdateMetadata updates an Pod's metadata.
func (manager StandardPodLifecycleManager) UpdateMetadata(ctx context.Context, r client.Client, _ *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) error {
	return r.Update(ctx, pod)
}

// PodIsUpdated determines whether a Pod is up to date.
//
// This does not need to check the metadata or the pod spec hash. This only
// needs to check aspects of the rollout that are not available in the
// PodIsUpdated metadata.
func (manager StandardPodLifecycleManager) PodIsUpdated(context.Context, client.Client, *fdbv1beta2.FoundationDBCluster, *corev1.Pod) (bool, error) {
	return true, nil
}

// GetPodSpec provides an external interface for the internal GetPodSpec method
// This is necessary for compatibility reasons.
func GetPodSpec(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) (*corev1.PodSpec, error) {
	return internal.GetPodSpec(cluster, processClass, idNum)
}

// GetDeletionMode returns the PodUpdateMode of the cluster if set or the default value Zone.
func (manager StandardPodLifecycleManager) GetDeletionMode(cluster *fdbv1beta2.FoundationDBCluster) fdbv1beta2.PodUpdateMode {
	if cluster.Spec.AutomationOptions.DeletionMode == "" {
		return fdbv1beta2.PodUpdateModeZone
	}

	return cluster.Spec.AutomationOptions.DeletionMode
}
