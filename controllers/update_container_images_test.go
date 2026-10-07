/*
 * update_container_images_test.go
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
	"slices"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("In-place container image reconciliation", func() {
	const containerName = "log-forwarder"
	var cluster *fdbv1beta2.FoundationDBCluster
	var originalPods map[string]*corev1.Pod
	var adminClient *mock.AdminClient
	ctx := context.Background()

	listPods := func() []*corev1.Pod {
		pods, err := clusterReconciler.PodLifecycleManager.GetPods(ctx, k8sClient, cluster)
		Expect(err).NotTo(HaveOccurred())
		return pods
	}
	confirmImages := func(pod *corev1.Pod) {
		pod.Status.Phase = corev1.PodRunning
		pod.Status.ContainerStatuses = nil
		for _, container := range pod.Spec.Containers {
			image := container.Image
			var imageID string
			if container.Name == containerName {
				image = "docker.io/" + image
				if strings.Contains(container.Image, "@sha256:") {
					image = "docker.io/example/log-forwarder:cached-tag"
					imageID = "docker-pullable://" + container.Image
				}
			}
			pod.Status.ContainerStatuses = append(
				pod.Status.ContainerStatuses,
				corev1.ContainerStatus{
					Name: container.Name, Image: image, ImageID: imageID, Ready: true,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			)
		}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}
	assertIdentity := func() {
		Expect(adminClient.KilledAddresses).To(BeEmpty())
		pods := listPods()
		Expect(pods).To(HaveLen(len(originalPods)))
		for _, pod := range pods {
			original := originalPods[pod.Name]
			Expect(original).NotTo(BeNil())
			Expect(pod.UID).To(Equal(original.UID))
			Expect(pod.Spec.NodeName).To(Equal(original.Spec.NodeName))
			Expect(pod.Status.PodIP).To(Equal(original.Status.PodIP))
			Expect(pod.Spec.Volumes).To(Equal(original.Spec.Volumes))
			Expect(pod.Spec.InitContainers).To(Equal(original.Spec.InitContainers))
			for _, container := range original.Spec.Containers {
				if container.Name == fdbv1beta2.MainContainerName ||
					container.Name == fdbv1beta2.SidecarContainerName {
					index := slices.IndexFunc(
						pod.Spec.Containers,
						func(item corev1.Container) bool { return item.Name == container.Name },
					)
					Expect(index).To(BeNumerically(">=", 0))
					Expect(pod.Spec.Containers[index]).To(Equal(container))
				}
			}
		}
		for _, group := range cluster.Status.ProcessGroups {
			Expect(group.IsMarkedForRemoval()).To(BeFalse())
		}
	}

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = fdbv1beta2.ProcessSettings{
			PodTemplate: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: containerName, Image: "example/log-forwarder:1"},
				},
			}},
		}
		cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{containerName}
	})

	JustBeforeEach(func() {
		Expect(setupClusterForTest(cluster)).To(Succeed())
		var err error
		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		clear(adminClient.KilledAddresses)
		originalPods = make(map[string]*corev1.Pod)
		for _, pod := range listPods() {
			confirmImages(pod)
			originalPods[pod.Name] = pod.DeepCopy()
		}
	})

	It("rejects opting FDB-managed or init containers into independent image updates", func() {
		rejectUpdate := func() {
			cluster.Generation++
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
			_, err := clusterReconciler.Reconcile(
				ctx,
				ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)},
			)
			Expect(err).To(MatchError(ContainSubstring("cannot be updated in place")))
			assertIdentity()
		}
		for _, name := range []string{fdbv1beta2.MainContainerName, fdbv1beta2.SidecarContainerName, fdbv1beta2.InitContainerName} {
			cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{name}
			rejectUpdate()
		}
		cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.InitContainers = []corev1.Container{
			{Name: "bootstrap", Image: "example/bootstrap:1"},
		}
		cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{"bootstrap"}
		rejectUpdate()
	})
})
