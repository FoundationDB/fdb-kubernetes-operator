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

package statuschecks

import (
	"net"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
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
		}

		DescribeTable("fetching the excluded and remaining processes from the status",
			func(status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress, expectedExcluded []fdbv1beta2.ProcessAddress, expectedRemaining []fdbv1beta2.ProcessAddress, expectedFullyExcluded []fdbv1beta2.ProcessAddress, expectedMissing []fdbv1beta2.ProcessAddress) {
				exclusions := getRemainingAndExcludedFromStatus(status, addresses)
				Expect(expectedExcluded).To(ConsistOf(exclusions.inProgress))
				Expect(expectedRemaining).To(ConsistOf(exclusions.notExcluded))
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
})
