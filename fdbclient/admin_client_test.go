/*
 * admin_client_test.go
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

package fdbclient

import (
	"net"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("admin_client_test", func() {
	Describe("helper methods", func() {
		Describe("parseExclusionOutput", func() {
			It("should map the output description to exclusion success", func() {
				output := "  10.1.56.36(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53(Whole machine)  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35(Whole machine)  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36": "Success",
					"10.1.56.43": "Success",
					"10.1.56.52": "Success",
					"10.1.56.53": "Missing",
					"10.1.56.35": "In Progress",
					"10.1.56.56": "Success",
				}))
			})

			It("should handle a lack of suffices in the output", func() {
				output := "  10.1.56.36  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36": "Success",
					"10.1.56.43": "Success",
					"10.1.56.52": "Success",
					"10.1.56.53": "Missing",
					"10.1.56.35": "In Progress",
					"10.1.56.56": "Success",
				}))
			})

			It("should handle ports in the output", func() {
				output := "  10.1.56.36:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53:4500  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35:4500  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36:4500": "Success",
					"10.1.56.43:4500": "Success",
					"10.1.56.52:4500": "Success",
					"10.1.56.53:4500": "Missing",
					"10.1.56.35:4500": "In Progress",
					"10.1.56.56:4500": "Success",
				}))
			})
		})
	})

	When("getting the excluded and remaining processes", func() {
		addr1 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.1"), "", 0, nil)
		addr2 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.2"), "", 0, nil)
		addr3 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.3"), "", 0, nil)
		addr4 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.4"), "", 0, nil)
		addr5 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.5"), "", 0, nil)

		DescribeTable("fetching the excluded and ramining processes from the status",
			func(status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress, expectedExcluded []fdbv1beta2.ProcessAddress, expectedRemaining []fdbv1beta2.ProcessAddress) {
				excluded, remaining := getRemainingAndExcludedFromStatus(status, addresses)
				Expect(expectedExcluded).To(ContainElements(excluded))
				Expect(len(expectedExcluded)).To(BeNumerically("==", len(excluded)))
				Expect(expectedRemaining).To(ContainElements(remaining))
				Expect(len(expectedRemaining)).To(BeNumerically("==", len(remaining)))
			},
			Entry("with an empty input address slice",
				&fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
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
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{},
				nil,
				nil,
			),
			Entry("when the process is excluded",
				&fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
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
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
			),
			Entry("when the process is not excluded",
				&fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
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
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr3},
				nil,
				[]fdbv1beta2.ProcessAddress{addr3},
			),
			Entry("when some processes are excludeded and some not",
				&fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
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
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr1, addr2, addr3, addr4},
				[]fdbv1beta2.ProcessAddress{addr1, addr4},
				[]fdbv1beta2.ProcessAddress{addr2, addr3},
			),
			Entry("when a process is missing",
				&fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
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
							},
						},
					},
				},
				[]fdbv1beta2.ProcessAddress{addr5},
				[]fdbv1beta2.ProcessAddress{addr5},
				[]fdbv1beta2.ProcessAddress{},
			),
		)
	})
})
