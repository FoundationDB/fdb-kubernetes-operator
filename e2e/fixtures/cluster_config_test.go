/*
 * cluster_config_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = FDescribe("Cluster configuration", func() {
	DescribeTable("when generating the Pod resources", func(config *ClusterConfig, processClass fdbv1beta2.ProcessClass, expected corev1.ResourceList) {
		Expect(config.generatePodResources(processClass)).To(Equal(expected))
	},
		Entry("empty config for general process class",
			&ClusterConfig{},
			fdbv1beta2.ProcessClassGeneral,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}),
		Entry("empty config for storage process class",
			&ClusterConfig{},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}),
		Entry("performance config for general process class",
			&ClusterConfig{
				Performance: true,
			},
			fdbv1beta2.ProcessClassGeneral,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("performance config for storage process class",
			&ClusterConfig{
				Performance: true,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("performance config for storage process class with multiple storage servers per disk",
			&ClusterConfig{
				Performance:         true,
				StorageServerPerPod: 2,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}),
		Entry("empty config for general process class for Kind",
			&ClusterConfig{
				cloudProvider: "kind",
			},
			fdbv1beta2.ProcessClassGeneral,
			corev1.ResourceList{}),
		Entry("empty config for storage process class",
			&ClusterConfig{
				cloudProvider: "kind",
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{}),
	)
})
