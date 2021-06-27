/*
 * choose_removals_test.go
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("choose_removals", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var adminClient *MockAdminClient
	var err error
	var requeue *Requeue
	var removals []string

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

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = ChooseRemovals{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		removals = nil
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.Remove {
				removals = append(removals, processGroup.ProcessGroupID)
			}
		}

	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not mark any removals", func() {
			Expect(removals).To(BeNil())
		})
	})

	Context("with a decreased process count", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 3
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should mark one of the process groups for removal", func() {
			Expect(removals).To(Equal([]string{"storage-4"}))
		})

		Context("with a process group already marked", func() {
			BeforeEach(func() {
				processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-2"))
				processGroup.Remove = true
				err = clusterReconciler.Status().Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should leave that process group for removal", func() {
				Expect(removals).To(Equal([]string{"storage-2"}))
			})
		})

		Context("with multiple processes on one rack", func() {
			BeforeEach(func() {
				adminClient.MockLocalityInfo("storage-1", map[string]string{fdbtypes.FDBLocalityZoneIDKey: "r1"})
				adminClient.MockLocalityInfo("storage-2", map[string]string{fdbtypes.FDBLocalityZoneIDKey: "r1"})
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should mark one of the process groups on that rack for removal", func() {
				Expect(removals).To(Equal([]string{"storage-2"}))
			})
		})
	})

	Context("with a decrease to multiple process counts", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 3
			cluster.Spec.ProcessCounts.ClusterController = 0
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should mark two of the process groups for removal", func() {
			Expect(removals).To(Equal([]string{"cluster_controller-1", "storage-4"}))
		})
	})

})
