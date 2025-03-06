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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Cluster configuration", func() {
	DescribeTable("when generating the Pod resources", func(config *ClusterConfig, processClass fdbv1beta2.ProcessClass, expected corev1.ResourceList) {
		config.SetDefaults(nil)
		generated := config.generatePodResources(processClass)
		Expect(generated.Cpu().String()).To(Equal(expected.Cpu().String()))
		Expect(generated.Memory().String()).To(Equal(expected.Memory().String()))
	},
		Entry("empty config for general process class",
			&ClusterConfig{
				Name:                "test",
				Namespace:           "unit-test",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				StorageServerPerPod: 1,
			},
			fdbv1beta2.ProcessClassGeneral,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("empty config for storage process class",
			&ClusterConfig{
				Name:                "test",
				Namespace:           "unit-test",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				StorageServerPerPod: 1,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("performance config for general process class",
			&ClusterConfig{
				Name:                "test",
				Namespace:           "unit-test",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				StorageServerPerPod: 1,
				Performance:         true,
			},
			fdbv1beta2.ProcessClassGeneral,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("performance config for storage process class",
			&ClusterConfig{
				Name:                "test",
				Namespace:           "unit-test",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				StorageServerPerPod: 1,
				Performance:         true,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("performance config for storage process class with multiple storage servers per disk",
			&ClusterConfig{
				StorageServerPerPod: 2,
				Name:                "test",
				Namespace:           "unit-test",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				Performance:         true,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}),

		Entry("performance config for storage process class with custom memory",
			&ClusterConfig{
				Name:                "test",
				Namespace:           "unit-test",
				MemoryPerPod:        "16Gi",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				StorageServerPerPod: 1,
				Performance:         true,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}),
		Entry("performance config for storage process class with multiple storage servers per disk with custom memory",
			&ClusterConfig{
				StorageServerPerPod: 2,
				Name:                "test",
				Namespace:           "unit-test",
				MemoryPerPod:        "16Gi",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				cloudProvider:       "test",
				Performance:         true,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			}),
		Entry("empty config for general process class for kind",
			&ClusterConfig{
				cloudProvider: "kind",
				Name:          "test",
				Namespace:     "unit-test",
				StorageEngine: fdbv1beta2.StorageEngineRocksDbV1,
			},
			fdbv1beta2.ProcessClassGeneral,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
		Entry("empty config for storage process class for kind",
			&ClusterConfig{
				cloudProvider:       "kind",
				Name:                "test",
				Namespace:           "unit-test",
				StorageEngine:       fdbv1beta2.StorageEngineRocksDbV1,
				StorageServerPerPod: 1,
			},
			fdbv1beta2.ProcessClassStorage,
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}),
	)
})
