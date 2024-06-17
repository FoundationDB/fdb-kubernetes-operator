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
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

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
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
		})

		JustBeforeEach(func() {
			result = removeProcessGroups{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
		})

		When("trying to remove a coordinator", func() {
			var coordinatorIP string
			var coordinatorSet map[string]fdbv1beta2.None

			BeforeEach(func() {

				adminClient, err := mock.NewMockAdminClient(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				coordinatorSet, err = adminClient.GetCoordinatorSet()
				Expect(err).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if _, ok := coordinatorSet[string(processGroup.ProcessGroupID)]; !ok {
						continue
					}

					processGroup.MarkForRemoval()
					coordinatorIP = processGroup.Addresses[0]
					break
				}

				Expect(coordinatorIP).NotTo(BeEmpty())
			})

			It("should not remove the coordinator", func() {
				Expect(result).NotTo(BeNil())
				Expect(result.message).To(Equal("Reconciliation needs to exclude more processes"))
			})

			It("should exclude the coordinator from the process group list to remove", func() {
				remaining := map[string]bool{
					coordinatorIP: false,
				}

				allExcluded, newExclusions, processes := clusterReconciler.getProcessGroupsToRemove(globalControllerLogger, cluster, remaining, coordinatorSet)
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
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				for _, address := range removedProcessGroup.Addresses {
					adminClient.ExcludedAddresses[address] = fdbv1beta2.None{}
				}
			})

			When("the Pod is marked as isolated", func() {
				BeforeEach(func() {
					pod := &corev1.Pod{}
					Expect(k8sClient.Get(context.Background(), ctrlClient.ObjectKey{Name: removedProcessGroup.GetPodName(cluster), Namespace: cluster.Namespace}, pod)).NotTo(HaveOccurred())
					pod.Annotations[fdbv1beta2.IsolateProcessGroupAnnotation] = "true"
					Expect(k8sClient.Update(context.Background(), pod)).NotTo(HaveOccurred())
				})

				It("should not remove that process group", func() {
					Expect(result).To(BeNil())
					// Ensure resources are not deleted
					removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
					Expect(err).To(BeNil())
					Expect(removed).To(BeFalse())
					Expect(include).To(BeFalse())
				})
			})

			When("using the default setting of EnforceFullReplicationForDeletion", func() {
				When("the cluster is fully replicated", func() {
					It("should successfully remove that process group", func() {
						Expect(result).To(BeNil())
						// Ensure resources are deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
					})
				})

				When("the cluster has degraded storage fault tolerance", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.TeamTracker = []fdbv1beta2.FoundationDBStatusTeamTracker{
							{
								Primary: true,
								State: fdbv1beta2.FoundationDBStatusDataState{
									Healthy:              false,
									MinReplicasRemaining: 2,
								},
							},
						}
					})

					It("should not remove that process group", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})

				When("the cluster has degraded log fault tolerance", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.Logs = []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:              true,
								LogReplicationFactor: 3,
								LogFaultTolerance:    1,
							},
						}
					})

					It("should not remove that process group", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})

				When("the cluster is not available", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
							Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
								DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
									Available: false,
								},
							},
						}
					})

					It("should not remove the process group and should not exclude processes", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.curError).To(HaveOccurred())
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})
			})

			When("the cluster has three_data_hall redundancy", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall

				})
				When("storage have 3 replicas", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.TeamTracker = []fdbv1beta2.FoundationDBStatusTeamTracker{
							{
								Primary: true,
								State: fdbv1beta2.FoundationDBStatusDataState{
									Healthy:              true,
									MinReplicasRemaining: 3,
								},
							},
						}
					})
					It("should successfully remove that process group", func() {
						Expect(result).To(BeNil())
						// Ensure resources are deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
					})
				})
				When("storage have 2 replicas", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.TeamTracker = []fdbv1beta2.FoundationDBStatusTeamTracker{
							{
								Primary: true,
								State: fdbv1beta2.FoundationDBStatusDataState{
									Healthy:              true,
									MinReplicasRemaining: 2,
								},
							},
						}
					})
					It("should not remove the process group and should not exclude processes", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals cannot proceed because cluster has degraded fault tolerance"))
						// Ensure resources are not deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeFalse())
						Expect(include).To(BeFalse())
					})
				})
			})

			When("removing multiple process groups", func() {
				var initialCnt int
				var secondRemovedProcessGroup *fdbv1beta2.ProcessGroupStatus

				When("the removal mode is the default zone", func() {
					BeforeEach(func() {
						Expect(cluster.Spec.AutomationOptions.RemovalMode).To(BeEmpty())
						initialCnt = len(cluster.Status.ProcessGroups)
						secondRemovedProcessGroup = cluster.Status.ProcessGroups[6]
						marked, processGroup := fdbv1beta2.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, secondRemovedProcessGroup.ProcessGroupID, secondRemovedProcessGroup.ProcessClass, removedProcessGroup.Addresses[0])
						Expect(marked).To(BeTrue())
						Expect(processGroup).To(BeNil())
						// Exclude the process group
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						for _, address := range secondRemovedProcessGroup.Addresses {
							adminClient.ExcludedAddresses[address] = fdbv1beta2.None{}
						}
					})

					It("should remove only one process group", func() {
						Expect(result).To(BeNil())
						Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 1))
						// Check if resources are deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						// Check if resources are deleted
						removedSecondary, includeSecondary, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
						Expect(err).To(BeNil())
						// Make sure only one of the process groups was deleted.
						Expect(removed).NotTo(Equal(removedSecondary))
						Expect(include).NotTo(Equal(includeSecondary))
					})

					When("a process group is marked as terminating and all resources are removed it should be removed", func() {
						BeforeEach(func() {
							secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
							Expect(removeProcessGroup(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup)).NotTo(HaveOccurred())
						})

						It("should remove the process group and the terminated process group", func() {
							Expect(result).To(BeNil())
							Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
							// Ensure resources are deleted
							removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
							// Ensure resources are deleted
							removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
						})
					})

					When("a process group is marked as terminating and the resources are not removed", func() {
						BeforeEach(func() {
							secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
						})

						It("should remove the process group and the terminated process group", func() {
							Expect(result).To(BeNil())
							Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
							// Ensure resources are deleted
							removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
							// Ensure resources are deleted
							removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
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
							removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeFalse())
							Expect(include).To(BeFalse())
							// Ensure resources are deleted
							removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeFalse())
							Expect(include).To(BeFalse())
						})
					})
				})

				When("the removal mode is PodUpdateModeAll", func() {
					BeforeEach(func() {
						// To allow multiple process groups to be removed we have to use the update mode all
						cluster.Spec.AutomationOptions.RemovalMode = fdbv1beta2.PodUpdateModeAll
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())

						initialCnt = len(cluster.Status.ProcessGroups)
						secondRemovedProcessGroup = cluster.Status.ProcessGroups[6]
						marked, processGroup := fdbv1beta2.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, secondRemovedProcessGroup.ProcessGroupID, secondRemovedProcessGroup.ProcessClass, removedProcessGroup.Addresses[0])
						Expect(marked).To(BeTrue())
						Expect(processGroup).To(BeNil())
						// Exclude the process group
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						for _, address := range secondRemovedProcessGroup.Addresses {
							adminClient.ExcludedAddresses[address] = fdbv1beta2.None{}
						}
					})

					It("should remove two process groups", func() {
						Expect(result).To(BeNil())
						Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
						// Ensure resources are deleted
						removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
						// Ensure resources are deleted as the RemovalMode is PodUpdateModeAll
						removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
						Expect(err).To(BeNil())
						Expect(removed).To(BeTrue())
						Expect(include).To(BeTrue())
					})

					When("a process group is marked as terminating and all resources are removed it should be removed", func() {
						BeforeEach(func() {
							secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
							Expect(removeProcessGroup(context.Background(), clusterReconciler, cluster, secondRemovedProcessGroup)).NotTo(HaveOccurred())
						})

						It("should remove the process group and the terminated process group", func() {
							Expect(result).To(BeNil())
							Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
							// Ensure resources are deleted
							removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
							// Ensure resources are deleted
							removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
						})
					})

					When("a process group is marked as terminating and the resources are not removed", func() {
						BeforeEach(func() {
							secondRemovedProcessGroup.ProcessGroupConditions = append(secondRemovedProcessGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating))
						})

						It("should remove the process group and the terminated process group", func() {
							Expect(result).To(BeNil())
							Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
							// Ensure resources are deleted
							removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
							// Ensure resources are deleted
							removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
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

						It("should remove all process groups", func() {
							Expect(result).To(BeNil())
							Expect(initialCnt - len(cluster.Status.ProcessGroups)).To(BeNumerically("==", 2))
							// Ensure resources are not deleted
							removed, include, err := confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, removedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
							// Ensure resources are deleted
							removed, include, err = confirmRemoval(context.Background(), globalControllerLogger, clusterReconciler, cluster, secondRemovedProcessGroup)
							Expect(err).To(BeNil())
							Expect(removed).To(BeTrue())
							Expect(include).To(BeTrue())
						})
					})
				})
			})
		})

		AfterEach(func() {
			k8sClient.Clear()
		})
	})

	Context("validating getProcessesToInclude", func() {
		var removedProcessGroups map[fdbv1beta2.ProcessGroupID]bool
		var status *fdbv1beta2.FoundationDBStatus
		var err error
		var adminClient *mock.AdminClient

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
						{ProcessGroupID: "globalControllerLogger-1", ProcessClass: "globalControllerLogger", Addresses: []string{"1.1.2.4"}},
						{ProcessGroupID: "globalControllerLogger-2", ProcessClass: "globalControllerLogger", Addresses: []string{"1.1.2.5"}},
						{ProcessGroupID: "globalControllerLogger-3", ProcessClass: "globalControllerLogger", Addresses: []string{"1.1.2.6"}},
						{ProcessGroupID: "globalControllerLogger-4", ProcessClass: "globalControllerLogger", Addresses: []string{"1.1.2.7"}},
					},
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						UseLocalitiesForExclusion: pointer.Bool(true),
					},
				},
			}
			removedProcessGroups = make(map[fdbv1beta2.ProcessGroupID]bool)
			adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("cluster doesn't support inclusions using locality", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.Default.String()
			})

			When("including no process", func() {
				It("should not include any process", func() {
					processesToInclude, err := getProcessesToInclude(logr.Logger{}, cluster, removedProcessGroups, status)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(processesToInclude)).To(Equal(0))
					Expect(len(cluster.Status.ProcessGroups)).To(Equal(16))
				})
			})

			When("including one process", func() {
				BeforeEach(func() {
					processGroup := cluster.Status.ProcessGroups[0]
					Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					processGroup.MarkForRemoval()
					for _, address := range processGroup.Addresses {
						adminClient.ExcludedAddresses[address] = fdbv1beta2.None{}
					}
					removedProcessGroups[processGroup.ProcessGroupID] = true
				})

				It("should include one process", func() {
					fdbProcessesToInclude, err := getProcessesToInclude(logr.Logger{}, cluster, removedProcessGroups, status)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(fdbProcessesToInclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToInclude, " ")).To(Equal("1.1.1.1"))
					Expect(len(cluster.Status.ProcessGroups)).To(Equal(15))
				})
			})
		})

		Context("cluster support inclusions using locality", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions.String()
			})

			When("including no process", func() {
				It("should not include any process", func() {
					fdbProcessesToInclude, err := getProcessesToInclude(logr.Logger{}, cluster, removedProcessGroups, status)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(fdbProcessesToInclude)).To(Equal(0))
					Expect(len(cluster.Status.ProcessGroups)).To(Equal(16))
				})
			})

			When("including one process", func() {
				var removedProcessGroup *fdbv1beta2.ProcessGroupStatus

				BeforeEach(func() {
					removedProcessGroup = cluster.Status.ProcessGroups[0]
					Expect(removedProcessGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					removedProcessGroup.MarkForRemoval()
					adminClient.ExcludedAddresses[removedProcessGroup.GetExclusionString()] = fdbv1beta2.None{}
					removedProcessGroups[removedProcessGroup.ProcessGroupID] = true
				})

				It("should include one process", func() {
					fdbProcessesToInclude, err := getProcessesToInclude(logr.Logger{}, cluster, removedProcessGroups, status)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(fdbProcessesToInclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToInclude, " ")).To(Equal(removedProcessGroup.GetExclusionString()))
					Expect(len(cluster.Status.ProcessGroups)).To(Equal(15))
				})
			})

			When("including a process which is excluded both by IP and locality", func() {
				var removedProcessGroup *fdbv1beta2.ProcessGroupStatus

				BeforeEach(func() {
					removedProcessGroup = cluster.Status.ProcessGroups[0]
					Expect(removedProcessGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					removedProcessGroup.MarkForRemoval()

					adminClient.ExcludedAddresses[removedProcessGroup.GetExclusionString()] = fdbv1beta2.None{}
					adminClient.ExcludedAddresses[removedProcessGroup.Addresses[0]] = fdbv1beta2.None{}
					removedProcessGroups[removedProcessGroup.ProcessGroupID] = true
				})

				It("should include one process", func() {
					fdbProcessesToInclude, err := getProcessesToInclude(logr.Logger{}, cluster, removedProcessGroups, status)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(fdbProcessesToInclude)).To(Equal(2))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToInclude, " ")).To(Equal(fmt.Sprintf("%s %s", removedProcessGroup.GetExclusionString(), removedProcessGroup.Addresses[0])))
					Expect(len(cluster.Status.ProcessGroups)).To(Equal(15))
				})
			})

			When("one excluded process is missing from excluded servers", func() {
				var removedProcessGroup *fdbv1beta2.ProcessGroupStatus
				var removedProcessGroup2 *fdbv1beta2.ProcessGroupStatus

				BeforeEach(func() {
					removedProcessGroup = cluster.Status.ProcessGroups[0]
					Expect(removedProcessGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
					removedProcessGroup.MarkForRemoval()
					removedProcessGroups[removedProcessGroup.ProcessGroupID] = true

					removedProcessGroup2 = cluster.Status.ProcessGroups[1]
					Expect(removedProcessGroup2.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-2")))
					removedProcessGroup2.MarkForRemoval()
					adminClient.ExcludedAddresses[removedProcessGroup2.GetExclusionString()] = fdbv1beta2.None{}
					removedProcessGroups[removedProcessGroup2.ProcessGroupID] = true
				})

				It("should include one process", func() {
					fdbProcessesToInclude, err := getProcessesToInclude(logr.Logger{}, cluster, removedProcessGroups, status)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(fdbProcessesToInclude)).To(Equal(1))
					Expect(fdbv1beta2.ProcessAddressesString(fdbProcessesToInclude, " ")).To(Equal(removedProcessGroup2.GetExclusionString()))
					Expect(len(cluster.Status.ProcessGroups)).To(Equal(14))
				})
			})
		})
	})
})
