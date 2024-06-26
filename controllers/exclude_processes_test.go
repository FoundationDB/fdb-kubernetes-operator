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
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"
	"k8s.io/utils/pointer"
	"net"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("exclude_processes", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var allowedExclusions int
	var ongoingExclusions int
	var missingProcesses []fdbv1beta2.ProcessGroupID
	var processClass fdbv1beta2.ProcessClass

	When("validating if processes can be excluded", func() {
		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))

			cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeSingle
		})

		AfterEach(func() {
			ongoingExclusions = 0
		})

		JustBeforeEach(func() {
			processCounts, err := cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			allowedExclusions, missingProcesses = getAllowedExclusionsAndMissingProcesses(globalControllerLogger, cluster, processClass, processCounts.Map()[processClass], ongoingExclusions, false)
		})

		When("using a small cluster", func() {
			When("using the storage process class", func() {
				BeforeEach(func() {
					processClass = fdbv1beta2.ProcessClassStorage
				})

				When("all processes are healthy", func() {
					When("no additional processes are running", func() {
						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(BeEmpty())
						})
					})

					When("no additional processes are running", func() {
						When("the redundancy mode is single", func() {
							It("should not allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 0))
								Expect(missingProcesses).To(BeEmpty())
							})
						})

						When("the redundancy mode is double", func() {
							BeforeEach(func() {
								cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeDouble
							})

							It("should not allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", cluster.DesiredFaultTolerance()))
								Expect(missingProcesses).To(BeEmpty())
							})

							When("there are failed processes", func() {
								var processGroupID fdbv1beta2.ProcessGroupID

								BeforeEach(func() {
									for idx, processGroup := range cluster.Status.ProcessGroups {
										if processGroup.ProcessClass != processClass {
											continue
										}

										processGroupID = processGroup.ProcessGroupID
										cluster.Status.ProcessGroups[idx].UpdateCondition(fdbv1beta2.MissingProcesses, true)
										cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().Add(-10 * time.Minute).Unix()
										break
									}
								})

								It("should not allow the exclusion", func() {
									Expect(allowedExclusions).To(BeZero())
									Expect(missingProcesses).To(ConsistOf(processGroupID))
								})
							})
						})

						When("the redundancy mode is triple", func() {
							BeforeEach(func() {
								cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeTriple
							})

							It("should allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", cluster.DesiredFaultTolerance()))
								Expect(missingProcesses).To(BeEmpty())
							})

							When("there are failed processes", func() {
								var processGroupID fdbv1beta2.ProcessGroupID

								BeforeEach(func() {
									for idx, processGroup := range cluster.Status.ProcessGroups {
										if processGroup.ProcessClass != processClass {
											continue
										}

										processGroupID = processGroup.ProcessGroupID
										cluster.Status.ProcessGroups[idx].UpdateCondition(fdbv1beta2.MissingProcesses, true)
										cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().Add(-10 * time.Minute).Unix()
										break
									}
								})

								It("should allow the exclusion", func() {
									Expect(allowedExclusions).To(BeNumerically("==", cluster.DesiredFaultTolerance()-1))
									Expect(missingProcesses).To(ConsistOf(processGroupID))
								})
							})
						})
					})

					When("one additional process is running", func() {
						BeforeEach(func() {
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						})

						It("should allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 1))
							Expect(missingProcesses).To(BeEmpty())
						})

						When("there is one ongoing exclusion", func() {
							BeforeEach(func() {
								ongoingExclusions = 1
							})

							It("should not allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 0))
								Expect(missingProcesses).To(BeEmpty())
							})
						})

						When("the additional process is marked for removal", func() {
							BeforeEach(func() {
								// In this case the process group is marked for removal but is counted against the valid
								// processes as the process is not yet excluded.
								cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].MarkForRemoval()
							})

							It("should allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 1))
								Expect(missingProcesses).To(BeEmpty())
							})
						})

						When("the additional process is marked for removal and is excluded", func() {
							BeforeEach(func() {
								// In this case we have no additional processes running, as the valid processes is the
								// same as the desired count of processes.
								cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].MarkForRemoval()
								cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].SetExclude()
							})

							It("should not allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 0))
								Expect(missingProcesses).To(BeEmpty())
							})
						})
					})
				})

				When("one process group is missing", func() {
					BeforeEach(func() {
						createMissingProcesses(cluster, 1, processClass)
					})

					When("no additional processes are running", func() {
						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(1))
						})
					})

					When("additional processes are running", func() {
						BeforeEach(func() {
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							// Add two process groups
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1338", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						})

						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(1))
						})

						When("there is one ongoing exclusion", func() {
							BeforeEach(func() {
								ongoingExclusions = 1
							})

							It("should not allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 0))
								Expect(missingProcesses).To(HaveLen(1))
							})
						})

						When("the missing timestamp is older than 5 minutes", func() {
							BeforeEach(func() {
								for idx, processGroup := range cluster.Status.ProcessGroups {
									timestamp := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses)
									if timestamp == nil {
										continue
									}

									cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().Add(-10 * time.Minute).Unix()
								}
							})

							It("should allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 1))
								Expect(missingProcesses).To(HaveLen(1))
							})
						})
					})
				})

				When("two process groups of a different type are missing", func() {
					BeforeEach(func() {
						createMissingProcesses(cluster, 2, fdbv1beta2.ProcessClassLog)
					})

					It("should allow the exclusion", func() {
						Expect(allowedExclusions).To(BeNumerically("==", 0))
						Expect(missingProcesses).To(BeEmpty())
					})
				})
			})
		})

		When("using a large cluster", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Storage = 20
				Expect(clusterReconciler.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			When("the storage class is used", func() {
				BeforeEach(func() {
					processClass = fdbv1beta2.ProcessClassStorage
				})

				When("two process groups are missing", func() {
					BeforeEach(func() {
						createMissingProcesses(cluster, 2, processClass)
					})

					When("no additional processes are running", func() {
						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(2))
						})
					})

					When("additional processes are running", func() {
						BeforeEach(func() {
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1338", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1339", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1340", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						})

						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(2))
						})

						When("the missing timestamps are older than 5 minutes", func() {
							BeforeEach(func() {
								for idx, processGroup := range cluster.Status.ProcessGroups {
									timestamp := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses)
									if timestamp == nil {
										continue
									}

									cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().Add(-10 * time.Minute).Unix()
								}
							})

							It("should allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 2))
								Expect(missingProcesses).To(HaveLen(2))
							})
						})
					})
				})

				When("five process groups are missing", func() {
					BeforeEach(func() {
						createMissingProcesses(cluster, 5, processClass)
					})

					It("should not allow the exclusion", func() {
						Expect(allowedExclusions).To(BeNumerically("==", 0))
						Expect(missingProcesses).To(HaveLen(5))
					})
				})
			})

			When("the log class is used", func() {
				BeforeEach(func() {
					processClass = fdbv1beta2.ProcessClassLog
				})

				When("two process groups are missing", func() {
					BeforeEach(func() {
						createMissingProcesses(cluster, 2, processClass)
					})

					When("no additional processes are running", func() {
						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(2))
						})
					})

					When("additional processes are running", func() {
						BeforeEach(func() {
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1338", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1339", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1340", processClass, nil))
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						})

						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(2))
						})

						When("the missing timestamps are older than 5 minutes", func() {
							BeforeEach(func() {
								for idx, processGroup := range cluster.Status.ProcessGroups {
									timestamp := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses)
									if timestamp == nil {
										continue
									}

									cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().Add(-10 * time.Minute).Unix()
								}
							})

							It("should allow the exclusion", func() {
								Expect(allowedExclusions).To(BeNumerically("==", 2))
								Expect(missingProcesses).To(HaveLen(2))
							})
						})
					})
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
						{ProcessGroupID: "storage-1", ProcessClass: fdbv1beta2.ProcessClassStorage, Addresses: []string{"1.1.1.1"}},
						{ProcessGroupID: "storage-2", ProcessClass: fdbv1beta2.ProcessClassStorage, Addresses: []string{"1.1.1.2"}},
						{ProcessGroupID: "storage-3", ProcessClass: fdbv1beta2.ProcessClassStorage, Addresses: []string{"1.1.1.3"}},
						{ProcessGroupID: "stateless-1", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.1.4"}},
						{ProcessGroupID: "stateless-2", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.1.5"}},
						{ProcessGroupID: "stateless-3", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.1.6"}},
						{ProcessGroupID: "stateless-4", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.1.7"}},
						{ProcessGroupID: "stateless-5", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.1.8"}},
						{ProcessGroupID: "stateless-6", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.1.9"}},
						{ProcessGroupID: "stateless-7", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.2.1"}},
						{ProcessGroupID: "stateless-8", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.2.2"}},
						{ProcessGroupID: "stateless-9", ProcessClass: fdbv1beta2.ProcessClassStateless, Addresses: []string{"1.1.2.3"}},
						{ProcessGroupID: "log-1", ProcessClass: fdbv1beta2.ProcessClassLog, Addresses: []string{"1.1.2.4"}},
						{ProcessGroupID: "log-2", ProcessClass: fdbv1beta2.ProcessClassLog, Addresses: []string{"1.1.2.5"}},
						{ProcessGroupID: "log-3", ProcessClass: fdbv1beta2.ProcessClassLog, Addresses: []string{"1.1.2.6"}},
						{ProcessGroupID: "log-4", ProcessClass: fdbv1beta2.ProcessClassLog, Addresses: []string{"1.1.2.7"}},
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
					fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
					Expect(fdbProcessesToExcludeByClass).To(HaveLen(0))
					Expect(ongoingExclusionsByClass).To(HaveLen(0))
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
					fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
					Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
					Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
					Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal("1.1.1.1"))
					Expect(ongoingExclusionsByClass).To(HaveLen(0))
				})

				When("the old IP address of this process group is already excluded", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[0]
						exclusions = append(exclusions, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(processGroup.Addresses[0])})

						processGroup.Addresses = append(processGroup.Addresses, "100.1.100.2")
					})

					It("should report the not yet excluded address of this process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal("100.1.100.2"))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
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
					fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
					Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
					Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
					Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal("1.1.1.1 1.1.1.2"))
					Expect(ongoingExclusionsByClass).To(HaveLen(0))
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

				When("the exclusion has not finished", func() {
					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal("1.1.1.1"))
						Expect(ongoingExclusionsByClass).To(HaveLen(1))
					})
				})

				When("the exclusion has finished", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[1]
						Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup.SetExclude()
						cluster.Status.ProcessGroups[1] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal("1.1.1.1"))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})
			})
		})

		Context("cluster supports locality based exclusions", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions.String()
			})

			When("there are no exclusions", func() {
				It("should not exclude anything", func() {
					fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
					Expect(fdbProcessesToExcludeByClass).To(HaveLen(0))
					Expect(ongoingExclusionsByClass).To(HaveLen(0))
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
					fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
					Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
					Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
					Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
					Expect(ongoingExclusionsByClass).To(HaveLen(0))
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
					fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
					Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
					Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
					Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
					Expect(ongoingExclusionsByClass).To(HaveLen(0))
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

				When("the exclusion has not finished", func() {
					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(2))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("the exclusion has finished", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[1]
						Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup.SetExclude()
						cluster.Status.ProcessGroups[1] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
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

				When("the exclusion has not finished", func() {
					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
						Expect(ongoingExclusionsByClass).To(HaveLen(1))
					})
				})

				When("the exclusion has finished", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[1]
						Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup.SetExclude()
						cluster.Status.ProcessGroups[1] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(exclusions, cluster)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(fdbProcessesToExcludeByClass).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage]).To(HaveLen(1))
						Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage], " ")).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})
			})
		})
	})

	DescribeTable("when getting the allowed exclusions", func(validProcesses int, desiredProcessCount int, ongoingExclusions int, faultTolerance int, expected int) {
		Expect(getAllowedExclusions(GinkgoLogr, validProcesses, desiredProcessCount, ongoingExclusions, faultTolerance)).To(BeNumerically("==", expected))
	},
		Entry("when as many valid processes are running as desired",
			10,
			10,
			0,
			2,
			2),
		Entry("when as many valid processes are running as desired but one exclusion is ongoing",
			10,
			10,
			1,
			2,
			1),
		Entry("when as many valid processes are running as desired but two exclusions are ongoing",
			10,
			10,
			2,
			2,
			0),
		Entry("when more valid processes are running than desired",
			20,
			10,
			0,
			2,
			12),
		Entry("when more valid processes are running than desired but there are 10 exclusions ongoing",
			20,
			10,
			10,
			2,
			2),
		Entry("when only 8 valid processes are running",
			8,
			10,
			0,
			2,
			0),
		Entry("when no valid processes are running",
			0,
			10,
			0,
			2,
			0),
	)

	When("running the reconciler", func() {
		var req *requeue
		var initialConnectionString string

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))

			cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeSingle
			initialConnectionString = cluster.Status.ConnectionString
		})

		JustBeforeEach(func() {
			req = excludeProcesses{}.reconcile(context.Background(), clusterReconciler, cluster, nil, GinkgoLogr)
		})

		// TODO: also add a test case with localities used
		When("a coordinator should be excluded", func() {
			BeforeEach(func() {
				adminClient, err := mock.NewMockAdminClient(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				coordinators, err := adminClient.GetCoordinatorSet()
				Expect(err).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if _, ok := coordinators[string(processGroup.ProcessGroupID)]; !ok {
						continue
					}

					processGroup.MarkForRemoval()
					break
				}
			})

			FIt("blub", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				Expect(req).To(BeNil())
				Expect(adminClient.ExcludedAddresses).To(HaveLen(1))

				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(initialConnectionString).NotTo(Equal(cluster.Status.ConnectionString))
			})
		})

		/*

		 */

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
