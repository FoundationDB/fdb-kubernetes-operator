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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

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

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		//Set maintenance mode on
		cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = pointer.Bool(true)
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

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
		Eventually(func() error {
			requeue = maintenanceModeChecker{}.reconcile(context.TODO(), clusterReconciler, cluster)
			return err
		}).ShouldNot(HaveOccurred())

		Eventually(func() error {
			_, err = reloadCluster(cluster)
			return err
		}).ShouldNot(HaveOccurred())
	})

	Context("maintenance mode is off", func() {
		When("status maintenance is empty", func() {
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})
		When("status maintenance is not empty", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{ZoneID: "storage-4"}
			})
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})

	})

	Context("maintenance mode is on", func() {
		BeforeEach(func() {
			Expect(adminClient.SetMaintenanceZone("operator-test-1-storage-1", 0)).NotTo(HaveOccurred())
		})

		When("status maintenance zone does not match", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{ZoneID: "operator-test-1-storage-4"}
			})
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})

		When("pod hasn't been updated yet", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					StartTimestamp: &metav1.Time{Time: time.Now()},
					ZoneID:         "operator-test-1-storage-1",
					ProcessGroups:  []string{"storage-1"},
				}
				adminClient.MockUptimeSecondsForMaintenanceZone(3600)
			})
			It("should be requeued", func() {
				Expect(requeue).ToNot(BeNil())
				Expect(requeue.message).To(Equal("Waiting for pod storage-1 to be updated"))
			})
		})

		When("pod is still down", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					StartTimestamp: &metav1.Time{Time: time.Now()},
					ZoneID:         "operator-test-1-storage-1",
					ProcessGroups:  []string{"storage-1"},
				}
				adminClient.MockMissingProcessGroup("storage-1", true)
			})
			It("should be requeued", func() {
				Expect(requeue).ToNot(BeNil())
				Expect(requeue.message).To(Equal("Waiting for all proceeses in zone operator-test-1-storage-1 to be up"))
			})
		})

		When("reconciliation should be successful", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					StartTimestamp: &metav1.Time{Time: time.Now().Add(-60 * time.Second)},
					ZoneID:         "operator-test-1-storage-1",
					ProcessGroups:  []string{"storage-1"},
				}

			})
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})
	})

	AfterEach(func() {
		mock.ClearMockAdminClients()
		k8sClient.Clear()
	})
})
