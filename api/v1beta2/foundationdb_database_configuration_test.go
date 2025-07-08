/*
 * foundationdb_database_configuration_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package v1beta2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseConfiguration", func() {
	When("a single dc cluster is provided", func() {
		var config *DatabaseConfiguration

		BeforeEach(func() {
			config = &DatabaseConfiguration{
				StorageEngine:  StorageEngineSSD,
				RedundancyMode: RedundancyModeTriple,
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:      3,
					Storage:   3,
					Proxies:   3,
					Resolvers: 1,
				},
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:        "primary",
								Priority:  1,
								Satellite: 0,
							},
						},
						SatelliteLogs:           0,
						SatelliteRedundancyMode: "",
					},
				},
			}
		})

		When("the configuration string is calculated", func() {
			It("should print the correct configuration string", func() {
				Expect(
					config.GetConfigurationString(),
				).To(Equal("triple ssd usable_regions=1 logs=3 resolvers=1 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\",\\\"priority\\\":1}]}]"))
			})
		})

		When("the configuration string is calculated with fail over", func() {
			It("should not change the config and print the correct configuration string", func() {
				newConfig := config.FailOver()
				Expect(newConfig).To(Equal(*config))
				Expect(
					newConfig.GetConfigurationString(),
				).To(Equal("triple ssd usable_regions=1 logs=3 resolvers=1 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\",\\\"priority\\\":1}]}]"))
			})
		})
	})

	When("a multi dc cluster is provided", func() {
		var config *DatabaseConfiguration

		BeforeEach(func() {
			config = &DatabaseConfiguration{
				StorageEngine:  StorageEngineSSD,
				RedundancyMode: RedundancyModeTriple,
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:      3,
					Storage:   3,
					Proxies:   3,
					Resolvers: 1,
				},
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:        "primary",
								Priority:  1,
								Satellite: 0,
							},
							{
								ID:        "primary-sat",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: RedundancyModeOneSatelliteSingle,
					},
					{
						DataCenters: []DataCenter{
							{
								ID:        "remote",
								Priority:  0,
								Satellite: 0,
							},
							{
								ID:        "remote-sat",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: RedundancyModeOneSatelliteDouble,
					},
				},
			}
		})

		When("the configuration string is calculated", func() {
			It("should print the correct configuration string", func() {
				Expect(
					config.GetConfigurationString(),
				).To(Equal("triple ssd usable_regions=1 logs=3 resolvers=1 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_single\\\"},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\"},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_double\\\"}]"))
			})
		})

		When("the configuration string is calculated with fail over", func() {
			It(
				"should change the priority of the primary and the remote DC and print the new configuration string",
				func() {
					newConfig := config.FailOver()
					Expect(newConfig).NotTo(Equal(*config))

					Expect(len(newConfig.Regions)).To(BeNumerically("==", 2))
					Expect(newConfig.Regions[0].SatelliteLogs).To(Equal(3))
					Expect(
						newConfig.Regions[0].SatelliteRedundancyMode,
					).To(Equal(RedundancyModeOneSatelliteSingle))
					Expect(len(newConfig.Regions[0].DataCenters)).To(BeNumerically("==", 2))
					Expect(newConfig.Regions[0].DataCenters[0].Priority).To(Equal(0))
					Expect(newConfig.Regions[0].DataCenters[0].ID).To(Equal("primary"))
					Expect(newConfig.Regions[0].DataCenters[0].Satellite).To(Equal(0))
					Expect(newConfig.Regions[0].DataCenters[1].Priority).To(Equal(1))
					Expect(newConfig.Regions[0].DataCenters[1].ID).To(Equal("primary-sat"))
					Expect(newConfig.Regions[0].DataCenters[1].Satellite).To(Equal(1))

					Expect(len(newConfig.Regions[1].DataCenters)).To(BeNumerically("==", 2))
					Expect(newConfig.Regions[1].SatelliteLogs).To(Equal(3))
					Expect(
						newConfig.Regions[1].SatelliteRedundancyMode,
					).To(Equal(RedundancyModeOneSatelliteDouble))
					Expect(newConfig.Regions[1].DataCenters[0].Priority).To(Equal(1))
					Expect(newConfig.Regions[1].DataCenters[0].ID).To(Equal("remote"))
					Expect(newConfig.Regions[1].DataCenters[1].Priority).To(Equal(1))
					Expect(newConfig.Regions[1].DataCenters[1].ID).To(Equal("remote-sat"))
					Expect(newConfig.Regions[1].DataCenters[1].Satellite).To(Equal(1))

					Expect(
						newConfig.GetConfigurationString(),
					).To(Equal("triple ssd usable_regions=1 logs=3 resolvers=1 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\"},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_single\\\"},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_double\\\"}]"))
				},
			)
		})
	})

	When("a three_data_hall cluster with the default values is provided", func() {
		var cluster *FoundationDBCluster

		BeforeEach(func() {
			cluster = &FoundationDBCluster{
				Spec: FoundationDBClusterSpec{
					Version:  "7.1.33",
					DataHall: "az1",
					ProcessCounts: ProcessCounts{
						Stateless: -1,
					},
					DatabaseConfiguration: DatabaseConfiguration{
						StorageEngine:  StorageEngineSSD,
						RedundancyMode: RedundancyModeThreeDataHall,
						UsableRegions:  1,
					},
				},
			}
		})

		When("getting the default process counts", func() {
			var err error
			var counts ProcessCounts

			BeforeEach(func() {
				counts, err = cluster.GetProcessCountsWithDefaults()
			})

			It("It should calculate the default process counts", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(counts.Log).To(BeNumerically("==", 6)) // 4 required + 2 additional
				Expect(counts.Storage).To(BeNumerically("==", 5))
			})
		})
	})

	When("using ProcessCounts", func() {
		When("calculating the total number of processes", func() {
			var counts ProcessCounts

			When("no negative process counts are specified", func() {
				BeforeEach(func() {
					counts = ProcessCounts{
						Storage: 5,
						Log:     5,
					}
				})

				It("should print the correct number of total processes", func() {
					Expect(counts.Total()).To(BeNumerically("==", 10))
				})
			})

			When("negative process counts are specified", func() {
				BeforeEach(func() {
					counts = ProcessCounts{
						Storage:   5,
						Log:       5,
						Stateless: -1,
					}
				})

				It("should print the correct number of total processes", func() {
					Expect(counts.Total()).To(BeNumerically("==", 10))
				})
			})
		})
	})
})
