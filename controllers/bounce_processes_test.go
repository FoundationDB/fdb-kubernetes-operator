/*
 * bounce_processes_test.go
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
	"sort"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("BounceProcesses", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var adminClient *MockAdminClient
	var lockClient *MockLockClient
	var requeue *Requeue
	var err error

	BeforeEach(func() {
		cluster = createDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		err = setupClusterForTest(cluster)
		Expect(err).NotTo(HaveOccurred())

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		lockClient = newMockLockClientUncast(cluster)

	})

	JustBeforeEach(func() {
		requeue = BounceProcesses{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(requeue).To(BeNil())
		})

		It("should not kill any processes", func() {
			Expect(adminClient.KilledAddresses).To(BeEmpty())
		})
	})

	Context("with incorrect processes", func() {
		BeforeEach(func() {
			processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
			processGroup.UpdateCondition(fdbtypes.IncorrectCommandLine, true, nil, "")

			processGroup = cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-2"))
			processGroup.UpdateCondition(fdbtypes.IncorrectCommandLine, true, nil, "")
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill the targeted processes", func() {
			addresses := make([]string, 0, 2)
			for _, processGroupID := range []string{"storage-1", "storage-2"} {
				processGroupAddresses := fdbtypes.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID).Addresses
				for _, address := range processGroupAddresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address))
				}
			}
			sort.Strings(adminClient.KilledAddresses)
			Expect(adminClient.KilledAddresses).To(Equal(addresses))
		})
	})

	Context("with multiple storage servers per pod", func() {
		BeforeEach(func() {
			cluster.Spec.StorageServersPerPod = 2
			err = k8sClient.Update(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			_, err = reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())

			processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-5"))
			processGroup.UpdateCondition(fdbtypes.IncorrectCommandLine, true, nil, "")

			processGroup = cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-6"))
			processGroup.UpdateCondition(fdbtypes.IncorrectCommandLine, true, nil, "")
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill the targeted processes", func() {
			addresses := make([]string, 0, 2)
			for _, processGroupID := range []string{"storage-5", "storage-6"} {
				processGroupAddresses := fdbtypes.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID).Addresses
				for _, address := range processGroupAddresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address), fmt.Sprintf("%s:4503", address))
				}
			}
			sort.Strings(adminClient.KilledAddresses)
			Expect(adminClient.KilledAddresses).To(Equal(addresses))
		})
	})

	Context("with a pending upgrade", func() {
		BeforeEach(func() {
			cluster.Spec.Version = fdbtypes.Versions.NextMajorVersion.String()
			for _, processGroup := range cluster.Status.ProcessGroups {
				processGroup.UpdateCondition(fdbtypes.IncorrectCommandLine, true, nil, "")
			}
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill all the processes", func() {
			addresses := make([]string, 0, len(cluster.Status.ProcessGroups))
			for _, processGroup := range cluster.Status.ProcessGroups {
				for _, address := range processGroup.Addresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address))
				}
			}
			sort.Strings(addresses)
			sort.Strings(adminClient.KilledAddresses)
			Expect(adminClient.KilledAddresses).To(Equal(addresses))
		})

		It("should update the running version in the status", func() {
			_, err = reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(cluster.Status.RunningVersion).To(Equal(fdbtypes.Versions.NextMajorVersion.String()))
		})

		It("should submit pending upgrade information for all the processes", func() {
			expectedUpgrades := make(map[string]bool, len(cluster.Status.ProcessGroups))
			for _, processGroup := range cluster.Status.ProcessGroups {
				expectedUpgrades[processGroup.ProcessGroupID] = true
			}
			Expect(lockClient.pendingUpgrades[fdbtypes.Versions.NextMajorVersion]).To(Equal(expectedUpgrades))
		})

		Context("with an unknown process", func() {
			BeforeEach(func() {
				adminClient.MockAdditionalProcesses([]fdbtypes.ProcessGroupStatus{{
					ProcessGroupID: "dc2-storage-1",
					ProcessClass:   "storage",
					Addresses:      []string{"1.2.3.4"},
				}})
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.Message).To(Equal("Waiting for processes to be updated: [dc2-storage-1]"))
			})

			It("should not kill any processes", func() {
				Expect(adminClient.KilledAddresses).To(BeEmpty())
			})

			It("should not update the running version in the status", func() {
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Status.RunningVersion).To(Equal(fdbtypes.Versions.Default.String()))
			})

			It("should submit pending upgrade information for all the processes", func() {
				expectedUpgrades := make(map[string]bool, len(cluster.Status.ProcessGroups))
				for _, processGroup := range cluster.Status.ProcessGroups {
					expectedUpgrades[processGroup.ProcessGroupID] = true
				}
				Expect(lockClient.pendingUpgrades[fdbtypes.Versions.NextMajorVersion]).To(Equal(expectedUpgrades))
			})

			Context("with a pending upgrade for the unknown process", func() {
				BeforeEach(func() {
					err = lockClient.AddPendingUpgrades(fdbtypes.Versions.NextMajorVersion, []string{"dc2-storage-1"})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should kill all the processes", func() {
					addresses := make([]string, 0, len(cluster.Status.ProcessGroups)+1)
					for _, processGroup := range cluster.Status.ProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses = append(addresses, fmt.Sprintf("%s:4501", address))
						}
					}
					addresses = append(addresses, "1.2.3.4:4501")
					sort.Strings(addresses)
					sort.Strings(adminClient.KilledAddresses)
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})
			})

			Context("with a pending upgrade to an older version", func() {
				BeforeEach(func() {
					err = lockClient.AddPendingUpgrades(fdbtypes.Versions.NextPatchVersion, []string{"dc2-storage-1"})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.Message).To(Equal("Waiting for processes to be updated: [dc2-storage-1]"))
				})

				It("should not kill any processes", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})
			})

			Context("with locks disabled", func() {
				BeforeEach(func() {
					disabled := true
					cluster.Spec.LockOptions.DisableLocks = &disabled
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should kill all the processes", func() {
					addresses := make([]string, 0, len(cluster.Status.ProcessGroups))
					for _, processGroup := range cluster.Status.ProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses = append(addresses, fmt.Sprintf("%s:4501", address))
						}
					}
					sort.Strings(addresses)
					sort.Strings(adminClient.KilledAddresses)
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should not submit pending upgrade information", func() {
					Expect(lockClient.pendingUpgrades).To(BeEmpty())
				})
			})
		})
	})
})
