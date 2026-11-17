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
	"errors"
	"slices"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("In-place container image reconciliation", func() {
	const auxiliary = "log-forwarder"
	const initialImage = "example/log-forwarder:1"
	var cluster *fdbv1beta2.FoundationDBCluster
	ctx := context.Background()
	listPods := func() []*corev1.Pod {
		pods, err := clusterReconciler.PodLifecycleManager.GetPods(ctx, k8sClient, cluster)
		Expect(err).NotTo(HaveOccurred())
		return pods
	}
	template := func(name string) *corev1.Container {
		containers := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers
		return &containers[slices.IndexFunc(containers, func(c corev1.Container) bool { return c.Name == name })]
	}
	setImage := func(image string) {
		template(auxiliary).Image = image
		cluster.Generation++
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
	}
	step := func() error {
		_, err := clusterReconciler.Reconcile(
			ctx,
			ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)},
		)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		return err
	}
	pendingPods := func() []*corev1.Pod {
		return slices.DeleteFunc(listPods(), func(pod *corev1.Pod) bool {
			return pod.Annotations[internal.InPlaceImageUpdateAnnotation] == ""
		})
	}
	onlyPending := func() *corev1.Pod {
		pods := pendingPods()
		Expect(pods).To(HaveLen(1))
		return pods[0]
	}
	status := func(pod *corev1.Pod, name string) *corev1.ContainerStatus {
		return &pod.Status.ContainerStatuses[slices.IndexFunc(pod.Status.ContainerStatuses,
			func(s corev1.ContainerStatus) bool { return s.Name == name })]
	}
	confirm := func(pod *corev1.Pod, image string) {
		s := status(pod, auxiliary)
		s.ContainerID += "-restarted"
		s.Image = "docker.io/" + strings.TrimPrefix(image, "docker.io/")
		s.ImageID = image
		s.Ready = true
		s.State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
		pod.Status.Phase = corev1.PodRunning
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}
	fail := func(pod *corev1.Pod, name string, phase corev1.PodPhase) {
		pod.Status.Phase = phase
		s := status(pod, name)
		s.Ready, s.State = false, corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
		}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		group := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups,
			internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta))
		group.UpdateCondition(fdbv1beta2.PodFailing, true)
		group.UpdateCondition(fdbv1beta2.PodPending, phase == corev1.PodPending)
		for _, condition := range group.ProcessGroupConditions {
			condition.Timestamp = time.Now().Add(-24 * time.Hour).Unix()
		}
		Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
	}
	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = fdbv1beta2.ProcessSettings{
			PodTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{
						Name:  auxiliary,
						Image: initialImage,
					}, {Name: "metrics", Image: "example/metrics:1"},
				}},
			},
		}
		cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{auxiliary, "metrics"}
		Expect(setupClusterForTest(cluster)).To(Succeed())
		for _, pod := range listPods() {
			pod.Status.ContainerStatuses = nil
			for _, c := range pod.Spec.Containers {
				pod.Status.ContainerStatuses = append(
					pod.Status.ContainerStatuses,
					corev1.ContainerStatus{
						Name: c.Name, Image: "docker.io/" + c.Image, ContainerID: c.Name, Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				)
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		}
	})

	It("waits for DNS publication and a ready digest before advancing the rollout", func() {
		service := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), service)).To(Succeed())
		service.Spec.PublishNotReadyAddresses = false
		Expect(k8sClient.Update(ctx, service)).To(Succeed())
		desired := "example/log-forwarder@sha256:" + strings.Repeat("a", 64)
		setImage(desired)
		Expect(step()).To(Succeed())
		Expect(pendingPods()).To(BeEmpty())
		admin, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		clear(admin.KilledAddresses)
		for _, group := range cluster.Status.ProcessGroups {
			admin.RunLoopBusy[group.ProcessGroupID] = clusterReconciler.HighRunLoopBusyThreshold
		}
		Expect(step()).To(Succeed())
		pod := onlyPending()
		clear(admin.RunLoopBusy)
		Expect(
			pod.Spec.Containers,
		).To(ContainElement(And(HaveField("Name", auxiliary), HaveField("Image", desired))))
		Expect(step()).To(Succeed())
		Expect(onlyPending().UID).To(Equal(pod.UID))
		confirm(onlyPending(), desired)
		// Runtimes may report a cached tag alongside the resolved digest
		confirmed := onlyPending()
		status(confirmed, auxiliary).Image = initialImage
		Expect(k8sClient.Status().Update(ctx, confirmed)).To(Succeed())
		Expect(step()).To(Succeed())
		Expect(onlyPending().UID).NotTo(Equal(pod.UID))
		Expect(admin.KilledAddresses).To(BeEmpty())
	})

	It("waits for an equivalent reference restart but permits rollback before it starts", func() {
		setImage("docker.io/" + initialImage)
		Expect(step()).To(Succeed())
		pod := onlyPending()
		Expect(step()).To(Succeed())
		Expect(onlyPending().UID).To(Equal(pod.UID))
		setImage(initialImage)
		Expect(step()).To(Succeed())
		Expect(step()).To(Succeed())
		Expect(pendingPods()).To(BeEmpty())
		Expect(cluster.Status.Generations.Reconciled).To(Equal(cluster.Generation))
	})

	It("rejects a stale marker clear with Patch", func() {
		clusterReconciler.PodLifecycleManager.(podmanager.PodLifecycleManagerWithPodUpdateMethod).SetUpdateMethod(
			podmanager.Patch,
		)
		setImage("example/log-forwarder:2")
		Expect(step()).To(Succeed())
		pod := onlyPending()
		confirm(pod, "example/log-forwarder:2")
		marker := pod.Annotations[internal.InPlaceImageUpdateAnnotation]
		clusterReconciler.Client = interceptor.NewClient(k8sClient, interceptor.Funcs{
			Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if obj.GetName() == pod.Name {
					pod.Labels["concurrent"] = "update"
					Expect(cli.Update(ctx, pod)).To(Succeed())
				}
				return cli.Patch(ctx, obj, patch, opts...)
			},
		})
		Expect(step()).To(Succeed())
		Expect(onlyPending().Annotations[internal.InPlaceImageUpdateAnnotation]).To(Equal(marker))
	})

	DescribeTable(
		"preserves an interrupted repair through stale cache and accepts rollback",
		func(phase corev1.PodPhase) {
			pod := listPods()[0]
			fail(pod, auxiliary, phase)
			cached := pod.DeepCopy()
			cached.Status.Phase = corev1.PodUnknown
			setImage("example/log-forwarder:missing")
			injected := errors.New("service unavailable after repair")
			clusterReconciler.Client = interceptor.NewClient(k8sClient, interceptor.Funcs{
				Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Service); ok && len(pendingPods()) > 0 {
						return injected
					}
					return cli.Get(ctx, key, obj, opts...)
				},
			})
			Expect(step()).To(MatchError(injected))
			Expect(onlyPending().UID).To(Equal(pod.UID))
			clusterReconciler.Client = interceptor.NewClient(k8sClient, interceptor.Funcs{
				Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if result, ok := obj.(*corev1.Pod); ok && key.Name == cached.Name {
						cached.DeepCopyInto(result)
						return nil
					}
					return cli.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if err := cli.List(ctx, list, opts...); err != nil {
						return err
					}
					if pods, ok := list.(*corev1.PodList); ok {
						for index := range pods.Items {
							if pods.Items[index].Name == cached.Name {
								cached.DeepCopyInto(&pods.Items[index])
							}
						}
					}
					return nil
				},
			})
			Expect(step()).To(Succeed())
			Expect(onlyPending().UID).To(Equal(pod.UID))
			Expect(
				cluster.Status.ProcessGroups,
			).NotTo(ContainElement(HaveField("RemovalTimestamp", Not(BeNil()))))
			clusterReconciler.APIReader = interceptor.NewClient(k8sClient, interceptor.Funcs{
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return injected
				},
			})
			Expect(step()).To(MatchError(injected))
			clusterReconciler.APIReader = k8sClient
			clusterReconciler.Client = k8sClient
			setImage(initialImage)
			Expect(step()).To(Succeed())
			confirm(onlyPending(), initialImage)
			Expect(step()).To(Succeed())
			Expect(pendingPods()).To(BeEmpty())
		},
		Entry("Running", corev1.PodRunning),
		Entry("Pending", corev1.PodPending),
	)

	It("cancels deselected containers even when a Pod is Unknown", func() {
		template("metrics").Image = "example/metrics:2"
		setImage("example/log-forwarder:2")
		Expect(step()).To(Succeed())
		pod := onlyPending()
		pod.Status.Phase = corev1.PodUnknown
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = []string{"metrics"}
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		Expect(step()).To(Succeed())
		Expect(onlyPending().Annotations[internal.InPlaceImageUpdateAnnotation]).To(And(
			ContainSubstring("metrics"), Not(ContainSubstring(auxiliary))))
	})

	DescribeTable("retains ordinary replacement", func(change string) {
		if change == "opt-out" {
			cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = nil
		}
		if change == "arguments" {
			template(auxiliary).Args = []string{"--changed"}
		}
		setImage("example/log-forwarder:2")
		Expect(step()).To(Succeed())
		if change == "FDB failure" || change == "cancel failure" {
			name := fdbv1beta2.MainContainerName
			if change == "cancel failure" {
				name = auxiliary
				cluster.Spec.AutomationOptions.InPlaceImageUpdateContainers = nil
				Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
			}
			fail(onlyPending(), name, corev1.PodRunning)
			Expect(step()).To(Succeed())
		}
		if change != "FDB failure" {
			Expect(pendingPods()).To(BeEmpty())
		}
		Expect(
			cluster.Status.ProcessGroups,
		).To(ContainElement(HaveField("RemovalTimestamp", Not(BeNil()))))
	}, Entry("opt-out", "opt-out"), Entry("arguments", "arguments"),
		Entry("FDB failure", "FDB failure"), Entry("cancel failure", "cancel failure"))
})
