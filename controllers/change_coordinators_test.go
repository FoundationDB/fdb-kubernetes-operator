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
	"fmt"
	"strings"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Reconcile.SelectCandidates", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var adminClient *MockAdminClient

	BeforeEach(func() {
		cluster = createDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		err := setupClusterForTest(cluster)
		Expect(err).NotTo(HaveOccurred())

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

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
				cluster.Spec.InstancesToRemove = []string{
					removedProcess,
				}
				Expect(cluster.InstanceIsBeingRemoved(removedProcess)).To(BeTrue())
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
				adminClient.ExcludedAddresses = append(adminClient.ExcludedAddresses, "1.1.0.2")
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
				cluster.Spec.InstancesToRemove = removals
				for _, removal := range removals {
					Expect(cluster.InstanceIsBeingRemoved(removal)).To(BeTrue())
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
	})

	When("Using a HA cluster with three DCs and two regions", func() {
		var status *fdbtypes.FoundationDBStatus
		var candidates []localityInfo

		JustBeforeEach(func() {
			cluster.Spec.UsableRegions = 2
			cluster.Spec.DataCenter = "primary"

			var err error
			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())

			// generate status for 2 dcs and 1 sate
			status.Cluster.Processes = generateProcessInfo(2, 1)

			candidates, err = selectCoordinators(cluster, status)
			Expect(err).NotTo(HaveOccurred())
		})

		When("all processes are healthy", func() {
			FIt("should only select storage processes in primary and remote and Tlog in satellite", func() {
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

		// TODO exclude a process
		// TODO mark a process for removal
		// TODO what happens if processes are not available in a DC (e.g. failure)
	})

	When("Using a HA cluster with four DCs and two regions", func() {
		var status *fdbtypes.FoundationDBStatus
		var candidates []localityInfo

		JustBeforeEach(func() {
			cluster.Spec.UsableRegions = 2
			cluster.Spec.DataCenter = "primary"

			var err error
			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())

			// generate status for 2 dcs and 1 sate
			status.Cluster.Processes = generateProcessInfo(2, 2)

			candidates, err = selectCoordinators(cluster, status)
			Expect(err).NotTo(HaveOccurred())
		})

		When("all processes are healthy", func() {
			FIt("should only select storage processes in primary and remote and Tlog in satellites", func() {
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

				for zone, zoneVal := range zoneCnt {
					expectedCnt := 2
					if zone == "dc0" {
						expectedCnt = 3
					}

					Expect(zoneVal).To(BeNumerically("==", expectedCnt))
				}
			})
		})

		// TODO exclude a process
		// TODO mark a process for removal
		// TODO what happens if processes are not available in a DC (e.g. failure)
	})

	// TODO add test case for multi KC
})

func generateProcessInfo(dcCount int, satCount int) map[string]fdbtypes.FoundationDBStatusProcessInfo {
	res := map[string]fdbtypes.FoundationDBStatusProcessInfo{}

	ssCnt := 8
	logCnt := 4

	for i := 0; i < dcCount; i++ {
		dcid := fmt.Sprintf("dc%d", i)

		for idx := 0; idx < ssCnt; idx++ {
			zondeid := fmt.Sprintf("%s-%s-%d", dcid, fdbtypes.ProcessClassStorage, idx)
			res[zondeid] = fdbtypes.FoundationDBStatusProcessInfo{
				ProcessClass: fdbtypes.ProcessClassStorage,
				Locality: map[string]string{
					fdbtypes.FDBLocalityInstanceIDKey: zondeid,
					fdbtypes.FDBLocalityZoneIDKey:     zondeid,
					fdbtypes.FDBLocalityDCIDKey:       dcid,
				},
			}
		}

		for idx := 0; idx < logCnt; idx++ {
			zondeid := fmt.Sprintf("%s-%s-%d", dcid, fdbtypes.ProcessClassLog, idx)
			res[zondeid] = fdbtypes.FoundationDBStatusProcessInfo{
				ProcessClass: fdbtypes.ProcessClassLog,
				Locality: map[string]string{
					fdbtypes.FDBLocalityInstanceIDKey: zondeid,
					fdbtypes.FDBLocalityZoneIDKey:     zondeid,
					fdbtypes.FDBLocalityDCIDKey:       dcid,
				},
			}
		}
	}

	for i := 0; i < satCount; i++ {
		dcid := fmt.Sprintf("sat%d", i)

		for idx := 0; idx < logCnt; idx++ {
			zondeid := fmt.Sprintf("%s-%s-%d", dcid, fdbtypes.ProcessClassLog, idx)
			res[zondeid] = fdbtypes.FoundationDBStatusProcessInfo{
				ProcessClass: fdbtypes.ProcessClassLog,
				Locality: map[string]string{
					fdbtypes.FDBLocalityInstanceIDKey: zondeid,
					fdbtypes.FDBLocalityZoneIDKey:     zondeid,
					fdbtypes.FDBLocalityDCIDKey:       dcid,
				},
			}
		}
	}

	return res
}
