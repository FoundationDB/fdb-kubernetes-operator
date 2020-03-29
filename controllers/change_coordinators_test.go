/*
 * change_coordinators_test.go
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

var _ = Describe("ChangeCoordinators", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var originalConnectionString fdbtypes.ConnectionString
	var adminClient *MockAdminClient

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true

		originalConnectionString, err = fdbtypes.ParseConnectionString(cluster.Spec.ConnectionString)
		Expect(err).NotTo(HaveOccurred())

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(ChangeCoordinators{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not change the coordinators", func() {
			Expect(cluster.Spec.ConnectionString).To(Equal(originalConnectionString.String()))
		})
	})

	Context("with an excluded coordinator", func() {
		BeforeEach(func() {
			status, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
			coordinator := status.Client.Coordinators.Coordinators[0].Address
			err = adminClient.ExcludeInstances([]string{coordinator})
			Expect(err).NotTo(HaveOccurred())
			shouldContinue = false
		})

		It("should not give an error", func() {
			Expect(err).To(BeNil())
		})

		It("should change the coordinators", func() {
			Expect(cluster.Spec.ConnectionString).NotTo(Equal(originalConnectionString.String()))
		})
	})
})
