/*
 * check_client_compatibility_test.go
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

package controllers

import (
	"net"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("check client compatibility", func() {
	When("getting the list of unsupported clients from the cluster status json", func() {
		var unsupportedClients []string
		var ignoredLogGroups map[fdbv1beta2.LogGroup]fdbv1beta2.None
		var status *fdbv1beta2.FoundationDBStatus

		BeforeEach(func() {
			status = &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Clients: fdbv1beta2.FoundationDBStatusClusterClientInfo{
						SupportedVersions: []fdbv1beta2.FoundationDBStatusSupportedVersion{
							{
								ClientVersion: "Unknown",
								ConnectedClients: []fdbv1beta2.FoundationDBStatusConnectedClient{
									{
										Address:  "10.1.38.92:52762",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.92:56406",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:43346",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:43354",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:51458",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:51472",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:59442",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:59942",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:60222",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:60230",
										LogGroup: "default",
									},
								},
								MaxProtocolClients: nil,
								ProtocolVersion:    "Unknown",
								SourceVersion:      "Unknown",
							},
							{
								ClientVersion: "6.1.8",
								ConnectedClients: []fdbv1beta2.FoundationDBStatusConnectedClient{
									{
										Address:  "10.1.38.106:35640",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.106:36128",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.106:36802",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.107:42234",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.107:49684",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:47320",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:47388",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:58734",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.103:60222",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:60230",
										LogGroup: "default",
									},
								},
								MaxProtocolClients: nil,
								ProtocolVersion:    "fdb00b061060001",
								SourceVersion:      "bd6b10cbcee08910667194e6388733acd3b80549",
							},
							{
								ClientVersion: "6.2.15",
								ConnectedClients: []fdbv1beta2.FoundationDBStatusConnectedClient{
									{
										Address:  "10.1.38.106:35640",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.106:36128",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.106:36802",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.107:42234",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.107:49684",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:47320",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:47388",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:58734",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.18.249:34874",
										LogGroup: "fdb-kubernetes-operator",
									},
									{
										Address:  "10.1.18.249:35022",
										LogGroup: "fdb-kubernetes-operator",
									},
									{
										Address:  "10.1.38.103:60222",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:60230",
										LogGroup: "default",
									},
								},
								MaxProtocolClients: []fdbv1beta2.FoundationDBStatusConnectedClient{
									{
										Address:  "10.1.38.106:35640",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.106:36128",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.106:36802",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.107:42234",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.107:49684",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:47320",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:47388",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.38.108:58734",
										LogGroup: "sample-cluster-client",
									},
									{
										Address:  "10.1.18.249:34874",
										LogGroup: "fdb-kubernetes-operator",
									},
									{
										Address:  "10.1.18.249:35022",
										LogGroup: "fdb-kubernetes-operator",
									},
									{
										Address:  "10.1.38.103:60222",
										LogGroup: "default",
									},
									{
										Address:  "10.1.38.103:60230",
										LogGroup: "default",
									},
								},
								ProtocolVersion: "fdb00b062010002",
								SourceVersion:   "20566f2ff06a7e822b30e8cfd91090fbd863a393",
							},
						},
					},
				},
			}
		})

		JustBeforeEach(func() {
			unsupportedClients = getUnsupportedClients(status, "fdb00b063010001", ignoredLogGroups)
		})

		AfterEach(func() {
			unsupportedClients = nil
			ignoredLogGroups = nil
		})

		When("no log groups should be ignored", func() {
			It("should show all clients as unsupported", func() {
				Expect(unsupportedClients).To(ConsistOf([]string{
					"10.1.38.106:35640 (sample-cluster-client)",
					"10.1.38.106:36128 (sample-cluster-client)",
					"10.1.38.106:36802 (sample-cluster-client)",
					"10.1.38.107:42234 (sample-cluster-client)",
					"10.1.38.107:49684 (sample-cluster-client)",
					"10.1.38.108:47320 (sample-cluster-client)",
					"10.1.38.108:47388 (sample-cluster-client)",
					"10.1.38.108:58734 (sample-cluster-client)",
					"10.1.18.249:34874 (fdb-kubernetes-operator)",
					"10.1.18.249:35022 (fdb-kubernetes-operator)",
					"10.1.38.103:60222 (default)",
					"10.1.38.103:60230 (default)",
				}))
			})
		})

		When("the fdb-kubernetes-operator log group should be ignored", func() {
			BeforeEach(func() {
				ignoredLogGroups = map[fdbv1beta2.LogGroup]fdbv1beta2.None{"fdb-kubernetes-operator": {}}
			})

			It("should show all clients as unsupported", func() {
				Expect(unsupportedClients).To(ConsistOf([]string{
					"10.1.38.106:35640 (sample-cluster-client)",
					"10.1.38.106:36128 (sample-cluster-client)",
					"10.1.38.106:36802 (sample-cluster-client)",
					"10.1.38.107:42234 (sample-cluster-client)",
					"10.1.38.107:49684 (sample-cluster-client)",
					"10.1.38.108:47320 (sample-cluster-client)",
					"10.1.38.108:47388 (sample-cluster-client)",
					"10.1.38.108:58734 (sample-cluster-client)",
					"10.1.38.103:60222 (default)",
					"10.1.38.103:60230 (default)",
				}))
			})
		})

		When("clients are matching the address of running processes", func() {
			BeforeEach(func() {
				status.Cluster.Processes = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
					"1": {
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("10.1.38.103"),
							Port:      4500,
							Flags: map[string]bool{
								"tls": true,
							},
						},
					},
				}

				It("should ignore clients running on the FoundationDB Pod", func() {
					Expect(unsupportedClients).To(ConsistOf([]string{
						"10.1.38.106:35640 (sample-cluster-client)",
						"10.1.38.106:36128 (sample-cluster-client)",
						"10.1.38.106:36802 (sample-cluster-client)",
						"10.1.38.107:42234 (sample-cluster-client)",
						"10.1.38.107:49684 (sample-cluster-client)",
						"10.1.38.108:47320 (sample-cluster-client)",
						"10.1.38.108:47388 (sample-cluster-client)",
						"10.1.38.108:58734 (sample-cluster-client)",
						"10.1.18.249:34874 (fdb-kubernetes-operator)",
						"10.1.18.249:35022 (fdb-kubernetes-operator)",
					}))
				})

				When("the fdb-kubernetes-operator log group should be ignored", func() {
					BeforeEach(func() {
						ignoredLogGroups = map[fdbv1beta2.LogGroup]fdbv1beta2.None{"fdb-kubernetes-operator": {}}
					})

					It("should show all clients as unsupported", func() {
						Expect(unsupportedClients).To(ConsistOf([]string{
							"10.1.38.106:35640 (sample-cluster-client)",
							"10.1.38.106:36128 (sample-cluster-client)",
							"10.1.38.106:36802 (sample-cluster-client)",
							"10.1.38.107:42234 (sample-cluster-client)",
							"10.1.38.107:49684 (sample-cluster-client)",
							"10.1.38.108:47320 (sample-cluster-client)",
							"10.1.38.108:47388 (sample-cluster-client)",
							"10.1.38.108:58734 (sample-cluster-client)",
						}))
					})
				})
			})
		})
	})
})
