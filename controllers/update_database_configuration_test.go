/*
 * update_database_configuration_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"slices"

	"k8s.io/utils/ptr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

var _ = Describe("update_database_configuration", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var requeue *requeue

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		Expect(internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})).To(Succeed())
		Expect(k8sClient.Create(context.TODO(), cluster)).To(Succeed())
	})

	JustBeforeEach(func() {
		requeue = updateDatabaseConfiguration{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
	})

	When("the cluster is not yet configured", func() {
		When("the operator is allowed to configure the database", func() {
			It("should perform the initial configuration", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.Configured).To(BeFalse())

				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())

				// Ensure the cluster is seen as configured based on the reported status.
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(fdbstatus.ClusterIsConfigured(cluster, status)).To(BeTrue())
			})
		})

		When("the operator is not allowed to configure the database", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.ConfigureDatabase = ptr.To(false)
			})

			It("should perform the initial configuration", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.Configured).To(BeFalse())

				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient.DatabaseConfiguration).To(BeNil())
			})
		})
	})

	When("the cluster is configured", func() {
		BeforeEach(func() {
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
		})

		When("no change is required", func() {
			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.Configured).To(BeTrue())
			})
		})

		When("the redundancy mode is changed", func() {
			var initialConfiguration fdbv1beta2.DatabaseConfiguration

			BeforeEach(func() {
				initialConfiguration = cluster.Status.DatabaseConfiguration
				cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeTriple
			})

			When("the changes are allowed", func() {
				It("should update the configuration", func() {
					Expect(requeue).To(BeNil())
					Expect(cluster.Status.Configured).To(BeTrue())

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())
					Expect(
						adminClient.DatabaseConfiguration.RedundancyMode,
					).To(Equal(fdbv1beta2.RedundancyModeTriple))
					Expect(adminClient.DatabaseConfiguration).NotTo(Equal(initialConfiguration))
				})
			})

			When("the database is unavailable", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
								Available: false,
							},
						},
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							DatabaseConfiguration: *adminClient.DatabaseConfiguration,
						},
					}
				})

				It("should not update the configuration", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(
						requeue.message,
					).To(Equal("Configuration change is not safe: cluster is unavailable, cannot change configuration, will retry"))
					Expect(cluster.Status.Configured).To(BeTrue())

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())
					Expect(
						adminClient.DatabaseConfiguration.RedundancyMode,
					).To(Equal(fdbv1beta2.RedundancyModeDouble))
				})
			})

			When("the database is data state is unhealthy", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
								Available: true,
							},
						},
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							DatabaseConfiguration: *adminClient.DatabaseConfiguration,
							Data: fdbv1beta2.FoundationDBStatusDataStatistics{
								State: fdbv1beta2.FoundationDBStatusDataState{
									Healthy: false,
									Name:    "primary",
								},
							},
						},
					}
				})

				It("should not update the configuration", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(
						requeue.message,
					).To(Equal("Configuration change is not safe: data distribution is not healthy: primary, will retry"))
					Expect(cluster.Status.Configured).To(BeTrue())

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())
					Expect(
						adminClient.DatabaseConfiguration.RedundancyMode,
					).To(Equal(fdbv1beta2.RedundancyModeDouble))
				})
			})

			When(
				"the database contains a message indicating that the configuration is not readable",
				func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
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
										Name: "unreadable_configuration",
									},
								},
								DatabaseConfiguration: *adminClient.DatabaseConfiguration,
								Data: fdbv1beta2.FoundationDBStatusDataStatistics{
									State: fdbv1beta2.FoundationDBStatusDataState{
										Healthy: true,
										Name:    "primary",
									},
								},
							},
						}
					})

					It("should not update the configuration", func() {
						Expect(requeue).NotTo(BeNil())
						Expect(
							requeue.message,
						).To(Equal("Configuration change is not safe: status contains error message: unreadable_configuration, will retry"))
						Expect(cluster.Status.Configured).To(BeTrue())

						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())
						Expect(
							adminClient.DatabaseConfiguration.RedundancyMode,
						).To(Equal(fdbv1beta2.RedundancyModeDouble))
					})
				},
			)

			When("the last recovery was less than 60 seconds ago", func() {
				BeforeEach(func() {
					clusterReconciler.EnableRecoveryState = true
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
								Available: true,
							},
						},
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							RecoveryState: fdbv1beta2.RecoveryState{
								SecondsSinceLastRecovered: 0.1,
							},
							DatabaseConfiguration: *adminClient.DatabaseConfiguration,
							Data: fdbv1beta2.FoundationDBStatusDataStatistics{
								State: fdbv1beta2.FoundationDBStatusDataState{
									Healthy: true,
									Name:    "primary",
								},
							},
						},
					}
				})

				AfterEach(func() {
					clusterReconciler.EnableRecoveryState = false
				})

				It("should not update the configuration", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(
						requeue.message,
					).To(Equal("Configuration change is not safe: cannot: change configuration, clusters last recovery was 0.10 seconds ago, waiting until the last recovery was 60 seconds ago, will retry"))
					Expect(cluster.Status.Configured).To(BeTrue())

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())
					Expect(
						adminClient.DatabaseConfiguration.RedundancyMode,
					).To(Equal(fdbv1beta2.RedundancyModeDouble))
				})
			})

			When("the redundancy mode is changed to or from three_data_hall_fallback", func() {
				var adminClient *mock.AdminClient

				// setStatus freezes the status reported by the admin client, using the current database configuration.
				setStatus := func(dataIsHealthy bool, processesPerDataHall map[string]int) {
					status := fallbackStatusWithDataHalls(processesPerDataHall)
					status.Cluster.DatabaseConfiguration = *adminClient.DatabaseConfiguration
					status.Cluster.Data.State = fdbv1beta2.FoundationDBStatusDataState{
						Healthy: dataIsHealthy,
						Name:    "primary",
					}
					adminClient.FrozenStatus = status
				}

				expectMode := func(mode fdbv1beta2.RedundancyMode) {
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal(mode))
				}

				expectBlocked := func(message string) {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.message).
						To(Equal("Configuration change is not safe: " + message + ", will retry"))
					expectMode(fdbv1beta2.RedundancyModeThreeDataHallFallback)
				}

				BeforeEach(func() {
					var err error
					adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.DataHall = "dh1"
				})

				When("moving to three_data_hall_fallback", func() {
					BeforeEach(func() {
						adminClient.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall
						cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHallFallback
						// Fallback is meant for degraded clusters, so an unhealthy cluster must not block the change.
						setStatus(false, nil)
					})

					It("should skip the health checks and update the configuration", func() {
						Expect(requeue).To(BeNil())
						expectMode(fdbv1beta2.RedundancyModeThreeDataHallFallback)
					})
				})

				When("moving back to three_data_hall", func() {
					BeforeEach(func() {
						adminClient.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHallFallback
						cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall
						setStatus(true, map[string]int{"dh1": 2, "dh2": 3, "dh3": 3})
					})

					It("should update the configuration if the cluster is healthy", func() {
						Expect(requeue).To(BeNil())
						expectMode(fdbv1beta2.RedundancyModeThreeDataHall)
					})

					When("the local data hall has not the fewest processes", func() {
						BeforeEach(func() {
							setStatus(true, map[string]int{"dh1": 3, "dh2": 2, "dh3": 3})
						})

						It("should not update the configuration", func() {
							expectBlocked(
								`local data hall "dh1" is not the data hall with the fewest processes ("dh2"): map[dh1:3 dh2:2 dh3:3]`,
							)
						})
					})

					When("the data distribution is not healthy", func() {
						BeforeEach(func() {
							setStatus(false, map[string]int{"dh1": 2, "dh2": 3, "dh3": 3})
						})

						It("should not update the configuration", func() {
							expectBlocked("data distribution is not healthy: primary")
						})
					})

					When("a process group has a condition that requires a replacement", func() {
						var processGroupID fdbv1beta2.ProcessGroupID

						BeforeEach(func() {
							processGroup := cluster.Status.ProcessGroups[0]
							processGroupID = processGroup.ProcessGroupID
							processGroup.ProcessGroupConditions = append(
								processGroup.ProcessGroupConditions,
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingPod),
							)
						})

						It("should not update the configuration", func() {
							expectBlocked(
								fmt.Sprintf(
									"process groups have error conditions: %s: MissingPod",
									processGroupID,
								),
							)
						})
					})
				})
			})
		})
	})
})

var _ = Describe("exitThreeDataHallFallbackAllowed", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = &fdbv1beta2.FoundationDBCluster{
			Spec: fdbv1beta2.FoundationDBClusterSpec{DataHall: "dh1"},
		}
	})

	DescribeTable("checking the process distribution across the data halls",
		func(localDataHall string, processesPerDataHall map[string]int, expectedError string) {
			cluster.Spec.DataHall = localDataHall
			expectFallbackExitError(
				exitThreeDataHallFallbackAllowed(cluster, fallbackStatusWithDataHalls(processesPerDataHall)),
				expectedError,
			)
		},
		Entry("the local data hall has the fewest processes",
			"dh1", map[string]int{"dh1": 2, "dh2": 3, "dh3": 3}, ""),
		Entry("the local data hall has not the fewest processes",
			"dh2", map[string]int{"dh1": 2, "dh2": 3, "dh3": 3},
			`local data hall "dh2" is not the data hall with the fewest processes ("dh1")`),
		Entry("the local data hall is tied for the fewest processes and is alphabetically first",
			"dh1", map[string]int{"dh1": 3, "dh2": 3, "dh3": 5}, ""),
		Entry("the local data hall is tied for the fewest processes but is not alphabetically first",
			"dh2", map[string]int{"dh1": 3, "dh2": 3, "dh3": 5},
			`local data hall "dh2" is not the data hall with the fewest processes ("dh1")`),
		Entry("processes are only running in two data halls",
			"dh1", map[string]int{"dh1": 3, "dh2": 3},
			"expected processes in 3 data halls, got 2"),
		Entry("the local data hall is not set",
			"", map[string]int{"dh1": 2, "dh2": 3, "dh3": 3},
			"spec.dataHall is not set"),
	)

	DescribeTable("excluding processes",
		func(excludedDataHall string, excludedProcesses int, expectedError string) {
			status := fallbackStatusWithDataHalls(map[string]int{"dh1": 3, "dh2": 3, "dh3": 3})
			fallbackMarkProcessesExcluded(status, excludedDataHall, excludedProcesses)
			expectFallbackExitError(exitThreeDataHallFallbackAllowed(cluster, status), expectedError)
		},
		Entry("excluded processes are not counted",
			"dh2", 2,
			`local data hall "dh1" is not the data hall with the fewest processes ("dh2")`),
		Entry("a data hall with only excluded processes is not counted",
			"dh3", 3,
			"expected processes in 3 data halls, got 2"),
	)

	DescribeTable("checking the conditions of the process groups",
		// Every condition type is added to its own process group, named storage-<index>.
		func(conditionTypes []fdbv1beta2.ProcessGroupConditionType, expectedError types.GomegaMatcher) {
			for idx, conditionType := range conditionTypes {
				cluster.Status.ProcessGroups = append(
					cluster.Status.ProcessGroups,
					&fdbv1beta2.ProcessGroupStatus{
						ProcessGroupID: fdbv1beta2.ProcessGroupID(fmt.Sprintf("storage-%d", idx)),
						ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
							fdbv1beta2.NewProcessGroupCondition(conditionType),
						},
					},
				)
			}

			err := exitThreeDataHallFallbackAllowed(
				cluster,
				fallbackStatusWithDataHalls(map[string]int{"dh1": 2, "dh2": 3, "dh3": 3}),
			)
			if expectedError == nil {
				Expect(err).NotTo(HaveOccurred())
				return
			}

			Expect(err).To(MatchError(expectedError))
		},
		Entry("no process group has a condition", nil, nil),
		Entry("process groups only have conditions that don't require a replacement",
			[]fdbv1beta2.ProcessGroupConditionType{
				fdbv1beta2.IncorrectPodSpec, fdbv1beta2.NodeTaintDetected, fdbv1beta2.ReadyCondition,
			}, nil),
		Entry("only conditions that require a replacement are reported",
			[]fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.IncorrectPodSpec, fdbv1beta2.MissingPod},
			Equal("process groups have error conditions: storage-1: MissingPod")),
		Entry("10 conditions are reported without truncation",
			slices.Repeat([]fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.PodFailing}, 10),
			And(ContainSubstring("storage-9: PodFailing"), Not(ContainSubstring("...")))),
		Entry("more than 10 conditions are truncated and the total is reported",
			slices.Repeat([]fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.PodFailing}, 12),
			And(
				ContainSubstring("storage-9: PodFailing, ... (12 total)"),
				Not(ContainSubstring("storage-10")),
			)),
	)
})

// fallbackStatusWithDataHalls returns an available status with the given number of processes in each data hall.
func fallbackStatusWithDataHalls(processesPerDataHall map[string]int) *fdbv1beta2.FoundationDBStatus {
	processes := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{}
	for dataHall, count := range processesPerDataHall {
		for idx := range count {
			processGroupID := fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-storage-%d", dataHall, idx))
			processes[processGroupID] = fdbv1beta2.FoundationDBStatusProcessInfo{
				Locality: map[string]string{fdbv1beta2.FDBLocalityDataHallKey: dataHall},
			}
		}
	}

	return &fdbv1beta2.FoundationDBStatus{
		Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
			DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{Available: true},
		},
		Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{Processes: processes},
	}
}

// fallbackMarkProcessesExcluded marks the given number of processes in the data hall as excluded.
func fallbackMarkProcessesExcluded(status *fdbv1beta2.FoundationDBStatus, dataHall string, count int) {
	for processGroupID, process := range status.Cluster.Processes {
		if count == 0 {
			return
		}

		if process.Locality[fdbv1beta2.FDBLocalityDataHallKey] != dataHall {
			continue
		}

		process.Excluded = true
		status.Cluster.Processes[processGroupID] = process
		count--
	}
}

// expectFallbackExitError expects no error if expectedError is empty, otherwise an error containing expectedError.
func expectFallbackExitError(err error, expectedError string) {
	if expectedError == "" {
		Expect(err).NotTo(HaveOccurred())
		return
	}

	Expect(err).To(MatchError(ContainSubstring(expectedError)))
}
