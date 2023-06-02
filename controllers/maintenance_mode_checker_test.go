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
	"strings"

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
	targetProcessGroup := "operator-test-1-storage-1"

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		// Set maintenance mode on
		cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = pointer.Bool(true)
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))
	})

	JustBeforeEach(func() {
		requeue = maintenanceModeChecker{}.reconcile(context.TODO(), clusterReconciler, cluster, nil)
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
			cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
				ZoneID: fdbv1beta2.FaultDomain(targetProcessGroup),
			}
			Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())
		})

		When("a different maintenance zone is active", func() {
			BeforeEach(func() {
				cluster.Status.MaintenanceModeInfo = fdbv1beta2.MaintenanceModeInfo{
					ZoneID: "this-zone-does-not-exist",
				}
				// Update the cluster status here as we will reload it.
				Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should not remove the maintenance status and not requeue", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{
					ZoneID: "this-zone-does-not-exist"}))
			})
		})

		When("Pod hasn't been updated yet", func() {
			BeforeEach(func() {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if !strings.HasSuffix(targetProcessGroup, string(processGroup.ProcessGroupID)) {
						continue
					}

					processGroup.UpdateCondition(fdbv1beta2.IncorrectPodSpec, true)
				}
			})

			It("should be requeued", func() {
				Expect(requeue).ToNot(BeNil())
				Expect(requeue.message).To(Equal("Waiting for 1 process groups in zone operator-test-1-storage-1 to be updated"))
			})
		})

		When("Pod is still down", func() {
			BeforeEach(func() {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if !strings.HasSuffix(targetProcessGroup, string(processGroup.ProcessGroupID)) {
						continue
					}

					processGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
				}
			})

			It("should be requeued", func() {
				Expect(requeue).ToNot(BeNil())
				Expect(requeue.message).To(Equal("Waiting for 1 process groups in zone operator-test-1-storage-1 to be updated"))
			})
		})

		When("all Pods are updated", func() {
			It("should not be requeued", func() {
				Expect(requeue).To(BeNil())
				Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{}))
			})
		})
	})
})
