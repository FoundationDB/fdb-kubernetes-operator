/*
 * status_checks_test.go
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

package fdbstatus

import (
	"fmt"
	"net"

	"github.com/go-logr/logr"
	"k8s.io/utils/pointer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("status_checks", func() {
	When("getting the excluded and remaining processes", func() {
		addr1 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.1"), "", 0, nil)
		addr2 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.2"), "", 0, nil)
		addr3 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.3"), "", 0, nil)
		addr4 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.4"), "", 0, nil)
		addr5 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.5"), "", 0, nil)
		status := &fdbv1beta2.FoundationDBStatus{
			Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
				DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
					Available: true,
				},
			},
			Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
				Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
					"1": {
						Address:  addr1,
						Excluded: true,
						Locality: map[string]string{
							fdbv1beta2.FDBLocalityInstanceIDKey: "1",
						},
					},
					"2": {
						Address: addr2,
						Locality: map[string]string{
							fdbv1beta2.FDBLocalityInstanceIDKey: "2",
						},
					},
					"3": {
						Address: addr3,
						Locality: map[string]string{
							fdbv1beta2.FDBLocalityInstanceIDKey: "3",
						},
					},
					"4": {
						Address:  addr4,
						Excluded: true,
						Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
							{
								Role: "tester",
							},
						},
						Locality: map[string]string{
							fdbv1beta2.FDBLocalityInstanceIDKey: "4",
						},
					},
				},
			},
		}
		DescribeTable("fetching the excluded and remaining processes from the status",
			func(status *fdbv1beta2.FoundationDBStatus,
				addresses []fdbv1beta2.ProcessAddress,
				expectedInProgress []fdbv1beta2.ProcessAddress,
				expectedNotExcluded []fdbv1beta2.ProcessAddress,
				expectedFullyExcluded []fdbv1beta2.ProcessAddress,
				expectedMissing []fdbv1beta2.ProcessAddress) {

				exclusions := getRemainingAndExcludedFromStatus(logr.Discard(), status, addresses)
				Expect(expectedInProgress).To(ConsistOf(exclusions.inProgress))
				Expect(expectedNotExcluded).To(ConsistOf(exclusions.notExcluded))
				Expect(expectedFullyExcluded).To(ConsistOf(exclusions.fullyExcluded))
				Expect(expectedMissing).To(ConsistOf(exclusions.missingInStatus))
			},
			Entry("with an empty input address slice",
				status,
				[]fdbv1beta2.ProcessAddress{},
				nil,
				nil,
				nil,
				nil,
			),
			Entry("when the process is excluded",
				status,
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
				nil,
			),
			Entry("when the process is not excluded",
				status,
				[]fdbv1beta2.ProcessAddress{addr3},
				nil,
				[]fdbv1beta2.ProcessAddress{addr3},
				nil,
				nil,
			),
			Entry("when some processes are excluded and some not",
				status,
				[]fdbv1beta2.ProcessAddress{addr1, addr2, addr3, addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr2, addr3},
				[]fdbv1beta2.ProcessAddress{addr1},
				nil,
			),
			Entry("when a process is missing",
				status,
				[]fdbv1beta2.ProcessAddress{addr5},
				nil,
				[]fdbv1beta2.ProcessAddress{},
				nil,
				[]fdbv1beta2.ProcessAddress{addr5},
			),
			Entry("when the process is excluded but the cluster status has multiple generations",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 2,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the process group has multiple processes and only one is fully excluded",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4-1": {
								Address:  addr4,
								Excluded: true,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityProcessIDKey: "4-1",
								},
							},
							"4-2": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: string(fdbv1beta2.ProcessRoleStorage),
									},
								},
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityProcessIDKey: "4-2",
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
				nil,
			),
			Entry("when the process group has multiple processes and both are fully excluded",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4-1": {
								Address:  addr4,
								Excluded: true,
							},
							"4-2": {
								Address:  addr4,
								Excluded: true,
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
			),
			Entry("when the process group has multiple processes and only one is excluded",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4-1": {
								Address:  addr4,
								Excluded: true,
							},
							"4-2": {
								Address:  addr4,
								Excluded: false,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: string(fdbv1beta2.ProcessRoleStorage),
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the process is excluded but the cluster is unavailable",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the machine-readable status contains the \"storage_servers_error\" message",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Messages: []fdbv1beta2.FoundationDBStatusMessage{
							{
								Name:        "storage_servers_error",
								Description: "...",
							},
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the machine-readable status contains the \"log_servers_error\" message",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Messages: []fdbv1beta2.FoundationDBStatusMessage{
							{
								Name:        "log_servers_error",
								Description: "...",
							},
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the machine-readable status contains the \"unreadable_configuration\" message",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Messages: []fdbv1beta2.FoundationDBStatusMessage{
							{
								Name:        "unreadable_configuration",
								Description: "...",
							},
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the machine-readable status contains the \"full_replication_timeout\" message",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Messages: []fdbv1beta2.FoundationDBStatusMessage{
							{
								Name:        "full_replication_timeout",
								Description: "...",
							},
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the machine-readable status contains the \"client_issues\" message",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Messages: []fdbv1beta2.FoundationDBStatusMessage{
							{
								Name:        "client_issues",
								Description: "...",
							},
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {
								Address:  addr1,
								Excluded: true,
							},
							"2": {
								Address: addr2,
							},
							"3": {
								Address: addr3,
							},
							"4": {
								Address:  addr4,
								Excluded: true,
								Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
									{
										Role: "tester",
									},
								},
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
				nil,
			),
			Entry("when the process is excluded and locality based exclusions are used",
				status,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:4", 0, nil)},
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:4", 0, nil)},
				nil,
				nil,
				nil,
			),
			Entry("when the process is not excluded and locality based exclusions are used",
				status,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:3", 0, nil)},
				nil,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:3", 0, nil)},
				nil,
				nil,
			),
			Entry("when the process is excluded and locality based exclusions are used and an IP address is provided",
				status,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:4", 0, nil), addr4},
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:4", 0, nil), addr4},
				nil,
				nil,
				nil,
			),
			Entry("when the process is not excluded and locality based exclusions are use and an IP address is provided",
				status,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:3", 0, nil), addr3},
				nil,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:3", 0, nil), addr3},
				nil,
				nil,
			),

			Entry("when one process is not excluded, one process is excluded and locality based exclusions are use and an IP address is provided",
				status,
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:3", 0, nil), addr3, fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:4", 0, nil), addr4},
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:4", 0, nil), addr4},
				[]fdbv1beta2.ProcessAddress{fdbv1beta2.NewProcessAddress(net.IP{}, "locality_instance_id:3", 0, nil), addr3},
				nil,
				nil,
			),
		)
	})

	When("getting the exclusions from the status", func() {
		var status *fdbv1beta2.FoundationDBStatus
		var exclusions []fdbv1beta2.ProcessAddress

		JustBeforeEach(func() {
			var err error
			exclusions, err = GetExclusions(status)
			Expect(err).NotTo(HaveOccurred())
		})

		When("the status contains no exclusions", func() {
			BeforeEach(func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							ExcludedServers: []fdbv1beta2.ExcludedServers{},
						},
					},
				}
			})

			It("should return an empty list", func() {
				Expect(exclusions).To(BeEmpty())
			})
		})

		When("the status contains exclusions with IP addresses", func() {
			BeforeEach(func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							ExcludedServers: []fdbv1beta2.ExcludedServers{
								{
									Address: "192.168.0.1",
								},
								{
									Address: "192.168.0.2",
								},
							},
						},
					},
				}
			})

			It("should return the excluded servers", func() {
				Expect(exclusions).To(HaveLen(2))
				Expect(exclusions).To(ConsistOf(
					fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP("192.168.0.1")},
					fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP("192.168.0.2")},
				))
			})
		})

		When("the status contains exclusions with localities", func() {
			BeforeEach(func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							ExcludedServers: []fdbv1beta2.ExcludedServers{
								{
									Locality: "test1",
								},
								{
									Locality: "test2",
								},
							},
						},
					},
				}
			})

			It("should return the excluded servers", func() {
				Expect(exclusions).To(HaveLen(2))
				Expect(exclusions).To(ConsistOf(
					fdbv1beta2.ProcessAddress{StringAddress: "test1"},
					fdbv1beta2.ProcessAddress{StringAddress: "test2"},
				))
			})
		})

		When("the status contains exclusions with localities and IP addresses", func() {
			BeforeEach(func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							ExcludedServers: []fdbv1beta2.ExcludedServers{
								{
									Locality: "test1",
								},
								{
									Address: "192.168.0.2",
								},
							},
						},
					},
				}
			})

			It("should return the excluded servers", func() {
				Expect(exclusions).To(HaveLen(2))
				Expect(exclusions).To(ConsistOf(
					fdbv1beta2.ProcessAddress{StringAddress: "test1"},
					fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP("192.168.0.2")},
				))
			})
		})
	})

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
		Entry("when recovered since is enabled and version supports it, it should use the minimum of recovered since or process uptime",
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
			30.0,
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

	Context("fault domain checks on status object", func() {
		var status *fdbv1beta2.FoundationDBStatus

		Context("storage server fault domain checks", func() {
			It("storage server team is missing", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{},
						},
					},
				}
				err := DoStorageServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("no team trackers specified in status"))
			})

			It("storage server team healthy", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy", MinReplicasRemaining: 4},
								},
							},
						},
					},
				}
				Expect(DoStorageServerFaultDomainCheckOnStatus(status)).NotTo(HaveOccurred())
			})

			It("primary storage server team unhealthy", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: false, Name: "healthy", MinReplicasRemaining: 4},
								},
								{
									Primary: false,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy", MinReplicasRemaining: 0},
								},
							},
						},
					},
				}
				err := DoStorageServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("team tracker in primary is in unhealthy state"))
			})

			It("remote storage server team unhealthy", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy", MinReplicasRemaining: 4},
								},
								{
									Primary: false,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: false, Name: "healthy", MinReplicasRemaining: 0},
								},
							},
						},
					},
				}
				err := DoStorageServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("team tracker in remote is in unhealthy state"))
			})
		})

		Context("log server fault domain checks", func() {
			It("no log server fault tolerance information in status", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{},
					},
				}

				err := DoLogServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("no log information specified in status"))
			})

			It("all primary log servers available", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:                       true,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
						},
					},
				}

				Expect(DoLogServerFaultDomainCheckOnStatus(status)).NotTo(HaveOccurred())
			})

			It("not enough replicas in the primary", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:                       true,
								LogFaultTolerance:             0,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
						},
					},
				}

				err := DoLogServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("primary log fault tolerance is not satisfied, replication factor: 2, current fault tolerance: 0"))
			})

			It("not enough replicas in remote", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:                       true,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       1,
								RemoteLogReplicationFactor:    4,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
						},
					},
				}

				err := DoLogServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("remote log fault tolerance is not satisfied, replication factor: 4, current fault tolerance: 1"))
			})

			It("not enough replicas in the satellite", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:                       true,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    1,
								SatelliteLogReplicationFactor: 4,
							},
						},
					},
				}

				err := DoLogServerFaultDomainCheckOnStatus(status)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("satellite log fault tolerance is not satisfied, replication factor: 4, current fault tolerance: 1"))
			})

			It("multiple log server sets", func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:                       true,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
							{
								Current:                       false,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
						},
					},
				}
				Expect(DoLogServerFaultDomainCheckOnStatus(status)).NotTo(HaveOccurred())
			})
		})

		When("doing the coordinator fault domain checks", func() {
			BeforeEach(func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
							QuorumReachable: true,
							Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.1"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.2"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.3"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.4"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.5"),
										Port:      4500,
									},
								},
							},
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{},
				}
			})

			When("all coordinators are reachable", func() {
				It("should report no error", func() {
					Expect(DoCoordinatorFaultDomainCheckOnStatus(status)).NotTo(HaveOccurred())
				})
			})

			When("one coordinator is unreachable", func() {
				BeforeEach(func() {
					status.Client.Coordinators.Coordinators[0].Reachable = false
				})

				It("should report an error", func() {
					err := DoCoordinatorFaultDomainCheckOnStatus(status)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(status.Client.Coordinators.Coordinators[0].Address.String()))
				})
			})

			When("two coordinators are unreachable", func() {
				BeforeEach(func() {
					status.Client.Coordinators.Coordinators[0].Reachable = false
					status.Client.Coordinators.Coordinators[1].Reachable = false
				})

				It("should report an error", func() {
					err := DoCoordinatorFaultDomainCheckOnStatus(status)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(And(ContainSubstring(status.Client.Coordinators.Coordinators[0].Address.String()), ContainSubstring(status.Client.Coordinators.Coordinators[1].Address.String())))
				})
			})

			When("the quorum of coordinators are unreachable", func() {
				BeforeEach(func() {
					status.Client.Coordinators.QuorumReachable = false
				})

				It("should report an error", func() {
					err := DoCoordinatorFaultDomainCheckOnStatus(status)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("quorum of coordinators is not reachable"))
				})
			})
		})

		Context("multiple fault domain checks", func() {
			BeforeEach(func() {
				status = &fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
							QuorumReachable: true,
							Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.1"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.2"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.3"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.4"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.5"),
										Port:      4500,
									},
								},
							},
						},
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy", MinReplicasRemaining: 4},
								},
								{
									Primary: false,
									State:   fdbv1beta2.FoundationDBStatusDataState{Description: "", Healthy: false, Name: "healthy", MinReplicasRemaining: 0},
								},
							},
						},
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								Current:                       true,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
							{
								Current:                       false,
								LogFaultTolerance:             1,
								LogReplicationFactor:          2,
								RemoteLogFaultTolerance:       0,
								RemoteLogReplicationFactor:    0,
								SatelliteLogFaultTolerance:    0,
								SatelliteLogReplicationFactor: 0,
							},
						},
					},
				}
			})

			It("do storage server fault domain check", func() {
				err := DoFaultDomainChecksOnStatus(status, true, false, false)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("team tracker in remote is in unhealthy state"))
			})

			It("do log server fault domain check", func() {
				Expect(DoFaultDomainChecksOnStatus(status, false, true, false)).NotTo(HaveOccurred())
			})

			It("do coordinator fault domain check", func() {
				Expect(DoFaultDomainChecksOnStatus(status, false, false, true)).NotTo(HaveOccurred())
			})

			It("do storage server and log server fault domain checks", func() {
				err := DoFaultDomainChecksOnStatus(status, true, true, false)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("team tracker in remote is in unhealthy state"))
			})

			It("do all fault domain checks", func() {
				err := DoFaultDomainChecksOnStatus(status, true, true, true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("team tracker in remote is in unhealthy state"))
			})
		})
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
						Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
							QuorumReachable: true,
							Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.1"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.2"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.3"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.4"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.5"),
										Port:      4500,
									},
								},
							},
						},
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State: fdbv1beta2.FoundationDBStatusDataState{
										Healthy:              true,
										MinReplicasRemaining: 3,
									},
								},
							},
						},
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								LogFaultTolerance:    2,
								LogReplicationFactor: 3,
							},
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
						Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
							QuorumReachable: true,
							Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.1"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.2"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.3"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.4"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.5"),
										Port:      4500,
									},
								},
							},
						},
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{},
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
						Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
							QuorumReachable: true,
							Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.1"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.2"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.3"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.4"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.5"),
										Port:      4500,
									},
								},
							},
						},
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State: fdbv1beta2.FoundationDBStatusDataState{
										Healthy:              false,
										MinReplicasRemaining: 2,
									},
								},
							},
						},
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								LogFaultTolerance:    2,
								LogReplicationFactor: 3,
							},
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
			Entry("logs are degraded",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
							QuorumReachable: true,
							Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.1"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.2"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.3"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.4"),
										Port:      4500,
									},
								},
								{
									Reachable: true,
									Address: fdbv1beta2.ProcessAddress{
										IPAddress: net.ParseIP("192.168.0.5"),
										Port:      4500,
									},
								},
							},
						},
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							RedundancyMode: fdbv1beta2.RedundancyModeTriple,
						},
						Data: fdbv1beta2.FoundationDBStatusDataStatistics{
							TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
								{
									Primary: true,
									State: fdbv1beta2.FoundationDBStatusDataState{
										Healthy:              true,
										MinReplicasRemaining: 3,
									},
								},
							},
						},
						Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
							{
								LogFaultTolerance:    1,
								LogReplicationFactor: 3,
							},
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
		Entry("one coordinator is not reachable",
			&fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
						QuorumReachable: true,
						Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
							{
								Reachable: false,
								Address: fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP("192.168.0.1"),
									Port:      4500,
								},
							},
							{
								Reachable: true,
								Address: fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP("192.168.0.2"),
									Port:      4500,
								},
							},
							{
								Reachable: true,
								Address: fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP("192.168.0.3"),
									Port:      4500,
								},
							},
							{
								Reachable: true,
								Address: fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP("192.168.0.4"),
									Port:      4500,
								},
							},
							{
								Reachable: true,
								Address: fdbv1beta2.ProcessAddress{
									IPAddress: net.ParseIP("192.168.0.5"),
									Port:      4500,
								},
							},
						},
					},
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
						RedundancyMode: fdbv1beta2.RedundancyModeTriple,
					},
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
							{
								Primary: true,
								State: fdbv1beta2.FoundationDBStatusDataState{
									Healthy:              true,
									MinReplicasRemaining: 3,
								},
							},
						},
					},
					Logs: []fdbv1beta2.FoundationDBStatusLogInfo{
						{
							LogFaultTolerance:    2,
							LogReplicationFactor: 3,
						},
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
			false)
	})

	When("performing the default safety check.", func() {
		DescribeTable("should return if the safety check is satisfied or not",
			func(status *fdbv1beta2.FoundationDBStatus, maximumActiveGeneration int, expected error) {
				err := DefaultSafetyChecks(status, maximumActiveGeneration, "test")
				if expected == nil {
					Expect(err).To(BeNil())
				} else {
					Expect(err).To(HaveOccurred())
					Expect(err).To(Equal(expected))
				}
			},
			Entry("cluster is fully reconciled",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				1,
				nil,
			),
			Entry("cluster is unavailable",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				1,
				fmt.Errorf("cluster is unavailable, cannot test"),
			),
			Entry("cluster has too many active generations",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 10,
						},
					},
				},
				1,
				fmt.Errorf("cluster has 10 active generations, but only 1 active generations are allowed to safely test"),
			),
			Entry("cluster has more than one active generations",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 10,
						},
					},
				},
				10,
				nil,
			),
		)
	})

	When("performing the exclude safety check.", func() {
		DescribeTable("should return if the safety check is satisfied or not",
			func(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, expected error) {
				err := CanSafelyExcludeProcessesWithRecoveryState(cluster, status, 120.0)
				if expected == nil {
					Expect(err).To(BeNil())
				} else {
					Expect(err).To(HaveOccurred())
					Expect(err).To(Equal(expected))
				}
			},
			Entry("cluster is fully reconciled",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				nil,
			),
			Entry("cluster is unavailable",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				fmt.Errorf("cluster is unavailable, cannot exclude processes"),
			),
			Entry("cluster has more than one active generations",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 10,
						},
					},
				},
				nil,
			),
			Entry("cluster has more than one active generations",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 11,
						},
					},
				},
				fmt.Errorf("cluster has 11 active generations, but only 10 active generations are allowed to safely exclude processes"),
			),
			Entry("cluster's last recovery is 10 seconds ago",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.57",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations:         1,
							SecondsSinceLastRecovered: 10.0,
						},
					},
				},
				fmt.Errorf("cannot: exclude processes, clusters last recovery was 10.00 seconds ago, wait until the last recovery was 120 seconds ago"),
			),
			Entry("cluster's last recovery is 120 seconds ago",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.57",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations:         1,
							SecondsSinceLastRecovered: 120.0,
						},
					},
				},
				nil,
			),
		)
	})

	When("performing the include safety check.", func() {
		DescribeTable("should return if the safety check is satisfied or not",
			func(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, expected error) {
				err := CanSafelyIncludeProcesses(cluster, status, 300.0)
				if expected == nil {
					Expect(err).To(BeNil())
				} else {
					Expect(err).To(HaveOccurred())
					Expect(err).To(Equal(expected))
				}
			},
			Entry("cluster is fully reconciled",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
							Name:              "fully_recovered",
						},
					},
				},
				nil,
			),
			Entry("cluster is not fully reconciled",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
							Name:              "recovery_transaction",
						},
					},
				},
				fmt.Errorf("cannot: include processes, cluster recovery state is recovery_transaction, but it must be \"fully_recovered\" or \"all_logs_recruited\""),
			),
			Entry("cluster is unavailable",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
							Name:              "fully_recovered",
						},
					},
				},
				fmt.Errorf("cluster is unavailable, cannot include processes"),
			),
			Entry("cluster has ten or less active generations",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 10,
							Name:              "fully_recovered",
						},
					},
				},
				nil,
			),
			Entry("cluster has more than ten active generations",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.20",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 11,
							Name:              "fully_recovered",
						},
					},
				},
				fmt.Errorf("cluster has 11 active generations, but only 10 active generations are allowed to safely include processes"),
			),
			Entry("cluster's last recovery is 10 seconds ago",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.57",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations:         1,
							SecondsSinceLastRecovered: 10.0,
							Name:                      "fully_recovered",
						},
					},
				},
				fmt.Errorf("cannot: include processes, clusters last recovery was 10.00 seconds ago, wait until the last recovery was 300 seconds ago"),
			),
			Entry("cluster's last recovery is 320 seconds ago",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.57",
					},
				},
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations:         1,
							SecondsSinceLastRecovered: 320.0,
							Name:                      "fully_recovered",
						},
					},
				},
				nil,
			),
		)
	})

	When("performing the bounce safety check.", func() {
		DescribeTable("should return if the safety check is satisfied or not",
			func(status *fdbv1beta2.FoundationDBStatus, currentUptime float64, minimumUptime float64, expected error) {
				err := CanSafelyBounceProcesses(currentUptime, minimumUptime, status)
				if expected == nil {
					Expect(err).NotTo(HaveOccurred())
				} else {
					Expect(err).To(HaveOccurred())
					Expect(err).To(Equal(expected))
				}
			},
			Entry("cluster is fully reconciled",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						BounceImpact: fdbv1beta2.FoundationDBBounceImpact{
							CanCleanBounce: pointer.Bool(true),
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				42.0,
				10.0,
				nil,
			),
			Entry("cluster is unavailable",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: false,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				42.0,
				10.0,
				fmt.Errorf("cluster is unavailable, cannot bounce processes"),
			),
			Entry("cluster has more than one active generations",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 10,
						},
					},
				},
				42.0,
				10.0,
				nil,
			),
			Entry("cluster has more than one active generations",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 11,
						},
					},
				},
				42.0,
				10.0,
				fmt.Errorf("cluster has 11 active generations, but only 10 active generations are allowed to safely bounce processes"),
			),
			Entry("cluster is not up for long enough",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
						},
					},
				},
				42.0,
				60.0,
				fmt.Errorf("cluster has only been up for 42.00 seconds, but must be up for 60.00 seconds to safely bounce"),
			),
			Entry("cluster cannot clean bounce",
				&fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						BounceImpact: fdbv1beta2.FoundationDBBounceImpact{
							CanCleanBounce: pointer.Bool(false),
						},
						RecoveryState: fdbv1beta2.RecoveryState{
							ActiveGenerations: 1,
							Name:              "exploding",
						},
					},
				},
				42.0,
				10.0,
				fmt.Errorf("cannot perform a clean bounce based on cluster status, current recovery state: exploding"),
			),
		)
	})

	DescribeTable("when testing if a configuration change is allowed", func(status *fdbv1beta2.FoundationDBStatus, useRecoveryState bool, expected error) {
		err := ConfigurationChangeAllowed(status, useRecoveryState)
		if expected == nil {
			Expect(err).NotTo(HaveOccurred())
			return
		}

		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(expected))
	},
		Entry(
			"status is healthy",
			&fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						State: fdbv1beta2.FoundationDBStatusDataState{
							Healthy: true,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 120.0,
					},
				},
			},
			true,
			nil,
		),
		Entry(
			"data status is unhealthy",
			&fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						State: fdbv1beta2.FoundationDBStatusDataState{
							Healthy: false,
							Name:    "primary",
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 120.0,
					},
				},
			},
			true,
			fmt.Errorf("data distribution is not healhty: primary"),
		),

		Entry(
			"last recovery is only 5 seconds ago",
			&fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						State: fdbv1beta2.FoundationDBStatusDataState{
							Healthy: true,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 5.0,
					},
				},
			},
			true,
			fmt.Errorf("clusters last recovery was 5.00 seconds ago, wait until the last recovery was 60 seconds ago"),
		),
		Entry(
			"status contains error message",
			&fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Messages: []fdbv1beta2.FoundationDBStatusMessage{
						{
							Name: "unreadable_configuration",
						},
					},
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						State: fdbv1beta2.FoundationDBStatusDataState{
							Healthy: true,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 120.0,
					},
				},
			},
			true,
			fmt.Errorf("status contains error message: unreadable_configuration"),
		),
		Entry(
			"worst storage lag is increased",
			&fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						State: fdbv1beta2.FoundationDBStatusDataState{
							Healthy: true,
						},
					},
					RecoveryState: fdbv1beta2.RecoveryState{
						SecondsSinceLastRecovered: 120.0,
					},
					Qos: fdbv1beta2.FoundationDBStatusQosInfo{
						WorstDataLagStorageServer: fdbv1beta2.FoundationDBStatusLagInfo{
							Seconds: 61.0,
						},
					},
				},
			},
			true,
			fmt.Errorf("worst data lag is to high, current worst data lag in seconds: 61.00, maximum allowed lag:  60.00"),
		),
	)

	DescribeTable("pretty printing the stored bytes", func(bytes int64, expected string) {
		Expect(PrettyPrintBytes(bytes)).To(Equal(expected))
	},
		Entry("a few bytes", int64(1023), "1023.00"),
		Entry("two KiB", int64(2*1024), "2.00Ki"),
		Entry("two and a half KiB", int64(2*1024+512), "2.50Ki"),
		Entry("three MiB", int64(3*1024*1024), "3.00Mi"),
		Entry("four GiB", int64(4*1024*1024*1024), "4.00Gi"),
		Entry("five TiB", int64(5*1024*1024*1024*1024), "5.00Ti"),
		Entry("six Pib", int64(6*1024*1024*1024*1024*1024), "6.00Pi"),
	)

	DescribeTable("checking the QoS status", func(input *fdbv1beta2.FoundationDBStatus, expected error) {
		result := CheckQosStatus(input)
		if expected == nil {
			Expect(result).To(Succeed())
		} else {
			Expect(result).To(MatchError(expected))
		}
	},
		Entry("all values are below threshold",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Qos: fdbv1beta2.FoundationDBStatusQosInfo{},
				},
			},
			nil,
		),
		Entry("worst data lag storage server is to high",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Qos: fdbv1beta2.FoundationDBStatusQosInfo{
						WorstDataLagStorageServer: fdbv1beta2.FoundationDBStatusLagInfo{
							Seconds: maximumDataLag + 1,
						},
					},
				},
			},
			fmt.Errorf("worst data lag is to high, current worst data lag in seconds: 61.00, maximum allowed lag:  60.00"),
		),
		Entry("worst durability lag storage server is to high",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Qos: fdbv1beta2.FoundationDBStatusQosInfo{
						WorstDurabilityLagStorageServer: fdbv1beta2.FoundationDBStatusLagInfo{
							Seconds: maximumDataLag + 1,
						},
					},
				},
			},
			fmt.Errorf("worst durability lag is to high, current worst durability lag in seconds: 61.00, maximum allowed lag:  60.00"),
		),
		Entry("worst queue bytes for log server is to high",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Qos: fdbv1beta2.FoundationDBStatusQosInfo{
						WorstQueueBytesLogServer: 500 * 1024 * 1024,
					},
				},
			},
			fmt.Errorf("worst queue bytes for log server is to high, current worst queue bytes: 500.00Mi, maximum allowed queue bytes: 250.00Mi"),
		),
		Entry("worst queue bytes for storage server is to high",
			&fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Qos: fdbv1beta2.FoundationDBStatusQosInfo{
						WorstQueueBytesStorageServer: 500 * 1024 * 1024,
					},
				},
			},
			fmt.Errorf("worst queue bytes for storage server is to high, current worst queue bytes: 500.00Mi, maximum allowed queue bytes: 250.00Mi"),
		),
	)
})
