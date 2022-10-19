/*
 * fault_tolerance_test.go
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

package internal

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("fault_tolerance", func() {
	Context("check if the cluster has the desired fault tolerance", func() {
		type testCase struct {
			expectedFaultTolerance                   int
			maxZoneFailuresWithoutLosingData         int
			maxZoneFailuresWithoutLosingAvailability int
			expected                                 bool
		}

		DescribeTable("should return if the cluster has the desired fault tolerance",
			func(input testCase) {
				Expect(hasDesiredFaultTolerance(
					input.expectedFaultTolerance,
					input.maxZoneFailuresWithoutLosingData,
					input.maxZoneFailuresWithoutLosingAvailability)).To(Equal(input.expected))
			},
			Entry("cluster is fully replicated",
				testCase{
					expectedFaultTolerance:                   1,
					maxZoneFailuresWithoutLosingData:         1,
					maxZoneFailuresWithoutLosingAvailability: 1,
					expected:                                 true,
				}),
			Entry("data is degraded",
				testCase{
					expectedFaultTolerance:                   1,
					maxZoneFailuresWithoutLosingData:         0,
					maxZoneFailuresWithoutLosingAvailability: 1,
					expected:                                 false,
				}),
			Entry("availability is degraded",
				testCase{
					expectedFaultTolerance:                   1,
					maxZoneFailuresWithoutLosingData:         1,
					maxZoneFailuresWithoutLosingAvailability: 0,
					expected:                                 false,
				}),
		)
	})

	When("checking if the cluster has the desired fault tolerance from the status", func() {
		log := logr.New(logf.NewDelegatingLogSink(logf.NullLogSink{}))

		DescribeTable("should return if the cluster has the desired fault tolerance",
			func(status *fdbv1beta2.FoundationDBStatus, cluster *fdbv1beta2.FoundationDBCluster, expected bool) {
				Expect(HasDesiredFaultToleranceFromStatus(log, status, cluster)).To(Equal(expected))
			},
			Entry("cluster is fully replicated",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						FaultTolerance: fdbv1beta2.FaultTolerance{
							MaxZoneFailuresWithoutLosingData:         2,
							MaxZoneFailuresWithoutLosingAvailability: 2,
						},
					},
				},
				&fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
					},
				},
				true),
			Entry("database is unavailable",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						FaultTolerance: fdbv1beta2.FaultTolerance{
							MaxZoneFailuresWithoutLosingData:         2,
							MaxZoneFailuresWithoutLosingAvailability: 2,
						},
					},
				},
				&fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
					},
				},
				false),
			Entry("data is degraded",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						FaultTolerance: fdbv1beta2.FaultTolerance{
							MaxZoneFailuresWithoutLosingData:         1,
							MaxZoneFailuresWithoutLosingAvailability: 2,
						},
					},
				},
				&fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
					},
				},
				false),
			Entry("availability is degraded",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						FaultTolerance: fdbv1beta2.FaultTolerance{
							MaxZoneFailuresWithoutLosingData:         2,
							MaxZoneFailuresWithoutLosingAvailability: 1,
						},
					},
				},
				&fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
					},
				},
				false),
		)
	})

	// HasDesiredFaultToleranceFromStatus
})
