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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("maintenance_mode_checker", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var adminClient *mockAdminClient

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		podUpCheckDelayInSeconds = 60
	})

	JustBeforeEach(func() {
		requeue = maintenanceModeChecker{}.reconcile(context.TODO(), clusterReconciler, cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
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
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{ZoneId: "storage-4"}
			})
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})

	})

	Context("maintenance mode is on", func() {
		BeforeEach(func() {
			adminClient.SetMaintenanceZone("storage-1")
		})

		When("status maintenance zone does not match", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{ZoneId: "storage-4"}
			})
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})

		When("delay has not expired", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					StartTimestamp: &metav1.Time{Time: time.Now()},
					ZoneId:         "storage-1",
					ProcessGroups:  []string{"storage-1"},
				}
			})
			It("should be requeued", func() {
				Expect(requeue).ToNot(BeNil())
				Expect(requeue.message).To(Equal("Waiting for delay to expire for checking if pods under maintenance mode have been bounced"))
			})
		})

		When("pod is still down", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					StartTimestamp: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
					ZoneId:         "storage-1",
					ProcessGroups:  []string{"storage-1"},
				}
				adminClient.MockMissingProcessGroup("storage-1", true)
			})
			It("should be requeued", func() {
				Expect(requeue).ToNot(BeNil())
				Expect(requeue.message).To(Equal("Waiting for all proceeses in zone storage-1 to be up"))
			})
		})

		When("reconciliation should be successful", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					StartTimestamp: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
					ZoneId:         "storage-1",
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
		clearMockAdminClients()
		k8sClient.Clear()
		podUpCheckDelayInSeconds = 0
	})
})
