/*
 * maintenance_mode_checker_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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
	"time"

	"k8s.io/utils/ptr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

var _ = Describe("maintenance_mode_checker", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var adminClient *mock.AdminClient
	var targetProcessGroup fdbv1beta2.ProcessGroupID
	var secondStorageProcess fdbv1beta2.ProcessGroupID
	var currentMaintenanceZone fdbv1beta2.FaultDomain

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = ptr.To(
			true,
		)
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		processGroups := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)
		targetProcessGroup = processGroups[0].ProcessGroupID
		currentMaintenanceZone = processGroups[0].FaultDomain
		secondStorageProcess = processGroups[1].ProcessGroupID
	})

	JustBeforeEach(func() {
		requeue = maintenanceModeChecker{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	When("no maintenance is ongoing", func() {
		When("no processes are in the maintenance list", func() {
			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})
		})

		When(
			"one processes is in the maintenance list with a more recent timestamp than the wait duration defines.",
			func() {
				BeforeEach(func() {
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{targetProcessGroup},
							time.Now().Unix(),
						),
					).NotTo(HaveOccurred())
				})

				It("shouldn't requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("shouldn't clear the entry", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(HaveLen(1))
					Expect(processesUnderMaintenance).To(HaveKey(targetProcessGroup))
				})
			},
		)

		When("one stale processes is in the maintenance list", func() {
			BeforeEach(func() {
				Expect(
					adminClient.SetProcessesUnderMaintenance(
						[]fdbv1beta2.ProcessGroupID{targetProcessGroup},
						time.Now().Add(-12*time.Hour).Unix(),
					),
				).NotTo(HaveOccurred())
			})

			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should clear the entry", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(BeEmpty())
			})
		})
	})

	When("the maintenance mode is active", func() {
		BeforeEach(func() {
			Expect(currentMaintenanceZone).NotTo(BeEmpty())
			Expect(
				adminClient.SetMaintenanceZone(string(currentMaintenanceZone), 3600),
			).NotTo(HaveOccurred())
		})

		When("no processes are in the maintenance list", func() {
			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})

			// We expect that the maintenance zone will be reset in this case as no processes are reported to be under maintenance.
			// We could hit such a case when the operator was able to remove the finished processes from the maintenance list
			// then for some reason the operator crashes/stops. In this case we want to make sure that the operator resets
			// the maintenance mode.
			It("should reset the maintenance", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.MaintenanceZone).To(BeEmpty())
			})

			It("should remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(BeEmpty())
			})
		})

		When(
			"one processes is in the maintenance list but the process was not yet restarted",
			func() {
				BeforeEach(func() {
					// The maintenance is targeted for the future.
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{targetProcessGroup},
							time.Now().Add(1*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.delayedRequeue).To(BeTrue())
					Expect(requeue.message).To(HavePrefix("Waiting for 1 processes in zone"))
				})

				It("shouldn't reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(Equal(currentMaintenanceZone))
				})

				It("shouldn't remove the entry from the maintenance list", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(HaveLen(1))
					Expect(processesUnderMaintenance).To(HaveKey(targetProcessGroup))
				})
			},
		)

		When(
			"one processes is in the maintenance list and the process is missing in the machine-readable status",
			func() {
				BeforeEach(func() {
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{targetProcessGroup},
							time.Now().Add(-1*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
					adminClient.MockMissingProcessGroup(targetProcessGroup, true)
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.delayedRequeue).To(BeTrue())
					Expect(requeue.message).To(HavePrefix("Waiting for 1 processes in zone"))
				})

				It("shouldn't reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(Equal(currentMaintenanceZone))
				})

				It("shouldn't remove the entry from the maintenance list", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(HaveLen(1))
					Expect(processesUnderMaintenance).To(HaveKey(targetProcessGroup))
				})
			},
		)

		When("one processes is in the maintenance list and the process was restarted", func() {
			BeforeEach(func() {
				Expect(
					adminClient.SetProcessesUnderMaintenance(
						[]fdbv1beta2.ProcessGroupID{targetProcessGroup},
						time.Now().Add(-1*time.Minute).Unix(),
					),
				).NotTo(HaveOccurred())
			})

			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should reset the maintenance", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.MaintenanceZone).To(BeEmpty())
			})

			It("should remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(BeEmpty())
			})
		})

		When(
			"one processes for a different zone is in the maintenance list and was recently added",
			func() {
				BeforeEach(func() {
					// The maintenance is targeted for the future.
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{secondStorageProcess},
							time.Now().Add(1*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.delayedRequeue).To(BeTrue())
					Expect(requeue.message).To(HavePrefix("Waiting for 1 processes in zone"))
				})

				It("shouldn't reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(Equal(currentMaintenanceZone))
				})

				It("shouldn't remove the entry from the maintenance list", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(HaveLen(1))
					Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
				})
			},
		)

		When(
			"one processes is in the maintenance list and the process was restarted and another one is missing",
			func() {
				BeforeEach(func() {
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{targetProcessGroup},
							time.Now().Add(-1*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{secondStorageProcess},
							time.Now().Add(-1*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
					adminClient.MockMissingProcessGroup(secondStorageProcess, true)
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.delayedRequeue).To(BeTrue())
					Expect(requeue.message).To(HavePrefix("Waiting for 1 processes in zone"))
				})

				It("shouldn't reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(Equal(currentMaintenanceZone))
				})

				It(
					"shouldn't remove the entry from the maintenance list for the second entry",
					func() {
						processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
						Expect(err).NotTo(HaveOccurred())
						Expect(processesUnderMaintenance).To(HaveLen(1))
						Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
					},
				)
			},
		)

		When(
			"one processes for a different zone is in the maintenance list and was recently added and the process is missing",
			func() {
				BeforeEach(func() {
					// The maintenance is targeted for the future.
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{secondStorageProcess},
							time.Now().Add(1*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
					adminClient.MockMissingProcessGroup(secondStorageProcess, true)
				})

				It("should requeue", func() {
					Expect(requeue).NotTo(BeNil())
					Expect(requeue.delayedRequeue).To(BeTrue())
					Expect(requeue.message).To(HavePrefix("Waiting for 1 processes in zone"))
				})

				It("shouldn't reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(Equal(currentMaintenanceZone))
				})

				It("shouldn't remove the entry from the maintenance list", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(HaveLen(1))
					Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
				})
			},
		)

		// In this case the process was added long enough ago to not block the removal of the maintenance zone but not
		// long enough ago to be considered stale.
		When(
			"one processes for a different zone is in the maintenance list and was added multiple minutes ago",
			func() {
				BeforeEach(func() {
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{secondStorageProcess},
							time.Now().Add(-10*time.Minute).Unix(),
						),
					).NotTo(HaveOccurred())
				})

				It("shouldn't requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(BeEmpty())
				})

				It("shouldn't remove the entry from the maintenance list", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(HaveLen(1))
					Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
				})
			},
		)

		When(
			"one processes for a different zone is in the maintenance list and is considered a stale entry",
			func() {
				BeforeEach(func() {
					Expect(
						adminClient.SetProcessesUnderMaintenance(
							[]fdbv1beta2.ProcessGroupID{secondStorageProcess},
							time.Now().Add(-10*time.Hour).Unix(),
						),
					).NotTo(HaveOccurred())
				})

				It("shouldn't requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should reset the maintenance", func() {
					status, err := adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
					Expect(status.Cluster.MaintenanceZone).To(BeEmpty())
				})

				It("should remove the entry from the maintenance list", func() {
					processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
					Expect(err).NotTo(HaveOccurred())
					Expect(processesUnderMaintenance).To(BeEmpty())
				})
			},
		)
	})
})
