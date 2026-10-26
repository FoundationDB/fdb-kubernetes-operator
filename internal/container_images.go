/*
 * container_images.go
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

package internal

import (
	"slices"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
)

// InPlaceImageUpdateAnnotation persists the containers awaiting kubelet confirmation across operator restarts
const InPlaceImageUpdateAnnotation = "foundationdb.org/in-place-image-update"

// GetInPlaceImageUpdates verifies the previous rendered spec before allowing only selected image changes
func GetInPlaceImageUpdates(
	cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod,
	desired *corev1.PodSpec,
) (map[string]string, error) {
	if len(cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers) == 0 {
		return nil, nil
	}
	previous := desired.DeepCopy()
	updates := make(map[string]string)
	for index := range previous.Containers {
		container := &previous.Containers[index]
		if !cluster.AllowsInPlaceImageUpdate(container.Name) {
			continue
		}
		for _, current := range pod.Spec.Containers {
			if current.Name == container.Name && current.Image != container.Image {
				updates[container.Name] = container.Image
				container.Image = current.Image
				break
			}
		}
	}
	if len(updates) == 0 {
		return nil, nil
	}
	// Live Pods contain admission defaults and node assignments absent from the recorded template
	hash, err := GetJSONHash(previous)
	if err != nil || hash != pod.Annotations[fdbv1beta2.LastSpecKey] {
		return nil, err
	}
	return updates, nil
}

// PendingImageUpdateContainers excludes containers whose in-place update policy was removed
func PendingImageUpdateContainers(
	cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod,
) []string {
	var pending []string
	for name := range strings.SplitSeq(pod.Annotations[InPlaceImageUpdateAnnotation], ",") {
		if name != "" && cluster.AllowsInPlaceImageUpdate(name) {
			pending = append(pending, name)
		}
	}
	return pending
}

// ContainersReadyExcept requires status for every container outside the supplied update set
func ContainersReadyExcept(pod *corev1.Pod, updating []string) bool {
	for _, container := range pod.Spec.Containers {
		if slices.Contains(updating, container.Name) {
			continue
		}
		index := slices.IndexFunc(
			pod.Status.ContainerStatuses,
			func(status corev1.ContainerStatus) bool {
				return status.Name == container.Name
			},
		)
		if index < 0 || !pod.Status.ContainerStatuses[index].Ready ||
			pod.Status.ContainerStatuses[index].State.Running == nil {
			return false
		}
	}
	return true
}

// InPlaceImagesReady requires kubelet status for the requested images, not just an accepted Pod update
func InPlaceImagesReady(pod *corev1.Pod, pending []string) bool {
	for _, name := range pending {
		index := slices.IndexFunc(
			pod.Spec.Containers,
			func(container corev1.Container) bool { return container.Name == name },
		)
		statusIndex := slices.IndexFunc(
			pod.Status.ContainerStatuses,
			func(status corev1.ContainerStatus) bool { return status.Name == name },
		)
		if index < 0 || statusIndex < 0 {
			return false
		}
		status := pod.Status.ContainerStatuses[statusIndex]
		if !status.Ready || status.State.Running == nil ||
			!sameImage(pod.Spec.Containers[index].Image, status) {
			return false
		}
	}
	return true
}

func sameImage(desired string, status corev1.ContainerStatus) bool {
	desiredRef, err := reference.ParseNormalizedNamed(desired)
	if err != nil {
		return false
	}
	// Runtimes may report a cached tag in Image even when the container was started by digest
	if canonical, ok := desiredRef.(reference.Canonical); ok {
		imageID := status.ImageID
		if _, id, prefixed := strings.Cut(imageID, "://"); prefixed {
			imageID = id
		}
		if imageID == canonical.Digest().String() {
			return true
		}
		if resolved, parseErr := reference.ParseNormalizedNamed(imageID); parseErr == nil {
			if resolvedCanonical, resolvedOK := resolved.(reference.Canonical); resolvedOK &&
				resolvedCanonical.Digest() == canonical.Digest() {
				return true
			}
		}
	}
	observedRef, err := reference.ParseNormalizedNamed(status.Image)
	return err == nil &&
		reference.TagNameOnly(desiredRef).String() == reference.TagNameOnly(observedRef).String()
}
