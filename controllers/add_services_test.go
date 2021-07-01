/*
 * add_services_test.go
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
	"sort"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("add_services", func() {

	var cluster *fdbtypes.FoundationDBCluster
	var err error
	var requeue *Requeue
	var initialServices *corev1.ServiceList
	var newServices *corev1.ServiceList

	BeforeEach(func() {
		cluster = createDefaultCluster()
		source := fdbtypes.PublicIPSourceService
		cluster.Spec.Services.PublicIPSource = &source
		enabled := true
		cluster.Spec.Services.Headless = &enabled

		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialServices = &corev1.ServiceList{}
		err = k8sClient.List(context.TODO(), initialServices)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = AddServices{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		newServices = &corev1.ServiceList{}
		err = k8sClient.List(context.TODO(), newServices)
		Expect(err).NotTo(HaveOccurred())
		sort.Slice(newServices.Items, func(i1, i2 int) bool {
			return newServices.Items[i1].Name < newServices.Items[i2].Name
		})
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create any services", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
		})
	})

	Context("with a process group with no service defined", func() {
		BeforeEach(func() {
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbtypes.NewProcessGroupStatus("storage-9", "storage", nil))
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should create an extra service", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items) + 1))
			lastService := newServices.Items[len(newServices.Items)-1]
			Expect(lastService.Name).To(Equal("operator-test-1-storage-9"))
			Expect(lastService.Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("storage-9"))
			Expect(lastService.Labels[fdbtypes.FDBProcessClassLabel]).To(Equal("storage"))
			Expect(lastService.Spec.ClusterIP).NotTo(Equal("None"))
			Expect(lastService.OwnerReferences).To(Equal(buildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)))
		})

		Context("with the pod public IP source", func() {
			BeforeEach(func() {
				source := fdbtypes.PublicIPSourcePod
				cluster.Spec.Services.PublicIPSource = &source
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not create any services", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
			})
		})

		Context("when the process group is being removed", func() {
			BeforeEach(func() {
				cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].Remove = true
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not create any pods", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
			})
		})
	})

	Context("with no headless service", func() {
		BeforeEach(func() {
			service := &corev1.Service{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, service)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should create a headless service", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))

			firstService := newServices.Items[0]

			Expect(firstService.Name).To(Equal("operator-test-1"))
			Expect(firstService.Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal(""))
			Expect(firstService.Spec.ClusterIP).To(Equal("None"))
		})

		Context("with the headless service disabled", func() {
			BeforeEach(func() {
				enabled := false
				cluster.Spec.Services.Headless = &enabled
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not create any services", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items) - 1))
			})
		})
	})
})
