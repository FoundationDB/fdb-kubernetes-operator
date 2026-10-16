/*
 * service_helper_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("service_helper.go", func() {
	Describe("GetGRPCService", func() {
		var cluster *v1beta2.FoundationDBCluster
		BeforeEach(func() {
			cluster = CreateDefaultCluster()
		})

		When("using the default configuration", func() {
			It("fills in required fields", func() {
				service, err := GetGRPCService(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(service.Name).To(Equal("operator-test-1-grpc"))
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
				Expect(service.Spec.Selector).To(Equal(map[string]string{
					"foundationdb.org/fdb-cluster-name": "operator-test-1",
				}))
				Expect(service.Spec.Ports).To(HaveExactElements([]corev1.ServicePort{
					{Port: 8080, TargetPort: intstr.FromInt32(8080)},
				}))
			})
		})

		When("customizing the service type", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.GRPCService = &corev1.Service{
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
				}
			})

			It("fills in missing fields", func() {
				service, err := GetGRPCService(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(service.Name).To(Equal("operator-test-1-grpc"))
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
			})
		})
	})
})
