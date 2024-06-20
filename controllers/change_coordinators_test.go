/*
 * change_coordinators_test.go
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
	"fmt"
	"math"
	"net"
	"strings"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/locality"
	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Change coordinators", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var adminClient *mock.AdminClient

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
			{
				ProcessClass: fdbv1beta2.ProcessClassStorage,
				Priority:     math.MaxInt32,
			},
			{
				ProcessClass: fdbv1beta2.ProcessClassLog,
				Priority:     0,
			},
		}
		Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())

		var err error
		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("selectCoordinators", func() {
		Context("with a single FDB cluster", func() {
			var status *fdbv1beta2.FoundationDBStatus
			var candidates []locality.Info

			JustBeforeEach(func() {
				var err error
				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				candidates, err = selectCoordinators(logr.Discard(), cluster, status)
				Expect(err).NotTo(HaveOccurred())
			})

			When("all processes are healthy", func() {
				It("should only select storage processes", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(candidate.Class).To(Equal(fdbv1beta2.ProcessClassStorage))
					}
				})
			})

			When("when one storage process is marked for removal", func() {
				removedProcess := fdbv1beta2.ProcessGroupID("storage-2")

				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
						removedProcess,
					}
					Expect(cluster.ProcessGroupIsBeingRemoved(removedProcess)).To(BeTrue())
				})

				It("should only select storage processes and exclude the removed process", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(candidate.ID).NotTo(Equal(removedProcess))
						Expect(candidate.Class).To(Equal(fdbv1beta2.ProcessClassStorage))
					}
				})
			})

			When("when one storage process is excluded", func() {
				BeforeEach(func() {
					address := cluster.Status.ProcessGroups[firstStorageIndex+1].Addresses[0]
					adminClient.ExcludedAddresses[address] = fdbv1beta2.None{}
				})

				It("should only select storage processes and exclude the excluded process", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(candidate.ID).NotTo(Equal("storage-2"))
						Expect(candidate.Class).To(Equal(fdbv1beta2.ProcessClassStorage))
					}
				})
			})

			When("when multiple storage process are marked for removal", func() {
				var removals []fdbv1beta2.ProcessGroupID

				BeforeEach(func() {
					removals = make([]fdbv1beta2.ProcessGroupID, 0, 2)

					for _, processGroup := range internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2) {
						removals = append(removals, processGroup.ProcessGroupID)
						processGroup.MarkForRemoval()
					}

					cluster.Spec.ProcessGroupsToRemove = removals
					for _, removal := range removals {
						Expect(cluster.ProcessGroupIsBeingRemoved(removal)).To(BeTrue())
					}
				})

				It("should select 2 storage processes and 1 TLog", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					storageCnt := 0
					logCnt := 0
					for _, candidate := range candidates {
						for _, removal := range removals {
							Expect(candidate.ID).NotTo(Equal(removal))
						}

						if candidate.Class == fdbv1beta2.ProcessClassStorage {
							storageCnt++
						}

						if candidate.Class == fdbv1beta2.ProcessClassLog {
							logCnt++
						}
					}

					Expect(storageCnt).To(BeNumerically("==", 2))
					Expect(logCnt).To(BeNumerically("==", 1))
				})
			})

			When("recruiting multiple times", func() {
				It("should return always the same processes", func() {
					initialCandidates := candidates

					for i := 0; i < 100; i++ {
						newCandidates, err := selectCoordinators(logr.Discard(), cluster, status)
						Expect(err).NotTo(HaveOccurred())
						Expect(newCandidates).To(Equal(initialCandidates))
					}
				})
			})

			When("the coordinator selection setting is changed", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbv1beta2.ProcessClassLog,
							Priority:     0,
						},
					}
				})

				It("should only select log processes", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(strings.HasPrefix(candidate.ID, string(fdbv1beta2.ProcessClassLog))).To(BeTrue())
					}
				})
			})

			When("maintenance mode is on", func() {
				BeforeEach(func() {
					Expect(adminClient.SetMaintenanceZone("operator-test-1-storage-1", 0)).NotTo(HaveOccurred())
				})

				It("should not select processes from the maintenance zone", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Should select storage processes only.
					for _, candidate := range candidates {
						Expect(candidate.ID).NotTo(Equal("storage-1"))
						Expect(candidate.Class).To(Equal(fdbv1beta2.ProcessClassStorage))
					}
				})
			})
		})

		When("Using a HA clusters", func() {
			var status *fdbv1beta2.FoundationDBStatus
			var candidates []locality.Info
			var excludes []string
			var removals []fdbv1beta2.ProcessGroupID
			var shouldFail bool
			var primaryID string

			BeforeEach(func() {
				// ensure a clean state
				candidates = []locality.Info{}
				excludes = []string{}
				removals = []fdbv1beta2.ProcessGroupID{}
				shouldFail = false
			})

			JustBeforeEach(func() {
				cluster.Spec.DatabaseConfiguration.UsableRegions = 2
				cluster.Spec.ProcessGroupsToRemove = removals
				var err error
				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				status.Cluster.Processes = generateProcessInfoForMultiRegion(cluster.Spec.DatabaseConfiguration, excludes, cluster.GetRunningVersion())

				candidates, err = selectCoordinators(testLogger, cluster, status)
				if shouldFail {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			})

			When("using 2 dcs with 1 satellite", func() {
				BeforeEach(func() {
					primaryID = internal.GenerateRandomString(10)
					remoteID := internal.GenerateRandomString(10)
					satelliteID := internal.GenerateRandomString(10)
					cluster.Spec.DataCenter = primaryID
					cluster.Spec.DatabaseConfiguration.Regions = []fdbv1beta2.Region{
						{
							DataCenters: []fdbv1beta2.DataCenter{
								{
									ID:       primaryID,
									Priority: 1,
								},
								{
									ID:        satelliteID,
									Satellite: 1,
									Priority:  1,
								},
								{
									ID:        remoteID,
									Satellite: 1,
								},
							},
						},
						{
							DataCenters: []fdbv1beta2.DataCenter{
								{
									ID: remoteID,
								},
								{
									ID:        satelliteID,
									Satellite: 1,
									Priority:  1,
								},
								{
									ID:        primaryID,
									Satellite: 1,
								},
							},
						},
					}
				})

				When("all processes are healthy", func() {
					It("should only select storage processes in primary and remote and Tlog in satellite", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
							if candidate.Class == fdbv1beta2.ProcessClassStorage {
								storageCnt++
							}

							if candidate.Class == fdbv1beta2.ProcessClassLog {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 3 SS in dc1 and 3 Tlogs in sat0
						Expect(storageCnt).To(BeNumerically("==", 6))
						Expect(logCnt).To(BeNumerically("==", 3))
						// We should have 3 different zones
						Expect(len(dcDistribution)).To(BeNumerically("==", 3))
						for _, dcCount := range dcDistribution {
							Expect(dcCount).To(BeNumerically("==", 3))
						}
					})
				})

				When("some processes are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							primaryID + "-storage-1",
							primaryID + "-storage-2",
							primaryID + "-storage-3",
							primaryID + "-storage-4",
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not excluded", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							for _, excluded := range excludes {
								Expect(candidate.ID).NotTo(Equal(excluded))
							}

							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
							if candidate.Class == fdbv1beta2.ProcessClassStorage {
								storageCnt++
							}

							if candidate.Class == fdbv1beta2.ProcessClassLog {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 3 SS in dc1 and 3 Tlogs in sat0
						Expect(storageCnt).To(BeNumerically("==", 6))
						Expect(logCnt).To(BeNumerically("==", 3))
						// We should have 3 different zones
						Expect(len(dcDistribution)).To(BeNumerically("==", 3))
						for _, dcCount := range dcDistribution {
							Expect(dcCount).To(BeNumerically("==", 3))
						}
					})
				})

				When("some processes are removed", func() {
					BeforeEach(func() {
						removals = []fdbv1beta2.ProcessGroupID{
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-1"),
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-2"),
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-3"),
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-4"),
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not removed", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							for _, removed := range removals {
								Expect(candidate.ID).NotTo(Equal(removed))
							}

							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
							if candidate.Class == fdbv1beta2.ProcessClassStorage {
								storageCnt++
							}

							if candidate.Class == fdbv1beta2.ProcessClassLog {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 3 SS in dc1 and 3 Tlogs in sat0
						Expect(storageCnt).To(BeNumerically("==", 6))
						Expect(logCnt).To(BeNumerically("==", 3))
						// We should have 3 different zones
						Expect(len(dcDistribution)).To(BeNumerically("==", 3))

						for _, dcCount := range dcDistribution {
							Expect(dcCount).To(BeNumerically("==", 3))
						}
					})
				})

				When("all processes in a dc are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							primaryID + "-storage-0",
							primaryID + "-storage-1",
							primaryID + "-storage-2",
							primaryID + "-storage-3",
							primaryID + "-storage-4",
							primaryID + "-storage-5",
							primaryID + "-storage-6",
							primaryID + "-storage-7",
							primaryID + "-log-0",
							primaryID + "-log-1",
							primaryID + "-log-2",
							primaryID + "-log-3",
						}

						shouldFail = true
					})

					It("should fail to select coordinators since we can only select 4 processes per dc", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
					})
				})

				When("recruiting multiple times", func() {
					It("should return always the same processes", func() {
						initialCandidates := candidates

						for i := 0; i < 100; i++ {
							newCandidates, err := selectCoordinators(logr.Discard(), cluster, status)
							Expect(err).NotTo(HaveOccurred())
							Expect(newCandidates).To(Equal(initialCandidates))
						}
					})
				})
			})

			When("using 2 dcs and 2 satellites", func() {
				BeforeEach(func() {
					primaryID = internal.GenerateRandomString(10)
					remoteID := internal.GenerateRandomString(10)
					primarySatelliteID := internal.GenerateRandomString(10)
					remoteSatelliteID := internal.GenerateRandomString(10)

					cluster.Spec.DataCenter = primaryID
					cluster.Spec.DatabaseConfiguration.Regions = []fdbv1beta2.Region{
						{
							DataCenters: []fdbv1beta2.DataCenter{
								{
									ID:       primaryID,
									Priority: 1,
								},
								{
									ID:        primarySatelliteID,
									Satellite: 1,
									Priority:  1,
								},
								{
									ID:        remoteSatelliteID,
									Satellite: 1,
								},
							},
						},
						{
							DataCenters: []fdbv1beta2.DataCenter{
								{
									ID: remoteID,
								},
								{
									ID:        remoteSatelliteID,
									Satellite: 1,
									Priority:  1,
								},
								{
									ID:        primarySatelliteID,
									Satellite: 1,
								},
							},
						},
					}
				})

				When("all processes are healthy", func() {
					It("should only select storage processes in primary and remote and Tlog in satellites", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
							if candidate.Class == fdbv1beta2.ProcessClassStorage {
								storageCnt++
							}

							if candidate.Class == fdbv1beta2.ProcessClassLog {
								logCnt++
							}
						}

						// We should have 3 SS in the primary dc 2 SS in the remote dc and 2 Tlogs in each satellite.
						Expect(storageCnt).To(BeNumerically("==", 5))
						Expect(logCnt).To(BeNumerically("==", 4))
						Expect(primaryID).To(Equal(cluster.DesiredDatabaseConfiguration().GetPrimaryDCID()))
						// We should have 4 different dcs,
						Expect(dcDistribution).To(HaveLen(4))
						for dcID, dcCount := range dcDistribution {
							if dcID == primaryID {
								Expect(dcCount).To(BeNumerically("==", 3))
								continue
							}

							Expect(dcCount).To(BeNumerically("==", 2))
						}
					})
				})

				When("some processes are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							primaryID + "-storage-1",
							primaryID + "-storage-2",
							primaryID + "-storage-3",
							primaryID + "-storage-4",
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not excluded", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							for _, excluded := range excludes {
								Expect(candidate.ID).NotTo(Equal(excluded))
							}
							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
							if candidate.Class == fdbv1beta2.ProcessClassStorage {
								storageCnt++
							}

							if candidate.Class == fdbv1beta2.ProcessClassLog {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 2 SS in dc1 and 2 Tlogs in sat0 and 2 Tlogs in sat1
						Expect(storageCnt).To(BeNumerically("==", 5))
						Expect(logCnt).To(BeNumerically("==", 4))
						// We should have 4 different dcs,
						Expect(dcDistribution).To(HaveLen(4))
						for dcID, dcCount := range dcDistribution {
							if dcID == primaryID {
								Expect(dcCount).To(BeNumerically("==", 3))
								continue
							}

							Expect(dcCount).To(BeNumerically("==", 2))
						}
					})
				})

				When("some processes are removed", func() {
					BeforeEach(func() {
						removals = []fdbv1beta2.ProcessGroupID{
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-1"),
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-2"),
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-3"),
							fdbv1beta2.ProcessGroupID(primaryID + "-storage-4"),
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not removed", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).To(HaveLen(cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							for _, removed := range removals {
								Expect(candidate.ID).NotTo(Equal(removed))
							}

							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
							if candidate.Class == fdbv1beta2.ProcessClassStorage {
								storageCnt++
							}

							if candidate.Class == fdbv1beta2.ProcessClassLog {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 2 SS in dc1 and 2 Tlogs each in sat0 and in sat1
						Expect(storageCnt).To(BeNumerically("==", 5))
						Expect(logCnt).To(BeNumerically("==", 4))
						// We should have 4 different dcs.
						Expect(dcDistribution).To(HaveLen(4))
						for dcID, dcCount := range dcDistribution {
							if dcID == primaryID {
								Expect(dcCount).To(BeNumerically("==", 3))
								continue
							}

							Expect(dcCount).To(BeNumerically("==", 2))
						}
					})
				})

				When("all processes in a dc are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							primaryID + "-storage-0",
							primaryID + "-storage-1",
							primaryID + "-storage-2",
							primaryID + "-storage-3",
							primaryID + "-storage-4",
							primaryID + "-storage-5",
							primaryID + "-storage-6",
							primaryID + "-storage-7",
							primaryID + "-log-0",
							primaryID + "-log-1",
							primaryID + "-log-2",
							primaryID + "-log-3",
						}
					})

					It("should select 3 processes in each remaining dc", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(candidates).NotTo(BeEmpty())

						dcDistribution := map[string]int{}
						for _, candidate := range candidates {
							dcDistribution[candidate.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]]++
						}

						// We should have 3 different dcs as the primary has no valid processes.
						Expect(dcDistribution).To(HaveLen(3))
						for dcID, dcCount := range dcDistribution {
							Expect(dcID).NotTo(Equal(primaryID))
							Expect(dcCount).To(BeNumerically("==", 3))
						}
					})
				})

				When("recruiting multiple times", func() {
					It("should return always the same processes", func() {
						initialCandidates := candidates

						for i := 0; i < 100; i++ {
							newCandidates, err := selectCoordinators(logr.Discard(), cluster, status)
							Expect(err).NotTo(HaveOccurred())
							Expect(newCandidates).To(Equal(initialCandidates))
						}
					})
				})
			})
		})

		When("using a FDB cluster with three_data_hall", func() {
			var status *fdbv1beta2.FoundationDBStatus
			var candidates []locality.Info

			JustBeforeEach(func() {
				cluster.Spec.DataHall = "az1"
				cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall

				var err error
				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				status.Cluster.Processes = generateProcessInfoForThreeDataHall(3, nil, cluster.GetRunningVersion())

				candidates, err = selectCoordinators(logr.Discard(), cluster, status)
				Expect(err).NotTo(HaveOccurred())
			})

			When("all processes are healthy", func() {
				It("should only select storage processes", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					dataHallCounts := map[string]int{}
					for _, candidate := range candidates {
						Expect(candidate.Class).To(Equal(fdbv1beta2.ProcessClassStorage))
						dataHallCounts[candidate.LocalityData[fdbv1beta2.FDBLocalityDataHallKey]]++
					}

					for _, dataHallCount := range dataHallCounts {
						Expect(dataHallCount).To(BeNumerically("==", 3))
					}
				})
			})

			When("when one storage process is marked for removal", func() {
				removedProcess := fdbv1beta2.ProcessGroupID("storage-2")

				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
						removedProcess,
					}
					Expect(cluster.ProcessGroupIsBeingRemoved(removedProcess)).To(BeTrue())
				})

				It("should only select storage processes and exclude the removed process", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					dataHallCounts := map[string]int{}
					for _, candidate := range candidates {
						Expect(candidate.ID).NotTo(Equal(removedProcess))
						Expect(candidate.Class).To(Equal(fdbv1beta2.ProcessClassStorage))
						dataHallCounts[candidate.LocalityData[fdbv1beta2.FDBLocalityDataHallKey]]++
					}

					for _, dataHallCount := range dataHallCounts {
						Expect(dataHallCount).To(BeNumerically("==", 3))
					}
				})
			})
		})
	})

	Describe("reconcile", func() {
		var requeue *requeue
		var originalConnectionString string

		BeforeEach(func() {
			originalConnectionString = cluster.Status.ConnectionString
		})

		JustBeforeEach(func() {
			requeue = changeCoordinators{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
		})

		When("the cluster is healthy", func() {
			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("leaves the cluster file intact", func() {
				Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
			})
		})

		When("enabling DNS in the cluster file", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				cluster.Spec.Version = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
				cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
			})

			When("the Pods do not have DNS names", func() {
				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should not change the cluster file", func() {
					Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
				})
			})

			When("the Pods have DNS names", func() {
				BeforeEach(func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						container := pod.Spec.Containers[1]
						container.Env = append(container.Env, corev1.EnvVar{Name: "FDB_DNS_NAME", Value: internal.GetPodDNSName(cluster, pod.Name)})
						pod.Spec.Containers[1] = container
						Expect(k8sClient.Update(context.TODO(), &pod)).NotTo(HaveOccurred())
					}
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should change the cluster file", func() {
					Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
					Expect(cluster.Status.ConnectionString).To(ContainSubstring("my-ns.svc.cluster.local"))
				})
			})
		})

		When("one coordinator is missing localities", func() {
			var badCoordinator fdbv1beta2.FoundationDBStatusProcessInfo

			BeforeEach(func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				for _, process := range status.Cluster.Processes {
					for _, role := range process.Roles {
						if role.Role != "coordinator" {
							continue
						}

						badCoordinator = process
					}
				}

				adminClient.MockMissingLocalities(fdbv1beta2.ProcessGroupID(badCoordinator.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]), true)
			})

			It("should change the coordinators to not include the coordinator with the missing localities", func() {
				Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
				Expect(cluster.Status.ConnectionString).NotTo(ContainSubstring(badCoordinator.Address.IPAddress.String()))
			})
		})
	})

	DescribeTable("selecting coordinator candidates", func(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, expected []locality.Info) {
		localities, err := selectCandidates(cluster, status)
		Expect(err).NotTo(HaveOccurred())
		Expect(localities).To(ConsistOf(expected))
	},
		Entry("No priorities are defined and all processes are upgraded",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.57",
				},
			},
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							CommandLine: "--public_address=192.168.0.1:4500",
							Version:     "7.1.57",
						},
					},
				},
			},
			[]locality.Info{
				{
					ID: "1",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "1",
					},
				},
			},
		),
		Entry("No priorities are defined and one processes must be upgraded",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.57",
				},
			},
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							CommandLine: "--public_address=192.168.0.1:4500",
							Version:     "7.1.57",
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							CommandLine: "--public_address=192.168.0.2:4500",
							Version:     "7.1.55",
						},
					},
				},
			},
			[]locality.Info{
				{
					ID: "1",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "1",
					},
				},
				{
					ID: "2",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "2",
					},
					Priority: math.MinInt,
				},
			},
		),
		Entry("Priorities are defined and one processes must be upgraded",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.57",
					CoordinatorSelection: []fdbv1beta2.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Priority:     1000,
						},
					},
				},
			},
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							CommandLine: "--public_address=192.168.0.1:4500",
							Version:     "7.1.57",
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							CommandLine: "--public_address=192.168.0.2:4500",
							Version:     "7.1.55",
						},
					},
				},
			},
			[]locality.Info{
				{
					ID: "1",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "1",
					},
					Priority: 1000,
				},
				{
					ID: "2",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "2",
					},
					Priority: math.MinInt + 1000,
				},
			},
		),
		Entry("No priorities are defined and one processes is using the binary from the shared volume",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.57",
				},
			},
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							CommandLine: "--public_address=192.168.0.1:4500",
							Version:     "7.1.57",
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							CommandLine: "/var/dynamic-conf/... --public_address=192.168.0.2:4500",
							Version:     "7.1.57",
						},
					},
				},
			},
			[]locality.Info{
				{
					ID: "1",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "1",
					},
				},
				{
					ID: "2",
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					Class: fdbv1beta2.ProcessClassStorage,
					LocalityData: map[string]string{
						fdbv1beta2.FDBLocalityInstanceIDKey: "2",
					},
					Priority: math.MinInt,
				},
			},
		),
	)
})

func generateProcessInfoForMultiRegion(config fdbv1beta2.DatabaseConfiguration, excludes []string, version string) map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo {
	res := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{}
	logCnt := 4

	mainDCs, satelliteDCs := config.GetMainDCsAndSatellites()
	for dcID := range mainDCs {
		generateProcessInfoDetails(res, dcID, "", 8, excludes, fdbv1beta2.ProcessClassStorage, version)
		generateProcessInfoDetails(res, dcID, "", logCnt, excludes, fdbv1beta2.ProcessClassLog, version)
	}

	for satelliteDCID := range satelliteDCs {
		generateProcessInfoDetails(res, satelliteDCID, "", logCnt, excludes, fdbv1beta2.ProcessClassLog, version)
	}

	return res
}

func generateProcessInfoForThreeDataHall(dataHallCount int, excludes []string, version string) map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo {
	res := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{}
	logCnt := 4

	for i := 0; i < dataHallCount; i++ {
		dataHallID := internal.GenerateRandomString(10)
		generateProcessInfoDetails(res, "", dataHallID, 8, excludes, fdbv1beta2.ProcessClassStorage, version)
		generateProcessInfoDetails(res, "", dataHallID, logCnt, excludes, fdbv1beta2.ProcessClassLog, version)
	}

	return res
}

func generateProcessInfoDetails(res map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo, dcID string, dataHall string, cnt int, excludes []string, pClass fdbv1beta2.ProcessClass, version string) {
	for idx := 0; idx < cnt; idx++ {
		excluded := false
		var zoneID string

		if dcID != "" {
			zoneID = fmt.Sprintf("%s-%s-%d", dcID, pClass, idx)
		}

		if dataHall != "" {
			zoneID = fmt.Sprintf("%s-%s-%d", dataHall, pClass, idx)
		}

		for _, exclude := range excludes {
			if exclude != zoneID {
				continue
			}

			excluded = true
			break
		}

		addr := fmt.Sprintf("1.1.1.%d:4501", len(res))
		processInfo := fdbv1beta2.FoundationDBStatusProcessInfo{
			ProcessClass: pClass,
			Version:      version,
			Locality: map[string]string{
				fdbv1beta2.FDBLocalityInstanceIDKey: zoneID,
				fdbv1beta2.FDBLocalityZoneIDKey:     zoneID,
			},
			Excluded: excluded,
			Address: fdbv1beta2.ProcessAddress{
				IPAddress: net.ParseIP(fmt.Sprintf("1.1.1.%d", len(res))),
				Port:      4501,
			},
			CommandLine: fmt.Sprintf("/fdbserver --public_address=%s", addr),
		}

		if dcID != "" {
			processInfo.Locality[fdbv1beta2.FDBLocalityDCIDKey] = dcID
		}

		if dataHall != "" {
			processInfo.Locality[fdbv1beta2.FDBLocalityDataHallKey] = dataHall
		}

		res[fdbv1beta2.ProcessGroupID(zoneID)] = processInfo
	}
}
