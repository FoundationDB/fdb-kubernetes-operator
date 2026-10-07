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
	"maps"
	"slices"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

type updateContainerImages struct{}

func (updateContainerImages) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	_ *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	if len(cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers) > 0 {
		if req := reconcileHeadlessService(ctx, r, cluster, logger); req != nil {
			req.delayedRequeue = true
			return req
		}
	}
	var candidate *corev1.Pod
	var candidateGroup *fdbv1beta2.ProcessGroupStatus
	var candidateSpec *corev1.PodSpec
	var candidateUpdates map[string]string
	var pending bool
	wait := &requeue{
		message:        "Waiting for auxiliary container images",
		delayedRequeue: true,
		delay:          5 * time.Second,
	}
	for _, group := range cluster.Status.ProcessGroups {
		if cluster.ProcessGroupIsBeingRemoved(group.ProcessGroupID) ||
			group.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			continue
		}
		if len(cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers) == 0 &&
			group.GetConditionTime(fdbv1beta2.UpdatingContainerImages) == nil {
			continue
		}
		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, group.GetPodName(cluster))
		if k8serrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
		pendingContainers := internal.PendingImageUpdateContainers(cluster, pod)
		if !pod.DeletionTimestamp.IsZero() {
			pending = pending || len(pendingContainers) > 0
			continue
		}
		if pod.Annotations[internal.InPlaceImageUpdateAnnotation] != strings.Join(
			pendingContainers,
			",",
		) {
			if err = r.PodLifecycleManager.UpdateContainerImages(ctx, r, cluster, pod, nil,
				pod.Annotations[fdbv1beta2.LastSpecKey], pendingContainers); err != nil {
				return &requeue{curError: err, delayedRequeue: true}
			}
			return wait
		}
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			pending = pending || len(pendingContainers) > 0
			continue
		}
		desired, err := internal.GetPodSpec(cluster, group)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
		updates, err := internal.GetInPlaceImageUpdates(cluster, pod, desired)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
		if len(pendingContainers) > 0 {
			// A corrected image or rollback must be allowed even while the previous image cannot start
			if len(updates) > 0 {
				if err = patchContainerImages(
					ctx,
					r,
					cluster,
					group,
					pod,
					desired,
					updates,
				); err != nil {
					return &requeue{curError: err, delayedRequeue: true}
				}
				return wait
			}
			if internal.InPlaceImagesReady(pod, pendingContainers) {
				if err = r.PodLifecycleManager.UpdateContainerImages(ctx, r, cluster, pod, nil,
					pod.Annotations[fdbv1beta2.LastSpecKey], nil); err != nil {
					return &requeue{curError: err, delayedRequeue: true}
				}
			} else {
				pending = true
			}
		} else if candidate == nil && len(updates) > 0 &&
			canStartImageUpdate(
				group,
				pod,
				slices.Collect(maps.Keys(updates)),
			) && !cluster.IsBeingUpgraded() {
			candidate, candidateSpec, candidateUpdates = pod, desired, updates
			candidateGroup = group
		}
	}
	if pending {
		return wait
	}
	if candidate != nil {
		logger.Info(
			"Updating auxiliary container images in place",
			"pod",
			candidate.Name,
			"images",
			candidateUpdates,
		)
		if err := patchContainerImages(
			ctx,
			r,
			cluster,
			candidateGroup,
			candidate,
			candidateSpec,
			candidateUpdates,
		); err != nil {
			return &requeue{curError: err, delayedRequeue: true}
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
		if condition.ProcessGroupConditionType != fdbv1beta2.IncorrectPodSpec &&
			condition.ProcessGroupConditionType != fdbv1beta2.IncorrectPodMetadata &&
			condition.ProcessGroupConditionType != fdbv1beta2.PodFailing &&
			condition.ProcessGroupConditionType != fdbv1beta2.PodPending {
			return false
		}
	}
	return internal.ContainersReadyExcept(pod, updating)
}

func patchContainerImages(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	group *fdbv1beta2.ProcessGroupStatus,
	pod *corev1.Pod,
	desired *corev1.PodSpec,
	updates map[string]string,
) error {
	names := append(
		internal.PendingImageUpdateContainers(cluster, pod),
		slices.Collect(maps.Keys(updates))...)
	slices.Sort(names)
	names = slices.Compact(names)
	hash, err := internal.GetJSONHash(desired)
	if err != nil {
		return err
	}
	if err = r.PodLifecycleManager.UpdateContainerImages(
		ctx,
		r,
		cluster,
		pod,
		updates,
		hash,
		names,
	); err != nil {
		return err
	}
	// Later replacement reconcilers must not act on a failure now covered by an accepted image repair
	group.UpdateCondition(fdbv1beta2.UpdatingContainerImages, true)
	if internal.ContainersReadyExcept(pod, names) {
		group.UpdateCondition(fdbv1beta2.PodFailing, false)
		group.UpdateCondition(fdbv1beta2.PodPending, false)
	}
	return nil
}
