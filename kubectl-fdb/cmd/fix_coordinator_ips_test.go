/*
 * fix_coordinator_ips_test.go
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

package cmd

import (
	"context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[plugin] fix-coordinator-ips command", func() {
	When("building cluster file update commands", func() {
		type testCase struct {
			Context          string
			ExpectedCommands [][]string
			ExpectedError    string
		}

		BeforeEach(func() {
			Expect(k8sClient.Create(context.TODO(), &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-1",
					Namespace: namespace,
					Labels: map[string]string{
						fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						fdbv1beta2.FDBClusterLabel:      clusterName,
					},
				},
			})).NotTo(HaveOccurred())
		})

		DescribeTable("should execute the provided command",
			func(input testCase) {
				cluster.Status.ConnectionString = "test:test@127.0.0.1:4501"
				commands, err := buildClusterFileUpdateCommands(cluster, k8sClient, input.Context, namespace, "/usr/local/bin/kubectl")

				if input.ExpectedError != "" {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal(input.ExpectedError))
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(commands).To(HaveLen(len(input.ExpectedCommands)))
					for index, command := range commands {
						Expect(command.Args).To(Equal(input.ExpectedCommands[index]))
					}
				}
			},
			Entry("instance with valid pod",
				testCase{
					ExpectedCommands: [][]string{
						{
							"/usr/local/bin/kubectl",
							"--namespace",
							"test",
							"exec",
							"-it",
							"-c",
							fdbv1beta2.MainContainerName,
							"instance-1",
							"--",
							"bash",
							"-c",
							"echo test:test@127.0.0.1:4501 > /var/fdb/data/fdb.cluster && pkill fdbserver",
						},
					},
				}),
			Entry("instance with explicit context",
				testCase{
					Context: "remote-kc",
					ExpectedCommands: [][]string{
						{
							"/usr/local/bin/kubectl",
							"--namespace",
							"test",
							"--context",
							"remote-kc",
							"exec",
							"-it",
							"-c",
							fdbv1beta2.MainContainerName,
							"instance-1",
							"--",
							"bash",
							"-c",
							"echo test:test@127.0.0.1:4501 > /var/fdb/data/fdb.cluster && pkill fdbserver",
						},
					},
				},
			),
		)
	})
	When("updating the connection string", func() {
		type testCase struct {
			Context                  string
			ExpectedConnectionString string
			ExpectedError            string
			AddressUpdates           map[string]string
		}

		BeforeEach(func() {
			cluster = &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					ProcessCounts: fdbv1beta2.ProcessCounts{
						Storage: 1,
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ConnectionString: "test:asdfkjh@127.0.0.1:4501,127.0.0.2:4501,127.0.0.3:4501",
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "storage-1", Addresses: []string{"127.0.0.1"}},
						{ProcessGroupID: "storage-2", Addresses: []string{"127.0.0.2"}},
						{ProcessGroupID: "storage-3", Addresses: []string{"127.0.0.3"}},
						{ProcessGroupID: "storage-4", Addresses: []string{"127.0.0.4"}},
						{ProcessGroupID: "storage-5", Addresses: []string{"127.0.0.5"}},
					},
				},
			}
		})

		DescribeTable("should execute the provided command",
			func(input testCase) {
				for processGroupID, address := range input.AddressUpdates {
					for _, processGroup := range cluster.Status.ProcessGroups {
						if processGroup.ProcessGroupID == processGroupID {
							if address == "" {
								processGroup.Addresses = nil
							} else {
								processGroup.Addresses = append(processGroup.Addresses, address)
							}
						}
					}
				}
				err := updateIPsInConnectionString(cluster)

				if input.ExpectedError != "" {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal(input.ExpectedError))
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.ConnectionString).To(Equal(input.ExpectedConnectionString))
				}
			},
			Entry("healthy cluster",
				testCase{
					ExpectedConnectionString: "test:asdfkjh@127.0.0.1:4501,127.0.0.2:4501,127.0.0.3:4501",
				},
			),
			Entry("updated address",
				testCase{
					AddressUpdates: map[string]string{
						"storage-1": "127.0.1.1",
						"storage-2": "127.0.1.2",
						"storage-5": "127.0.1.5",
					},
					ExpectedConnectionString: "test:asdfkjh@127.0.1.1:4501,127.0.1.2:4501,127.0.0.3:4501",
				},
			),
			Entry("IP address with no process group",
				testCase{
					AddressUpdates: map[string]string{
						"storage-1": "",
					},
					ExpectedConnectionString: "test:asdfkjh@127.0.0.1:4501,127.0.0.2:4501,127.0.0.3:4501",
				},
			),
		)
	})
})
