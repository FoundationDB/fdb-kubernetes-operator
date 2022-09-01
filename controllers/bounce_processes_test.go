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
	"net"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("bounceProcesses", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var adminClient *mockAdminClient
	var lockClient *mockLockClient
	var requeue *requeue
	var err error

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		err = setupClusterForTest(cluster)
		Expect(err).NotTo(HaveOccurred())

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		lockClient = newMockLockClientUncast(cluster)

	})

	JustBeforeEach(func() {
		requeue = bounceProcesses{}.reconcile(context.TODO(), clusterReconciler, cluster)
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
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")

			processGroup = cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-2"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill the targeted processes", func() {
			addresses := make([]string, 0, 2)
			for _, processGroupID := range []string{"storage-1", "storage-2"} {
				processGroupAddresses := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID).Addresses
				for _, address := range processGroupAddresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address))
				}
			}

			Expect(len(adminClient.KilledAddresses)).To(Equal(len(addresses)))
			Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
		})
	})

	Context("with excluded and incorrect processes", func() {
		BeforeEach(func() {
			processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")

			processGroup = cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-2"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
			processGroup.MarkForRemoval()
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill the targeted processes", func() {
			addresses := make([]string, 0, 1)
			for _, processGroupID := range []string{"storage-1"} {
				processGroupAddresses := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID).Addresses
				for _, address := range processGroupAddresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address))
				}
			}

			Expect(len(adminClient.KilledAddresses)).To(Equal(len(addresses)))
			Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
		})
	})

	Context("with a manually excluded process", func() {
		BeforeEach(func() {
			processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
			for _, address := range processGroup.Addresses {
				err := adminClient.ExcludeProcesses([]fdbv1beta2.ProcessAddress{{StringAddress: address, Port: 4501}})
				Expect(err).To(BeNil())
			}

		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill the targeted processes", func() {
			addresses := make([]string, 0, 1)
			for _, processGroupID := range []string{"storage-1"} {
				processGroupAddresses := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID).Addresses
				for _, address := range processGroupAddresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address))
				}
			}
			Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
		})
	})

	Context("with Pod in pending state", func() {
		BeforeEach(func() {
			processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
			processGroup.UpdateCondition(fdbv1beta2.PodPending, true, nil, "")
			cluster.Spec.AutomationOptions.IgnorePendingPodsDuration = 1 * time.Nanosecond
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not kill the pending process", func() {
			addresses := make([]string, 0, 1)
			processGroupAddresses := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-1").Addresses
			for _, address := range processGroupAddresses {
				addresses = append(addresses, fmt.Sprintf("%s:4501", address))
			}
			Expect(adminClient.KilledAddresses).NotTo(ContainElements(addresses))
		})
	})

	When("a process group has the MissingProcess condition", func() {
		BeforeEach(func() {
			processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
			processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
				ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
				Timestamp:                 time.Now().Add(-2 * time.Minute).Unix(),
			})

			processGroup = cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-2"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not kill the missing process but all other processes", func() {
			addresses := make([]string, 0, 1)
			processGroupAddresses := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-1").Addresses
			for _, address := range processGroupAddresses {
				addresses = append(addresses, fmt.Sprintf("%s:4501", address))
			}
			Expect(adminClient.KilledAddresses).NotTo(ContainElements(addresses))
			Expect(len(adminClient.KilledAddresses)).To(BeNumerically("==", 1))
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
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")

			processGroup = cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-3]
			Expect(processGroup.ProcessGroupID).To(Equal("storage-6"))
			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should kill the targeted processes", func() {
			addresses := make([]string, 0, 2)
			for _, processGroupID := range []string{"storage-5", "storage-6"} {
				processGroupAddresses := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID).Addresses
				for _, address := range processGroupAddresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address), fmt.Sprintf("%s:4503", address))
				}
			}
			Expect(len(adminClient.KilledAddresses)).To(BeNumerically("==", len(addresses)))
			Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
		})
	})

	Context("with a pending upgrade", func() {
		BeforeEach(func() {
			cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
			for _, processGroup := range cluster.Status.ProcessGroups {
				processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true, nil, "")
			}
		})

		It("should requeue", func() {
			Expect(requeue).NotTo(BeNil())
		})

		It("should kill all the processes", func() {
			addresses := make([]string, 0, len(cluster.Status.ProcessGroups))
			for _, processGroup := range cluster.Status.ProcessGroups {
				for _, address := range processGroup.Addresses {
					addresses = append(addresses, fmt.Sprintf("%s:4501", address))
				}
			}
			Expect(len(adminClient.KilledAddresses)).To(BeNumerically("==", len(addresses)))
			Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
		})

		It("should update the running version in the status", func() {
			_, err = reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.NextMajorVersion.String()))
		})

		It("should submit pending upgrade information for all the processes", func() {
			expectedUpgrades := make(map[string]bool, len(cluster.Status.ProcessGroups))
			for _, processGroup := range cluster.Status.ProcessGroups {
				expectedUpgrades[processGroup.ProcessGroupID] = true
			}
			Expect(lockClient.pendingUpgrades[fdbv1beta2.Versions.NextMajorVersion]).To(Equal(expectedUpgrades))
		})

		Context("with an unknown process", func() {
			BeforeEach(func() {
				adminClient.MockAdditionalProcesses([]fdbv1beta2.ProcessGroupStatus{{
					ProcessGroupID: "dc2-storage-1",
					ProcessClass:   "storage",
					Addresses:      []string{"1.2.3.4"},
				}})
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.message).To(Equal("Waiting for processes to be updated: [dc2-storage-1]"))
			})

			It("should not kill any processes", func() {
				Expect(adminClient.KilledAddresses).To(BeEmpty())
			})

			It("should not update the running version in the status", func() {
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.Default.String()))
			})

			It("should submit pending upgrade information for all the processes", func() {
				expectedUpgrades := make(map[string]bool, len(cluster.Status.ProcessGroups))
				for _, processGroup := range cluster.Status.ProcessGroups {
					expectedUpgrades[processGroup.ProcessGroupID] = true
				}
				Expect(lockClient.pendingUpgrades[fdbv1beta2.Versions.NextMajorVersion]).To(Equal(expectedUpgrades))
			})

			Context("with a pending upgrade for the unknown process", func() {
				BeforeEach(func() {
					err = lockClient.AddPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion, []string{"dc2-storage-1"})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
				})

				It("should kill all the processes", func() {
					addresses := make([]string, 0, len(cluster.Status.ProcessGroups)+1)
					for _, processGroup := range cluster.Status.ProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses = append(addresses, fmt.Sprintf("%s:4501", address))
						}
					}
					addresses = append(addresses, "1.2.3.4:4501")
					Expect(len(adminClient.KilledAddresses)).To(BeNumerically("==", len(addresses)))
					Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
				})
			})

			Context("with a pending upgrade to an older version", func() {
				BeforeEach(func() {
					err = lockClient.AddPendingUpgrades(fdbv1beta2.Versions.NextPatchVersion, []string{"dc2-storage-1"})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.message).To(Equal("Waiting for processes to be updated: [dc2-storage-1]"))
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

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
				})

				It("should kill all the processes", func() {
					addresses := make([]string, 0, len(cluster.Status.ProcessGroups))
					for _, processGroup := range cluster.Status.ProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses = append(addresses, fmt.Sprintf("%s:4501", address))
						}
					}
					Expect(len(adminClient.KilledAddresses)).To(BeNumerically("==", len(addresses)))
					Expect(adminClient.KilledAddresses).To(ContainElements(addresses))
				})

				It("should not submit pending upgrade information", func() {
					Expect(lockClient.pendingUpgrades).To(BeEmpty())
				})
			})
		})
	})

	DescribeTable("when getting the minimum uptime and the address map", func(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, useRecoveryState bool, expectedMinimumUptime float64, expectedAddressMap map[string][]fdbv1beta2.ProcessAddress) {
		minimumUptime, addressMap, err := getMinimumUptimeAndAddressMap(cluster, status, useRecoveryState)
		Expect(err).NotTo(HaveOccurred())
		Expect(minimumUptime).To(BeNumerically("==", expectedMinimumUptime))
		Expect(len(addressMap)).To(BeNumerically("==", len(expectedAddressMap)))
		for key, value := range expectedAddressMap {
			Expect(addressMap).To(HaveKeyWithValue(key, value))
		}
	},
		Entry("when recovered since is not available",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.Default.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 90.0,
					},
				},
			},
			true,
			30.0,
			map[string][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
		Entry("when recovered since is enabled and version supports it",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.SupportsRecoveryState.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 90.0,
					},
				},
			},
			true,
			90.0,
			map[string][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
		Entry("when recovered since is disabled and version supports it",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.SupportsRecoveryState.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 90.0,
					},
				},
			},
			false,
			30.0,
			map[string][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
	)
})
