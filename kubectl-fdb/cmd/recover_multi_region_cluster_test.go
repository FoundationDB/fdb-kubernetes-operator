/*
 * recover_multi_region_cluster_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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

package cmd

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[plugin] running the recover multi-region cluster command", func() {
	var imageTypeUnified = fdbv1beta2.ImageTypeUnified
	var imageTypeSplit = fdbv1beta2.ImageTypeSplit
	type dataDirTest struct {
		input    string
		pod      *corev1.Pod
		cluster  *fdbv1beta2.FoundationDBCluster
		expected string
	}

	DescribeTable("when getting the data dir for uploading files", func(test dataDirTest) {
		Expect(getDataDir(test.input, test.pod, test.cluster)).To(Equal(test.expected))
	},
		Entry("data dir is already correct",
			dataDirTest{
				input: "/var/fdb/data",
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
				},
				cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						ImageType: &imageTypeSplit,
					},
				},
				expected: "/var/fdb/data",
			},
		),
		Entry("data dir has process directory and target pod has single process",
			dataDirTest{
				input: "/var/fdb/data/2",
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
				},
				cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						ImageType: &imageTypeSplit,
					},
				},
				expected: "/var/fdb/data",
			},
		),
		Entry("data dir has process directory and target pod has multiple processes",
			dataDirTest{
				input: "/var/fdb/data/2",
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
				},
				cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						StorageServersPerPod: 2,
						ImageType:            &imageTypeSplit,
					},
				},
				expected: "/var/fdb/data/1",
			},
		),
		Entry("data dir has process directory and target pod has single process with unified image",
			dataDirTest{
				input: "/var/fdb/data/2",
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
				},
				cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						ImageType: &imageTypeUnified,
					},
				},
				expected: "/var/fdb/data/1",
			},
		),
		Entry("data dir has process directory and target pod has multiple processes with unified image",
			dataDirTest{
				input: "/var/fdb/data/2",
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
				},
				cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						ImageType:            &imageTypeUnified,
						StorageServersPerPod: 2,
					},
				},
				expected: "/var/fdb/data/1",
			},
		),
	)
})
