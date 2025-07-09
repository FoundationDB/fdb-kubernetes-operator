/*
 * update_database_configuration_test.go
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

package controllers

import (
	"context"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
				cluster.Spec.AutomationOptions.ConfigureDatabase = pointer.Bool(false)
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
			Expect(result.Requeue).To(BeFalse())

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
					).To(Equal("Configuration change is not safe: clusters last recovery was 0.10 seconds ago, wait until the last recovery was 60 seconds ago, will retry"))
					Expect(cluster.Status.Configured).To(BeTrue())

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient.DatabaseConfiguration).NotTo(BeNil())
					Expect(
						adminClient.DatabaseConfiguration.RedundancyMode,
					).To(Equal(fdbv1beta2.RedundancyModeDouble))
				})
			})
		})
	})
})
