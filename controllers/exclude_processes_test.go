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
	"strings"
	"time"

	"k8s.io/utils/ptr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/coordination"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
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
			cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(false)
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

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
			allowedExclusions, missingProcesses = getAllowedExclusionsAndMissingProcesses(
				globalControllerLogger,
				cluster,
				processClass,
				processCounts.Map()[processClass],
				ongoingExclusions,
				false,
			)
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
								Expect(
									allowedExclusions,
								).To(BeNumerically("==", cluster.DesiredFaultTolerance()))
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
										cluster.Status.ProcessGroups[idx].UpdateCondition(
											fdbv1beta2.MissingProcesses,
											true,
										)
										cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().
											Add(-10 * time.Minute).
											Unix()
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
								Expect(
									allowedExclusions,
								).To(BeNumerically("==", cluster.DesiredFaultTolerance()))
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
										cluster.Status.ProcessGroups[idx].UpdateCondition(
											fdbv1beta2.MissingProcesses,
											true,
										)
										cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().
											Add(-10 * time.Minute).
											Unix()
										break
									}
								})

								It("should allow the exclusion", func() {
									Expect(
										allowedExclusions,
									).To(BeNumerically("==", cluster.DesiredFaultTolerance()-1))
									Expect(missingProcesses).To(ConsistOf(processGroupID))
								})
							})
						})
					})

					When("one additional process is running", func() {
						BeforeEach(func() {
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil),
							)
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

						When(
							"the additional process is marked for removal and is excluded",
							func() {
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
							},
						)
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
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							// Add two process groups
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1338", processClass, nil),
							)
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
									timestamp := processGroup.GetConditionTime(
										fdbv1beta2.MissingProcesses,
									)
									if timestamp == nil {
										continue
									}

									cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().
										Add(-10 * time.Minute).
										Unix()
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
				Expect(result.RequeueAfter).To(BeZero())

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
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1338", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1339", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1340", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						})

						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(2))
						})

						When("the missing timestamps are older than 5 minutes", func() {
							BeforeEach(func() {
								for idx, processGroup := range cluster.Status.ProcessGroups {
									timestamp := processGroup.GetConditionTime(
										fdbv1beta2.MissingProcesses,
									)
									if timestamp == nil {
										continue
									}

									cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().
										Add(-10 * time.Minute).
										Unix()
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
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1337", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1338", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1339", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								fdbv1beta2.NewProcessGroupStatus("storage-1340", processClass, nil),
							)
							cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						})

						It("should not allow the exclusion", func() {
							Expect(allowedExclusions).To(BeNumerically("==", 0))
							Expect(missingProcesses).To(HaveLen(2))
						})

						When("the missing timestamps are older than 5 minutes", func() {
							BeforeEach(func() {
								for idx, processGroup := range cluster.Status.ProcessGroups {
									timestamp := processGroup.GetConditionTime(
										fdbv1beta2.MissingProcesses,
									)
									if timestamp == nil {
										continue
									}

									cluster.Status.ProcessGroups[idx].ProcessGroupConditions[0].Timestamp = time.Now().
										Add(-10 * time.Minute).
										Unix()
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

	// TODO (johscheuer) add test cases for global synchronization mode --> map[fdbv1beta2.ProcessGroupID]time.Time{}, make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction
	When("validating getProcessesToExclude", func() {
		var exclusions []fdbv1beta2.ProcessAddress

		BeforeEach(func() {
			cluster = &fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses:      []string{"1.1.1.1"},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses:      []string{"1.1.1.2"},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses:      []string{"1.1.1.3"},
						},
						{
							ProcessGroupID: "stateless-1",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.1.4"},
						},
						{
							ProcessGroupID: "stateless-2",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.1.5"},
						},
						{
							ProcessGroupID: "stateless-3",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.1.6"},
						},
						{
							ProcessGroupID: "stateless-4",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.1.7"},
						},
						{
							ProcessGroupID: "stateless-5",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.1.8"},
						},
						{
							ProcessGroupID: "stateless-6",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.1.9"},
						},
						{
							ProcessGroupID: "stateless-7",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.2.1"},
						},
						{
							ProcessGroupID: "stateless-8",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.2.2"},
						},
						{
							ProcessGroupID: "stateless-9",
							ProcessClass:   fdbv1beta2.ProcessClassStateless,
							Addresses:      []string{"1.1.2.3"},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses:      []string{"1.1.2.4"},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses:      []string{"1.1.2.5"},
						},
						{
							ProcessGroupID: "log-3",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses:      []string{"1.1.2.6"},
						},
						{
							ProcessGroupID: "log-4",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses:      []string{"1.1.2.7"},
						},
					},
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						UseLocalitiesForExclusion: ptr.To(true),
					},
				},
			}
			exclusions = []fdbv1beta2.ProcessAddress{}
		})

		When("the cluster doesn't supports locality based exclusions", func() {
			When("the synchronization mode is local", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.MinimumVersion.String()
					cluster.Spec.AutomationOptions.SynchronizationMode = ptr.To(
						string(fdbv1beta2.SynchronizationModeLocal),
					)
				})

				When("there are no exclusions", func() {
					It("should not exclude anything", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(0))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding one process", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(1))
						entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
						Expect(
							entry.processGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(
							fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
						).To(Equal("1.1.1.1"))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})

					When("the old IP address of this process group is already excluded", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[0]
							exclusions = append(
								exclusions,
								fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP(processGroup.Addresses[0]),
								},
							)

							processGroup.Addresses = append(processGroup.Addresses, "100.1.100.2")
						})

						It("should report the not yet excluded address of this process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(cluster.Status.ProcessGroups[0].ProcessGroupID))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal("100.1.100.2"))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})

				When("excluding two process", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(2))

						var addresses []fdbv1beta2.ProcessAddress
						for _, entry := range fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage] {
							Expect(
								entry.processGroupID,
							).To(Or(Equal(fdbv1beta2.ProcessGroupID("storage-1")), Equal(fdbv1beta2.ProcessGroupID("storage-2"))))
							addresses = append(addresses, entry.addresses...)
						}

						Expect(
							fdbv1beta2.ProcessAddressesString(addresses, " "),
						).To(Equal("1.1.1.1 1.1.1.2"))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding two process with one already excluded", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2

						exclusions = append(
							exclusions,
							fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP(processGroup2.Addresses[0]),
							},
						)
					})

					When("the exclusion has not finished", func() {
						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal("1.1.1.1"))
							Expect(ongoingExclusionsByClass).To(HaveLen(1))
						})
					})

					When("the exclusion has finished", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[1]
							Expect(
								processGroup.ProcessGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
							processGroup.SetExclude()
							cluster.Status.ProcessGroups[1] = processGroup
						})

						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal("1.1.1.1"))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})
			})

			When("the synchronization mode is global", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.MinimumVersion.String()
					cluster.Spec.AutomationOptions.SynchronizationMode = ptr.To(
						string(fdbv1beta2.SynchronizationModeGlobal),
					)
				})

				When("there are no exclusions", func() {
					It("should not exclude anything", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(0))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding one process", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(1))
						entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
						Expect(
							entry.processGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(
							fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
						).To(Equal("1.1.1.1"))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})

					When("the old IP address of this process group is already excluded", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[0]
							exclusions = append(
								exclusions,
								fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP(processGroup.Addresses[0]),
								},
							)

							processGroup.Addresses = append(processGroup.Addresses, "100.1.100.2")
						})

						It("should report the not yet excluded address of this process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(cluster.Status.ProcessGroups[0].ProcessGroupID))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal("100.1.100.2"))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})

				When("excluding two process", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(2))

						var addresses []fdbv1beta2.ProcessAddress
						for _, entry := range fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage] {
							Expect(
								entry.processGroupID,
							).To(Or(Equal(fdbv1beta2.ProcessGroupID("storage-1")), Equal(fdbv1beta2.ProcessGroupID("storage-2"))))
							addresses = append(addresses, entry.addresses...)
						}

						Expect(
							fdbv1beta2.ProcessAddressesString(addresses, " "),
						).To(Equal("1.1.1.1 1.1.1.2"))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding two process with one already excluded", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2

						exclusions = append(
							exclusions,
							fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP(processGroup2.Addresses[0]),
							},
						)
					})

					When("the exclusion has not finished", func() {
						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal("1.1.1.1"))
							Expect(ongoingExclusionsByClass).To(HaveLen(1))
						})
					})

					When("the exclusion has finished", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[1]
							Expect(
								processGroup.ProcessGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
							processGroup.SetExclude()
							cluster.Status.ProcessGroups[1] = processGroup
						})

						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal("1.1.1.1"))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})
			})
		})

		When("the cluster supports locality based exclusions", func() {
			When("the synchronization mode is local", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions.String()
					cluster.Spec.AutomationOptions.SynchronizationMode = ptr.To(
						string(fdbv1beta2.SynchronizationModeLocal),
					)
				})

				When("there are no exclusions", func() {
					It("should not exclude anything", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(0))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding one process", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(1))
						entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
						Expect(
							entry.processGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(
							fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
						).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding two process", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(2))
						var addresses []fdbv1beta2.ProcessAddress
						for _, entry := range fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage] {
							Expect(
								entry.processGroupID,
							).To(Or(Equal(fdbv1beta2.ProcessGroupID("storage-1")), Equal(fdbv1beta2.ProcessGroupID("storage-2"))))
							addresses = append(addresses, entry.addresses...)
						}

						Expect(
							fdbv1beta2.ProcessAddressesString(addresses, " "),
						).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding two process with one already excluded using IP", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2

						exclusions = append(
							exclusions,
							fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP(processGroup2.Addresses[0]),
							},
						)
					})

					When("the exclusion has not finished", func() {
						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(2))
							var addresses []fdbv1beta2.ProcessAddress
							for _, entry := range fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage] {
								Expect(
									entry.processGroupID,
								).To(Or(Equal(fdbv1beta2.ProcessGroupID("storage-1")), Equal(fdbv1beta2.ProcessGroupID("storage-2"))))
								addresses = append(addresses, entry.addresses...)
							}

							Expect(
								fdbv1beta2.ProcessAddressesString(addresses, " "),
							).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})

					When("the exclusion has finished", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[1]
							Expect(
								processGroup.ProcessGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
							processGroup.SetExclude()
							cluster.Status.ProcessGroups[1] = processGroup
						})

						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})

				When("excluding two process with one already excluded using locality", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2

						exclusions = append(
							exclusions,
							fdbv1beta2.ProcessAddress{
								StringAddress: processGroup2.GetExclusionString(),
							},
						)
					})

					When("the exclusion has not finished", func() {
						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
							Expect(ongoingExclusionsByClass).To(HaveLen(1))
						})
					})

					When("the exclusion has finished", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[1]
							Expect(
								processGroup.ProcessGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
							processGroup.SetExclude()
							cluster.Status.ProcessGroups[1] = processGroup
						})

						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})
			})

			When("the synchronization mode is global", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions.String()
					cluster.Spec.AutomationOptions.SynchronizationMode = ptr.To(
						string(fdbv1beta2.SynchronizationModeGlobal),
					)
				})

				When("there are no exclusions", func() {
					It("should not exclude anything", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(0))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding one process", func() {
					BeforeEach(func() {
						processGroup := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(1))
						entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
						Expect(
							entry.processGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(
							fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
						).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding two process", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2
					})

					It("should report the excluded process", func() {
						fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
							exclusions,
							cluster,
							map[fdbv1beta2.ProcessGroupID]time.Time{},
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
						)
						Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
						Expect(
							fdbProcessesToExcludeByClass,
						).To(HaveKey(fdbv1beta2.ProcessClassStorage))
						Expect(
							fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
						).To(HaveLen(2))
						var addresses []fdbv1beta2.ProcessAddress
						for _, entry := range fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage] {
							Expect(
								entry.processGroupID,
							).To(Or(Equal(fdbv1beta2.ProcessGroupID("storage-1")), Equal(fdbv1beta2.ProcessGroupID("storage-2"))))
							addresses = append(addresses, entry.addresses...)
						}

						Expect(
							fdbv1beta2.ProcessAddressesString(addresses, " "),
						).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
						Expect(ongoingExclusionsByClass).To(HaveLen(0))
					})
				})

				When("excluding two process with one already excluded using IP", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2

						exclusions = append(
							exclusions,
							fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP(processGroup2.Addresses[0]),
							},
						)
					})

					When("the exclusion has not finished", func() {
						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(2))
							var addresses []fdbv1beta2.ProcessAddress
							for _, entry := range fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage] {
								Expect(
									entry.processGroupID,
								).To(Or(Equal(fdbv1beta2.ProcessGroupID("storage-1")), Equal(fdbv1beta2.ProcessGroupID("storage-2"))))
								addresses = append(addresses, entry.addresses...)
							}

							Expect(
								fdbv1beta2.ProcessAddressesString(addresses, " "),
							).To(Equal(fmt.Sprintf("%s %s", cluster.Status.ProcessGroups[0].GetExclusionString(), cluster.Status.ProcessGroups[1].GetExclusionString())))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})

					When("the exclusion has finished", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[1]
							Expect(
								processGroup.ProcessGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
							processGroup.SetExclude()
							cluster.Status.ProcessGroups[1] = processGroup
						})

						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})

				When("excluding two process with one already excluded using locality", func() {
					BeforeEach(func() {
						processGroup1 := cluster.Status.ProcessGroups[0]
						Expect(
							processGroup1.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
						processGroup1.MarkForRemoval()
						cluster.Status.ProcessGroups[0] = processGroup1

						processGroup2 := cluster.Status.ProcessGroups[1]
						Expect(
							processGroup2.ProcessGroupID,
						).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
						processGroup2.MarkForRemoval()
						cluster.Status.ProcessGroups[1] = processGroup2

						exclusions = append(
							exclusions,
							fdbv1beta2.ProcessAddress{
								StringAddress: processGroup2.GetExclusionString(),
							},
						)
					})

					When("the exclusion has not finished", func() {
						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
							Expect(ongoingExclusionsByClass).To(HaveLen(1))
						})
					})

					When("the exclusion has finished", func() {
						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[1]
							Expect(
								processGroup.ProcessGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
							processGroup.SetExclude()
							cluster.Status.ProcessGroups[1] = processGroup
						})

						It("should report the excluded process", func() {
							fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
								exclusions,
								cluster,
								map[fdbv1beta2.ProcessGroupID]time.Time{},
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{},
							)
							Expect(fdbProcessesToExcludeByClass).To(HaveLen(1))
							Expect(
								fdbProcessesToExcludeByClass,
							).To(HaveKey(fdbv1beta2.ProcessClassStorage))
							Expect(
								fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage],
							).To(HaveLen(1))
							entry := fdbProcessesToExcludeByClass[fdbv1beta2.ProcessClassStorage][0]
							Expect(
								entry.processGroupID,
							).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
							Expect(
								fdbv1beta2.ProcessAddressesString(entry.addresses, " "),
							).To(Equal(cluster.Status.ProcessGroups[0].GetExclusionString()))
							Expect(ongoingExclusionsByClass).To(HaveLen(0))
						})
					})
				})
			})
		})
	})

	DescribeTable(
		"when getting the allowed exclusions",
		func(validProcesses int, desiredProcessCount int, ongoingExclusions int, faultTolerance int, expected int) {
			Expect(
				getAllowedExclusions(
					GinkgoLogr,
					validProcesses,
					desiredProcessCount,
					ongoingExclusions,
					faultTolerance,
				),
			).To(BeNumerically("==", expected))
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
		Entry(
			"when more valid processes are running than desired but there are 10 exclusions ongoing",
			20,
			10,
			10,
			2,
			2,
		),
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
			Expect(result.RequeueAfter).To(BeZero())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))

			cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeSingle
			initialConnectionString = cluster.Status.ConnectionString
		})

		JustBeforeEach(func() {
			req = excludeProcesses{}.reconcile(
				context.Background(),
				clusterReconciler,
				cluster,
				nil,
				GinkgoLogr,
			)
		})

		When("the synchronization mode is local", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.SynchronizationMode = ptr.To(
					string(fdbv1beta2.SynchronizationModeLocal),
				)
			})

			When("a coordinator should be excluded", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClient(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					coordinators := fdbstatus.GetCoordinatorsFromStatus(status)

					for _, processGroup := range cluster.Status.ProcessGroups {
						if _, ok := coordinators[string(processGroup.ProcessGroupID)]; !ok {
							continue
						}

						processGroup.MarkForRemoval()
						break
					}
				})

				It("should exclude the process and change the coordinators", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					Expect(req).To(BeNil())
					Expect(adminClient.ExcludedAddresses).To(HaveLen(1))

					_, err = reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(initialConnectionString).NotTo(Equal(cluster.Status.ConnectionString))
				})

				When("using localities", func() {
					BeforeEach(func() {
						cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
							true,
						)
					})

					It("should exclude the process and change the coordinators", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						Expect(req).To(BeNil())
						Expect(adminClient.ExcludedAddresses).To(HaveLen(1))

						_, err = reloadCluster(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(
							initialConnectionString,
						).NotTo(Equal(cluster.Status.ConnectionString))
					})
				})
			})

			When("transaction system processes should be excluded", func() {
				When("a stateless and a log process should be excluded", func() {
					BeforeEach(func() {
						var pickedStateless, pickedLog bool

						_, processGroupIDs, err := cluster.GetCurrentProcessGroupsAndProcessCounts()
						Expect(err).NotTo(HaveOccurred())
						cluster.Status.ProcessGroups = append(
							cluster.Status.ProcessGroups,
							fdbv1beta2.NewProcessGroupStatus(
								cluster.GetNextRandomProcessGroupID(
									fdbv1beta2.ProcessClassLog,
									processGroupIDs[fdbv1beta2.ProcessClassLog],
								),
								fdbv1beta2.ProcessClassLog,
								nil,
							),
						)
						cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil
						cluster.Status.ProcessGroups = append(
							cluster.Status.ProcessGroups,
							fdbv1beta2.NewProcessGroupStatus(
								cluster.GetNextRandomProcessGroupID(
									fdbv1beta2.ProcessClassStateless,
									processGroupIDs[fdbv1beta2.ProcessClassStateless],
								),
								fdbv1beta2.ProcessClassStateless,
								nil,
							),
						)
						cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-1].ProcessGroupConditions = nil

						markedForRemoval := make([]fdbv1beta2.ProcessGroupID, 0, 2)
						for _, processGroup := range cluster.Status.ProcessGroups {
							if !processGroup.ProcessClass.IsTransaction() ||
								processGroup.ProcessClass == fdbv1beta2.ProcessClassClusterController {
								continue
							}

							if pickedLog && pickedStateless {
								break
							}

							if !processGroup.ProcessClass.IsLogProcess() {
								if pickedStateless {
									continue
								}
								pickedStateless = true
							}

							if processGroup.ProcessClass.IsLogProcess() {
								if pickedLog {
									continue
								}
								pickedLog = true
							}

							processGroup.MarkForRemoval()
							markedForRemoval = append(markedForRemoval, processGroup.ProcessGroupID)
						}

						Expect(markedForRemoval).To(HaveLen(2))
					})

					When("no processes are missing", func() {
						It("should exclude the process", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(req).To(BeNil())
							Expect(adminClient.ExcludedAddresses).To(HaveLen(2))
						})
					})

					When("a transaction process is missing", func() {
						var missingProcessGroup *fdbv1beta2.ProcessGroupStatus

						BeforeEach(func() {
							_, processGroupIDs, err := cluster.GetCurrentProcessGroupsAndProcessCounts()
							Expect(err).NotTo(HaveOccurred())

							missingProcessGroup = fdbv1beta2.NewProcessGroupStatus(
								cluster.GetNextRandomProcessGroupID(
									fdbv1beta2.ProcessClassLog,
									processGroupIDs[fdbv1beta2.ProcessClassLog],
								),
								fdbv1beta2.ProcessClassLog,
								nil,
							)
							cluster.Status.ProcessGroups = append(
								cluster.Status.ProcessGroups,
								missingProcessGroup,
							)
							// We have to set InSimulation here to false, otherwise the MissingProcess timestamp will be ignored.
							clusterReconciler.SimulationOptions.SimulateTime = false
						})

						It("should not exclude the process", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(req).NotTo(BeNil())
							Expect(adminClient.ExcludedAddresses).To(BeEmpty())
						})

						When("the transaction process is missing for more than 10 minutes", func() {
							BeforeEach(func() {
								missingProcessGroup.ProcessGroupConditions[0].Timestamp = time.Now().
									Add(-10 * time.Minute).
									Unix()
							})

							It("should exclude the process", func() {
								adminClient, err := mock.NewMockAdminClientUncast(
									cluster,
									k8sClient,
								)
								Expect(err).NotTo(HaveOccurred())

								Expect(req).To(BeNil())
								Expect(adminClient.ExcludedAddresses).To(HaveLen(2))
							})
						})
					})
				})
			})
		})

		When("the synchronization mode is global", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.SynchronizationMode = ptr.To(
					string(fdbv1beta2.SynchronizationModeGlobal),
				)
			})

			When("a coordinator should be excluded", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClient(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					coordinators := fdbstatus.GetCoordinatorsFromStatus(status)

					for _, processGroup := range cluster.Status.ProcessGroups {
						if _, ok := coordinators[string(processGroup.ProcessGroupID)]; !ok {
							continue
						}

						processGroup.MarkForRemoval()
						break
					}
				})

				It("should exclude the process and change the coordinators", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					Expect(req).To(BeNil())
					Expect(adminClient.ExcludedAddresses).To(HaveLen(1))

					_, err = reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(initialConnectionString).NotTo(Equal(cluster.Status.ConnectionString))
				})

				When("using localities", func() {
					BeforeEach(func() {
						cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
							true,
						)
					})

					It("should exclude the process and change the coordinators", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						Expect(req).To(BeNil())
						Expect(adminClient.ExcludedAddresses).To(HaveLen(1))

						_, err = reloadCluster(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(
							initialConnectionString,
						).NotTo(Equal(cluster.Status.ConnectionString))
					})
				})
			})

			When("a process from another cluster is pending to be excluded", func() {
				otherProcessGroupID := fdbv1beta2.ProcessGroupID("another-cluster-log-1")
				var targetedProcessGroupID fdbv1beta2.ProcessGroupID

				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					Expect(
						adminClient.UpdatePendingForExclusion(
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
								otherProcessGroupID: fdbv1beta2.UpdateActionAdd,
							},
						),
					).To(Succeed())

					adminClient.MockAdditionalProcesses([]fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: otherProcessGroupID,
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"10.3.10.3:4500",
							},
						},
					})

					// Mark the first log process group to be removed.
					for idx, processGroup := range cluster.Status.ProcessGroups {
						if processGroup.ProcessClass != fdbv1beta2.ProcessClassLog {
							continue
						}

						cluster.Status.ProcessGroups[idx].MarkForRemoval()
						targetedProcessGroupID = processGroup.ProcessGroupID
						break
					}
				})

				When(
					"the process group from the other cluster is not ready to be excluded",
					func() {
						It("should not exclude the processes", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(req).NotTo(BeNil())
							Expect(
								req.curError,
							).To(MatchError(Equal("not all processes are ready: another-cluster-log-1")))
							Expect(adminClient.ExcludedAddresses).To(BeEmpty())
						})
					},
				)

				When("the process group from the other cluster is ready to be excluded", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						Expect(
							adminClient.UpdateReadyForExclusion(
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
									otherProcessGroupID: fdbv1beta2.UpdateActionAdd,
								},
							),
						).To(Succeed())
					})

					When("localities are used for exclusion", func() {
						BeforeEach(func() {
							cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
								true,
							)
						})

						It("should exclude the processes", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(req).To(BeNil())
							Expect(adminClient.ExcludedAddresses).To(HaveLen(2))
							for excludedAddress := range adminClient.ExcludedAddresses {
								Expect(
									excludedAddress,
								).To(HavePrefix(fdbv1beta2.FDBLocalityExclusionPrefix))
							}
						})
					})

					When("localities for exclusion are disabled", func() {
						BeforeEach(func() {
							cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
								false,
							)
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(
								adminClient.UpdateProcessAddresses(
									map[fdbv1beta2.ProcessGroupID][]string{
										otherProcessGroupID: {
											"192.168.0.2",
										},
										targetedProcessGroupID: {
											"192.168.0.3",
										},
									},
								),
							).To(Succeed())
						})

						It("should exclude the processes", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(req).To(BeNil())
							Expect(adminClient.ExcludedAddresses).To(HaveLen(2))
							for excludedAddress := range adminClient.ExcludedAddresses {
								Expect(
									excludedAddress,
								).NotTo(HavePrefix(fdbv1beta2.FDBLocalityExclusionPrefix))
							}
						})
					})
				})
			})

			When("multiple processes from another cluster are pending to be excluded", func() {
				firstOtherProcessGroupID := fdbv1beta2.ProcessGroupID("another-cluster-log-1")
				secondOtherProcessGroupID := fdbv1beta2.ProcessGroupID("another-cluster-log-2")
				thirdOtherProcessGroupID := fdbv1beta2.ProcessGroupID("another-cluster-log-3")
				fourthOtherProcessGroupID := fdbv1beta2.ProcessGroupID("another-cluster-log-4")

				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					adminClient.MockAdditionTimeForGlobalCoordination = time.Now().
						Add(-coordination.IgnoreMissingProcessDuration)

					Expect(
						adminClient.UpdatePendingForExclusion(
							map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
								firstOtherProcessGroupID:  fdbv1beta2.UpdateActionAdd,
								secondOtherProcessGroupID: fdbv1beta2.UpdateActionAdd,
								thirdOtherProcessGroupID:  fdbv1beta2.UpdateActionAdd,
								fourthOtherProcessGroupID: fdbv1beta2.UpdateActionAdd,
							},
						),
					).To(Succeed())

					adminClient.MockAdditionalProcesses([]fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: firstOtherProcessGroupID,
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"10.3.10.3:4500",
							},
						},
						{
							ProcessGroupID: secondOtherProcessGroupID,
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"10.3.10.4:4500",
							},
						},
						{
							ProcessGroupID: thirdOtherProcessGroupID,
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"10.3.10.5:4500",
							},
						},
						{
							ProcessGroupID: fourthOtherProcessGroupID,
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"10.3.10.6:4500",
							},
						},
					})

					// When all log processes should be removed.
					for idx, processGroup := range cluster.Status.ProcessGroups {
						if processGroup.ProcessClass != fdbv1beta2.ProcessClassLog {
							continue
						}

						cluster.Status.ProcessGroups[idx].MarkForRemoval()
					}
				})

				When(
					"the process group from the other cluster is not ready to be excluded",
					func() {
						It("should only exclude one process in the local cluster", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(req).NotTo(BeNil())
							Expect(req.delayedRequeue).To(BeTrue())
							Expect(req.delay).To(BeNumerically("==", 5*time.Minute))
							Expect(adminClient.ExcludedAddresses).To(HaveLen(1))

							for excludedAddress := range adminClient.ExcludedAddresses {
								splits := strings.Split(excludedAddress, ":")
								Expect(splits).To(HaveLen(2))
								Expect(splits[1]).NotTo(HavePrefix("another-cluster"))
							}
						})
					},
				)

				When("only one process in both dcs are ready", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						Expect(
							adminClient.UpdateReadyForExclusion(
								map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
									firstOtherProcessGroupID: fdbv1beta2.UpdateActionAdd,
								},
							),
						).To(Succeed())
					})

					It("should exclude two processes", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						Expect(req).NotTo(BeNil())
						Expect(req.delayedRequeue).To(BeTrue())
						Expect(req.delay).To(BeNumerically("==", 5*time.Minute))
						Expect(adminClient.ExcludedAddresses).To(HaveLen(2))

						for excludedAddress := range adminClient.ExcludedAddresses {
							Expect(
								excludedAddress,
							).To(HavePrefix(fdbv1beta2.FDBLocalityExclusionPrefix))
						}
					})
				})
			})
		})
	})
})

func createMissingProcesses(
	cluster *fdbv1beta2.FoundationDBCluster,
	count int,
	processClass fdbv1beta2.ProcessClass,
) {
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
