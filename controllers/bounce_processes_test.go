/*
 * bounce_processes_test.go
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

var _ = Describe("BounceProcesses", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var adminClient *MockAdminClient
	var originalGeneration int64

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		shouldContinue = true
		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		originalGeneration = cluster.ObjectMeta.Generation
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(BounceProcesses{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not kill any processes", func() {
			Expect(adminClient.KilledAddresses).To(BeNil())
		})

		It("should not update the generation in the status", func() {
			Expect(cluster.Status.Generations).To(Equal(fdbtypes.ClusterGenerationStatus{Reconciled: originalGeneration}))
		})
	})

	Context("with incorrect processes in the status", func() {
		BeforeEach(func() {
			cluster.Status.IncorrectProcesses = map[string]int64{"storage-2": 0}
		})

		It("should kill the processes", func() {
			Expect(adminClient.KilledAddresses).To(Equal([]string{"1.1.0.2:4501"}))
		})

		It("should not update the generation in the status", func() {
			Expect(cluster.Status.Generations).To(Equal(fdbtypes.ClusterGenerationStatus{Reconciled: originalGeneration}))
		})

		Context("with kills disabled", func() {
			BeforeEach(func() {
				var flag = false
				cluster.Spec.AutomationOptions.KillProcesses = &flag
				shouldContinue = false
			})

			It("should give an error", func() {
				Expect(err).NotTo(BeNil())
				Expect(err.Error()).To(Equal("Kills are disabled"))
			})

			It("should not kill the processes", func() {
				Expect(adminClient.KilledAddresses).To(BeNil())
			})

			It("should update the generation in the status", func() {
				Expect(cluster.Status.Generations).To(Equal(fdbtypes.ClusterGenerationStatus{Reconciled: originalGeneration, NeedsBounce: originalGeneration}))
			})
		})

		Context("with an invalid instance ID in the list", func() {
			BeforeEach(func() {
				cluster.Status.IncorrectProcesses = map[string]int64{"2": 0, "storage-3": 0}
				shouldContinue = false
			})

			It("should give an error", func() {
				Expect(err).NotTo(BeNil())
				Expect(err.Error()).To(Equal("Could not find address for instance 2"))
			})

			It("should not kill the processes", func() {
				Expect(adminClient.KilledAddresses).To(BeNil())
			})
		})

		Context("with an old running version", func() {
			BeforeEach(func() {
				cluster.Spec.RunningVersion = Versions.Default.String()
				cluster.Spec.Version = Versions.NextMajorVersion.String()
				shouldContinue = false
			})

			It("should not give an error", func() {
				Expect(err).To(BeNil())
			})

			It("should update the running version", func() {
				Expect(cluster.Spec.RunningVersion).To(Equal(Versions.NextMajorVersion.String()))
			})
		})
	})
})
