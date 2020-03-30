/*
 * set_default_values_test.go
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

var _ = Describe("SetDefaultValues", func() {
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

		Expect(cluster.Spec.RedundancyMode).To(Equal("double"))
		Expect(cluster.Spec.StorageEngine).To(Equal("ssd"))
		Expect(cluster.Spec.UsableRegions).To(Equal(1))
		Expect(cluster.Spec.RunningVersion).To(Equal(Versions.Default.String()))
	})

	AfterEach(func() {
		if cluster.ObjectMeta.Generation > originalGeneration {
			Eventually(func() (int64, error) { return reloadCluster(k8sClient, cluster) }, 5).Should(BeNumerically(">", originalGeneration))
		}
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(SetDefaultValues{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not change anything", func() {
			Expect(cluster.Spec.RedundancyMode).To(Equal("double"))
			Expect(cluster.Spec.StorageEngine).To(Equal("ssd"))
			Expect(cluster.Spec.UsableRegions).To(Equal(1))
			Expect(cluster.Spec.RunningVersion).To(Equal(Versions.Default.String()))
		})
	})

	Context("with no redundancy mode", func() {
		BeforeEach(func() {
			cluster.Spec.RedundancyMode = ""
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should set the redundancy mode to double", func() {
			Expect(cluster.Spec.RedundancyMode).To(Equal("double"))
		})
	})

	Context("with no storage engine", func() {
		BeforeEach(func() {
			cluster.Spec.StorageEngine = ""
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should set the storage engine to ssd", func() {
			Expect(cluster.Spec.StorageEngine).To(Equal("ssd"))
		})
	})

	Context("with no usable regions", func() {
		BeforeEach(func() {
			cluster.Spec.UsableRegions = 0
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should set the usable regions to 1", func() {
			Expect(cluster.Spec.UsableRegions).To(Equal(1))
		})
	})

	Context("with no runningVersion", func() {
		BeforeEach(func() {
			cluster.Spec.RunningVersion = ""
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should set the running version to the main version", func() {
			Expect(cluster.Spec.RunningVersion).To(Equal(cluster.Spec.Version))
		})
	})
})
