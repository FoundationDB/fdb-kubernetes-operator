/*
 * update_pods_test.go
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
	"context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("update_pods", func() {
	FContext("validating CanDeletePods()", func() {
		var fdbCluster *fdbtypes.FoundationDBCluster
		var adminClient *mockAdminClient
		var err error

		BeforeEach(func() {
			fdbCluster = internal.CreateDefaultCluster()
			err = setupClusterForTest(fdbCluster)
			Expect(err).NotTo(HaveOccurred())
			adminClient, err = newMockAdminClientUncast(fdbCluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
		})

		type testCase struct {
			expected bool
			cluster  *fdbtypes.FoundationDBCluster
		}

		DescribeTable("should return whether pod deletion is allowed",
			func(tc testCase) {
				deletionAllowed, err := clusterReconciler.PodLifecycleManager.CanDeletePods(adminClient, context.TODO(), tc.cluster)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(deletionAllowed).Should(Equal(tc.expected))
			},
			Entry("the cluster doesn't have zone fault tolerance in status",
				testCase{
					expected: true,
					cluster: &fdbtypes.FoundationDBCluster{
						Spec: fdbtypes.FoundationDBClusterSpec{
							Version: fdbtypes.Versions.MinimumVersion.String(),
						},
					},
				}),
			Entry("the cluster doesn't have enough fault tolerance",
				testCase{
					expected: false,
					cluster: &fdbtypes.FoundationDBCluster{
						Spec: fdbtypes.FoundationDBClusterSpec{
							Version: fdbtypes.Versions.Default.String(),
							DatabaseConfiguration: fdbtypes.DatabaseConfiguration{
								RedundancyMode: fdbtypes.RedundancyModeTriple,
							},
						},
					},
				}),
		)
	})
})
