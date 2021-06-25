/*
 * coordinator_selection_test.go
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

package v1beta1

import (
	"math"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] Coordinator Selection", func() {
	When("checking if a process is eligible as coordinator candidate", func() {
		type testCase struct {
			cluster  *FoundationDBCluster
			pClass   ProcessClass
			expected bool
		}

		DescribeTable("should return if the process class is eligible",
			func(tc testCase) {
				Expect(tc.cluster.IsEligibleAsCandidate(tc.pClass)).To(Equal(tc.expected))
			},
			Entry("storage class without any configuration is eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStorage,
					expected: true,
				}),
			Entry("log class without any configuration is eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassLog,
					expected: true,
				}),
			Entry("transaction class without any configuration is eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassTransaction,
					expected: true,
				}),
			Entry("stateless class without any configuration is not eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStateless,
					expected: false,
				}),
			Entry("cluster controller class without any configuration is not eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassClusterController,
					expected: false,
				}),
			Entry("storage class with only storage classes is eligible",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassStorage,
					expected: true,
				}),
			Entry("log class with only storage classes is not eligible",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassLog,
					expected: true,
				}),
		)
	})

	When("getting the priority of a process class", func() {
		type testCase struct {
			cluster  *FoundationDBCluster
			pClass   ProcessClass
			expected int
		}

		DescribeTable("should return the expected process class",
			func(tc testCase) {
				Expect(tc.cluster.GetClassPriority(tc.pClass)).To(Equal(tc.expected))
			},
			Entry("storage class without any configuration returns highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStorage,
					expected: math.MinInt64,
				}),
			Entry("log class without any configuration highest prioritye",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassLog,
					expected: math.MinInt64,
				}),
			Entry("transaction class without any configuration highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassTransaction,
					expected: math.MinInt64,
				}),
			Entry("stateless class without any configuration highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStateless,
					expected: math.MinInt64,
				}),
			Entry("cluster controller class without any configuration highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassClusterController,
					expected: math.MinInt64,
				}),
			Entry("storage class with only storage classes returns 1 as priority",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassStorage,
					expected: 1,
				}),
			Entry("log class with only storage classes returns highest priority",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassLog,
					expected: math.MinInt64,
				}),
		)
	})
})
