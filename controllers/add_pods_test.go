/*
 * add_pods_test.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("add_pods", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var initialPods *corev1.PodList
	var newPods *corev1.PodList
	var processGroupWithoutPod *fdbv1beta2.ProcessGroupStatus

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialPods = &corev1.PodList{}
		err = k8sClient.List(context.TODO(), initialPods)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = addPods{}.reconcile(context.TODO(), clusterReconciler, cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		newPods = &corev1.PodList{}
		err = k8sClient.List(context.TODO(), newPods)
		Expect(err).NotTo(HaveOccurred())
		sort.Slice(newPods.Items, func(i1, i2 int) bool {
			return newPods.Items[i1].Name < newPods.Items[i2].Name
		})
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create any pods", func() {
			Expect(newPods.Items).To(HaveLen(len(initialPods.Items)))
		})
	})

	Context("with a storage process group with no pod defined", func() {
		BeforeEach(func() {
			processGroupWithoutPod = fdbv1beta2.NewProcessGroupStatus("storage-9", "storage", []string{"100.101.102.103"})
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, processGroupWithoutPod)
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should create an extra pod", func() {
			expectNewPodToHaveBeenCreated(initialPods, newPods, cluster)
		})

		When("the process group is being removed", func() {
			BeforeEach(func() {
				processGroupWithoutPod.MarkForRemoval()
			})

			When("the process group is excluded", func() {
				BeforeEach(func() {
					// When the system is actually running, this would previously have been set by
					// the `removeProcessGroups` subreconciler and saved in the cluster status,
					// which would then be visible to the next run of the `addPods` subreconciler.
					processGroupWithoutPod.SetExclude()
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should not create any pods", func() {
					Expect(newPods.Items).To(HaveLen(len(initialPods.Items)))
				})
			})

			When("the process group is not excluded", func() {
				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should create an extra pod", func() {
					expectNewPodToHaveBeenCreated(initialPods, newPods, cluster)
				})
			})
		})
	})
})

func expectNewPodToHaveBeenCreated(initialPods *corev1.PodList, newPods *corev1.PodList, cluster *fdbv1beta2.FoundationDBCluster) {
	Expect(newPods.Items).To(HaveLen(len(initialPods.Items) + 1))
	lastPod := newPods.Items[len(newPods.Items)-1]
	Expect(lastPod.Name).To(Equal("operator-test-1-storage-9"))
	Expect(lastPod.Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("storage-9"))
	Expect(lastPod.Labels[fdbv1beta2.FDBProcessClassLabel]).To(Equal("storage"))
	Expect(lastPod.OwnerReferences).To(Equal(internal.BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)))
}
