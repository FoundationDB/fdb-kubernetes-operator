/*
 * choose_removals_test.go
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("ChooseRemovals", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var originalGeneration int64

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true
		originalGeneration = cluster.ObjectMeta.Generation
	})

	AfterEach(func() {
		if cluster.ObjectMeta.Generation > originalGeneration {
			Eventually(func() (int64, error) { return reloadCluster(k8sClient, cluster) }, 5).Should(BeNumerically(">", originalGeneration))
		}
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(ChooseRemovals{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not set any pending removals", func() {
			Expect(cluster.Spec.InstancesToRemove).To(BeNil())
		})
	})

	Context("with a reduced storage process count", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 3
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should set a single item in the instances to remove", func() {
			Expect(cluster.Spec.InstancesToRemove).To(Equal([]string{"storage-4"}))
		})

		It("should set the address in the pending removals map", func() {
			Expect(cluster.Spec.PendingRemovals).To(Equal(map[string]string{"operator-test-1-storage-4": "1.1.0.4"}))
		})

		Context("with an existing item in the instances to remove", func() {
			BeforeEach(func() {
				cluster.Spec.InstancesToRemove = []string{"storage-3"}
				cluster.Status.ProcessCounts.IncreaseCount("storage", -1)
			})

			It("should not return an error", func() {
				Expect(err).To(BeNil())
			})

			It("should not change the instances to remove", func() {
				Expect(cluster.Spec.InstancesToRemove).To(Equal([]string{"storage-3"}))
			})

			It("should set the address in the pending removals map", func() {
				Expect(cluster.Spec.PendingRemovals).To(Equal(map[string]string{"operator-test-1-storage-3": "1.1.0.3"}))
			})

			Context("with the address in the pending removals map already", func() {
				BeforeEach(func() {
					cluster.Spec.PendingRemovals = map[string]string{"operator-test-1-storage-3": "1.1.0.2"}
					shouldContinue = true
				})

				It("should not return an error", func() {
					Expect(err).To(BeNil())
				})

				It("should not change the instances to remove", func() {
					Expect(cluster.Spec.InstancesToRemove).To(Equal([]string{"storage-3"}))
				})

				It("should not change the address in the pending removals map", func() {
					Expect(cluster.Spec.PendingRemovals).To(Equal(map[string]string{"operator-test-1-storage-3": "1.1.0.2"}))
				})
			})
		})
	})
})
