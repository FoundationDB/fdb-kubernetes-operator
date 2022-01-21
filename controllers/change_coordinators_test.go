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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Change coordinators", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var adminClient *mockAdminClient

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{
			{
				ProcessClass: fdbtypes.ProcessClassStorage,
				Priority:     math.MaxInt32,
			},
			{
				ProcessClass: fdbtypes.ProcessClassLog,
				Priority:     0,
			},
		}
		err := setupClusterForTest(cluster)
		Expect(err).NotTo(HaveOccurred())

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("selectCoordinators", func() {
		Context("with a single FDB cluster", func() {
			var status *fdbtypes.FoundationDBStatus
			var candidates []localityInfo

			JustBeforeEach(func() {
				var err error
				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				candidates, err = selectCoordinators(cluster, status)
				Expect(err).NotTo(HaveOccurred())
			})

			When("all processes are healthy", func() {
				It("should only select storage processes", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(strings.HasPrefix(candidate.ID, "storage")).To(BeTrue())
					}
				})
			})

			When("when one storage process is marked for removal", func() {
				removedProcess := "storage-2"

				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []string{
						removedProcess,
					}
					Expect(cluster.ProcessGroupIsBeingRemoved(removedProcess)).To(BeTrue())
				})

				It("should only select storage processes and exclude the removed process", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(candidate.ID).NotTo(Equal(removedProcess))
						Expect(strings.HasPrefix(candidate.ID, "storage")).To(BeTrue())
					}
				})
			})

			When("when one storage process is excluded", func() {
				BeforeEach(func() {
					address := cluster.Status.ProcessGroups[firstStorageIndex+1].Addresses[0]
					adminClient.ExcludedAddresses = append(adminClient.ExcludedAddresses, address)
				})

				It("should only select storage processes and exclude the excluded process", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(candidate.ID).NotTo(Equal("storage-2"))
						Expect(strings.HasPrefix(candidate.ID, "storage")).To(BeTrue())
					}
				})
			})

			When("when multiple storage process are marked for removal", func() {
				removals := []string{
					"storage-2",
					"storage-3",
				}

				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = removals
					for _, removal := range removals {
						Expect(cluster.ProcessGroupIsBeingRemoved(removal)).To(BeTrue())
					}
				})

				It("should select 2 storage processes and 1 TLog", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					storageCnt := 0
					logCnt := 0
					for _, candidate := range candidates {
						for _, removal := range removals {
							Expect(candidate.ID).NotTo(Equal(removal))
						}

						if strings.HasPrefix(candidate.ID, "storage") {
							storageCnt++
						}

						if strings.HasPrefix(candidate.ID, "log") {
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
						newCandidates, err := selectCoordinators(cluster, status)
						Expect(err).NotTo(HaveOccurred())
						Expect(newCandidates).To(Equal(initialCandidates))
					}
				})
			})

			When("the coordinator selection setting is changed", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbtypes.ProcessClassLog,
							Priority:     0,
						},
					}
				})

				It("should only select log processes", func() {
					Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 3))
					Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

					// Only select Storage processes since we select 3 processes and we have 4 storage processes
					for _, candidate := range candidates {
						Expect(strings.HasPrefix(candidate.ID, string(fdbtypes.ProcessClassLog))).To(BeTrue())
					}
				})
			})
		})

		When("Using a HA clusters", func() {
			var status *fdbtypes.FoundationDBStatus
			var candidates []localityInfo
			var excludes []string
			var removals []string
			var dcCnt int
			var satCnt int
			var shouldFail bool

			BeforeEach(func() {
				// ensure a clean state
				candidates = []localityInfo{}
				excludes = []string{}
				removals = []string{}
				shouldFail = false
			})

			JustBeforeEach(func() {
				cluster.Spec.DatabaseConfiguration.UsableRegions = 2
				cluster.Spec.DataCenter = "primary"
				cluster.Spec.ProcessGroupsToRemove = removals

				var err error
				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				// generate status for 2 dcs and 1 sate
				status.Cluster.Processes = generateProcessInfo(dcCnt, satCnt, excludes)

				candidates, err = selectCoordinators(cluster, status)
				if shouldFail {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			})

			When("using 2 dcs with 1 satellite", func() {
				BeforeEach(func() {
					dcCnt = 2
					satCnt = 1
				})

				When("all processes are healthy", func() {
					It("should only select storage processes in primary and remote and Tlog in satellite", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 3 SS in dc1 and 3 Tlogs in sat0
						Expect(storageCnt).To(BeNumerically("==", 6))
						Expect(logCnt).To(BeNumerically("==", 3))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 3))

						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically("==", 3))
						}
					})
				})

				When("some processes are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							"dc0-storage-1",
							"dc0-storage-2",
							"dc0-storage-3",
							"dc0-storage-4",
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not excluded", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							for _, excluded := range excludes {
								Expect(candidate.ID).NotTo(Equal(excluded))
							}

							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 3 SS in dc1 and 3 Tlogs in sat0
						Expect(storageCnt).To(BeNumerically("==", 6))
						Expect(logCnt).To(BeNumerically("==", 3))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 3))

						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically("==", 3))
						}
					})
				})

				When("some processes are removed", func() {
					BeforeEach(func() {
						removals = []string{
							"dc0-storage-1",
							"dc0-storage-2",
							"dc0-storage-3",
							"dc0-storage-4",
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not removed", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							for _, removed := range removals {
								Expect(candidate.ID).NotTo(Equal(removed))
							}

							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 3 SS in dc1 and 3 Tlogs in sat0
						Expect(storageCnt).To(BeNumerically("==", 6))
						Expect(logCnt).To(BeNumerically("==", 3))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 3))

						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically("==", 3))
						}
					})
				})

				When("all processes in a dc are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							"dc0-storage-0",
							"dc0-storage-1",
							"dc0-storage-2",
							"dc0-storage-3",
							"dc0-storage-4",
							"dc0-storage-5",
							"dc0-storage-6",
							"dc0-storage-7",
							"dc0-log-0",
							"dc0-log-1",
							"dc0-log-2",
							"dc0-log-3",
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
							newCandidates, err := selectCoordinators(cluster, status)
							Expect(err).NotTo(HaveOccurred())
							Expect(newCandidates).To(Equal(initialCandidates))
						}
					})
				})
			})

			When("using 2 dcs and 2 satellites", func() {
				BeforeEach(func() {
					dcCnt = 2
					satCnt = 2
				})

				When("all processes are healthy", func() {
					It("should only select storage processes in primary and remote and Tlog in satellites", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 2 SS in dc1 and 2 Tlogs in sat0 and 2 Tlogs in sat1
						Expect(storageCnt).To(BeNumerically("==", 5))
						Expect(logCnt).To(BeNumerically("==", 4))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 4))
						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically(">=", 2))
						}
					})
				})

				When("some processes are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							"dc0-storage-1",
							"dc0-storage-2",
							"dc0-storage-3",
							"dc0-storage-4",
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not excluded", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							for _, excluded := range excludes {
								Expect(candidate.ID).NotTo(Equal(excluded))
							}

							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 2 SS in dc1 and 2 Tlogs in sat0 and 2 Tlogs in sat1
						Expect(storageCnt).To(BeNumerically("==", 5))
						Expect(logCnt).To(BeNumerically("==", 4))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 4))
						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically(">=", 2))
						}
					})
				})

				When("some processes are removed", func() {
					BeforeEach(func() {
						removals = []string{
							"dc0-storage-1",
							"dc0-storage-2",
							"dc0-storage-3",
							"dc0-storage-4",
						}
					})

					It("should only select storage processes in primary and remote and Tlog in satellites and processes that are not removed", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							for _, removed := range removals {
								Expect(candidate.ID).NotTo(Equal(removed))
							}

							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc0 2 SS in dc1 and 2 Tlogs each in sat0 and in sat1
						Expect(storageCnt).To(BeNumerically("==", 5))
						Expect(logCnt).To(BeNumerically("==", 4))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 4))
						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically(">=", 2))
						}
					})
				})

				When("all processes in a dc are excluded", func() {
					BeforeEach(func() {
						excludes = []string{
							"dc0-storage-0",
							"dc0-storage-1",
							"dc0-storage-2",
							"dc0-storage-3",
							"dc0-storage-4",
							"dc0-storage-5",
							"dc0-storage-6",
							"dc0-storage-7",
							"dc0-log-0",
							"dc0-log-1",
							"dc0-log-2",
							"dc0-log-3",
						}
					})

					It("should select 3 processes in each remaining dc", func() {
						Expect(cluster.DesiredCoordinatorCount()).To(BeNumerically("==", 9))
						Expect(len(candidates)).To(BeNumerically("==", cluster.DesiredCoordinatorCount()))

						// Only select Storage processes since we select 3 processes and we have 4 storage processes
						storageCnt := 0
						logCnt := 0
						zoneCnt := map[string]int{}
						for _, candidate := range candidates {
							for _, excluded := range excludes {
								Expect(candidate.ID).NotTo(Equal(excluded))
							}

							zone := strings.Split(candidate.ID, "-")[0]
							zoneCnt[zone]++

							if strings.Contains(candidate.ID, "storage") {
								storageCnt++
							}

							if strings.Contains(candidate.ID, "log") {
								logCnt++
							}
						}

						// We should have 3 SS in dc1 and 3 Tlogs each in sat0 and sat1
						Expect(storageCnt).To(BeNumerically("==", 3))
						Expect(logCnt).To(BeNumerically("==", 6))
						// We should have 3 different zones
						Expect(len(zoneCnt)).To(BeNumerically("==", 3))
						for _, zoneVal := range zoneCnt {
							Expect(zoneVal).To(BeNumerically("==", 3))
						}
					})
				})

				When("recruiting multiple times", func() {
					It("should return always the same processes", func() {
						initialCandidates := candidates

						for i := 0; i < 100; i++ {
							newCandidates, err := selectCoordinators(cluster, status)
							Expect(err).NotTo(HaveOccurred())
							Expect(newCandidates).To(Equal(initialCandidates))
						}
					})
				})
			})
		})

		// TODO add test case for multi KC

		When("Sorting the localities", func() {
			var localities []localityInfo

			BeforeEach(func() {
				localities = []localityInfo{
					{
						ID:    "storage-1",
						Class: fdbtypes.ProcessClassStorage,
					},
					{
						ID:    "tlog-1",
						Class: fdbtypes.ProcessClassTransaction,
					},
					{
						ID:    "log-1",
						Class: fdbtypes.ProcessClassLog,
					},
					{
						ID:    "storage-51",
						Class: fdbtypes.ProcessClassStorage,
					},
				}
			})

			When("no other preferences are defined", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{}
				})

				It("should sort the localities based on the IDs", func() {
					sortLocalities(cluster, localities)

					Expect(localities[0].Class).To(Equal(fdbtypes.ProcessClassLog))
					Expect(localities[0].ID).To(Equal("log-1"))
					Expect(localities[1].Class).To(Equal(fdbtypes.ProcessClassStorage))
					Expect(localities[1].ID).To(Equal("storage-1"))
					Expect(localities[2].Class).To(Equal(fdbtypes.ProcessClassStorage))
					Expect(localities[2].ID).To(Equal("storage-51"))
					Expect(localities[3].Class).To(Equal(fdbtypes.ProcessClassTransaction))
					Expect(localities[3].ID).To(Equal("tlog-1"))
				})
			})

			When("when the storage class is preferred", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbtypes.ProcessClassStorage,
							Priority:     0,
						},
					}
				})

				It("should sort the localities based on the provided config", func() {
					sortLocalities(cluster, localities)

					Expect(localities[0].Class).To(Equal(fdbtypes.ProcessClassStorage))
					Expect(localities[0].ID).To(Equal("storage-1"))
					Expect(localities[1].Class).To(Equal(fdbtypes.ProcessClassStorage))
					Expect(localities[1].ID).To(Equal("storage-51"))
					Expect(localities[2].Class).To(Equal(fdbtypes.ProcessClassLog))
					Expect(localities[2].ID).To(Equal("log-1"))
					Expect(localities[3].Class).To(Equal(fdbtypes.ProcessClassTransaction))
					Expect(localities[3].ID).To(Equal("tlog-1"))
				})
			})

			When("when the storage class is preferred over transaction class", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbtypes.ProcessClassStorage,
							Priority:     1,
						},
						{
							ProcessClass: fdbtypes.ProcessClassTransaction,
							Priority:     0,
						},
					}
				})

				It("should sort the localities based on the provided config", func() {
					sortLocalities(cluster, localities)

					Expect(localities[0].Class).To(Equal(fdbtypes.ProcessClassStorage))
					Expect(localities[0].ID).To(Equal("storage-1"))
					Expect(localities[1].Class).To(Equal(fdbtypes.ProcessClassStorage))
					Expect(localities[1].ID).To(Equal("storage-51"))
					Expect(localities[2].Class).To(Equal(fdbtypes.ProcessClassTransaction))
					Expect(localities[2].ID).To(Equal("tlog-1"))
					Expect(localities[3].Class).To(Equal(fdbtypes.ProcessClassLog))
					Expect(localities[3].ID).To(Equal("log-1"))
				})
			})
		})
	})

	Describe("reconcile", func() {
		var requeue *requeue
		var err error
		var originalConnectionString string

		BeforeEach(func() {
			originalConnectionString = cluster.Status.ConnectionString
		})

		JustBeforeEach(func() {
			requeue = changeCoordinators{}.reconcile(context.TODO(), clusterReconciler, cluster)
			Expect(err).NotTo(HaveOccurred())
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
			})

			Context("when the pods do not have DNS names", func() {

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should not change the cluster file", func() {
					Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
				})
			})

			Context("when the pods have DNS names", func() {
				BeforeEach(func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						container := pod.Spec.Containers[1]
						container.Env = append(container.Env, corev1.EnvVar{Name: "FDB_DNS_NAME", Value: internal.GetPodDNSName(cluster, pod.Name)})
						pod.Spec.Containers[1] = container
						err = k8sClient.Update(context.TODO(), &pod)
						Expect(err).NotTo(HaveOccurred())
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
	})
})

func generateProcessInfo(dcCount int, satCount int, excludes []string) map[string]fdbtypes.FoundationDBStatusProcessInfo {
	res := map[string]fdbtypes.FoundationDBStatusProcessInfo{}
	logCnt := 4

	for i := 0; i < dcCount; i++ {
		dcid := fmt.Sprintf("dc%d", i)

		generateProcessInfoDetails(res, dcid, 8, excludes, fdbtypes.ProcessClassStorage)
		generateProcessInfoDetails(res, dcid, logCnt, excludes, fdbtypes.ProcessClassLog)
	}

	for i := 0; i < satCount; i++ {
		dcid := fmt.Sprintf("sat%d", i)

		generateProcessInfoDetails(res, dcid, logCnt, excludes, fdbtypes.ProcessClassLog)
	}

	return res
}

func generateProcessInfoDetails(res map[string]fdbtypes.FoundationDBStatusProcessInfo, dcID string, cnt int, excludes []string, pClass fdbtypes.ProcessClass) {
	for idx := 0; idx < cnt; idx++ {
		excluded := false
		zoneID := fmt.Sprintf("%s-%s-%d", dcID, pClass, idx)

		for _, exclude := range excludes {
			if exclude != zoneID {
				continue
			}

			excluded = true
			break
		}

		addr := fmt.Sprintf("1.1.1.%d:4501", len(res))
		res[zoneID] = fdbtypes.FoundationDBStatusProcessInfo{
			ProcessClass: pClass,
			Locality: map[string]string{
				fdbtypes.FDBLocalityInstanceIDKey: zoneID,
				fdbtypes.FDBLocalityZoneIDKey:     zoneID,
				fdbtypes.FDBLocalityDCIDKey:       dcID,
			},
			Excluded: excluded,
			Address: fdbtypes.ProcessAddress{
				IPAddress: net.ParseIP(fmt.Sprintf("1.1.1.%d", len(res))),
				Port:      4501,
			},
			CommandLine: fmt.Sprintf("/fdbserver --public_address=%s", addr),
		}
	}
}
