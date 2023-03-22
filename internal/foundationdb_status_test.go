/*
 * foundationdb_status_test.go
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
	"github.com/go-logr/logr"
	"net"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Internal FoundationDBStatus", func() {
	When("parsing the status for coordinators", func() {
		type testCase struct {
			status   *fdbv1beta2.FoundationDBStatus
			expected map[string]fdbv1beta2.None
		}

		DescribeTable("parse the status",
			func(tc testCase) {
				coordinators := GetCoordinatorsFromStatus(tc.status)
				Expect(coordinators).To(Equal(tc.expected))
			},
			Entry("no coordinators",
				testCase{
					status:   &fdbv1beta2.FoundationDBStatus{},
					expected: map[string]fdbv1beta2.None{},
				}),
			Entry("single coordinators",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"foo": {
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "foo",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role: "coordinator",
										},
									},
								},
								"bar": {
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "bar",
									},
								},
							},
						},
					},
					expected: map[string]fdbv1beta2.None{
						"foo": {},
					},
				}),
			Entry("multiple coordinators",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"foo": {
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "foo",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role: "coordinator",
										},
									},
								},
								"bar": {
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "bar",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role: "coordinator",
										},
									},
								},
							},
						},
					},
					expected: map[string]fdbv1beta2.None{
						"foo": {},
						"bar": {},
					},
				}),
		)
	})

	DescribeTable("when getting the minimum uptime and the address map", func(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, useRecoveryState bool, expectedMinimumUptime float64, expectedAddressMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress) {
		minimumUptime, addressMap, err := GetMinimumUptimeAndAddressMap(logr.Discard(), cluster, status, useRecoveryState)
		Expect(err).NotTo(HaveOccurred())
		Expect(minimumUptime).To(BeNumerically("==", expectedMinimumUptime))
		Expect(len(addressMap)).To(BeNumerically("==", len(expectedAddressMap)))
		for key, value := range expectedAddressMap {
			Expect(addressMap).To(HaveKeyWithValue(key, value))
		}
	},
		Entry("when recovered since is not available",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.Default.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 90.0,
					},
				},
			},
			true,
			30.0,
			map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
		Entry("when recovered since is enabled and version supports it",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.SupportsRecoveryState.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 90.0,
					},
				},
			},
			true,
			90.0,
			map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
		Entry("when recovered since is disabled and version supports it",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.SupportsRecoveryState.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
					},
				},
			},
			false,
			30.0,
			map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
		Entry("when recovered since is not available and multiple processes are reporting but one is missing localities",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.Default.String(),
				},
			}, &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"test": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.1"),
							},
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityInstanceIDKey: "test",
							},
							UptimeSeconds: 30.0,
						},
						"bad_processes": {
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("127.0.0.2"),
							},
							UptimeSeconds: 0.0,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 90.0,
					},
				},
			},
			true,
			30.0,
			map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.ProcessAddress{
				"test": {
					{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
				},
			}),
	)
})
