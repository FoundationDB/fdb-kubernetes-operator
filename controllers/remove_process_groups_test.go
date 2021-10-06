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

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("remove_process_groups", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var result *requeue

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
		result = removeProcessGroups{}.reconcile(clusterReconciler, context.TODO(), cluster)
	})

	When("trying to remove a coordinator", func() {
		coordinatorIP := "1.1.1.1"
		coordinatorID := "storage-1"

		BeforeEach(func() {
			marked, processGroup := fdbtypes.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, coordinatorID, fdbtypes.ProcessClassStorage, coordinatorIP)
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

			allExcluded, processes := clusterReconciler.getProcessGroupsToRemove(cluster, remaining)
			Expect(allExcluded).To(BeFalse())
			Expect(processes).To(BeEmpty())
		})
	})

	When("removing a process group", func() {
		BeforeEach(func() {
			removedProcessGroup := cluster.Status.ProcessGroups[0]
			marked, processGroup := fdbtypes.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, removedProcessGroup.ProcessGroupID, removedProcessGroup.ProcessClass, removedProcessGroup.Addresses[0])
			Expect(marked).To(BeTrue())
			Expect(processGroup).To(BeNil())
		})

		When("using the default setting of EnforceFullReplicationForDeletion", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.EnforceFullReplicationForDeletion = nil
			})

			When("the cluster is fully replicated", func() {
				It("should successfully remove that process group", func() {
					Expect(result).To(BeNil())
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
				})
			})

			When("the cluster is not available", func() {
				BeforeEach(func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.frozenStatus = &fdbtypes.FoundationDBStatus{
						Client: fdbtypes.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbtypes.FoundationDBStatusClientDBStatus{
								Available: false,
							},
						},
					}
				})

				It("should not remove that process group", func() {
					Expect(result).NotTo(BeNil())
					Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
				})
			})
		})

		When("enabling the EnforceFullReplicationForDeletion setting", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.EnforceFullReplicationForDeletion = pointer.BoolPtr(true)
			})

			When("the cluster is fully replicated", func() {
				It("should successfully remove that process group", func() {
					Expect(result).To(BeNil())
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
				})
			})

			When("the cluster is not available", func() {
				BeforeEach(func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.frozenStatus = &fdbtypes.FoundationDBStatus{
						Client: fdbtypes.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbtypes.FoundationDBStatusClientDBStatus{
								Available: false,
							},
						},
					}
				})

				It("should not remove that process group", func() {
					Expect(result).NotTo(BeNil())
					Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
				})
			})
		})

		When("disabling the EnforceFullReplicationForDeletion setting", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.EnforceFullReplicationForDeletion = pointer.BoolPtr(false)
			})

			When("the cluster is fully replicated", func() {
				It("should successfully remove that process group", func() {
					Expect(result).To(BeNil())
				})
			})

			When("the cluster is not fully replicated", func() {
				BeforeEach(func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.maxZoneFailuresWithoutLosingAvailability = pointer.Int(0)
				})

				It("should not remove that process group", func() {
					Expect(result).To(BeNil())
				})
			})

			When("the cluster has degraded data fault tolerance", func() {
				BeforeEach(func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.maxZoneFailuresWithoutLosingData = pointer.Int(0)
				})

				It("should not remove that process group", func() {
					Expect(result).To(BeNil())
				})
			})

			When("the cluster is not available", func() {
				BeforeEach(func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.frozenStatus = &fdbtypes.FoundationDBStatus{
						Client: fdbtypes.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbtypes.FoundationDBStatusClientDBStatus{
								Available: false,
							},
						},
					}
				})

				It("should not remove that process group", func() {
					Expect(result).To(BeNil())
				})
			})
		})
	})

	AfterEach(func() {
		k8sClient.Clear()
	})
})
