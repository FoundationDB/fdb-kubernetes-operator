/*
 * admin_client_mock_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2022 Apple Inc. and the FoundationDB project authors
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

package mock

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/removals"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("mock_client", func() {
	When("checking if it's safe to delete a process group", func() {
		type testCase struct {
			cluster    *fdbv1beta2.FoundationDBCluster
			exclusions []fdbv1beta2.ProcessAddress
			remaining  map[string]bool
		}

		DescribeTable("should return the correct processes that cannot be safely removed",
			func(input testCase) {
				Expect(k8sClient.Create(context.TODO(), input.cluster)).NotTo(HaveOccurred())

				for _, processGroup := range input.cluster.Status.ProcessGroups {
					pAddr, err := fdbv1beta2.ParseProcessAddress(processGroup.Addresses[0])
					Expect(err).NotTo(HaveOccurred())
					Expect(k8sClient.Create(context.TODO(), &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      processGroup.GetPodName(input.cluster),
							Namespace: input.cluster.GetNamespace(),
							Labels: map[string]string{
								fdbv1beta2.FDBClusterLabel:        input.cluster.Name,
								fdbv1beta2.FDBProcessGroupIDLabel: string(processGroup.ProcessGroupID),
								fdbv1beta2.FDBProcessClassLabel:   string(processGroup.ProcessClass),
							},
						},
						Status: corev1.PodStatus{
							PodIP: pAddr.MachineAddress(),
						},
					})).To(Succeed())

				}

				admin, err := NewMockAdminClient(input.cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(admin.ExcludeProcesses(input.exclusions)).To(Succeed())
				status, err := admin.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				remaining, err := removals.GetRemainingMap(GinkgoLogr, admin, input.cluster, status, 0)
				Expect(err).NotTo(HaveOccurred())

				Expect(remaining).To(Equal(input.remaining))
			},
			Entry("Empty list of removals",
				testCase{
					cluster: &fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "dev-1",
						},
						Spec: fdbv1beta2.FoundationDBClusterSpec{
							Version: fdbv1beta2.Versions.Default.String(),
						},
						Status: fdbv1beta2.FoundationDBClusterStatus{
							RequiredAddresses: fdbv1beta2.RequiredAddressSet{
								NonTLS: true,
							},
						},
					},
					exclusions: nil,
					remaining:  nil,
				}),
			Entry("Process group that skips exclusion",
				testCase{
					cluster: &fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "dev-1",
						},
						Spec: fdbv1beta2.FoundationDBClusterSpec{
							Version: fdbv1beta2.Versions.Default.String(),
						},
						Status: fdbv1beta2.FoundationDBClusterStatus{
							ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
								{
									ProcessGroupID: "storage-1",
									ProcessClass:   fdbv1beta2.ProcessClassStorage,
									Addresses: []string{
										"1.1.1.1:4500",
									},
									ExclusionSkipped: true,
									RemovalTimestamp: &metav1.Time{Time: time.Now()},
								},
								{
									ProcessGroupID: "storage-2",
									ProcessClass:   fdbv1beta2.ProcessClassStorage,
									Addresses: []string{
										"1.1.1.2:4500",
									},
									RemovalTimestamp: &metav1.Time{Time: time.Now()},
								},
							},
							RequiredAddresses: fdbv1beta2.RequiredAddressSet{
								NonTLS: true,
							},
						},
					},
					exclusions: []fdbv1beta2.ProcessAddress{},
					remaining: map[string]bool{
						"locality_instance_id:storage-2": true,
					},
				}),
			Entry("Process group that is excluded by the client",
				testCase{
					cluster: &fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "dev-1",
						},
						Spec: fdbv1beta2.FoundationDBClusterSpec{
							Version: fdbv1beta2.Versions.Default.String(),
						},
						Status: fdbv1beta2.FoundationDBClusterStatus{
							ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
								{
									ProcessGroupID: "storage-1",
									ProcessClass:   fdbv1beta2.ProcessClassStorage,
									Addresses: []string{
										"1.1.1.1:4500",
									},
									RemovalTimestamp: &metav1.Time{Time: time.Now()},
								},
								{
									ProcessGroupID: "storage-2",
									ProcessClass:   fdbv1beta2.ProcessClassStorage,
									Addresses: []string{
										"1.1.1.2:4500",
									},
									RemovalTimestamp: &metav1.Time{Time: time.Now()},
								},
							},
							RequiredAddresses: fdbv1beta2.RequiredAddressSet{
								NonTLS: true,
							},
						},
					},
					exclusions: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.1"),
						},
					},
					remaining: map[string]bool{
						"locality_instance_id:storage-1": false, // Already excluded
						"locality_instance_id:storage-2": true,
					},
				}),
		)
	})

	When("changing the commandline arguments", func() {
		var adminClient *AdminClient
		var initialCommandline string
		var processAddress fdbv1beta2.ProcessAddress
		targetProcess := "storage-1"
		newKnob := "--knob_dummy=1"

		BeforeEach(func() {
			cluster := internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			storagePod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetProcess,
					Namespace: cluster.GetNamespace(),
					Labels: map[string]string{
						fdbv1beta2.FDBClusterLabel:        cluster.Name,
						fdbv1beta2.FDBProcessGroupIDLabel: targetProcess,
						fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					},
				},
			}
			Expect(k8sClient.Create(context.TODO(), storagePod)).NotTo(HaveOccurred())

			var err error
			adminClient, err = NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			status, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())

			initialCommandline = getCommandlineForProcessFromStatus(status, targetProcess)
			Expect(initialCommandline).NotTo(BeEmpty())

			for _, process := range status.Cluster.Processes {
				if process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey] != targetProcess {
					continue
				}

				processAddress = process.Address
				break
			}

			// Update the knobs for storage
			processes := cluster.Spec.Processes
			if processes == nil {
				processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{}
			}
			config := processes[fdbv1beta2.ProcessClassGeneral]
			config.CustomParameters = append(config.CustomParameters, fdbv1beta2.FoundationDBCustomParameter(newKnob))
			processes[fdbv1beta2.ProcessClassGeneral] = config
			cluster.Spec.Processes = processes
			adminClient.Cluster = cluster
		})

		When("the process is not restarted", func() {
			It("should not update the command line arguments", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				newCommandline := getCommandlineForProcessFromStatus(status, targetProcess)
				Expect(newCommandline).To(Equal(initialCommandline))
				Expect(newCommandline).NotTo(ContainSubstring(newKnob))
			})
		})

		When("the process is restarted", func() {
			It("should update the command line arguments", func() {
				Expect(adminClient.KillProcesses([]fdbv1beta2.ProcessAddress{processAddress})).NotTo(HaveOccurred())
				Expect(adminClient.KilledAddresses).To(HaveLen(1))
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				newCommandline := getCommandlineForProcessFromStatus(status, targetProcess)
				Expect(newCommandline).NotTo(Equal(initialCommandline))
				Expect(newCommandline).To(ContainSubstring(newKnob))
			})
		})
	})

	When("an error is mocked", func() {
		var adminClient *AdminClient
		var status *fdbv1beta2.FoundationDBStatus
		var err error

		BeforeEach(func() {
			cluster := internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			adminClient, err = NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			adminClient.MockError(fmt.Errorf("mocked"))
			status, err = adminClient.GetStatus()
		})

		It("should return an error", func() {
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})
	})

	When("adding additional processes", func() {
		var adminClient *AdminClient
		var status *fdbv1beta2.FoundationDBStatus

		BeforeEach(func() {
			cluster := internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			var err error
			adminClient, err = NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			adminClient.MockAdditionalProcesses([]fdbv1beta2.ProcessGroupStatus{{
				ProcessGroupID: "dc2-storage-1",
				ProcessClass:   "storage",
				Addresses:      []string{"1.2.3.4"},
			}})

			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		It("should add the additional process once", func() {
			Expect(status.Cluster.Processes).To(HaveKey(fdbv1beta2.ProcessGroupID("dc2-storage-1")))
		})
	})

	When("getting the status", func() {
		var adminClient *AdminClient
		var status *fdbv1beta2.FoundationDBStatus

		BeforeEach(func() {
			cluster := internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			var err error
			adminClient, err = NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		It("should add team tracker information", func() {
			Expect(status.Cluster.Data.TeamTrackers).To(HaveLen(1))
		})

		It("should add logs information", func() {
			Expect(status.Cluster.Logs).To(HaveLen(1))
		})
	})

	When("working with the multi-region coordination methods", func() {
		var adminClient *AdminClient
		var cluster *fdbv1beta2.FoundationDBCluster

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()

			var err error
			adminClient, err = NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
		})

		When("updating the pending for removal", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdatePendingForRemoval(updates)).NotTo(HaveOccurred())
			})

			When("adding a pending for removal", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForRemoval).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForRemoval).To(HaveLen(1))
						Expect(adminClient.pendingForRemoval).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a pending for removal", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("shouldn't change the map", func() {
						Expect(adminClient.pendingForRemoval).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.pendingForRemoval).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.pendingForRemoval = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should not remove the process group from the map", func() {
							Expect(adminClient.pendingForRemoval).To(BeEmpty())
						})
					})
				})
			})
		})

		When("updating the pending for exclusion", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdatePendingForExclusion(updates)).NotTo(HaveOccurred())
			})

			When("adding a pending for exclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForExclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForExclusion).To(HaveLen(1))
						Expect(adminClient.pendingForExclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a pending for exclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("shouldn't change the map", func() {
						Expect(adminClient.pendingForExclusion).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.pendingForExclusion).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.pendingForExclusion = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should not remove the process group from the map", func() {
							Expect(adminClient.pendingForExclusion).To(BeEmpty())
						})
					})
				})
			})
		})

		When("updating the pending for inclusion", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdatePendingForInclusion(updates)).NotTo(HaveOccurred())
			})

			When("adding a pending for inclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForInclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForInclusion).To(HaveLen(1))
						Expect(adminClient.pendingForInclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a pending for inclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("shouldn't change the map", func() {
						Expect(adminClient.pendingForRemoval).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.pendingForInclusion).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.pendingForInclusion = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should not remove the process group from the map", func() {
							Expect(adminClient.pendingForInclusion).To(BeEmpty())
						})
					})
				})
			})
		})

		When("updating the pending for restart", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdatePendingForRestart(updates)).NotTo(HaveOccurred())
			})

			When("adding a pending for restart", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForRestart).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.pendingForRestart).To(HaveLen(1))
						Expect(adminClient.pendingForRestart).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a pending for restart", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("shouldn't change the map", func() {
						Expect(adminClient.pendingForRestart).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.pendingForRestart).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.pendingForRestart = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should not remove the process group from the map", func() {
							Expect(adminClient.pendingForRestart).To(BeEmpty())
						})
					})
				})
			})
		})

		When("updating the ready for exclusion", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdateReadyForExclusion(updates)).NotTo(HaveOccurred())
			})

			When("adding a ready for exclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.readyForExclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.readyForExclusion).To(HaveLen(1))
						Expect(adminClient.readyForExclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a ready for exclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("shouldn't change the map", func() {
						Expect(adminClient.readyForExclusion).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.readyForExclusion).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.readyForExclusion = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should remove the process group from the map", func() {
							Expect(adminClient.readyForExclusion).To(BeEmpty())
						})
					})
				})
			})
		})

		When("updating the ready for inclusion", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdateReadyForInclusion(updates)).NotTo(HaveOccurred())
			})

			When("adding a ready for inclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.readyForInclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.readyForInclusion).To(HaveLen(1))
						Expect(adminClient.readyForInclusion).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a ready for inclusion", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("shouldn't change map", func() {
						Expect(adminClient.readyForInclusion).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.readyForInclusion).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.readyForInclusion = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should not remove the process group from the map", func() {
							Expect(adminClient.readyForInclusion).To(BeEmpty())
						})
					})
				})
			})
		})

		When("updating the ready for restart", func() {
			var updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction

			JustBeforeEach(func() {
				Expect(adminClient.UpdateReadyForRestart(updates)).NotTo(HaveOccurred())
			})

			When("adding a ready for restart", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionAdd}
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.readyForRestart).To(HaveKey(fdbv1beta2.ProcessGroupID("doesntExist")))
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionAdd}
					})

					BeforeEach(func() {
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					It("should add the process group to the map", func() {
						Expect(adminClient.readyForRestart).To(HaveLen(1))
						Expect(adminClient.readyForRestart).To(HaveKey(fdbv1beta2.ProcessGroupID("storage-1")))
					})
				})
			})

			When("removing a ready for restart", func() {
				When("the process group doesn't exist", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"doesntExist": fdbv1beta2.UpdateActionDelete}
					})

					It("should not add the process group to the map", func() {
						Expect(adminClient.readyForRestart).To(BeEmpty())
					})
				})

				When("the process group exists", func() {
					BeforeEach(func() {
						updates = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{"storage-1": fdbv1beta2.UpdateActionDelete}
						cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}))
						adminClient.Cluster = cluster
					})

					When("the process group is not in the map", func() {
						It("should not add the process group to the map", func() {
							Expect(adminClient.readyForRestart).To(BeEmpty())
						})
					})

					When("the process group is in the map", func() {
						BeforeEach(func() {
							adminClient.readyForRestart = map[fdbv1beta2.ProcessGroupID]time.Time{"storage-1": {}}
						})

						It("should not remove the process group from the map", func() {
							Expect(adminClient.readyForRestart).To(BeEmpty())
						})
					})
				})
			})
		})

		When("getting the pending for removal", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetPendingForRemoval(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no pending for removal exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two pending for removal exists", func() {
				BeforeEach(func() {
					adminClient.pendingForRemoval = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})

		When("getting the pending for exclusion", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetPendingForExclusion(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no pending for exclusion exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two pending for exclusion exists", func() {
				BeforeEach(func() {
					adminClient.pendingForExclusion = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})

		When("getting the pending for inclusion", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetPendingForInclusion(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no pending for inclusion exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two pending for inclusion exists", func() {
				BeforeEach(func() {
					adminClient.pendingForInclusion = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})

		When("getting the pending for restart", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetPendingForRestart(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no pending for restart exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two pending for restart exists", func() {
				BeforeEach(func() {
					adminClient.pendingForRestart = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})

		When("getting the ready for exclusion", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetReadyForExclusion(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no ready for exclusion exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two ready for exclusion exists", func() {
				BeforeEach(func() {
					adminClient.readyForExclusion = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
					adminClient.Cluster.Status.ProcessGroups = append(adminClient.Cluster.Status.ProcessGroups,
						fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}),
						fdbv1beta2.NewProcessGroupStatus("prefix-storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.3"}),
					)
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})

		When("getting the ready for inclusion", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetReadyForInclusion(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no ready for inclusion exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two ready for inclusion exists", func() {
				BeforeEach(func() {
					adminClient.readyForInclusion = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
					adminClient.Cluster.Status.ProcessGroups = append(adminClient.Cluster.Status.ProcessGroups,
						fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}),
						fdbv1beta2.NewProcessGroupStatus("prefix-storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.3"}),
					)
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})

		When("getting the ready for restart", func() {
			var result map[fdbv1beta2.ProcessGroupID]time.Time
			var prefix string

			JustBeforeEach(func() {
				var err error
				result, err = adminClient.GetReadyForRestart(prefix)
				Expect(err).NotTo(HaveOccurred())
			})

			When("no ready for restart exists", func() {
				It("should return an empty map", func() {
					Expect(result).To(BeEmpty())
				})
			})

			When("two ready for restart exists", func() {
				BeforeEach(func() {
					adminClient.readyForRestart = map[fdbv1beta2.ProcessGroupID]time.Time{
						"storage-1":        {},
						"prefix-storage-1": {},
					}
					adminClient.Cluster.Status.ProcessGroups = append(adminClient.Cluster.Status.ProcessGroups,
						fdbv1beta2.NewProcessGroupStatus("storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.2"}),
						fdbv1beta2.NewProcessGroupStatus("prefix-storage-1", fdbv1beta2.ProcessClassStorage, []string{"192.168.0.3"}),
					)
				})

				When("no prefix is specified", func() {
					It("should return all entries", func() {
						Expect(result).To(HaveLen(2))
					})
				})

				When("a prefix is specified", func() {
					BeforeEach(func() {
						prefix = "prefix"
					})

					It("should return all matching entries", func() {
						Expect(result).To(HaveLen(1))
						Expect(result).To(HaveKey(fdbv1beta2.ProcessGroupID("prefix-storage-1")))
					})
				})
			})
		})
	})
})

func getCommandlineForProcessFromStatus(status *fdbv1beta2.FoundationDBStatus, targetProcess string) string {
	for _, process := range status.Cluster.Processes {
		if process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey] != targetProcess {
			continue
		}

		return process.CommandLine
	}

	return ""
}
