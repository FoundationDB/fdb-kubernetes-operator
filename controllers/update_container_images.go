/*
 * update_container_images.go
 *
 * (c) Copyright 2026 Palantir Technologies Inc. All rights reserved.
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"maps"
	"slices"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type updateContainerImages struct{}

func (updateContainerImages) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	_ *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	enabled := len(cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers) > 0
	if !enabled &&
		!slices.ContainsFunc(
			cluster.Status.ProcessGroups,
			func(group *fdbv1beta2.ProcessGroupStatus) bool {
				return group.GetConditionTime(fdbv1beta2.UpdatingContainerImages) != nil
			},
		) {
		return nil
	}
	manager, ok := r.PodLifecycleManager.(podmanager.PodLifecycleManagerWithContainerImageUpdates)
	if !ok {
		return &requeue{
			curError: fmt.Errorf(
				"pod lifecycle manager does not support in-place container image updates",
			),
		}
	}
	if r.APIReader == nil {
		return &requeue{
			curError: fmt.Errorf("in-place container image updates require an API reader"),
		}
	}
	// Live state must also protect accepted repairs from failure replacement after an interrupted reconciliation
	pods, err := manager.GetPodsForImageUpdate(
		ctx,
		r.APIReader,
		cluster,
		internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}
	byGroup := make(map[fdbv1beta2.ProcessGroupID]*corev1.Pod, len(pods))
	pendingByPod := make(map[string]map[string]internal.PendingImageUpdate)
	pendingPods := make(map[string]bool)
	for _, pod := range pods {
		id := internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta)
		byGroup[id] = pod
		pending, parseErr := internal.GetPendingImageUpdates(cluster, pod)
		if parseErr != nil {
			return &requeue{curError: parseErr}
		}
		pendingByPod[pod.Name] = pending
		if len(pending) > 0 && !cluster.ProcessGroupIsBeingRemoved(id) {
			pendingPods[pod.Name] = true
		}
		if group := fdbv1beta2.FindProcessGroupByID(
			cluster.Status.ProcessGroups,
			id,
		); group != nil {
			suppressImageUpdateFailures(group, pod, pending)
		}
	}
	wait := &requeue{
		message:        "Waiting for auxiliary container images",
		delayedRequeue: true,
		delay:          5 * time.Second,
	}
	if enabled && internal.GetHeadlessService(cluster) != nil {
		service := &corev1.Service{}
		err = r.APIReader.Get(ctx, client.ObjectKeyFromObject(cluster), service)
		if k8serrors.IsNotFound(err) {
			return wait
		}
		if err != nil {
			return &requeue{curError: err}
		}
		if !service.Spec.PublishNotReadyAddresses {
			return wait
		}
	}
	var candidate *containerImageUpdate
	for _, group := range cluster.Status.ProcessGroups {
		if cluster.ProcessGroupIsBeingRemoved(group.ProcessGroupID) ||
			group.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			continue
		}
		pod := byGroup[group.ProcessGroupID]
		if pod == nil {
			continue
		}
		updatesInProgress := pendingByPod[pod.Name]
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		// Cancellation must work even when the Pod is not running or the entire policy was removed
		marker, err := internal.ImageUpdateAnnotation(updatesInProgress)
		if err != nil {
			return &requeue{curError: err}
		}
		if pod.Annotations[internal.InPlaceImageUpdateAnnotation] != marker {
			if err = manager.UpdateContainerImages(
				ctx,
				r,
				cluster,
				pod,
				nil,
				pod.Annotations[fdbv1beta2.LastSpecKey],
				marker,
			); err != nil {
				return &requeue{curError: err}
			}
			return wait
		}
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}
		desired, err := internal.GetPodSpec(cluster, group)
		if err != nil {
			return &requeue{curError: err}
		}
		updates, err := internal.GetInPlaceImageUpdates(cluster, pod, desired)
		if err != nil {
			return &requeue{curError: err}
		}
		update := &containerImageUpdate{
			pod:     pod,
			group:   group,
			desired: desired,
			images:  updates,
			pending: updatesInProgress,
		}
		if len(updatesInProgress) > 0 {
			// Allow corrections and rollbacks while a previous image cannot start
			if len(updates) > 0 {
				if err = update.apply(ctx, r, manager, cluster); err != nil {
					return &requeue{curError: err}
				}
				return wait
			}
			if internal.InPlaceImagesReady(pod, updatesInProgress) {
				if err = manager.UpdateContainerImages(
					ctx,
					r,
					cluster,
					pod,
					nil,
					pod.Annotations[fdbv1beta2.LastSpecKey],
					"",
				); err != nil {
					return &requeue{curError: err}
				}
				delete(pendingPods, pod.Name)
			}
		} else if candidate == nil && len(updates) > 0 && !cluster.IsBeingUpgraded() && canStartImageUpdate(group, pod, slices.Collect(maps.Keys(updates))) {
			candidate = update
		}
	}
	if len(pendingPods) > 0 {
		return wait
	}
	if candidate != nil {
		logger.Info(
			"Updating auxiliary container images in place",
			"pod",
			candidate.pod.Name,
			"images",
			candidate.images,
		)
		if err = candidate.apply(ctx, r, manager, cluster); err != nil {
			return &requeue{curError: err}
		}
		return wait
	}
	return nil
}

func canStartImageUpdate(
	group *fdbv1beta2.ProcessGroupStatus,
	pod *corev1.Pod,
	updating []string,
) bool {
	for _, condition := range group.ProcessGroupConditions {
		switch condition.ProcessGroupConditionType {
		case fdbv1beta2.IncorrectPodSpec,
			fdbv1beta2.IncorrectPodMetadata,
			fdbv1beta2.PodFailing,
			fdbv1beta2.PodPending,
			fdbv1beta2.ProcessHasHighRunLoopBusy:
		default:
			return false
		}
	}
	return internal.ContainersReadyExcept(pod, updating)
}

func suppressImageUpdateFailures(
	group *fdbv1beta2.ProcessGroupStatus,
	pod *corev1.Pod,
	pending map[string]internal.PendingImageUpdate,
) {
	if len(pending) > 0 &&
		(pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending) &&
		internal.ContainersReadyExcept(pod, slices.Collect(maps.Keys(pending))) {
		group.UpdateCondition(fdbv1beta2.PodFailing, false)
		group.UpdateCondition(fdbv1beta2.PodPending, false)
	}
}

type containerImageUpdate struct {
	pod     *corev1.Pod
	group   *fdbv1beta2.ProcessGroupStatus
	desired *corev1.PodSpec
	images  map[string]string
	pending map[string]internal.PendingImageUpdate
}

func (update *containerImageUpdate) apply(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	manager podmanager.PodLifecycleManagerWithContainerImageUpdates,
	cluster *fdbv1beta2.FoundationDBCluster,
) error {
	internal.RecordImageUpdates(update.pod, update.images, update.pending)
	hash, err := internal.GetJSONHash(update.desired)
	if err != nil {
		return err
	}
	marker, err := internal.ImageUpdateAnnotation(update.pending)
	if err != nil {
		return err
	}
	if err = manager.UpdateContainerImages(
		ctx,
		r,
		cluster,
		update.pod,
		update.images,
		hash,
		marker,
	); err != nil {
		return err
	}
	suppressImageUpdateFailures(update.group, update.pod, update.pending)
	return nil
}
