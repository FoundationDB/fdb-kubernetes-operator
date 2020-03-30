/*
 * update_sidecar_versions_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"

	"context"
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("UpdateSidecarVersions", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var originalPods *corev1.PodList

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		shouldContinue = true
		originalPods = &corev1.PodList{}
		err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Eventually(func() (bool, error) { return checkClusterReconciled(k8sClient, cluster) }, 5*time.Second).Should(BeTrue())
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(UpdateSidecarVersions{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})
	})

	Context("with a pending upgrade", func() {
		BeforeEach(func() {
			cluster.Spec.Version = Versions.NextMajorVersion.String()
		})

		It("should update the sidecar version", func() {
			err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(originalPods.Items[0].Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", Versions.Default.String())))
			Expect(originalPods.Items[0].Spec.Containers[1].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", Versions.NextMajorVersion.String())))
		})
	})
})
