/*
 * add_pvcs_test.go
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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("add_pvcs", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error
	var requeue *Requeue
	var initialPVCs *corev1.PersistentVolumeClaimList
	var newPVCs *corev1.PersistentVolumeClaimList

	BeforeEach(func() {
		cluster = createDefaultCluster()
		err = internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialPVCs = &corev1.PersistentVolumeClaimList{}
		err = k8sClient.List(context.TODO(), initialPVCs)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = AddPVCs{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		newPVCs = &corev1.PersistentVolumeClaimList{}
		err = k8sClient.List(context.TODO(), newPVCs)
		Expect(err).NotTo(HaveOccurred())
		sort.Slice(newPVCs.Items, func(i1, i2 int) bool {
			return newPVCs.Items[i1].Name < newPVCs.Items[i2].Name
		})
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create any PVCs", func() {
			Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items)))
		})
	})

	Context("with a storage process group with no PVC defined", func() {
		BeforeEach(func() {
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbtypes.NewProcessGroupStatus("storage-9", "storage", nil))
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should create an extra PVC", func() {
			Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items) + 1))
			lastPVC := newPVCs.Items[len(newPVCs.Items)-1]
			Expect(lastPVC.Name).To(Equal("operator-test-1-storage-9-data"))
			Expect(lastPVC.Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("storage-9"))
			Expect(lastPVC.Labels[fdbtypes.FDBProcessClassLabel]).To(Equal("storage"))

			Expect(lastPVC.OwnerReferences).To(Equal(buildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)))
		})

		Context("when the process group is being removed", func() {
			BeforeEach(func() {
				cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].Remove = true
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not create any PVCs", func() {
				Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items)))
			})
		})
	})

	Context("with a stateless process group with no PVC defined", func() {
		BeforeEach(func() {
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbtypes.NewProcessGroupStatus("stateless-9", "stateless", nil))
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create an extra PVC", func() {
			Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items)))
		})
	})
})
