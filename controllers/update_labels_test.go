/*
 * update_config_map_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("UpdateLabels", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var podName types.NamespacedName
	var pod *corev1.Pod
	var volumeClaimName types.NamespacedName
	var volumeClaim *corev1.PersistentVolumeClaim

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true

		pod = &corev1.Pod{}
		podName = types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-storage-2"}
		err = k8sClient.Get(context.TODO(), podName, pod)
		Expect(err).NotTo(HaveOccurred())

		volumeClaim = &corev1.PersistentVolumeClaim{}
		volumeClaimName = types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-storage-2-data"}
		err = k8sClient.Get(context.TODO(), volumeClaimName, volumeClaim)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Eventually(func() (bool, error) { return checkClusterReconciled(k8sClient, cluster) }, 5).Should(BeTrue())
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(UpdateLabels{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})
	})

	Context("with a change to the pod labels", func() {
		BeforeEach(func() {
			cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"test-label": "test-value"},
				},
			}
		})

		It("should update the labels on the pods", func() {
			Eventually(func() (string, error) {
				err = k8sClient.Get(context.TODO(), podName, pod)
				return pod.ObjectMeta.Labels["test-label"], err
			}, 5).Should(Equal("test-value"))
		})
	})

	Context("with a change to the pod annotations", func() {
		BeforeEach(func() {
			cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"test-annotation": "test-value"},
				},
			}
		})

		It("should update the annotations on the pods", func() {
			Eventually(func() (string, error) {
				err = k8sClient.Get(context.TODO(), podName, pod)
				return pod.ObjectMeta.Annotations["test-annotation"], err
			}, 5).Should(Equal("test-value"))
		})
	})

	Context("with a change to the PVC labels", func() {
		BeforeEach(func() {
			cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"test-label": "test-value"},
				},
			}
		})

		It("should update the labels on the PVCs", func() {
			Eventually(func() (string, error) {
				err = k8sClient.Get(context.TODO(), volumeClaimName, volumeClaim)
				return volumeClaim.ObjectMeta.Labels["test-label"], err
			}, 5).Should(Equal("test-value"))
		})
	})

	Context("with a change to the pod annotations", func() {
		BeforeEach(func() {
			cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"test-annotation": "test-value"},
				},
			}
		})

		It("should update the annotations on the PVCs", func() {
			Eventually(func() (string, error) {
				err = k8sClient.Get(context.TODO(), volumeClaimName, volumeClaim)
				return volumeClaim.ObjectMeta.Annotations["test-annotation"], err
			}, 5).Should(Equal("test-value"))
		})
	})
})
