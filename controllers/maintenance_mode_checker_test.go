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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"
	"k8s.io/utils/pointer"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("maintenance_mode_checker", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var adminClient *mock.AdminClient
	targetProcessGroup := fdbv1beta2.ProcessGroupID("storage-1")

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = pointer.Bool(true)
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = maintenanceModeChecker{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
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

		When("one processes is in the maintenance list with a recent timestamp", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{targetProcessGroup}, time.Now().Unix())).NotTo(HaveOccurred())
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
		})

		When("one stale processes is in the maintenance list", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{targetProcessGroup}, time.Now().Add(-12*time.Hour).Unix())).NotTo(HaveOccurred())
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
		var currentMaintenanceZone fdbv1beta2.FaultDomain
		var secondStorageProcess fdbv1beta2.ProcessGroupID

		BeforeEach(func() {
			for _, pg := range cluster.Status.ProcessGroups {
				if pg.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				if pg.ProcessGroupID == targetProcessGroup {
					currentMaintenanceZone = pg.FaultDomain
					continue
				}

				if secondStorageProcess != "" {
					continue
				}

				secondStorageProcess = pg.ProcessGroupID
			}

			Expect(currentMaintenanceZone).NotTo(BeEmpty())
			Expect(adminClient.SetMaintenanceZone(string(currentMaintenanceZone), 3600)).NotTo(HaveOccurred())
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
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(BeEmpty())
			})

			It("shouldn remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(BeEmpty())
			})
		})

		When("one processes is in the maintenance list but the process was not yet restarted", func() {
			BeforeEach(func() {
				// The maintenance is targeted for the future.
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{targetProcessGroup}, time.Now().Add(1*time.Minute).Unix())).NotTo(HaveOccurred())
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.delayedRequeue).To(BeTrue())
				Expect(requeue.message).To(Equal("Waiting for 1 processes in zone operator-test-1-storage-1 to be updated"))
			})

			It("shouldn't reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(Equal(string(currentMaintenanceZone)))
			})

			It("shouldn't remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(HaveLen(1))
				Expect(processesUnderMaintenance).To(HaveKey(targetProcessGroup))
			})
		})

		When("one processes is in the maintenance list and the process is missing in the machine-readable status", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{targetProcessGroup}, time.Now().Add(-1*time.Minute).Unix())).NotTo(HaveOccurred())
				adminClient.MockMissingProcessGroup(targetProcessGroup, true)
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.delayedRequeue).To(BeTrue())
				Expect(requeue.message).To(Equal("Waiting for 1 processes in zone operator-test-1-storage-1 to be updated"))
			})

			It("shouldn't reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(Equal(string(currentMaintenanceZone)))
			})

			It("shouldn't remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(HaveLen(1))
				Expect(processesUnderMaintenance).To(HaveKey(targetProcessGroup))
			})
		})

		When("one processes is in the maintenance list and the process was restarted", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{targetProcessGroup}, time.Now().Add(-1*time.Minute).Unix())).NotTo(HaveOccurred())
			})

			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(BeEmpty())
			})

			It("should remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(BeEmpty())
			})
		})

		When("one processes for a different zone is in the maintenance list and was recently added", func() {
			BeforeEach(func() {
				// The maintenance is targeted for the future.
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{secondStorageProcess}, time.Now().Add(1*time.Minute).Unix())).NotTo(HaveOccurred())
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.delayedRequeue).To(BeTrue())
				Expect(requeue.message).To(Equal("Waiting for 1 processes in zone operator-test-1-storage-1 to be updated"))
			})

			It("shouldn't reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(Equal(string(currentMaintenanceZone)))
			})

			It("shouldn't remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(HaveLen(1))
				Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
			})
		})

		When("one processes is in the maintenance list and the process was restarted and another one is missing", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{targetProcessGroup}, time.Now().Add(-1*time.Minute).Unix())).NotTo(HaveOccurred())
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{secondStorageProcess}, time.Now().Add(-1*time.Minute).Unix())).NotTo(HaveOccurred())
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.delayedRequeue).To(BeTrue())
				Expect(requeue.message).To(Equal("Waiting for 1 processes in zone operator-test-1-storage-1 to be updated"))
			})

			It("shouldn't reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(Equal(string(currentMaintenanceZone)))
			})

			It("shouldn't remove the entry from the maintenance list for the second entry", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(HaveLen(1))
				Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
			})
		})

		When("one processes for a different zone is in the maintenance list and was recently added and the process is missing", func() {
			BeforeEach(func() {
				// The maintenance is targeted for the future.
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{secondStorageProcess}, time.Now().Add(1*time.Minute).Unix())).NotTo(HaveOccurred())
				adminClient.MockMissingProcessGroup(secondStorageProcess, true)
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.delayedRequeue).To(BeTrue())
				Expect(requeue.message).To(Equal("Waiting for 1 processes in zone operator-test-1-storage-1 to be updated"))
			})

			It("shouldn't reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(Equal(string(currentMaintenanceZone)))
			})

			It("shouldn't remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(HaveLen(1))
				Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
			})
		})

		When("one processes for a different zone is in the maintenance list and was added multiple minutes ago", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{secondStorageProcess}, time.Now().Add(-10*time.Minute).Unix())).NotTo(HaveOccurred())
			})

			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(BeEmpty())
			})

			It("shouldn't remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(HaveLen(1))
				Expect(processesUnderMaintenance).To(HaveKey(secondStorageProcess))
			})
		})

		When("one processes for a different zone is in the maintenance list and was added multiple hours ago", func() {
			BeforeEach(func() {
				Expect(adminClient.SetProcessesUnderMaintenance([]fdbv1beta2.ProcessGroupID{secondStorageProcess}, time.Now().Add(-10*time.Hour).Unix())).NotTo(HaveOccurred())
			})

			It("shouldn't requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should reset the maintenance", func() {
				maintenanceZone, err := adminClient.GetMaintenanceZone()
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenanceZone).To(BeEmpty())
			})

			It("should remove the entry from the maintenance list", func() {
				processesUnderMaintenance, err := adminClient.GetProcessesUnderMaintenance()
				Expect(err).NotTo(HaveOccurred())
				Expect(processesUnderMaintenance).To(BeEmpty())
			})
		})
	})
})
