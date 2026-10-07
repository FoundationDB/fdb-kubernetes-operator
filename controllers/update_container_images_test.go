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
	"time"

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
	setImage := func(image string) {
		containers := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers
		for index := range containers {
			if containers[index].Name == containerName {
				containers[index].Image = image
			}
		}
		cluster.Generation++
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
	}
	step := func() {
		_, err := clusterReconciler.Reconcile(
			ctx,
			ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
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
	pendingPods := func() []*corev1.Pod {
		var pending []*corev1.Pod
		for _, pod := range listPods() {
			if pod.Annotations[internal.InPlaceImageUpdateAnnotation] != "" {
				pending = append(pending, pod)
			}
		}
		return pending
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

	It(
		"updates one Pod at a time and preserves Pod identity without requesting FDB restarts",
		func() {
			desiredImage := "example/log-forwarder@sha256:" + strings.Repeat("a", 64)
			setImage(desiredImage)
			for range len(originalPods) {
				step()
				pending := pendingPods()
				Expect(pending).To(HaveLen(1))
				Expect(cluster.Status.Generations.Reconciled).NotTo(Equal(cluster.Generation))
				assertIdentity()
				Expect(pending[0].Spec.Containers).To(ContainElement(And(
					HaveField("Name", containerName), HaveField("Image", desiredImage),
				)))
				// An accepted patch with stale kubelet status must not release the next Pod
				step()
				stillPending := pendingPods()
				Expect(stillPending).To(HaveLen(1))
				Expect(stillPending[0].Name).To(Equal(pending[0].Name))
				confirmImages(stillPending[0])
			}
			step()
			Expect(pendingPods()).To(BeEmpty())
			Expect(cluster.Status.Generations.Reconciled).To(Equal(cluster.Generation))
			assertIdentity()
			for _, pod := range listPods() {
				Expect(pod.Spec.Containers).To(ContainElement(And(
					HaveField("Name", containerName), HaveField("Image", desiredImage),
				)))
			}
		},
	)

	DescribeTable("corrects an unhealthy auxiliary image without replacement and accepts rollback",
		func(phase corev1.PodPhase) {
			pod := originalPods[cluster.Status.ProcessGroups[0].GetPodName(cluster)].DeepCopy()
			pod.Status.Phase = phase
			for index := range pod.Status.ContainerStatuses {
				status := &pod.Status.ContainerStatuses[index]
				if status.Name == containerName {
					status.Ready = false
					status.State = corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
					}
				}
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
			group := cluster.Status.ProcessGroups[0]
			group.UpdateCondition(fdbv1beta2.PodFailing, true)
			group.UpdateCondition(fdbv1beta2.PodPending, phase == corev1.PodPending)
			for index := range group.ProcessGroupConditions {
				group.ProcessGroupConditions[index].Timestamp = time.Now().
					Add(-24 * time.Hour).
					Unix()
			}
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			setImage("example/log-forwarder:missing")
			step()
			pending := pendingPods()
			Expect(pending).To(HaveLen(1))
			Expect(pending[0].Name).To(Equal(pod.Name))
			Expect(pending[0].Spec.Containers).To(ContainElement(And(
				HaveField(
					"Name",
					containerName,
				),
				HaveField("Image", "example/log-forwarder:missing"),
			)))
			assertIdentity()
			for _, group := range cluster.Status.ProcessGroups {
				Expect(group.GetConditionTime(fdbv1beta2.PodFailing)).To(BeNil())
				Expect(group.GetConditionTime(fdbv1beta2.PodPending)).To(BeNil())
			}
			step()
			Expect(pendingPods()).To(HaveLen(1))
			Expect(cluster.Status.Generations.Reconciled).NotTo(Equal(cluster.Generation))
			assertIdentity()

			setImage("example/log-forwarder:1")
			step()
			pending = pendingPods()
			Expect(pending).To(HaveLen(1))
			Expect(pending[0].Name).To(Equal(pod.Name))
			Expect(pending[0].Spec.Containers).To(ContainElement(And(
				HaveField("Name", containerName), HaveField("Image", "example/log-forwarder:1"),
			)))
			confirmImages(pending[0])
			step()
			Expect(pendingPods()).To(BeEmpty())
			Expect(cluster.Status.Generations.Reconciled).To(Equal(cluster.Generation))
			assertIdentity()
		},
		Entry("Running Pod with an expired failure condition", corev1.PodRunning),
		Entry("Pending Pod with expired failure conditions", corev1.PodPending),
	)

	When("multiple auxiliary containers are selected", func() {
		const metricsContainerName = "metrics-exporter"

		BeforeEach(func() {
			template := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate
			template.Spec.Containers = append(template.Spec.Containers,
				corev1.Container{Name: metricsContainerName, Image: "example/metrics-exporter:1"})
			cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{
				containerName,
				metricsContainerName,
			}
		})

		It(
			"releases a deselected container with an unverifiable image and updates the remaining selection",
			func() {
				setImage("example/log-forwarder:2")
				for range len(originalPods) - 1 {
					step()
					pending := pendingPods()
					Expect(pending).To(HaveLen(1))
					confirmImages(pending[0])
				}
				step()
				pending := pendingPods()
				Expect(pending).To(HaveLen(1))
				pod := pending[0]
				for index := range pod.Status.ContainerStatuses {
					status := &pod.Status.ContainerStatuses[index]
					if status.Name == containerName {
						status.Image = "runtime/unverifiable-alias:cached"
					}
				}
				Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
				step()
				Expect(pendingPods()).To(HaveLen(1))
				Expect(cluster.Status.Generations.Reconciled).NotTo(Equal(cluster.Generation))

				pod.Status.Phase = corev1.PodUnknown
				Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
				cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{
					metricsContainerName,
				}
				cluster.Generation++
				Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
				step()
				Expect(pendingPods()).To(BeEmpty())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
				confirmImages(pod)
				step()
				Expect(cluster.Status.Generations.Reconciled).To(Equal(cluster.Generation))
				assertIdentity()

				containers := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers
				for index := range containers {
					if containers[index].Name == metricsContainerName {
						containers[index].Image = "example/metrics-exporter:2"
					}
				}
				cluster.Generation++
				Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
				for range len(originalPods) {
					step()
					pending = pendingPods()
					Expect(pending).To(HaveLen(1))
					Expect(
						pending[0].Annotations[internal.InPlaceImageUpdateAnnotation],
					).To(Equal(metricsContainerName))
					Expect(pending[0].Spec.Containers).To(ContainElement(And(
						HaveField(
							"Name",
							metricsContainerName,
						),
						HaveField("Image", "example/metrics-exporter:2"),
					)))
					confirmImages(pending[0])
				}
				step()
				Expect(pendingPods()).To(BeEmpty())
				Expect(cluster.Status.Generations.Reconciled).To(Equal(cluster.Generation))
				assertIdentity()
			},
		)
	})

	DescribeTable(
		"keeps the normal rollout for other changes",
		func(mutate func()) {
			mutate()
			setImage("example/log-forwarder:2")
			step()
			Expect(pendingPods()).To(BeEmpty())
			var replaced bool
			for _, group := range cluster.Status.ProcessGroups {
				replaced = replaced || group.IsMarkedForRemoval()
			}
			Expect(replaced).To(BeTrue())
		},
		Entry(
			"without opt-in",
			func() { cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = nil },
		),
		Entry("with an argument change", func() {
			containers := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers
			for index := range containers {
				if containers[index].Name == containerName {
					containers[index].Args = []string{"--changed"}
				}
			}
		}),
	)

	It("still replaces failed FDB processes during an auxiliary image rollout", func() {
		setImage("example/log-forwarder:2")
		step()
		pending := pendingPods()
		Expect(pending).To(HaveLen(1))
		pod := pending[0]
		for index := range pod.Status.ContainerStatuses {
			status := &pod.Status.ContainerStatuses[index]
			if status.Name == fdbv1beta2.MainContainerName {
				status.Ready = false
				status.State = corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				}
			}
		}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		for _, group := range cluster.Status.ProcessGroups {
			if group.GetPodName(cluster) == pod.Name {
				group.UpdateCondition(fdbv1beta2.PodFailing, true)
				for index := range group.ProcessGroupConditions {
					group.ProcessGroupConditions[index].Timestamp = time.Now().
						Add(-24 * time.Hour).
						Unix()
				}
			}
		}
		Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
		step()
		var replaced bool
		for _, group := range cluster.Status.ProcessGroups {
			if group.GetPodName(cluster) == pod.Name {
				replaced = group.IsMarkedForRemoval()
			}
		}
		Expect(replaced).To(BeTrue())
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
