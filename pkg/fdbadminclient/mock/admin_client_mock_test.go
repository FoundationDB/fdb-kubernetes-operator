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
	"context"
	"fmt"
	"net"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

func getCommandlineForProcessFromStatus(status *fdbv1beta2.FoundationDBStatus, processGroupID string) string {
	for _, process := range status.Cluster.Processes {
		locality := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		if locality != processGroupID {
			continue
		}

		return process.CommandLine
	}

	return ""
}

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

	When("changing the commandline arguments", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var adminClient *mockAdminClient
		var initialCommandline string
		var processAddress fdbv1beta2.ProcessAddress
		targetProcess := "storage-1"
		newKnob := "--knob_dummy=1"

		BeforeEach(func() {
			var err error
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

			adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			status, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())

			for _, process := range status.Cluster.Processes {
				locality := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
				if locality != targetProcess {
					continue
				}

				processAddress = process.Address
				break
			}

			initialCommandline = getCommandlineForProcessFromStatus(status, targetProcess)
			Expect(initialCommandline).NotTo(BeEmpty())
			// Update the knobs for storage
			processes := cluster.Spec.Processes
			if processes == nil {
				processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{}
			}
			config := processes[fdbv1beta2.ProcessClassGeneral]
			config.CustomParameters = append(config.CustomParameters, fdbv1beta2.FoundationDBCustomParameter(newKnob))
			processes[fdbv1beta2.ProcessClassGeneral] = config
			cluster.Spec.Processes = processes
			fmt.Println(cluster.Spec.Processes)

			Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
		})

		When("the process is not restarted", func() {
			It("should not update the command line arguments", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				newCommandline := getCommandlineForProcessFromStatus(status, targetProcess)
				Expect(newCommandline).To(Equal(initialCommandline))
				Expect(newCommandline).NotTo(ContainSubstring(newKnob))
			})
		})

		When("the process is restarted", func() {
			It("should update the command line arguments", func() {
				Expect(adminClient.KillProcesses([]fdbv1beta2.ProcessAddress{processAddress})).NotTo(HaveOccurred())
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				newCommandline := getCommandlineForProcessFromStatus(status, targetProcess)
				Expect(newCommandline).NotTo(Equal(initialCommandline))
				Expect(newCommandline).To(ContainSubstring(newKnob))
			})
		})
	})
})
