/*
 * generate_initial_cluster_file_test.go
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

var _ = Describe("GenerateInitialClusterFile", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var originalConnectionString string

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true
		originalConnectionString = cluster.Spec.ConnectionString
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(GenerateInitialClusterFile{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not change the connection string", func() {
			Expect(cluster.Spec.ConnectionString).To(Equal(originalConnectionString))
		})
	})

	Context("with an empty connection string", func() {
		BeforeEach(func() {
			cluster.Spec.ConnectionString = ""
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should generate a connection string", func() {
			connectionString, err := fdbtypes.ParseConnectionString(cluster.Spec.ConnectionString)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(connectionString.Coordinators)).To(Equal(3))
		})
	})
})
