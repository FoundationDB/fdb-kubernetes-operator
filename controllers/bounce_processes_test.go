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
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/buggify"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"
	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("bounceProcesses", func() {
	When("bouncing processes", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var adminClient *mock.AdminClient
		var lockClient *mock.LockClient
		var requeue *requeue
		var err error

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			cluster.Spec.LockOptions.DisableLocks = pointer.Bool(false)
			Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())

			adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			lockClient = mock.NewMockLockClientUncast(cluster)
		})

		JustBeforeEach(func() {
			requeue = bounceProcesses{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
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

		When("two processes have the IncorrectCommandLine condition", func() {
			var pickedProcessGroups []*fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				pickedProcessGroups = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)

				for _, processGroup := range pickedProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should kill the targeted processes", func() {
				addresses := make(map[string]fdbv1beta2.None, 2)
				for _, processGroup := range pickedProcessGroups {
					for _, address := range processGroup.Addresses {
						addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
					}
				}
				Expect(adminClient.KilledAddresses).To(Equal(addresses))
			})

			When("one process is marked for removal", func() {
				BeforeEach(func() {
					pickedProcessGroups[0].MarkForRemoval()
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should kill the targeted processes", func() {
					addresses := make(map[string]fdbv1beta2.None, 1)
					for _, processGroup := range pickedProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
						}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})
			})

			When("one process was manually excluded", func() {
				BeforeEach(func() {
					for _, address := range pickedProcessGroups[0].Addresses {
						err := adminClient.ExcludeProcesses([]fdbv1beta2.ProcessAddress{{StringAddress: address, Port: 4501}})
						Expect(err).To(BeNil())
					}
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should kill the targeted processes", func() {
					addresses := make(map[string]fdbv1beta2.None, 1)
					for _, processGroup := range pickedProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
						}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})
			})
		})

		Context("with Pod in pending state", func() {
			var pickedProcessGroups []*fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				pickedProcessGroups = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)

				for _, processGroup := range pickedProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
					processGroup.UpdateCondition(fdbv1beta2.PodPending, true)
				}

				cluster.Spec.AutomationOptions.IgnorePendingPodsDuration = 1 * time.Nanosecond
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not kill the pending process", func() {
				for _, address := range pickedProcessGroups[0].Addresses {
					addr := fmt.Sprintf("%s:4501", address)
					Expect(adminClient.KilledAddresses).NotTo(HaveKey(addr))
				}
			})
		})

		When("a process group has the MissingProcess condition", func() {
			var pickedProcessGroups []*fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				pickedProcessGroups = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)

				for _, processGroup := range pickedProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
				}

				pickedProcessGroups[0].ProcessGroupConditions = append(pickedProcessGroups[0].ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
					ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
					Timestamp:                 time.Now().Add(-2 * time.Minute).Unix(),
				})
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not kill the missing process but all other processes", func() {
				Expect(adminClient.KilledAddresses).To(HaveLen(1))
				for _, address := range pickedProcessGroups[0].Addresses {
					addr := fmt.Sprintf("%s:4501", address)
					Expect(adminClient.KilledAddresses).NotTo(HaveKey(addr))
				}
			})
		})

		When("using multiple storage servers per pod", func() {
			var pickedProcessGroups []*fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())

				pickedProcessGroups = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)
				for _, processGroup := range pickedProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should kill the targeted processes", func() {
				addresses := make(map[string]fdbv1beta2.None, 2)
				for _, processGroup := range pickedProcessGroups {
					for _, address := range processGroup.Addresses {
						addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
						addresses[fmt.Sprintf("%s:4503", address)] = fdbv1beta2.None{}
					}
				}
				Expect(adminClient.KilledAddresses).To(Equal(addresses))
			})

			When("doing an upgrade", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
					for _, processGroup := range cluster.Status.ProcessGroups {
						processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
					}
				})

				When("all processes are reporting", func() {
					It("should requeue", func() {
						Expect(requeue).NotTo(BeNil())
					})

					It("should kill all the processes", func() {
						addresses := make(map[string]fdbv1beta2.None, len(cluster.Status.ProcessGroups))
						for _, processGroup := range cluster.Status.ProcessGroups {
							for _, address := range processGroup.Addresses {
								addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
								if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
									addresses[fmt.Sprintf("%s:4503", address)] = fdbv1beta2.None{}
								}
							}
						}
						Expect(adminClient.KilledAddresses).To(Equal(addresses))
					})

					It("should update the running version in the status", func() {
						_, err = reloadCluster(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.NextMajorVersion.String()))
					})

					It("should submit pending upgrade information for all the processes", func() {
						expectedUpgrades := make(map[fdbv1beta2.ProcessGroupID]bool, len(cluster.Status.ProcessGroups))
						for _, processGroup := range cluster.Status.ProcessGroups {
							expectedUpgrades[processGroup.ProcessGroupID] = true
						}
						pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingUpgrades).To(Equal(expectedUpgrades))
					})
				})

				When("the processes of a pod are missing", func() {
					var missingProcessGroup *fdbv1beta2.ProcessGroupStatus

					BeforeEach(func() {
						for _, processGroup := range cluster.Status.ProcessGroups {
							if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
								continue
							}

							missingProcessGroup = processGroup
							break
						}

						Expect(missingProcessGroup.ProcessClass).To(Equal(fdbv1beta2.ProcessClassStorage))
						adminClient.MockMissingProcessGroup(missingProcessGroup.ProcessGroupID, true)
						missingProcessGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
					})

					When("they are missing for a short time", func() {
						BeforeEach(func() {
							missingProcessGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
						})

						It("should requeue", func() {
							Expect(requeue).NotTo(BeNil())
							Expect(requeue.message).To(Equal(fmt.Sprintf("could not find address for processes: %s", []fdbv1beta2.ProcessGroupID{missingProcessGroup.ProcessGroupID})))
						})

						It("shouldn't kill any processes", func() {
							Expect(adminClient.KilledAddresses).To(BeEmpty())
						})

						It("should not submit pending upgrade information", func() {
							pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
							Expect(err).NotTo(HaveOccurred())
							Expect(pendingUpgrades).To(BeEmpty())
						})
					})

					When("they are missing for a long time", func() {
						BeforeEach(func() {
							missingProcessGroup.ProcessGroupConditions = []*fdbv1beta2.ProcessGroupCondition{
								{
									ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
									Timestamp:                 time.Now().Add(-5 * time.Minute).Unix(),
								},
							}
						})

						It("should requeue", func() {
							Expect(requeue).NotTo(BeNil())
							Expect(requeue.message).To(Equal("fetch latest status after upgrade"))
						})

						It("should kill the processes except the missing process", func() {
							Expect(adminClient.KilledAddresses).NotTo(BeEmpty())
							Expect(adminClient.KilledAddresses).NotTo(ContainElement(missingProcessGroup.Addresses))
						})

						It("should not submit pending upgrade information", func() {
							pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
							Expect(err).NotTo(HaveOccurred())
							Expect(pendingUpgrades).NotTo(BeEmpty())
						})
					})
				})
			})
		})

		Context("with multiple log servers per pod", func() {
			var pickedProcessGroups []*fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				cluster.Spec.LogServersPerPod = 2
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())

				pickedProcessGroups = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassLog, 2)
				for _, processGroup := range pickedProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should kill the targeted processes", func() {
				addresses := make(map[string]fdbv1beta2.None, 2)
				for _, processGroup := range pickedProcessGroups {
					for _, address := range processGroup.Addresses {
						addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
						addresses[fmt.Sprintf("%s:4503", address)] = fdbv1beta2.None{}
					}
				}
				Expect(adminClient.KilledAddresses).To(Equal(addresses))
			})

			When("doing an upgrade", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
					for _, processGroup := range cluster.Status.ProcessGroups {
						processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
					}
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
				})

				It("should kill all the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(cluster.Status.ProcessGroups))
					for _, processGroup := range cluster.Status.ProcessGroups {
						for _, address := range processGroup.Addresses {
							addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
							if processGroup.ProcessClass == fdbv1beta2.ProcessClassLog {
								addresses[fmt.Sprintf("%s:4503", address)] = fdbv1beta2.None{}
							}
						}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should update the running version in the status", func() {
					_, err = reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.NextMajorVersion.String()))
				})

				It("should submit pending upgrade information for all the processes", func() {
					expectedUpgrades := make(map[fdbv1beta2.ProcessGroupID]bool, len(cluster.Status.ProcessGroups))
					for _, processGroup := range cluster.Status.ProcessGroups {
						expectedUpgrades[processGroup.ProcessGroupID] = true
					}
					pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingUpgrades).To(Equal(expectedUpgrades))
				})
			})
		})

		Context("with a pending upgrade", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
				for _, processGroup := range cluster.Status.ProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
				}
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
			})

			It("should kill all the processes", func() {
				addresses := make(map[string]fdbv1beta2.None, len(cluster.Status.ProcessGroups))
				for _, processGroup := range cluster.Status.ProcessGroups {
					for _, address := range processGroup.Addresses {
						addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
					}
				}
				Expect(adminClient.KilledAddresses).To(Equal(addresses))
			})

			It("should update the running version in the status", func() {
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.NextMajorVersion.String()))
			})

			It("should submit pending upgrade information for all the processes", func() {
				expectedUpgrades := make(map[fdbv1beta2.ProcessGroupID]bool, len(cluster.Status.ProcessGroups))
				for _, processGroup := range cluster.Status.ProcessGroups {
					expectedUpgrades[processGroup.ProcessGroupID] = true
				}
				pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
				Expect(err).NotTo(HaveOccurred())
				Expect(pendingUpgrades).To(Equal(expectedUpgrades))
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
					expectedUpgrades := make(map[fdbv1beta2.ProcessGroupID]bool, len(cluster.Status.ProcessGroups))
					for _, processGroup := range cluster.Status.ProcessGroups {
						expectedUpgrades[processGroup.ProcessGroupID] = true
					}
					pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingUpgrades).To(Equal(expectedUpgrades))
				})

				Context("with a pending upgrade for the unknown process", func() {
					BeforeEach(func() {
						Expect(lockClient.AddPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion, []fdbv1beta2.ProcessGroupID{"dc2-storage-1"})).NotTo(HaveOccurred())
					})

					It("should requeue", func() {
						Expect(requeue).NotTo(BeNil())
					})

					It("should kill all the processes", func() {
						addresses := make(map[string]fdbv1beta2.None, len(cluster.Status.ProcessGroups)+1)
						for _, processGroup := range cluster.Status.ProcessGroups {
							for _, address := range processGroup.Addresses {
								addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
							}
						}
						addresses["1.2.3.4:4501"] = fdbv1beta2.None{}
						Expect(adminClient.KilledAddresses).To(Equal(addresses))
					})
				})

				Context("with a pending upgrade to an older version", func() {
					BeforeEach(func() {
						err = lockClient.AddPendingUpgrades(fdbv1beta2.Versions.NextPatchVersion, []fdbv1beta2.ProcessGroupID{"dc2-storage-1"})
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
						cluster.Spec.LockOptions.DisableLocks = pointer.Bool(true)
					})

					It("should requeue", func() {
						Expect(requeue).NotTo(BeNil())
					})

					It("should kill all the processes", func() {
						addresses := make(map[string]fdbv1beta2.None, len(cluster.Status.ProcessGroups))
						for _, processGroup := range cluster.Status.ProcessGroups {
							for _, address := range processGroup.Addresses {
								addresses[fmt.Sprintf("%s:4501", address)] = fdbv1beta2.None{}
							}
						}
						Expect(adminClient.KilledAddresses).To(Equal(addresses))
					})

					It("should not submit pending upgrade information", func() {
						pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingUpgrades).To(BeEmpty())
					})
				})
			})

			When("one process is missing for a short time", func() {
				var missingProcessGroup *fdbv1beta2.ProcessGroupStatus

				BeforeEach(func() {
					missingProcessGroup = cluster.Status.ProcessGroups[0]
					adminClient.MockMissingProcessGroup(missingProcessGroup.ProcessGroupID, true)
					missingProcessGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.message).To(Equal(fmt.Sprintf("could not find address for processes: %s", []fdbv1beta2.ProcessGroupID{missingProcessGroup.ProcessGroupID})))
				})

				It("shouldn't kill any processes", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})

				It("should not submit pending upgrade information", func() {
					pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingUpgrades).To(BeEmpty())
				})
			})

			When("one process is missing is missing for a long time", func() {
				var missingProcessGroup *fdbv1beta2.ProcessGroupStatus

				BeforeEach(func() {
					missingProcessGroup = cluster.Status.ProcessGroups[0]
					adminClient.MockMissingProcessGroup(missingProcessGroup.ProcessGroupID, true)
					missingProcessGroup.ProcessGroupConditions = []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
							Timestamp:                 time.Now().Add(-5 * time.Minute).Unix(),
						},
					}
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.message).To(Equal("fetch latest status after upgrade"))
				})

				It("should kill the processes except the missing process", func() {
					Expect(adminClient.KilledAddresses).NotTo(BeEmpty())
					Expect(adminClient.KilledAddresses).NotTo(ContainElement(missingProcessGroup.Addresses))
				})

				It("should not submit pending upgrade information", func() {
					pendingUpgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion)
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingUpgrades).NotTo(BeEmpty())
				})
			})
		})

		When("the buggify option ignoreDuringRestart is set", func() {
			var ignoredProcessGroup *fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				ignoredProcessGroup = cluster.Status.ProcessGroups[0]
				cluster.Spec.Buggify.IgnoreDuringRestart = []fdbv1beta2.ProcessGroupID{ignoredProcessGroup.ProcessGroupID}
				for _, processGroup := range cluster.Status.ProcessGroups {
					processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
				}
			})

			It("should not requeue", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(requeue).To(BeNil())
			})

			It("should not kill any processes", func() {
				ignoredAddress := ignoredProcessGroup.Addresses[0]

				for address := range adminClient.KilledAddresses {
					Expect(address).NotTo(HavePrefix(ignoredAddress))
				}

				Expect(adminClient.KilledAddresses).To(HaveLen(len(cluster.Status.ProcessGroups) - 1))
			})

			When("filtering process groups for buggify", func() {
				var filteredAddresses []fdbv1beta2.ProcessAddress
				var removed bool

				BeforeEach(func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					processAddresses := make([]fdbv1beta2.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
					for _, process := range status.Cluster.Processes {
						processAddresses = append(processAddresses, process.Address)
					}

					filteredAddresses, removed = buggify.FilterIgnoredProcessGroups(cluster, processAddresses, status)
				})

				It("should filter the ignored address", func() {
					Expect(removed).To(BeTrue())
					Expect(len(filteredAddresses)).To(BeNumerically("==", len(cluster.Status.ProcessGroups)-1))
				})
			})

			When("buggify ignore processes is set and only this process has the IncorrectCommandLine condition", func() {
				var filteredAddresses []fdbv1beta2.ProcessAddress
				var removed bool

				BeforeEach(func() {
					for _, processGroup := range cluster.Status.ProcessGroups {
						processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, processGroup.ProcessGroupID == ignoredProcessGroup.ProcessGroupID)
					}

					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					processAddresses := make([]fdbv1beta2.ProcessAddress, 0, len(cluster.Status.ProcessGroups))
					for _, process := range status.Cluster.Processes {
						if fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]) != ignoredProcessGroup.ProcessGroupID {
							continue
						}

						processAddresses = append(processAddresses, process.Address)
					}

					filteredAddresses, removed = buggify.FilterIgnoredProcessGroups(cluster, processAddresses, status)
				})

				It("should filter the ignored address", func() {
					Expect(removed).To(BeTrue())
					Expect(filteredAddresses).To(BeEmpty())
				})

				It("should not requeue", func() {
					Expect(err).NotTo(HaveOccurred())
					Expect(requeue).To(BeNil())
				})

				It("should not kill any processes", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})
			})
		})

		When("when there are unreachable processes", func() {
			When("the unreachable processes include at least one tester process", func() {
				BeforeEach(func() {
					adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
								Available: true,
							},
						},
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Messages: []fdbv1beta2.FoundationDBStatusMessage{
								{
									Name: "status_incomplete",
								},
								{
									Name: "unreachable_processes",
									UnreachableProcesses: []fdbv1beta2.FoundationDBUnreachableProcess{
										{
											Address: "192.168.0.1:4500:tls",
										},
									},
								},
							},
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"1": {
									ProcessClass: fdbv1beta2.ProcessClassStateless,
									Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role: string(fdbv1beta2.ProcessRoleClusterController),
										},
									},
									UptimeSeconds: 61.0,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "1",
									},
								},
								"2": {
									ProcessClass:  fdbv1beta2.ProcessClassTest,
									Address:       fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.1:4500:tls"},
									UptimeSeconds: 61.0,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "2",
									},
								},
							},
						},
					}
				})

				It("should not requeue", func() {
					Expect(err).NotTo(HaveOccurred())
					Expect(requeue).To(BeNil())
				})

				It("should kill the cluster controller", func() {
					Expect(adminClient.KilledAddresses).To(HaveKey("192.168.0.2:4500:tls"))
				})

				When("the minimum required uptime is bigger than the current uptime", func() {
					BeforeEach(func() {
						clusterReconciler.MinimumRequiredUptimeCCBounce = 2 * time.Minute
					})

					AfterEach(func() {
						clusterReconciler.MinimumRequiredUptimeCCBounce = 0
					})

					It("should not requeue", func() {
						Expect(err).NotTo(HaveOccurred())
						Expect(requeue).To(BeNil())
					})

					It("should not kill the cluster controller", func() {
						Expect(adminClient.KilledAddresses).To(BeEmpty())
					})
				})

				When("the FDB version automatically removes old tester processes", func() {
					var previousVersion string

					BeforeEach(func() {
						previousVersion = cluster.Status.RunningVersion
						cluster.Spec.Version = "7.1.57"
						cluster.Status.RunningVersion = "7.1.57"
					})

					AfterEach(func() {
						cluster.Status.RunningVersion = previousVersion
						cluster.Spec.Version = previousVersion
					})

					It("should not requeue", func() {
						Expect(err).NotTo(HaveOccurred())
						Expect(requeue).To(BeNil())
					})

					It("should not kill the cluster controller", func() {
						Expect(adminClient.KilledAddresses).To(BeEmpty())
					})
				})
			})

			When("the unreachable processes include no tester processes", func() {
				BeforeEach(func() {
					adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Messages: []fdbv1beta2.FoundationDBStatusMessage{
								{
									Name: "status_incomplete",
								},
								{
									Name: "unreachable_processes",
									UnreachableProcesses: []fdbv1beta2.FoundationDBUnreachableProcess{
										{
											Address: "192.168.0.1:4500:tls",
										},
									},
								},
							},
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"1": {
									ProcessClass: fdbv1beta2.ProcessClassStateless,
									Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role: string(fdbv1beta2.ProcessRoleClusterController),
										},
									},
								},
								"2": {
									ProcessClass: fdbv1beta2.ProcessClassStorage,
									Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.1:4500:tls"},
								},
							},
						},
					}
				})

				It("should not requeue", func() {
					Expect(err).NotTo(HaveOccurred())
					Expect(requeue).To(BeNil())
				})

				It("should not kill the cluster controller", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})
			})

			When("the cluster message does not contain unreachable processes", func() {
				BeforeEach(func() {
					adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
								Available: true,
							},
						},
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Messages: []fdbv1beta2.FoundationDBStatusMessage{
								{
									Name: "status_incomplete",
								},
							},
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"1": {
									ProcessClass: fdbv1beta2.ProcessClassStateless,
									Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role: string(fdbv1beta2.ProcessRoleClusterController),
										},
									},
								},
								"2": {
									ProcessClass: fdbv1beta2.ProcessClassTest,
									Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.1:4500:tls"},
								},
							},
						},
					}
				})

				It("should not requeue", func() {
					Expect(err).NotTo(HaveOccurred())
					Expect(requeue).To(BeNil())
				})

				It("should not kill the cluster controller", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})
			})
		})
	})

	DescribeTable("when getting all the addresses to upgrade from the current status", func(inputStatus *fdbv1beta2.FoundationDBStatus, pendingUpgrades map[fdbv1beta2.ProcessGroupID]bool, version string, expectedAddresses []fdbv1beta2.ProcessAddress, expectedNotReady []string) {
		addresses, notReady := getUpgradeAddressesFromStatus(GinkgoLogr, inputStatus, pendingUpgrades, version)
		Expect(addresses).To(ConsistOf(expectedAddresses))
		Expect(notReady).To(ConsistOf(expectedNotReady))
	},
		Entry("no tester processes are present",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
						},
					},
				},
			},
			map[fdbv1beta2.ProcessGroupID]bool{
				"1": true,
			},
			"7.1.56",
			[]fdbv1beta2.ProcessAddress{{StringAddress: "192.168.0.2:4500:tls"}},
			[]string{},
		),
		Entry("a tester process is present",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassTest,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.3:4500:tls"},
						},
					},
				},
			},
			map[fdbv1beta2.ProcessGroupID]bool{
				"1": true,
			},
			"7.1.56",
			[]fdbv1beta2.ProcessAddress{{StringAddress: "192.168.0.2:4500:tls"}},
			[]string{},
		),
		Entry("a process with missing localities is present",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassTest,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.3:4500:tls"},
						},
						"3": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality:     map[string]string{},
							Version:      "6.3.43",
							Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.4:4500:tls"},
						},
					},
				},
			},
			map[fdbv1beta2.ProcessGroupID]bool{
				"1": true,
			},
			"7.1.56",
			[]fdbv1beta2.ProcessAddress{{StringAddress: "192.168.0.2:4500:tls"}},
			[]string{},
		),
		Entry("a process which is already upgraded is present",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassTest,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.3:4500:tls"},
						},
						"3": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality:     map[string]string{},
							Version:      "6.3.43",
							Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.4:4500:tls"},
						},
						"4": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "4",
							},
							Version: "7.1.56",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.5:4500:tls"},
						},
					},
				},
			},
			map[fdbv1beta2.ProcessGroupID]bool{
				"1": true,
			},
			"7.1.56",
			[]fdbv1beta2.ProcessAddress{{StringAddress: "192.168.0.2:4500:tls"}},
			[]string{},
		),
		Entry("a process is missing in the pending upgrades map",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "1",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.2:4500:tls"},
						},
						"2": {
							ProcessClass: fdbv1beta2.ProcessClassTest,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "2",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.3:4500:tls"},
						},
						"3": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality:     map[string]string{},
							Version:      "6.3.43",
							Address:      fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.4:4500:tls"},
						},
						"4": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "4",
							},
							Version: "7.1.56",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.5:4500:tls"},
						},
						"5": {
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "5",
							},
							Version: "6.3.43",
							Address: fdbv1beta2.ProcessAddress{StringAddress: "192.168.0.6:4500:tls"},
						},
					},
				},
			},
			map[fdbv1beta2.ProcessGroupID]bool{
				"1": true,
			},
			"7.1.56",
			[]fdbv1beta2.ProcessAddress{{StringAddress: "192.168.0.2:4500:tls"}},
			[]string{"5"},
		),
	)
})
