/*
 * update_pod_config_test.go
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
	"context"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("UpdatePodConfig", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var requeue *Requeue
	var err error
	var instances []FdbInstance

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = setupClusterForTest(cluster)
		Expect(err).NotTo(HaveOccurred())

		instances, err = clusterReconciler.PodLifecycleManager.GetInstances(clusterReconciler, cluster, context.TODO(), internal.GetSinglePodListOptions(cluster, "storage-1")...)
		Expect(err).NotTo(HaveOccurred())

		for _, container := range instances[0].Pod.Spec.Containers {
			instances[0].Pod.Status.ContainerStatuses = append(instances[0].Pod.Status.ContainerStatuses, corev1.ContainerStatus{Ready: true, Name: container.Name})
		}
	})

	JustBeforeEach(func() {
		requeue = UpdatePodConfig{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(requeue).To(BeNil())
		})
	})

	When("a Pod is stuck in Pending", func() {
		BeforeEach(func() {
			instances[0].Pod.Status.Phase = corev1.PodPending
		})

		It("should not requeue", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(requeue).To(BeNil())
		})
	})
})
