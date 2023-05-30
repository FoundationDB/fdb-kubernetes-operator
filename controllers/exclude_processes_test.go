/*
 * exclude_processes_test.go
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
	"net"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("exclude_processes", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error

	Describe("canExcludeNewProcesses", func() {
		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
		})

		Context("with a small cluster", func() {
			When("all processes are healthy", func() {
				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdbv1beta2.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})

			When("one process group is missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 1, fdbv1beta2.ProcessClassStorage)
				})

				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdbv1beta2.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})

			When("two process groups are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 2, fdbv1beta2.ProcessClassStorage)
				})

				It("should not allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdbv1beta2.ProcessClassStorage)
					Expect(canExclude).To(BeFalse())
					Expect(missing).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1", "storage-2"}))
				})
			})

			When("two process groups of a different type are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 2, fdbv1beta2.ProcessClassLog)
				})

				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdbv1beta2.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})
		})

		Context("with a large cluster", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Storage = 20
				err = clusterReconciler.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			When("two process groups are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 2, fdbv1beta2.ProcessClassStorage)
				})

				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdbv1beta2.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})

			When("five process groups are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 5, fdbv1beta2.ProcessClassStorage)
				})

				It("should not allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdbv1beta2.ProcessClassStorage)
					Expect(canExclude).To(BeFalse())
					Expect(missing).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1", "storage-10", "storage-11", "storage-12", "storage-13"}))
				})
			})
		})
	})

	When("validating getProcessesToExclude", func() {
		var exclusions []fdbv1beta2.ProcessAddress

		BeforeEach(func() {
			cluster = &fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "storage-1", ProcessClass: "storage", Addresses: []string{"1.1.1.1"}},
						{ProcessGroupID: "storage-2", ProcessClass: "storage", Addresses: []string{"1.1.1.2"}},
						{ProcessGroupID: "storage-3", ProcessClass: "storage", Addresses: []string{"1.1.1.3"}},
						{ProcessGroupID: "stateless-1", ProcessClass: "stateless", Addresses: []string{"1.1.1.4"}},
						{ProcessGroupID: "stateless-2", ProcessClass: "stateless", Addresses: []string{"1.1.1.5"}},
						{ProcessGroupID: "stateless-3", ProcessClass: "stateless", Addresses: []string{"1.1.1.6"}},
						{ProcessGroupID: "stateless-4", ProcessClass: "stateless", Addresses: []string{"1.1.1.7"}},
						{ProcessGroupID: "stateless-5", ProcessClass: "stateless", Addresses: []string{"1.1.1.8"}},
						{ProcessGroupID: "stateless-6", ProcessClass: "stateless", Addresses: []string{"1.1.1.9"}},
						{ProcessGroupID: "stateless-7", ProcessClass: "stateless", Addresses: []string{"1.1.2.1"}},
						{ProcessGroupID: "stateless-8", ProcessClass: "stateless", Addresses: []string{"1.1.2.2"}},
						{ProcessGroupID: "stateless-9", ProcessClass: "stateless", Addresses: []string{"1.1.2.3"}},
						{ProcessGroupID: "log-1", ProcessClass: "log", Addresses: []string{"1.1.2.4"}},
						{ProcessGroupID: "log-2", ProcessClass: "log", Addresses: []string{"1.1.2.5"}},
						{ProcessGroupID: "log-3", ProcessClass: "log", Addresses: []string{"1.1.2.6"}},
						{ProcessGroupID: "log-4", ProcessClass: "log", Addresses: []string{"1.1.2.7"}},
					},
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						UseLocalitiesForExclusion: pointer.Bool(true),
					},
				},
			}
			exclusions = []fdbv1beta2.ProcessAddress{}
		})

		Context("cluster doesn't supports locality based exclusions", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.Default.String()
			})

			When("there are no exclusions", func() {
				It("should not exclude anything", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 0)
					Expect(len(processClassesToExclude)).To(Equal(0))
					Expect(len(fdbProcessesToExclude)).To(Equal(0))
				})
			})

			When("excluding one process", func() {
				BeforeEach(func() {
					processGroup := cluster.Status.ProcessGroups[0]
					Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup
				})

				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 0)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal("1.1.1.1"))
				})
			})

			When("excluding two process", func() {
				BeforeEach(func() {
					processGroup1 := cluster.Status.ProcessGroups[0]
					Expect(processGroup1.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup1.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup1

					processGroup2 := cluster.Status.ProcessGroups[1]
					Expect(processGroup2.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
					processGroup2.MarkForRemoval()
					cluster.Status.ProcessGroups[1] = processGroup2
				})

				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 0)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal("1.1.1.1 1.1.1.2"))
				})
			})

			When("excluding two process with one already excluded", func() {
				BeforeEach(func() {
					processGroup1 := cluster.Status.ProcessGroups[0]
					Expect(processGroup1.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup1.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup1

					processGroup2 := cluster.Status.ProcessGroups[1]
					Expect(processGroup2.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
					processGroup2.MarkForRemoval()
					cluster.Status.ProcessGroups[1] = processGroup2

					exclusions = append(exclusions, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(processGroup2.Addresses[0])})
				})

				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 1)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal("1.1.1.1"))
				})
			})
		})

		Context("cluster supports locality based exclusions", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
			})

			When("there are no exclusions", func() {
				It("should not exclude anything", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 0)
					Expect(len(processClassesToExclude)).To(Equal(0))
					Expect(len(fdbProcessesToExclude)).To(Equal(0))
				})
			})

			When("excluding one process", func() {
				BeforeEach(func() {
					processGroup := cluster.Status.ProcessGroups[0]
					Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup
				})

				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 0)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
				})
			})

			When("excluding two process", func() {
				BeforeEach(func() {
					processGroup1 := cluster.Status.ProcessGroups[0]
					Expect(processGroup1.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup1.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup1

					processGroup2 := cluster.Status.ProcessGroups[1]
					Expect(processGroup2.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
					processGroup2.MarkForRemoval()
					cluster.Status.ProcessGroups[1] = processGroup2
				})

				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 0)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
				})
			})

			When("excluding two process with one already excluded using IP", func() {
				BeforeEach(func() {
					processGroup1 := cluster.Status.ProcessGroups[0]
					Expect(processGroup1.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup1.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup1

					processGroup2 := cluster.Status.ProcessGroups[1]
					Expect(processGroup2.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
					processGroup2.MarkForRemoval()
					cluster.Status.ProcessGroups[1] = processGroup2

					exclusions = append(exclusions, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(processGroup2.Addresses[0])})
				})

				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 1)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
				})
			})

			When("excluding two process with one already excluded using locality", func() {
				BeforeEach(func() {
					processGroup1 := cluster.Status.ProcessGroups[0]
					Expect(processGroup1.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup1.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup1

					processGroup2 := cluster.Status.ProcessGroups[1]
					Expect(processGroup2.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
					processGroup2.MarkForRemoval()
					cluster.Status.ProcessGroups[1] = processGroup2

					exclusions = append(exclusions, fdbv1beta2.ProcessAddress{StringAddress: processGroup2.GetExclusionString()})
				})
				It("should report the excluded process", func() {
					fdbProcessesToExclude, processClassesToExclude := getProcessesToExclude(exclusions, cluster, 1)
					Expect(len(processClassesToExclude)).To(Equal(1))
					Expect(processClassesToExclude).To(Equal(map[fdbv1beta2.ProcessClass]fdbv1beta2.None{fdbv1beta2.ProcessClassStorage: {}}))
					Expect(len(fdbProcessesToExclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExclude, " ")).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
				})
			})
		})
	})
})

func createMissingProcesses(cluster *fdbv1beta2.FoundationDBCluster, count int, processClass fdbv1beta2.ProcessClass) {
	missing := 0
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.ProcessClass == processClass {
			processGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
			missing++
			if missing == count {
				break
			}
		}
	}
	Expect(missing).To(Equal(count))
}
