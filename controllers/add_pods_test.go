/*
 * add_pods_test.go
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
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("AddPods", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var originalPods *corev1.PodList
	var shouldContinue bool
	var err error

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		originalPods = &corev1.PodList{}
		err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(originalPods.Items)).NotTo(Equal(0))
		shouldContinue = true
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(AddPods{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not create any pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))
		})
	})

	Context("with an increase in storage processes", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 5
		})

		It("should create a pod", func() {
			pods := &corev1.PodList{}
			Eventually(func() (int, error) {
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				return len(pods.Items), err
			}, 5).Should(Equal(len(originalPods.Items) + 1))

			sortPodsByID(pods)
			Expect(pods.Items[len(pods.Items)-1].Labels["fdb-instance-id"]).To(Equal("storage-5"))
		})

		Context("with a pod pending deletion", func() {
			BeforeEach(func() {
				now := metav1.Now()
				originalPods.Items[0].ObjectMeta.DeletionTimestamp = &now
				err = k8sClient.Delete(context.TODO(), &originalPods.Items[0])
				Expect(err).NotTo(HaveOccurred())
				shouldContinue = false
			})

			It("should give an error", func() {
				Expect(err).NotTo(BeNil())
				Expect(err.Error()).To(Equal("Cluster has pod that is pending deletion"))
			})
		})

		Context("with a re-usable PVC", func() {
			BeforeEach(func() {
				ref, err := buildOwnerReference(context.TODO(), cluster.TypeMeta, cluster.ObjectMeta, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "operator-test-1-storage-6-data",
						Namespace: cluster.Namespace,
						Labels: map[string]string{
							"fdb-cluster-name":  cluster.Name,
							"fdb-process-class": "storage",
							"fdb-instance-id":   "storage-6",
						},
						OwnerReferences: ref,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"storage": resource.MustParse("128Gi"),
							},
						},
					},
				}

				pvcs := &corev1.PersistentVolumeClaimList{}

				Eventually(func() (int, error) {
					err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
					return len(pvcs.Items), err
				}, 5).Should(Equal(8))

				err = k8sClient.Create(context.TODO(), pvc)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should create a pod to re-use the PVC", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, 5).Should(Equal(len(originalPods.Items) + 1))

				sortPodsByID(pods)
				Expect(pods.Items[len(pods.Items)-1].Labels["fdb-instance-id"]).To(Equal("storage-6"))
			})
		})
	})
})
