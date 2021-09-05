/*
 * remove_process_groups_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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
	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("remove_process_groups", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var result *Requeue

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err := k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		originalPods := &corev1.PodList{}
		err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(originalPods.Items)).To(Equal(17))

		_, err = reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		result = RemoveProcessGroups{}.Reconcile(clusterReconciler, context.TODO(), cluster)
	})

	When("removing a process group", func() {
		BeforeEach(func() {
			removedProcessGroup := cluster.Status.ProcessGroups[0]
			_, _ = fdbtypes.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, removedProcessGroup.ProcessGroupID, removedProcessGroup.ProcessClass, removedProcessGroup.Addresses[0])
		})

		It("should successfully remove that process group", func() {
			Expect(result).To(BeNil())
		})

		When("enabling the EnforceFullReplicationForDeletion setting", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.EnforceFullReplicationForDeletion = pointer.BoolPtr(true)
			})

			When("the cluster is fully replicated", func() {
				It("should successfully remove that process group", func() {
					Expect(result).To(BeNil())
				})
			})

			When("the cluster is not fully replicated", func() {
				BeforeEach(func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.frozenStatus = &fdbtypes.FoundationDBStatus{
						Cluster: fdbtypes.FoundationDBStatusClusterInfo{
							FullReplication: false,
						},
					}
				})

				It("should not remove that process group", func() {
					Expect(result).NotTo(BeNil())
					Expect(result.Message).To(Equal("Cluster is not fully replicated but is required for removals"))
				})
			})
		})
	})

	AfterEach(func() {
		k8sClient.Clear()
	})
})
