/*
 * replace_failed_process_groups_test.go
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
	ctx "context"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("replace_failed_process_groups", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var result *requeue

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		cluster.Spec.AutomationOptions.Replacements.FaultDomainBasedReplacements = pointer.Bool(false)
		Expect(k8sClient.Create(ctx.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))
	})

	When("replacing pods on tainted nodes", func() {
		BeforeEach(func() {
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
				{
					Key:               pointer.String("foundationdb.org/testing"),
					DurationInSeconds: pointer.Int64(10),
				},
			}
		})

		When("a single process group is running on a tainted node", func() {
			var targetProcessGroup *fdbv1beta2.ProcessGroupStatus

			When("the process group has the NodeTaintDetected condition but not the NodeTaintReplacing", func() {
				BeforeEach(func() {
					targetProcessGroup = cluster.Status.ProcessGroups[0]
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintDetected, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintDetected, time.Now().Add(-10*time.Minute).Unix())
				})

				It("should not replace the process group", func() {
					Expect(targetProcessGroup.ProcessGroupConditions).To(HaveLen(1))
					Expect(targetProcessGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))

					Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster, nil, GinkgoLogr)).To(BeNil())
					Expect(getRemovedProcessGroupIDs(cluster)).To(BeEmpty())
				})
			})

			When("the process group has both the NodeTaintDetected condition and the NodeTaintReplacing condition", func() {
				BeforeEach(func() {
					targetProcessGroup = cluster.Status.ProcessGroups[0]
					timestamp := time.Now().Add(-10 * time.Minute).Unix()
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintDetected, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintDetected, timestamp)
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintReplacing, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintReplacing, timestamp)
				})

				It("should replace the process group", func() {
					Expect(targetProcessGroup.ProcessGroupConditions).To(HaveLen(2))
					Expect(targetProcessGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
					Expect(targetProcessGroup.ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))

					Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster, nil, GinkgoLogr)).NotTo(BeNil())
					Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf(targetProcessGroup.ProcessGroupID))
				})
			})

			When("the process group has both the NodeTaintDetected condition and the NodeTaintReplacing condition but the node taint feature is disabled", func() {
				BeforeEach(func() {
					targetProcessGroup = cluster.Status.ProcessGroups[0]
					timestamp := time.Now().Add(-10 * time.Minute).Unix()
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintDetected, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintDetected, timestamp)
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintReplacing, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintReplacing, timestamp)
					cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = nil
				})

				It("should not replace the process group", func() {
					Expect(targetProcessGroup.ProcessGroupConditions).To(HaveLen(2))
					Expect(targetProcessGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
					Expect(targetProcessGroup.ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))

					Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster, nil, GinkgoLogr)).To(BeNil())
					Expect(getRemovedProcessGroupIDs(cluster)).To(BeEmpty())
				})
			})
		})

		When("multiple process groups are running on tainted nodes", func() {
			var targetProcessGroups []fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				targetProcessGroups = make([]fdbv1beta2.ProcessGroupID, 2)
				for i := 0; i < 2; i++ {
					targetProcessGroup := cluster.Status.ProcessGroups[i]
					timestamp := time.Now().Add(-10 * time.Minute).Unix()
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintDetected, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintDetected, timestamp)
					targetProcessGroup.UpdateCondition(fdbv1beta2.NodeTaintReplacing, true)
					targetProcessGroup.UpdateConditionTime(fdbv1beta2.NodeTaintReplacing, timestamp)
					targetProcessGroups[i] = targetProcessGroup.ProcessGroupID
				}
			})

			When("only one fault domain with tainted nodes is allowed", func() {
				It("shouldn't remove any pods on tainted nodes", func() {
					Expect(cluster.Status.ProcessGroups[0].ProcessGroupConditions).To(HaveLen(2))
					Expect(cluster.Status.ProcessGroups[0].ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
					Expect(cluster.Status.ProcessGroups[0].ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))
					Expect(cluster.Status.ProcessGroups[1].ProcessGroupConditions).To(HaveLen(2))
					Expect(cluster.Status.ProcessGroups[1].ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
					Expect(cluster.Status.ProcessGroups[1].ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))

					Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster, nil, GinkgoLogr)).To(BeNil())
					Expect(getRemovedProcessGroupIDs(cluster)).To(BeEmpty())
				})
			})

			When("multiple fault domain with tainted nodes is allowed", func() {
				BeforeEach(func() {
					val := intstr.FromInt(10)
					cluster.Spec.AutomationOptions.Replacements.MaxFaultDomainsWithTaintedProcessGroups = &val
					cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(10)
					cluster.Spec.AutomationOptions.RemovalMode = fdbv1beta2.PodUpdateModeAll
				})

				It("should remove all pods on tainted nodes", func() {
					Expect(cluster.Status.ProcessGroups[0].ProcessGroupConditions).To(HaveLen(2))
					Expect(cluster.Status.ProcessGroups[0].ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
					Expect(cluster.Status.ProcessGroups[0].ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))
					Expect(cluster.Status.ProcessGroups[1].ProcessGroupConditions).To(HaveLen(2))
					Expect(cluster.Status.ProcessGroups[1].ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
					Expect(cluster.Status.ProcessGroups[1].ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))

					Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster, nil, GinkgoLogr)).NotTo(BeNil())
					Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf(targetProcessGroups))
				})
			})
		})
	})

	When("replacing failed process groups", func() {
		JustBeforeEach(func() {
			adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(adminClient).NotTo(BeNil())
			Expect(internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})).NotTo(HaveOccurred())
			result = replaceFailedProcessGroups{}.reconcile(ctx.Background(), clusterReconciler, cluster, nil, globalControllerLogger)
		})

		Context("with no missing processes", func() {
			It("should return nil",
				func() {
					Expect(result).To(BeNil())
				})

			It("should not mark anything for removal", func() {
				Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
			})
		})

		When("fault domain replacements are disabled", func() {
			var processGroup *fdbv1beta2.ProcessGroupStatus
			var secondProcessGroup *fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				processGroups := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)
				processGroup = processGroups[0]
				secondProcessGroup = processGroups[1]
			})

			Context("with a process that has been missing for a long time", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
				})

				Context("with no other removals", func() {
					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
					})

					It("should not be marked to skip exclusion", func() {
						for _, pg := range cluster.Status.ProcessGroups {
							if pg.ProcessGroupID != processGroup.ProcessGroupID {
								continue
							}

							Expect(pg.ExclusionSkipped).To(BeFalse())
						}
					})

					When("EmptyMonitorConf is set to true", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.EmptyMonitorConf = true
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for all process groups", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"*"}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for the specific process group", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for the main container", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
								{
									ContainerName: fdbv1beta2.MainContainerName,
									Targets:       []fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID},
								},
							}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for the sidecar container", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
								{
									ContainerName: fdbv1beta2.SidecarContainerName,
									Targets:       []fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID},
								},
							}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})
				})

				Context("with multiple failed processes", func() {
					BeforeEach(func() {
						secondProcessGroup.ProcessGroupConditions = append(secondProcessGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
							ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
							Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
						})
					})

					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the first process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
					})

					It("should not be marked to skip exclusion", func() {
						for _, pg := range cluster.Status.ProcessGroups {
							if pg.ProcessGroupID != processGroup.ProcessGroupID {
								continue
							}

							Expect(pg.ExclusionSkipped).To(BeFalse())
						}
					})
				})

				Context("with another in-flight exclusion", func() {
					BeforeEach(func() {
						secondProcessGroup.MarkForRemoval()
					})

					It("should not return nil", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.delayedRequeue).To(BeTrue())
						Expect(result.message).To(Equal("More failed process groups are detected"))
					})

					It("should not mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{secondProcessGroup.ProcessGroupID}))
					})

					When("max concurrent replacements is set to two", func() {
						BeforeEach(func() {
							cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(2)
						})

						It("should requeue", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID, secondProcessGroup.ProcessGroupID}))
						})
					})

					When("max concurrent replacements is set to zero", func() {
						BeforeEach(func() {
							cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(0)
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{secondProcessGroup.ProcessGroupID}))
						})
					})
				})

				Context("with another complete exclusion", func() {
					BeforeEach(func() {
						secondProcessGroup.MarkForRemoval()
						secondProcessGroup.SetExclude()
					})

					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID, secondProcessGroup.ProcessGroupID}))
					})
				})

				Context("with no addresses", func() {
					BeforeEach(func() {
						processGroup.Addresses = nil
						cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(false)
					})

					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
					})

					It("should marked to skip exclusion", func() {
						for _, pg := range cluster.Status.ProcessGroups {
							if pg.ProcessGroupID != processGroup.ProcessGroupID {
								continue
							}

							Expect(pg.ExclusionSkipped).To(BeTrue())
						}
					})

					When("the cluster is not available", func() {
						BeforeEach(func() {
							processGroup.Addresses = nil

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

						It("should return nil", func() {
							Expect(result).NotTo(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("the cluster doesn't have full fault tolerance", func() {
						BeforeEach(func() {
							processGroup.Addresses = nil
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

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("the cluster uses localities for exclusions", func() {
						BeforeEach(func() {
							processGroup.Addresses = nil
							cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions71.String()
							cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsLocalityBasedExclusions71.String()
							cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(true)
							Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())
						})

						It("should requeue", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
						})

						It("should not skip the exclusion", func() {
							for _, pg := range cluster.Status.ProcessGroups {
								if pg.ProcessGroupID != processGroup.ProcessGroupID {
									continue
								}

								Expect(pg.ExclusionSkipped).To(BeFalse())
							}
						})

					})
				})

				Context("with maintenance mode enabled", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient.SetMaintenanceZone("operator-test-1-"+string(processGroup.ProcessGroupID), 0)).NotTo(HaveOccurred())
					})

					It("should not mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(BeEmpty())
					})
				})
			})

			Context("with a process that has been missing for a brief time", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
						Timestamp:                 time.Now().Unix(),
					})
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			Context("with a process that has had an incorrect pod spec for a long time", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.IncorrectPodSpec,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("a process is not marked for removal but is excluded", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.ProcessIsMarkedAsExcluded,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
				})

				It("should return not nil",
					func() {
						Expect(result).NotTo(BeNil())
					})

				It("should mark the process group to be removed", func() {
					removedIDs := getRemovedProcessGroupIDs(cluster)
					Expect(removedIDs).To(HaveLen(1))
					Expect(removedIDs).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
				})
			})

			When("a process is marked for removal and has the ProcessIsMarkedAsExcluded condition", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.ProcessIsMarkedAsExcluded,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
					processGroup.MarkForRemoval()
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should mark the process group to be removed", func() {
					// The process group is marked as removal in the BeforeEach step.
					removedIDs := getRemovedProcessGroupIDs(cluster)
					Expect(removedIDs).To(HaveLen(1))
					Expect(removedIDs).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
				})
			})
		})

		When("fault domain replacements are enabled", func() {
			var processGroup *fdbv1beta2.ProcessGroupStatus
			var secondProcessGroup *fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				cluster.Spec.AutomationOptions.Replacements.FaultDomainBasedReplacements = pointer.Bool(true)
				Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

				processGroups := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)
				processGroup = processGroups[0]
				secondProcessGroup = processGroups[1]
			})

			Context("with a process that has been missing for a long time", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
				})

				Context("with no other removals", func() {
					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
					})

					It("should not be marked to skip exclusion", func() {
						for _, pg := range cluster.Status.ProcessGroups {
							if pg.ProcessGroupID != processGroup.ProcessGroupID {
								continue
							}

							Expect(pg.ExclusionSkipped).To(BeFalse())
						}
					})

					When("EmptyMonitorConf is set to true", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.EmptyMonitorConf = true
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for all process groups", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"*"}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for the specific process group", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for the main container", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
								{
									ContainerName: fdbv1beta2.MainContainerName,
									Targets:       []fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID},
								},
							}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("Crash loop is set for the sidecar container", func() {
						BeforeEach(func() {
							cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
								{
									ContainerName: fdbv1beta2.SidecarContainerName,
									Targets:       []fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID},
								},
							}
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})
				})

				Context("with multiple failed processes", func() {
					BeforeEach(func() {
						secondProcessGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
							ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
							Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
						})
					})

					When("those failed processes are on different fault domains", func() {
						It("should requeue", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark the first process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
						})

						It("should not be marked to skip exclusion", func() {
							for _, pg := range cluster.Status.ProcessGroups {
								if pg.ProcessGroupID != processGroup.ProcessGroupID {
									continue
								}

								Expect(pg.ExclusionSkipped).To(BeFalse())
							}
						})
					})

					When("those failed processes are on the same fault domain", func() {
						BeforeEach(func() {
							secondProcessGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
								ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
								Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
							})
							// Put the storage-3 on the same fault domain
							secondProcessGroup.FaultDomain = fdbv1beta2.FaultDomain(cluster.Name + "-" + string(processGroup.ProcessGroupID))
						})

						It("should requeue", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark both process groups for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID, secondProcessGroup.ProcessGroupID}))
						})
					})
				})

				Context("with another in-flight exclusion", func() {
					BeforeEach(func() {
						secondProcessGroup.MarkForRemoval()
					})

					It("should not return nil", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.delayedRequeue).To(BeTrue())
						Expect(result.message).To(Equal("More failed process groups are detected"))
					})

					It("should not mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{secondProcessGroup.ProcessGroupID}))
					})

					When("both processes are in the same fault domain", func() {
						BeforeEach(func() {
							// Put the storage-3 on the same fault domain
							secondProcessGroup.FaultDomain = fdbv1beta2.FaultDomain(cluster.Name + "-" + string(processGroup.ProcessGroupID))
						})

						It("should not return nil", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.delayedRequeue).To(BeFalse())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID, secondProcessGroup.ProcessGroupID}))
						})
					})

					When("max concurrent replacements is set to two", func() {
						BeforeEach(func() {
							cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(2)
						})

						It("should requeue", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID, secondProcessGroup.ProcessGroupID}))
						})
					})

					When("max concurrent replacements is set to zero", func() {
						BeforeEach(func() {
							cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(0)
						})

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{secondProcessGroup.ProcessGroupID}))
						})
					})
				})

				Context("with another complete exclusion", func() {
					BeforeEach(func() {
						secondProcessGroup.MarkForRemoval()
						secondProcessGroup.SetExclude()
					})

					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID, secondProcessGroup.ProcessGroupID}))
					})
				})

				Context("with no addresses", func() {
					BeforeEach(func() {
						processGroup.Addresses = nil
					})

					It("should requeue", func() {
						Expect(result).NotTo(BeNil())
						Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
					})

					It("should mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
					})

					It("should marked to skip exclusion", func() {
						for _, pg := range cluster.Status.ProcessGroups {
							if pg.ProcessGroupID != "storage-2" {
								continue
							}

							Expect(pg.ExclusionSkipped).To(BeTrue())
						}
					})

					When("the cluster is not available", func() {
						BeforeEach(func() {
							processGroup.Addresses = nil

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

						It("should return nil", func() {
							Expect(result).NotTo(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("the cluster doesn't have full fault tolerance", func() {
						BeforeEach(func() {
							processGroup.Addresses = nil
							cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(false)

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

						It("should return nil", func() {
							Expect(result).To(BeNil())
						})

						It("should not mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
						})
					})

					When("the cluster uses localities for exclusions", func() {
						BeforeEach(func() {
							processGroup.Addresses = nil

							cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions71.String()
							cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsLocalityBasedExclusions71.String()
							cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(true)
							Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())
						})

						It("should requeue", func() {
							Expect(result).NotTo(BeNil())
							Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
						})

						It("should mark the process group for removal", func() {
							Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
						})

						It("should not skip the exclusion", func() {
							for _, pg := range cluster.Status.ProcessGroups {
								if pg.ProcessGroupID != processGroup.ProcessGroupID {
									continue
								}

								Expect(pg.ExclusionSkipped).To(BeFalse())
							}
						})

					})
				})

				Context("with maintenance mode enabled", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient.SetMaintenanceZone("operator-test-1-"+string(processGroup.ProcessGroupID), 0)).NotTo(HaveOccurred())
					})

					It("should not mark the process group for removal", func() {
						Expect(getRemovedProcessGroupIDs(cluster)).To(BeEmpty())
					})
				})
			})

			Context("with a process that has been missing for a brief time", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
						Timestamp:                 time.Now().Unix(),
					})
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			Context("with a process that has had an incorrect pod spec for a long time", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.IncorrectPodSpec,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(ConsistOf([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("a process is not marked for removal but is excluded", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.ProcessIsMarkedAsExcluded,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
				})

				It("should return not nil",
					func() {
						Expect(result).NotTo(BeNil())
					})

				It("should mark the process group to be removed", func() {
					removedIDs := getRemovedProcessGroupIDs(cluster)
					Expect(removedIDs).To(HaveLen(1))
					Expect(removedIDs).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
				})
			})

			When("a process is marked for removal and has the ProcessIsMarkedAsExcluded condition", func() {
				BeforeEach(func() {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
						ProcessGroupConditionType: fdbv1beta2.ProcessIsMarkedAsExcluded,
						Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
					})
					processGroup.MarkForRemoval()
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should mark the process group to be removed", func() {
					// The process group is marked as removal in the BeforeEach step.
					removedIDs := getRemovedProcessGroupIDs(cluster)
					Expect(removedIDs).To(HaveLen(1))
					Expect(removedIDs).To(ConsistOf([]fdbv1beta2.ProcessGroupID{processGroup.ProcessGroupID}))
				})
			})
		})
	})
})

// getRemovedProcessGroupIDs returns a list of ids for the process groups that are marked for removal.
func getRemovedProcessGroupIDs(cluster *fdbv1beta2.FoundationDBCluster) []fdbv1beta2.ProcessGroupID {
	results := make([]fdbv1beta2.ProcessGroupID, 0)
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.IsMarkedForRemoval() {
			results = append(results, processGroupStatus.ProcessGroupID)
		}
	}

	return results
}
