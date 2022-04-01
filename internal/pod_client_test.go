/*
 * pod_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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

package internal

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pod_client", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		err := NormalizeClusterSpec(cluster, DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with TLS disabled", func() {
		BeforeEach(func() {
			cluster.Spec.SidecarContainer.EnableTLS = false
		})

		It("should not have TLS sidecar TLS", func() {
			pod, err := GetPod(cluster, fdbv1beta2.ProcessClassStorage, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(podHasSidecarTLS(pod)).To(BeFalse())
		})
	})

	Context("with TLS enabled", func() {
		BeforeEach(func() {
			cluster.Spec.SidecarContainer.EnableTLS = true
		})

		It("should have TLS sidecar TLS", func() {
			pod, err := GetPod(cluster, fdbv1beta2.ProcessClassStorage, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(podHasSidecarTLS(pod)).To(BeTrue())
		})
	})
})
