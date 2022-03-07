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
	"fmt"
	"net"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

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

	When("Removing warnings in JSON", func() {
		type testCase struct {
			input       string
			expected    string
			expectedErr error
		}

		DescribeTable("Test remove warnings in JSON string",
			func(tc testCase) {
				result, err := removeWarningsInJSON(tc.input)
				// We need the if statement to make ginkgo happy:
				//   Refusing to compare <nil> to <nil>.
				//   Be explicit and use BeNil() instead.
				//   This is to avoid mistakes where both sides of an assertion are erroneously uninitialized.
				// ¯\_(ツ)_/¯
				if tc.expectedErr == nil {
					Expect(err).To(BeNil())
				} else {
					Expect(err).To(Equal(tc.expectedErr))
				}
				Expect(result).To(Equal(tc.expected))
			},
			Entry("Valid JSON without warning",
				testCase{
					input:       "{}",
					expected:    "{}",
					expectedErr: nil,
				},
			),
			Entry("Valid JSON with warning",
				testCase{
					input: `
 # Warning Slow response
 
 {}`,
					expected:    "{}",
					expectedErr: nil,
				},
			),
			Entry("Invalid JSON",
				testCase{
					input:       "}",
					expected:    "",
					expectedErr: fmt.Errorf("the JSON string doesn't contain a starting '{'"),
				},
			),
		)
	})

	When("getting the excluded and remaining processes", func() {
		addr1 := fdbtypes.NewProcessAddress(net.ParseIP("127.0.0.1"), "", 0, nil)
		addr2 := fdbtypes.NewProcessAddress(net.ParseIP("127.0.0.2"), "", 0, nil)
		addr3 := fdbtypes.NewProcessAddress(net.ParseIP("127.0.0.3"), "", 0, nil)
		addr4 := fdbtypes.NewProcessAddress(net.ParseIP("127.0.0.4"), "", 0, nil)
		addr5 := fdbtypes.NewProcessAddress(net.ParseIP("127.0.0.5"), "", 0, nil)

		DescribeTable("fetching the excluded and remaining processes from the status",
			func(status *fdbtypes.FoundationDBStatus, addresses []fdbtypes.ProcessAddress, expectedExcluded []fdbtypes.ProcessAddress, expectedRemaining []fdbtypes.ProcessAddress) {
				excluded, remaining := getRemainingAndExcludedFromStatus(status, addresses)
				Expect(expectedExcluded).To(ContainElements(excluded))
				Expect(len(expectedExcluded)).To(BeNumerically("==", len(excluded)))
				Expect(expectedRemaining).To(ContainElements(remaining))
				Expect(len(expectedRemaining)).To(BeNumerically("==", len(remaining)))
			},
			Entry("with an empty input address slice",
				&fdbtypes.FoundationDBStatus{
					Cluster: fdbtypes.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
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
				[]fdbtypes.ProcessAddress{},
				nil,
				nil,
			),
			Entry("when the process is excluded",
				&fdbtypes.FoundationDBStatus{
					Cluster: fdbtypes.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
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
				[]fdbtypes.ProcessAddress{addr4},
				[]fdbtypes.ProcessAddress{addr4},
				nil,
			),
			Entry("when the process is not excluded",
				&fdbtypes.FoundationDBStatus{
					Cluster: fdbtypes.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
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
				[]fdbtypes.ProcessAddress{addr3},
				nil,
				[]fdbtypes.ProcessAddress{addr3},
			),
			Entry("when some processes are excluded and some not",
				&fdbtypes.FoundationDBStatus{
					Cluster: fdbtypes.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
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
				[]fdbtypes.ProcessAddress{addr1, addr2, addr3, addr4},
				[]fdbtypes.ProcessAddress{addr1, addr4},
				[]fdbtypes.ProcessAddress{addr2, addr3},
			),
			Entry("when a process is missing",
				&fdbtypes.FoundationDBStatus{
					Cluster: fdbtypes.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
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
				[]fdbtypes.ProcessAddress{addr5},
				[]fdbtypes.ProcessAddress{addr5},
				[]fdbtypes.ProcessAddress{},
			),
		)
	})
})
