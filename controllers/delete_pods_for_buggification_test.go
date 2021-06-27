/*
 * delete_pods_for_buggification_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("delete_pods_for_buggification", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error
	var requeue *Requeue
	var originalPods *corev1.PodList

	BeforeEach(func() {
		cluster = createDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		err = internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())

		originalPods = &corev1.PodList{}
		err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = DeletePodsForBuggification{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		if requeue != nil {
			Expect(requeue.Error).NotTo(HaveOccurred())
		}
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not delete any pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with a buggification that needs to be enabled", func() {
		BeforeEach(func() {
			cluster.Spec.Buggify.CrashLoop = []string{"storage-1"}
		})

		It("should requeue", func() {
			Expect(requeue).NotTo(BeNil())
			Expect(requeue.Message).To(Equal("Pods need to be recreated"))
		})

		It("should delete the pod", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("with a wildcard buggification", func() {
		BeforeEach(func() {
			cluster.Spec.Buggify.CrashLoop = []string{"*"}
		})

		It("should requeue", func() {
			Expect(requeue).NotTo(BeNil())
			Expect(requeue.Message).To(Equal("Pods need to be recreated"))
		})

		It("should delete the pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(0))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("with a buggification that needs to be disabled", func() {
		BeforeEach(func() {
			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			pod.Spec.Containers[0].Args = []string{"crash-loop"}
			err = k8sClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should requeue", func() {
			Expect(requeue).NotTo(BeNil())
			Expect(requeue.Message).To(Equal("Pods need to be recreated"))
		})

		It("should delete the pod", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("with a buggification that is already on", func() {
		BeforeEach(func() {
			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			pod.Spec.Containers[0].Args = []string{"crash-loop"}
			err = k8sClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			cluster.Spec.Buggify.CrashLoop = []string{"storage-1"}
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not delete any pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with a change to an environment variable", func() {
		BeforeEach(func() {
			cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foundationdb",
							Env: []corev1.EnvVar{
								{
									Name:  "TEST_CHANGE",
									Value: "1",
								},
							},
						},
					},
				},
			}}}
			err = internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not delete any pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
