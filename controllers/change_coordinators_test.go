/*
 * change_coordinators_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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
	"math"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Change coordinators", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
			{
				ProcessClass: fdbv1beta2.ProcessClassStorage,
				Priority:     math.MaxInt32,
			},
			{
				ProcessClass: fdbv1beta2.ProcessClassLog,
				Priority:     0,
			},
		}
		Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())
	})

	Describe("reconcile", func() {
		var requeue *requeue
		var originalConnectionString string

		BeforeEach(func() {
			originalConnectionString = cluster.Status.ConnectionString
		})

		JustBeforeEach(func() {
			requeue = changeCoordinators{}.reconcile(
				context.TODO(),
				clusterReconciler,
				cluster,
				nil,
				testLogger,
			)
		})

		When("the cluster is healthy", func() {
			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("leaves the cluster file intact", func() {
				Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
			})
		})

		When("the Pods do not have DNS names", func() {
			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not change the cluster file", func() {
				Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
			})
		})

		When("the connection string shouldn't be using DNS entries", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = ptr.To(false)
				pods := &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), pods)).NotTo(HaveOccurred())

				for _, pod := range pods.Items {
					container := pod.Spec.Containers[1]
					container.Env = append(
						container.Env,
						corev1.EnvVar{
							Name:  fdbv1beta2.EnvNameDNSName,
							Value: internal.GetPodDNSName(cluster, pod.Name),
						},
					)
					pod.Spec.Containers[1] = container
					Expect(k8sClient.Update(context.TODO(), &pod)).NotTo(HaveOccurred())
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should change the cluster file", func() {
				Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
				Expect(
					cluster.Status.ConnectionString,
				).NotTo(ContainSubstring("my-ns.svc.cluster.local"))
			})
		})

		When("one coordinator is missing localities", func() {
			var badCoordinator fdbv1beta2.FoundationDBStatusProcessInfo

			BeforeEach(func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				for _, process := range status.Cluster.Processes {
					for _, role := range process.Roles {
						if role.Role != "coordinator" {
							continue
						}

						badCoordinator = process
					}
				}

				adminClient.MockMissingLocalities(
					fdbv1beta2.ProcessGroupID(
						badCoordinator.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
					),
					true,
				)
			})

			It(
				"should change the coordinators to not include the coordinator with the missing localities",
				func() {
					Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
					Expect(
						cluster.Status.ConnectionString,
					).NotTo(ContainSubstring(badCoordinator.Address.IPAddress.String()))
				},
			)
		})

		When("safety checks are enabled", func() {
			BeforeEach(func() {
				clusterReconciler.MinimumUptimeForCoordinatorChangeWithUndesiredProcess = 5 * time.Minute
				clusterReconciler.MinimumUptimeForCoordinatorChangeWithMissingProcess = 10 * time.Minute
				clusterReconciler.EnableRecoveryState = true
			})

			When("one coordinator is undesired", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())

					coordinators := map[string]fdbv1beta2.None{}
					for _, coordinator := range status.Client.Coordinators.Coordinators {
						coordinators[coordinator.Address.String()] = fdbv1beta2.None{}
					}

					for _, process := range status.Cluster.Processes {
						if _, ok := coordinators[process.Address.String()]; !ok {
							continue
						}
						Expect(adminClient.ExcludeProcesses([]fdbv1beta2.ProcessAddress{
							{
								IPAddress: process.Address.IPAddress,
							},
						})).To(Succeed())
						break
					}
				})

				When("the cluster is up for long enough", func() {
					It("should change the coordinators", func() {
						Expect(requeue).To(BeNil())
					})
				})

				When("Too many active generations are present", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.ActiveGenerations = ptr.To(11)
					})

					AfterEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.ActiveGenerations = nil
					})

					It("should defer coordinator change and requeue with delay", func() {
						Expect(requeue).NotTo(BeNil())
						Expect(requeue.delayedRequeue).To(BeTrue())
						Expect(requeue.curError).To(HaveOccurred())
						Expect(
							requeue.curError.Error(),
						).To(ContainSubstring("cluster has 11 active generations, but only 10 active generations are allowed to safely change coordinators"))
						Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
					})
				})

				When("the cluster is only up for 10 seconds", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.SecondsSinceLastRecovered = ptr.To(10.0)
					})

					AfterEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.SecondsSinceLastRecovered = nil
					})

					It("should defer coordinator change and requeue with delay", func() {
						Expect(requeue).NotTo(BeNil())
						Expect(requeue.delayedRequeue).To(BeTrue())
						Expect(requeue.curError).To(HaveOccurred())
						Expect(
							requeue.curError.Error(),
						).To(Equal("cannot: change coordinators: cluster is not up for long enough, clusters last recovery was 10.00 seconds ago, wait until the last recovery was 300 seconds ago"))
						Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
					})
				})
			})

			When("one coordinator is missing", func() {
				BeforeEach(func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())

					coordinators := map[string]fdbv1beta2.None{}
					for _, coordinator := range status.Client.Coordinators.Coordinators {
						coordinators[coordinator.Address.String()] = fdbv1beta2.None{}
					}

					for _, process := range status.Cluster.Processes {
						if _, ok := coordinators[process.Address.String()]; !ok {
							continue
						}
						adminClient.MockMissingProcessGroup(
							fdbv1beta2.ProcessGroupID(
								process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
							),
							true,
						)
						break
					}
				})

				When("the cluster is up for long enough", func() {
					It("should change the coordinators", func() {
						Expect(requeue).To(BeNil())
					})
				})

				When("Multiple active generations are present", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.ActiveGenerations = ptr.To(11)
					})

					AfterEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.ActiveGenerations = nil
					})

					It("should change the coordinators", func() {
						Expect(requeue).To(BeNil())
					})
				})

				When("the cluster is only up for 10 seconds", func() {
					BeforeEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.SecondsSinceLastRecovered = ptr.To(10.0)
					})

					AfterEach(func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						adminClient.SecondsSinceLastRecovered = nil
					})

					It("should defer coordinator change and requeue with delay", func() {
						Expect(requeue).NotTo(BeNil())
						Expect(requeue.delayedRequeue).To(BeTrue())
						Expect(requeue.curError).To(HaveOccurred())
						Expect(
							requeue.curError.Error(),
						).To(Equal("cannot: change coordinators: cluster has 1 missing coordinators, clusters last recovery was 10.00 seconds ago, wait until the last recovery was 600 seconds ago"))
						Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
					})
				})
			})
		})
	})
})
