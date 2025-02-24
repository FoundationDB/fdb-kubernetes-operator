/*
 * buggify_no_schedule_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[plugin] buggify no-schedule instances command", func() {
	When("running buggify no-schedule instances command", func() {
		When("adding instances to no-schedule list from a cluster", func() {
			type processGroupOptionsTestCase struct {
				ProcessGroupOpts                  processGroupSelectionOptions
				ExpectedProcessGroupsInNoSchedule map[string][]fdbv1beta2.ProcessGroupID // keyed by cluster-name
			}
			BeforeEach(func() {
				cluster = generateClusterStruct(clusterName, namespace)
				Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

				secondCluster = generateClusterStruct(secondClusterName, namespace)
				Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
				Expect(createPods(secondClusterName, namespace)).NotTo(HaveOccurred())
			})

			DescribeTable("should add all targeted processes to no-schedule list",
				func(tc processGroupOptionsTestCase) {
					cmd := newBuggifyNoSchedule(genericclioptions.IOStreams{})
					tc.ProcessGroupOpts.namespace = namespace
					err := updateNoScheduleList(cmd, k8sClient, buggifyProcessGroupOptions{}, tc.ProcessGroupOpts)
					Expect(err).NotTo(HaveOccurred())

					for cluster, processGroupsInNoSchedule := range tc.ExpectedProcessGroupsInNoSchedule {
						var resCluster fdbv1beta2.FoundationDBCluster
						err = k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      cluster,
						}, &resCluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(processGroupsInNoSchedule).To(ContainElements(resCluster.Spec.Buggify.NoSchedule))
						Expect(resCluster.Spec.Buggify.NoSchedule).To(ContainElements(processGroupsInNoSchedule))
						Expect(len(processGroupsInNoSchedule)).To(BeNumerically("==", len(resCluster.Spec.Buggify.NoSchedule)))
					}
				},
				Entry("Adding single instance.",
					processGroupOptionsTestCase{
						ProcessGroupOpts: processGroupSelectionOptions{
							ids:         []string{"test-storage-1"},
							clusterName: clusterName,
						},
						ExpectedProcessGroupsInNoSchedule: map[string][]fdbv1beta2.ProcessGroupID{
							clusterName: {"test-storage-1"},
						},
					}),
				Entry("Adding multiple instances.",
					processGroupOptionsTestCase{
						ProcessGroupOpts: processGroupSelectionOptions{
							ids:         []string{"test-storage-1", "test-storage-2"},
							clusterName: clusterName,
						},
						ExpectedProcessGroupsInNoSchedule: map[string][]fdbv1beta2.ProcessGroupID{
							clusterName: {"test-storage-1", "test-storage-2"},
						},
					}),
				Entry("adds multiple instances across clusters.",
					processGroupOptionsTestCase{
						ProcessGroupOpts: processGroupSelectionOptions{
							ids: []string{
								// the helper function to create pods for the k8s client uses "instance" not "storage" processGroup names
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
								fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
								fmt.Sprintf("%s-%s-2", secondClusterName, fdbv1beta2.ProcessClassStorage),
							},
							clusterLabel: fdbv1beta2.FDBClusterLabel,
						},
						ExpectedProcessGroupsInNoSchedule: map[string][]fdbv1beta2.ProcessGroupID{
							clusterName: {
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage)),
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage)),
							},
							secondClusterName: {
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", secondClusterName, fdbv1beta2.ProcessClassStorage)),
							},
						},
					}),
				Entry("adds process groups with ProcessClassStorage.",
					processGroupOptionsTestCase{
						ProcessGroupOpts: processGroupSelectionOptions{
							processClass: string(fdbv1beta2.ProcessClassStorage),
							clusterName:  clusterName,
						},
						ExpectedProcessGroupsInNoSchedule: map[string][]fdbv1beta2.ProcessGroupID{
							clusterName: {
								// the helper function to create pods for the k8s client uses "instance" not "storage" processGroup names
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage)),
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage)),
							},
						},
					}),
				Entry("adds process groups with condition MissingProcesses.",
					processGroupOptionsTestCase{
						ProcessGroupOpts: processGroupSelectionOptions{
							clusterName: clusterName,
							conditions:  []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.MissingProcesses},
						},
						ExpectedProcessGroupsInNoSchedule: map[string][]fdbv1beta2.ProcessGroupID{
							clusterName: {
								// the helper function to create pods for the k8s client uses "instance" not "storage" processGroup names
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage)),
							},
						},
					}),
			)

			When("a process group is already in no-schedule", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{"test-storage-1"}
				})

				type testCase struct {
					Instances                     []string
					ExpectedInstancesInNoSchedule []fdbv1beta2.ProcessGroupID
				}

				DescribeTable("should add all targeted processes to no-schedule list",
					func(tc testCase) {
						cmd := newBuggifyNoSchedule(genericclioptions.IOStreams{})
						processGroupOpts := processGroupSelectionOptions{
							ids:         tc.Instances,
							clusterName: clusterName,
							namespace:   namespace,
						}
						err := updateNoScheduleList(cmd, k8sClient, buggifyProcessGroupOptions{}, processGroupOpts)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(tc.ExpectedInstancesInNoSchedule).To(ContainElements(resCluster.Spec.Buggify.NoSchedule))
						Expect(len(tc.ExpectedInstancesInNoSchedule)).To(BeNumerically("==", len(resCluster.Spec.Buggify.NoSchedule)))
					},
					Entry("Adding the same instance.",
						testCase{
							Instances:                     []string{"test-storage-1"},
							ExpectedInstancesInNoSchedule: []fdbv1beta2.ProcessGroupID{"test-storage-1"},
						}),
					Entry("Adding different instance.",
						testCase{
							Instances:                     []string{"test-storage-2"},
							ExpectedInstancesInNoSchedule: []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2"},
						}),
					Entry("Adding multiple instances.",
						testCase{
							Instances:                     []string{"test-storage-2", "test-storage-3"},
							ExpectedInstancesInNoSchedule: []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2", "test-storage-3"},
						}),
				)
			})
		})

		When("removing process group from no-schedule list from a cluster", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2", "test-storage-3"}
			})

			type testCase struct {
				Instances                     []string
				ExpectedInstancesInNoSchedule []fdbv1beta2.ProcessGroupID
			}

			DescribeTable("should remove all targeted processes from the no-schedule list",
				func(tc testCase) {
					cmd := newBuggifyNoSchedule(genericclioptions.IOStreams{})
					processGroupOpts := processGroupSelectionOptions{
						ids:         tc.Instances,
						clusterName: clusterName,
						namespace:   namespace,
					}
					opts := buggifyProcessGroupOptions{
						wait:  false,
						clear: true,
						clean: false,
					}
					err := updateNoScheduleList(cmd, k8sClient, opts, processGroupOpts)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesInNoSchedule).To(Equal(resCluster.Spec.Buggify.NoSchedule))
					Expect(len(tc.ExpectedInstancesInNoSchedule)).To(BeNumerically("==", len(resCluster.Spec.Buggify.NoSchedule)))
				},
				Entry("Removing single instance.",
					testCase{
						Instances:                     []string{"test-storage-1"},
						ExpectedInstancesInNoSchedule: []fdbv1beta2.ProcessGroupID{"test-storage-2", "test-storage-3"},
					}),
				Entry("Removing multiple instances.",
					testCase{
						Instances:                     []string{"test-storage-2", "test-storage-3"},
						ExpectedInstancesInNoSchedule: []fdbv1beta2.ProcessGroupID{"test-storage-1"},
					}),
			)

		})

		When("clearing no-schedule list", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2", "storage-3"}
			})

			It("should clear the no-schedule list", func() {
				cmd := newBuggifyNoSchedule(genericclioptions.IOStreams{})
				processGroupOpts := processGroupSelectionOptions{
					clusterName: clusterName,
					namespace:   namespace,
				}
				opts := buggifyProcessGroupOptions{
					wait:  false,
					clear: false,
					clean: true,
				}
				err := updateNoScheduleList(cmd, k8sClient, opts, processGroupOpts)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resCluster.Spec.Buggify.NoSchedule)).To(Equal(0))
			})
			It("should error if no cluster name is provided", func() {
				cmd := newBuggifyNoSchedule(genericclioptions.IOStreams{})
				opts := buggifyProcessGroupOptions{
					containerName: fdbv1beta2.MainContainerName,
					wait:          false,
					clear:         false,
					clean:         true,
				}
				processGroupOpts := processGroupSelectionOptions{
					clusterName:  "",
					clusterLabel: "attempt-cross-cluster-clean",
					namespace:    namespace,
				}
				err := updateNoScheduleList(cmd, k8sClient, opts, processGroupOpts)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("clean option requires cluster-name argument"))
			})
		})
	})
})
