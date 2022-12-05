/*
 * admin_client_mock_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2022 Apple Inc. and the FoundationDB project authors
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

package mock

import (
	"net"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("mock_client", func() {
	When("checking if it's safe to delete a process group", func() {
		type testCase struct {
			cluster    *fdbv1beta2.FoundationDBCluster
			removals   []fdbv1beta2.ProcessAddress
			exclusions []fdbv1beta2.ProcessAddress
			remaining  []fdbv1beta2.ProcessAddress
		}

		DescribeTable("should return the correct image",
			func(input testCase) {
				admin, err := NewMockAdminClient(input.cluster, nil)
				Expect(err).NotTo(HaveOccurred())

				err = admin.ExcludeProcesses(input.exclusions)
				Expect(err).NotTo(HaveOccurred())

				remaining, err := admin.CanSafelyRemove(input.removals)
				Expect(err).NotTo(HaveOccurred())

				Expect(remaining).To(ContainElements(input.remaining))
				Expect(len(remaining)).To(Equal(len(input.remaining)))
			},
			Entry("Empty list of removals",
				testCase{
					cluster:    &fdbv1beta2.FoundationDBCluster{},
					removals:   []fdbv1beta2.ProcessAddress{},
					exclusions: []fdbv1beta2.ProcessAddress{},
					remaining:  []fdbv1beta2.ProcessAddress{},
				}),
			Entry("Process group that skips exclusion",
				testCase{
					cluster: &fdbv1beta2.FoundationDBCluster{
						Status: fdbv1beta2.FoundationDBClusterStatus{
							ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
								{
									Addresses: []string{
										"1.1.1.1:4500",
									},
									ExclusionSkipped: true,
								},
								{
									Addresses: []string{
										"1.1.1.2:4500",
									},
								},
							},
						},
					},
					removals: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.1"),
							Port:      4500,
						},
						{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      4500,
						},
					},
					exclusions: []fdbv1beta2.ProcessAddress{},
					remaining: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      4500,
						},
					},
				}),
			Entry("Process group that is excluded by the client",
				testCase{
					cluster: &fdbv1beta2.FoundationDBCluster{
						Status: fdbv1beta2.FoundationDBClusterStatus{
							ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
								{
									Addresses: []string{
										"1.1.1.1:4500",
									},
								},
								{
									Addresses: []string{
										"1.1.1.2:4500",
									},
								},
							},
						},
					},
					removals: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.1"),
							Port:      4500,
						},
						{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      4500,
						},
					},
					exclusions: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.1"),
							Port:      4500,
						},
					},
					remaining: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      4500,
						},
					},
				}),
			Entry("Process group that is excluded in the cluster status",
				testCase{
					cluster: &fdbv1beta2.FoundationDBCluster{
						Status: fdbv1beta2.FoundationDBClusterStatus{
							ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
								{
									Addresses: []string{
										"1.1.1.1:4500",
									},
									ExclusionTimestamp: &metav1.Time{Time: time.Now()},
								},
								{
									Addresses: []string{
										"1.1.1.2:4500",
									},
								},
							},
						},
					},
					removals: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.1"),
							Port:      4500,
						},
						{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      4500,
						},
					},
					exclusions: []fdbv1beta2.ProcessAddress{},
					remaining: []fdbv1beta2.ProcessAddress{
						{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      4500,
						},
					},
				}),
		)
	})
})
