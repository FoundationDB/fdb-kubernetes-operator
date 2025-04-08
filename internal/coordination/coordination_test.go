/*
 * coordination_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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

package coordination

import (
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/v2/mock-kubernetes-client/client"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
)

var _ = Describe("operator_coordination", func() {
	When("updating the coordination state", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var processGroup *fdbv1beta2.ProcessGroupStatus
		var k8sClient *mockclient.MockClient

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			k8sClient = mockclient.NewMockClient(scheme.Scheme)
			Expect(internal.SetupClusterForTest(cluster, k8sClient)).To(Succeed())
			cluster.Status.Configured = true
			// Pick a random process group
			processGroups := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)
			Expect(processGroups).To(HaveLen(1))
			processGroup = processGroups[0]
			Expect(processGroup).NotTo(BeNil())
		})

		AfterEach(func() {
			mock.ClearMockAdminClients()
		})

		JustBeforeEach(func() {
			adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(UpdateGlobalCoordinationState(logr.Discard(), cluster, adminClient)).To(Succeed())
		})

		When("a process group is marked for removal", func() {
			BeforeEach(func() {
				processGroup.MarkForRemoval()
				Expect(cluster.ProcessGroupIsBeingRemoved(processGroup.ProcessGroupID)).To(BeTrue())
			})

			When("the synchronization mode is local", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeLocal))
				})

				It("shouldn't update anything", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					pendingForRemoval, err := adminClient.GetPendingForRemoval("")
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingForRemoval).To(BeEmpty())
				})
			})

			When("the synchronization mode is global", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeGlobal))
				})

				When("the process group is not already excluded", func() {
					It("should update all the mappings", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						pendingForRemoval, err := adminClient.GetPendingForRemoval("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForRemoval).To(HaveKey(processGroup.ProcessGroupID))

						pendingForExclusion, err := adminClient.GetPendingForExclusion("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForExclusion).To(HaveKey(processGroup.ProcessGroupID))

						pendingForInclusion, err := adminClient.GetPendingForInclusion("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForInclusion).To(HaveKey(processGroup.ProcessGroupID))
					})
				})

				When("the process group is already excluded", func() {
					BeforeEach(func() {
						processGroup.ExclusionSkipped = true
					})

					It("should update all the mappings", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						pendingForRemoval, err := adminClient.GetPendingForRemoval("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForRemoval).To(HaveKey(processGroup.ProcessGroupID))

						pendingForExclusion, err := adminClient.GetPendingForExclusion("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForExclusion).NotTo(HaveKey(processGroup.ProcessGroupID))

						pendingForInclusion, err := adminClient.GetPendingForInclusion("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForInclusion).To(HaveKey(processGroup.ProcessGroupID))

						readyForInclusion, err := adminClient.GetReadyForInclusion("")
						Expect(err).NotTo(HaveOccurred())
						Expect(readyForInclusion).To(HaveKey(processGroup.ProcessGroupID))
					})

					When("the process was in the pending and ready for exclusion list", func() {
						BeforeEach(func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							Expect(adminClient.UpdateReadyForExclusion(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
								processGroup.ProcessGroupID: fdbv1beta2.UpdateActionAdd,
							})).NotTo(HaveOccurred())
							Expect(adminClient.UpdatePendingForExclusion(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
								processGroup.ProcessGroupID: fdbv1beta2.UpdateActionAdd,
							})).NotTo(HaveOccurred())
						})

						It("should update remove them from the exclusions set", func() {
							adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
							Expect(err).NotTo(HaveOccurred())

							pendingForRemoval, err := adminClient.GetPendingForRemoval("")
							Expect(err).NotTo(HaveOccurred())
							Expect(pendingForRemoval).To(HaveKey(processGroup.ProcessGroupID))

							pendingForExclusion, err := adminClient.GetPendingForExclusion("")
							Expect(err).NotTo(HaveOccurred())
							Expect(pendingForExclusion).NotTo(HaveKey(processGroup.ProcessGroupID))

							readyForExclusion, err := adminClient.GetReadyForExclusion("")
							Expect(err).NotTo(HaveOccurred())
							Expect(readyForExclusion).NotTo(HaveKey(processGroup.ProcessGroupID))
						})
					})
				})
			})
		})

		When("a process group has the IncorrectCommandLine condition", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
			})

			When("the synchronization mode is local", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeLocal))
				})

				It("shouldn't update anything", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					pendingForRemoval, err := adminClient.GetPendingForRemoval("")
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingForRemoval).To(BeEmpty())
				})
			})

			When("the synchronization mode is global", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeGlobal))
				})

				When("the process group is not already added", func() {
					It("should update all the mappings", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						pendingForRestart, err := adminClient.GetPendingForRestart("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForRestart).To(HaveKey(processGroup.ProcessGroupID))
					})
				})
			})
		})

		When("a process group was restarted and the IncorrectCommandLine is removed", func() {
			BeforeEach(func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				Expect(adminClient.UpdateReadyForRestart(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
					processGroup.ProcessGroupID: fdbv1beta2.UpdateActionAdd,
				})).NotTo(HaveOccurred())
				Expect(adminClient.UpdatePendingForRestart(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
					processGroup.ProcessGroupID: fdbv1beta2.UpdateActionAdd,
				})).NotTo(HaveOccurred())
			})

			When("the synchronization mode is local", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeLocal))
				})

				It("shouldn't update anything", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					pendingForRestart, err := adminClient.GetPendingForRestart("")
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingForRestart).To(HaveKey(processGroup.ProcessGroupID))

					readyForRestart, err := adminClient.GetPendingForRestart("")
					Expect(err).NotTo(HaveOccurred())
					Expect(readyForRestart).To(HaveKey(processGroup.ProcessGroupID))
				})
			})

			When("the synchronization mode is global", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeGlobal))
				})

				When("no other entry exists", func() {
					It("should remove the entries for the process group", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						pendingForRestart, err := adminClient.GetPendingForRestart("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForRestart).To(BeEmpty())

						readyForRestart, err := adminClient.GetPendingForRestart("")
						Expect(err).NotTo(HaveOccurred())
						Expect(readyForRestart).To(BeEmpty())
					})
				})

				When("another entry exists", func() {
					var pickedProcessGroupID fdbv1beta2.ProcessGroupID

					BeforeEach(func() {
						cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeGlobal))

						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						processGroups := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStateless, 1)
						Expect(processGroups).To(HaveLen(1))
						pickedProcessGroup := processGroups[0]
						Expect(pickedProcessGroup).NotTo(BeNil())
						pickedProcessGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, true)
						pickedProcessGroupID = pickedProcessGroup.ProcessGroupID

						Expect(adminClient.UpdateReadyForRestart(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
							pickedProcessGroupID: fdbv1beta2.UpdateActionAdd,
						})).NotTo(HaveOccurred())
						Expect(adminClient.UpdatePendingForRestart(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
							pickedProcessGroupID: fdbv1beta2.UpdateActionAdd,
						})).NotTo(HaveOccurred())
					})

					It("should only remove all the entries for the process group", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						pendingForRestart, err := adminClient.GetPendingForRestart("")
						Expect(err).NotTo(HaveOccurred())
						Expect(pendingForRestart).To(HaveLen(1))
						Expect(pendingForRestart).To(HaveKey(pickedProcessGroupID))

						readyForRestart, err := adminClient.GetPendingForRestart("")
						Expect(err).NotTo(HaveOccurred())
						Expect(readyForRestart).To(HaveLen(1))
						Expect(readyForRestart).To(HaveKey(pickedProcessGroupID))
					})
				})
			})
		})

		When("a process group was removed", func() {
			BeforeEach(func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				Expect(adminClient.UpdatePendingForRemoval(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{
					processGroup.ProcessGroupID: fdbv1beta2.UpdateActionAdd,
				})).NotTo(HaveOccurred())

				beforeLen := len(cluster.Status.ProcessGroups)
				var currentIdx int
				for _, pg := range cluster.Status.ProcessGroups {
					if pg.ProcessGroupID == processGroup.ProcessGroupID {
						continue
					}

					cluster.Status.ProcessGroups[currentIdx] = pg
					currentIdx++
				}

				cluster.Status.ProcessGroups = cluster.Status.ProcessGroups[:currentIdx]
				Expect(beforeLen).To(BeNumerically(">", len(cluster.Status.ProcessGroups)))

				pendingForRemoval, err := adminClient.GetPendingForRemoval("")
				Expect(err).NotTo(HaveOccurred())
				Expect(pendingForRemoval).To(HaveKey(processGroup.ProcessGroupID))
			})

			When("the synchronization mode is local", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeLocal))
				})

				It("shouldn't update anything", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					pendingForRemoval, err := adminClient.GetPendingForRemoval("")
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingForRemoval).To(HaveKey(processGroup.ProcessGroupID))
				})
			})

			When("the synchronization mode is global", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.SynchronizationMode = pointer.String(string(fdbv1beta2.SynchronizationModeGlobal))
				})

				It("should remove the entries for the non exiting process group", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					pendingForRemoval, err := adminClient.GetPendingForRemoval("")
					Expect(err).NotTo(HaveOccurred())
					Expect(pendingForRemoval).To(BeEmpty())
				})
			})
		})
	})

	DescribeTable("validating if all processes are ready", func(pendingProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, readyProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, waitTime time.Duration, expectedErr string) {
		err := AllProcessesReady(GinkgoLogr, pendingProcessGroups, readyProcessGroups, waitTime)
		if expectedErr != "" {
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(HavePrefix(expectedErr)))
		} else {
			Expect(err).To(Succeed())
		}
	},
		Entry("no processes provided",
			nil,
			nil,
			30*time.Second,
			"",
		),
		Entry("only pending processes provided",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			nil,
			30*time.Second,
			"not all processes are ready:",
		),
		Entry("pending and ready processes are matching",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			30*time.Second,
			"",
		),
		Entry("pending and ready processes are matching but where added recently",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now(),
			},
			30*time.Second,
			"last pending process group was added:",
		),
		Entry("two pending and one ready processes are provided",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
				"storage-2": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			30*time.Second,
			"not all processes are ready:",
		),
		Entry("more processes are ready than pending",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
				"storage-2": time.Now().Add(-1 * time.Minute),
			},
			30*time.Second,
			"not all processes are ready:",
		),
	)

	DescribeTable("validating if all processes are read for exclusion", func(pendingProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, readyProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time, waitTime time.Duration, expectedErr string, expectedAllowedCount int) {
		allowed, err := AllProcessesReadyForExclusion(GinkgoLogr, pendingProcessGroups, readyProcessGroups, waitTime)
		if expectedErr != "" {
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(HavePrefix(expectedErr)))
		} else {
			Expect(err).To(Succeed())
			Expect(allowed).To(HaveLen(expectedAllowedCount))
		}
	},
		Entry("no processes provided",
			nil,
			nil,
			30*time.Second,
			"",
			0,
		),
		Entry("only pending processes provided",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			nil,
			30*time.Second,
			"not all processes are ready:",
			0,
		),
		Entry("pending and ready processes are matching",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			30*time.Second,
			"",
			1,
		),
		Entry("pending and ready processes are matching but where added recently",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now(),
			},
			30*time.Second,
			"last pending process group was added:",
			0,
		),
		Entry("two pending and one ready processes are provided",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
				"storage-2": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			30*time.Second,
			"not all processes are ready:",
			0,
		),
		Entry("more processes are ready than pending",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-1 * time.Minute),
				"storage-2": time.Now().Add(-1 * time.Minute),
			},
			30*time.Second,
			"not all processes are ready:",
			0,
		),
		Entry("two pending and one ready processes are provided and is ready for at least 5 minutes",
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-2 * IgnoreMissingProcessDuration),
				"storage-2": time.Now().Add(-2 * IgnoreMissingProcessDuration),
			},
			map[fdbv1beta2.ProcessGroupID]time.Time{
				"storage-1": time.Now().Add(-2 * IgnoreMissingProcessDuration),
			},
			30*time.Second,
			"",
			1,
		),
	)
})
