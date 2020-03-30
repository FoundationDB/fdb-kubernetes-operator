/*
 * update_database_configuration_test.go
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

var _ = Describe("UpdateDatabaseConfiguration", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var originalGeneration int64
	var adminClient *MockAdminClient

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true
		originalGeneration = cluster.ObjectMeta.Generation

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(adminClient).NotTo(BeNil())
	})

	AfterEach(func() {
		if cluster.ObjectMeta.Generation > originalGeneration {
			Eventually(func() (int64, error) { return reloadCluster(k8sClient, cluster) }, 5).Should(BeNumerically(">", originalGeneration))
		}
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(UpdateDatabaseConfiguration{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})
	})

	Context("with a change to the redundancy mode", func() {
		BeforeEach(func() {
			cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"
		})

		It("should change the database configuration", func() {
			Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("triple"))
		})
	})

	Context("with a new database configuration", func() {
		BeforeEach(func() {
			cluster.Spec.Configured = false
			cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"
			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should mark the database as configured", func() {
			Expect(cluster.Spec.Configured).To(BeTrue())
		})

		It("should change the database configuration", func() {
			Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("triple"))
		})
	})

	Context("with a change to the region configuration", func() {
		BeforeEach(func() {
			cluster.Spec.DatabaseConfiguration = fdbtypes.DatabaseConfiguration{
				RedundancyMode: "double",
				UsableRegions:  2,
				Regions: []fdbtypes.Region{
					fdbtypes.Region{
						DataCenters: []fdbtypes.DataCenter{
							fdbtypes.DataCenter{
								ID:       "dc1",
								Priority: 1,
							},
							fdbtypes.DataCenter{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					fdbtypes.Region{
						DataCenters: []fdbtypes.DataCenter{
							fdbtypes.DataCenter{
								ID:       "dc3",
								Priority: 0,
							},
							fdbtypes.DataCenter{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}

			shouldContinue = false
		})

		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})

		It("should make the first change in the database configuration", func() {
			Expect(adminClient.DatabaseConfiguration.UsableRegions).To(Equal(1))
			Expect(adminClient.DatabaseConfiguration.Regions).To(Equal([]fdbtypes.Region{
				{
					DataCenters: []fdbtypes.DataCenter{
						{ID: "dc1", Priority: 1, Satellite: 0},
						{ID: "dc2", Priority: 1, Satellite: 1},
					},
					SatelliteLogs:           3,
					SatelliteRedundancyMode: "one_satellite_double",
				},
				{
					DataCenters: []fdbtypes.DataCenter{
						{ID: "dc3", Priority: -1, Satellite: 0},
						{ID: "dc4", Priority: 1, Satellite: 1},
					},
					SatelliteLogs:           3,
					SatelliteRedundancyMode: "one_satellite_double",
				},
			}))
		})
	})
})
