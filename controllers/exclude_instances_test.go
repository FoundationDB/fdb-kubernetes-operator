/*
 * exclude_instances_test.go
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

var _ = Describe("ExcludeInstances", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var adminClient *MockAdminClient

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(ExcludeInstances{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not exclude anything", func() {
			Expect(adminClient.ExcludedAddresses).To(BeNil())
		})
	})

	Context("with a process in the pending removals map", func() {
		BeforeEach(func() {
			cluster.Spec.PendingRemovals = map[string]string{"operator-test-1-storage-4": "1.1.0.4"}
		})

		It("should exclude the processes", func() {
			Expect(adminClient.ExcludedAddresses).To(Equal([]string{"1.1.0.4:4501"}))
		})
	})
})
