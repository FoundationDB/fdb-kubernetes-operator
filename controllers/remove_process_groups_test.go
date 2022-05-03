/*
 * remove_process_groups_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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
	"time"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("remove_process_groups", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var result *requeue

	Context("validating process removal", func() {
		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			err := k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
		})

		JustBeforeEach(func() {
			result = removeProcessGroups{}.reconcile(context.TODO(), clusterReconciler, cluster)
		})

		When("trying to remove a coordinator", func() {
			coordinatorIP := "1.1.1.1"
			coordinatorID := "storage-1"

			BeforeEach(func() {
				marked, processGroup := fdbv1beta2.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, coordinatorID, fdbv1beta2.ProcessClassStorage, coordinatorIP)
				Expect(marked).To(BeTrue())
				Expect(processGroup).To(BeNil())
			})

			It("should not remove the coordinator", func() {
				Expect(result).NotTo(BeNil())
				Expect(result.message).To(Equal("Reconciliation needs to exclude more processes"))
			})

			It("should exclude the coordinator from the process group list to remove", func() {
				remaining := map[string]bool{
					coordinatorIP: false,
				}

				allExcluded, newExclusions, processes := clusterReconciler.getProcessGroupsToRemove(cluster, remaining)
				Expect(allExcluded).To(BeFalse())
				Expect(processes).To(BeEmpty())
				Expect(newExclusions).To(BeFalse())
			})
		})

		When("removing a process group", func() {
			var removedProcessGroup *fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				removedProcessGroup = cluster.Status.ProcessGroups[0]
				marked, processGroup := fdbv1beta2.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, removedProcessGroup.ProcessGroupID, removedProcessGroup.ProcessClass, removedProcessGroup.Addresses[0])
				Expect(marked).To(BeTrue())
				Expect(processGroup).To(BeNil())
				// Exclude the process group
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.ExcludedAddresses = removedProcessGroup.Addresses
			})

			When("using the default setting of EnforceFullReplicationForDeletion", func() {
				When("the cluster is fully replicated", func() {
					It("should successfully remove that process group", func() {
						Expect(result).To(BeNil())
						// Ensure resources are deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
					})
				})

				When("the cluster has degraded availability fault tolerance", func() {
					BeforeEach(func() {
						adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.maxZoneFailuresWithoutLosingAvailability = pointer.Int(0)
					})

					It("should not remove that process group", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})

				When("the cluster has degraded data fault tolerance", func() {
					BeforeEach(func() {
						adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.maxZoneFailuresWithoutLosingData = pointer.Int(0)
					})

					It("should not remove that process group", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})

				When("the cluster is not available", func() {
					BeforeEach(func() {
						adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.frozenStatus = &fdbv1beta2.FoundationDBStatus{
							Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
								DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
									Available: false,
								},
							},
						}
					})

					It("should not remove that process group", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})
			})

			When("Removing multiple process groups", func() {
				var initialCnt int
				var secondRemovedProcessGroup *fdbv1beta2.ProcessGroupStatus

				BeforeEach(func() {
					initialCnt = len(cluster.Status.ProcessGroups)
					secondRemovedProcessGroup = cluster.Status.ProcessGroups[1]
					marked, processGroup := fdbv1beta2.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, secondRemovedProcessGroup.ProcessGroupID, secondRemovedProcessGroup.ProcessClass, removedProcessGroup.Addresses[0])
					Expect(marked).To(BeTrue())
					Expect(processGroup).To(BeNil())
					// Exclude the process group
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.ExcludedAddresses = append(adminClient.ExcludedAddresses, secondRemovedProcessGroup.Addresses...)
				})

				It("should remove only one process group", func() {
					Expect(result).To(BeNil())
					Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 1))
					// Ensure resources are deleted
					removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
					Expect(err).To(BeNil())
					Expect(removed).To(BeTrue())
					Expect(include).To(BeTrue())
					// Ensure resources are not deleted
					removed, include, err = confirmRemoval(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup.ProcessGroupID)
					Expect(err).To(BeNil())
					Expect(removed).To(BeFalse())
					Expect(include).To(BeFalse())
				})

				When("a process group is marked as terminating and all resources are removed it should be removed", func() {
					BeforeEach(func() {
						secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
						err := removeProcessGroup(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup.ProcessGroupID)
						Expect(err).NotTo(HaveOccurred())
						// Sleep here to prevent some timeing issues.
						time.Sleep(10 * time.Microsecond)
					})

					It("should remove the process group and the terminated process group", func() {
						Expect(result).To(BeNil())
						Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
						// Ensure resources are deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
						// Ensure resources are deleted
						removed, include, err = confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
					})
				})

				When("a process group is marked as terminating and the resources are not removed", func() {
					BeforeEach(func() {
						secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
						// Sleep here to prevent some timeing issues.
						time.Sleep(10 * time.Microsecond)
					})

					It("should remove the process group and the terminated process group", func() {
						Expect(result).To(BeNil())
						Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
						// Ensure resources are deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
						// Ensure resources are deleted
						removed, include, err = confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
					})
				})

				When("a process group is marked as terminating and not fully removed", func() {
					BeforeEach(func() {
						secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
						// Set the wait time to the default value
						cluster.Spec.AutomationOptions.WaitBetweenRemovalsSeconds = pointer.Int(60)
					})

					It("should remove only one process group", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(HavePrefix("not allowed to remove process groups, waiting:"))
						Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 0))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), clusterReconciler, cluster, removedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
						// Ensure resources are deleted
						removed, include, err = confirmRemoval(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup.ProcessGroupID)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})
			})
		})

		AfterEach(func() {
			k8sClient.Clear()
		})
	})

	Context("validating getProcessesToInclude", func() {
		var removedProcessGroups map[string]bool

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
			removedProcessGroups = make(map[string]bool)
		})

		Context("cluster doesn't support inclusions using locality", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.Default.String()
			})

			When("including no process", func() {
				It("should not include any process", func() {
					fdbProcessesToInclude, processGroups, err := getProcessesToInclude(cluster, removedProcessGroups)
					Expect(err).To(BeNil())
					Expect(len(fdbProcessesToInclude)).To(Equal(0))
					Expect(len(processGroups)).To(Equal(16))
				})
			})

			When("including one process", func() {
				BeforeEach(func() {
					processGroup := cluster.Status.ProcessGroups[0]
					Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
					processGroup.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup

					removedProcessGroups[processGroup.ProcessGroupID] = true
				})

				It("should include one process", func() {
					fdbProcessesToInclude, processGroups, err := getProcessesToInclude(cluster, removedProcessGroups)
					Expect(err).To(BeNil())
					Expect(len(fdbProcessesToInclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToInclude, " ")).To(Equal("1.1.1.1"))
					Expect(len(processGroups)).To(Equal(15))
				})
			})
		})

		Context("cluster support inclusions using locality", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
			})

			When("including no process", func() {
				It("should not include any process", func() {
					fdbProcessesToInclude, processGroups, err := getProcessesToInclude(cluster, removedProcessGroups)
					Expect(err).To(BeNil())
					Expect(len(fdbProcessesToInclude)).To(Equal(0))
					Expect(len(processGroups)).To(Equal(16))
				})
			})

			When("including one process", func() {
				BeforeEach(func() {
					processGroup := cluster.Status.ProcessGroups[0]
					Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
					processGroup.MarkForRemoval()
					cluster.Status.ProcessGroups[0] = processGroup

					removedProcessGroups[processGroup.ProcessGroupID] = true
				})

				It("should include one process", func() {
					fdbProcessesToInclude, processGroups, err := getProcessesToInclude(cluster, removedProcessGroups)
					Expect(err).To(BeNil())
					Expect(len(fdbProcessesToInclude)).To(Equal(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToInclude, " ")).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[0].Addresses[0])))
					Expect(len(processGroups)).To(Equal(15))
				})
			})
		})
	})
})
