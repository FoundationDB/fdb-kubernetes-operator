/*
 * update_pods_test.go
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
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("update_pods", func() {
	Context("When deleting Pods for an update", func() {
		var updates map[string][]*corev1.Pod

		type testCase struct {
			cluster              *fdbtypes.FoundationDBCluster
			expectedDeletionsCnt int
		}

		BeforeEach(func() {
			updates = map[string][]*corev1.Pod{
				"zone1": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod2",
						},
					},
				},
				"zone2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod3",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod4",
						},
					},
				},
			}
		})

		DescribeTable("should delete the Pods based on the deletion mode",
			func(input testCase) {
				err := internal.NormalizeClusterSpec(input.cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				_, deletion := getPodsToDelete(input.cluster, updates)
				Expect(len(deletion)).To(Equal(input.expectedDeletionsCnt))
			},
			Entry("With the default deletion Policy",
				testCase{
					cluster: &fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test",
						},
					},
					expectedDeletionsCnt: 2,
				}),
			Entry("With the deletion mode Zone",
				testCase{
					cluster: &fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test",
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
								DeletionMode: fdbtypes.DeletionModeZone,
							},
						},
					},
					expectedDeletionsCnt: 2,
				}),
			Entry("With the deletion mode Process Group",
				testCase{
					cluster: &fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test",
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
								DeletionMode: fdbtypes.DeletionModeProcessGroup,
							},
						},
					},
					expectedDeletionsCnt: 1,
				}),
			Entry("With the deletion mode All",
				testCase{
					cluster: &fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test",
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
								DeletionMode: fdbtypes.DeletionModeAll,
							},
						},
					},
					expectedDeletionsCnt: 4,
				}),
		)
	})
})
