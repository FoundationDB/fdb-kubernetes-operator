/*
 * remove_process_group_test.go
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
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[plugin] remove process groups command", func() {
	When("running remove process groups command", func() {
		When("removing process groups from a cluster", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessGroupIDPrefix = ""
				cluster.Status = fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-42",
							Addresses:      []string{"1.2.3.4"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
						},
						{
							ProcessGroupID: "storage-1",
						},
					},
				}
			})

			type testCase struct {
				Instances                                 []string
				WithExclusion                             bool
				ExpectedInstancesToRemove                 []fdbv1beta2.ProcessGroupID
				ExpectedInstancesToRemoveWithoutExclusion []fdbv1beta2.ProcessGroupID
				ExpectedProcessCounts                     fdbv1beta2.ProcessCounts
				RemoveAllFailed                           bool
			}

			DescribeTable("should cordon all targeted processes",
				func(tc testCase) {
					cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
					err := replaceProcessGroups(cmd, k8sClient, clusterName, tc.Instances, namespace, replaceProcessGroupsOptions{
						clusterLabel:      "",
						processClass:      "",
						withExclusion:     tc.WithExclusion,
						wait:              false,
						removeAllFailed:   tc.RemoveAllFailed,
						useProcessGroupID: false,
					})
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesToRemove).To(ContainElements(resCluster.Spec.ProcessGroupsToRemove))
					Expect(len(tc.ExpectedInstancesToRemove)).To(BeNumerically("==", len(resCluster.Spec.ProcessGroupsToRemove)))
					Expect(tc.ExpectedInstancesToRemoveWithoutExclusion).To(ContainElements(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion))
					Expect(len(tc.ExpectedInstancesToRemoveWithoutExclusion)).To(BeNumerically("==", len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)))
					Expect(tc.ExpectedProcessCounts.Storage).To(Equal(resCluster.Spec.ProcessCounts.Storage))
				},
				Entry("Remove process group with exclusion",
					testCase{
						Instances:                 []string{"test-storage-1"},
						WithExclusion:             true,
						ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{"storage-1"},
						ExpectedInstancesToRemoveWithoutExclusion: []fdbv1beta2.ProcessGroupID{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: false,
					}),
				Entry("Remove process group without exclusion",
					testCase{
						Instances:                 []string{"test-storage-1"},
						WithExclusion:             false,
						ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{},
						ExpectedInstancesToRemoveWithoutExclusion: []fdbv1beta2.ProcessGroupID{"storage-1"},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
					}),
				Entry("Remove all failed instances",
					testCase{
						Instances:                 []string{"test-storage-42"},
						WithExclusion:             true,
						ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{"storage-42"},
						ExpectedInstancesToRemoveWithoutExclusion: []fdbv1beta2.ProcessGroupID{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: true,
					}),
			)

			When("a process group was already marked for removal", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{"storage-1"}
				})

				When("adding the same process group to the removal list without exclusion", func() {
					It("should add the process group to the removal without exclusion list", func() {
						removals := []string{"test-storage-1"}
						cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
						err := replaceProcessGroups(cmd, k8sClient, clusterName, removals, namespace, replaceProcessGroupsOptions{
							clusterLabel:      "",
							processClass:      "",
							withExclusion:     false,
							wait:              false,
							removeAllFailed:   false,
							useProcessGroupID: false,
						})
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)

						Expect(err).NotTo(HaveOccurred())
						Expect(resCluster.Spec.ProcessGroupsToRemove).To(ContainElements(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(len(resCluster.Spec.ProcessGroupsToRemove)).To(BeNumerically("==", len(removals)))
						Expect(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", len(removals)))
					})
				})

				When("adding the same process group to the removal list", func() {
					It("should add the process group to the removal without exclusion list", func() {
						removals := []string{"test-storage-1"}
						cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
						err := replaceProcessGroups(cmd, k8sClient, clusterName, removals, namespace, replaceProcessGroupsOptions{
							clusterLabel:      "",
							processClass:      "",
							withExclusion:     true,
							wait:              false,
							removeAllFailed:   false,
							useProcessGroupID: false,
						})
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)

						Expect(err).NotTo(HaveOccurred())
						Expect(resCluster.Spec.ProcessGroupsToRemove).To(ContainElements(fdbv1beta2.ProcessGroupID("storage-1")))
						Expect(len(resCluster.Spec.ProcessGroupsToRemove)).To(BeNumerically("==", len(removals)))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", 0))
					})
				})
			})

			When("processes are removed by pod and clusterLabel criteria", func() {
				BeforeEach(func() {
					// creating Pods for first cluster.
					cluster = generateClusterStruct(clusterName, namespace) // the status is overwritten by prior tests
					Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

					// creating a second cluster
					secondCluster = generateClusterStruct(secondClusterName, namespace)
					Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
					Expect(createPods(secondClusterName, namespace)).NotTo(HaveOccurred())
				})

				// since these tests involve multiple clusters, this allows us to more easily check expected counts on
				// a cluster-by-cluster basis
				type clusterData struct {
					ExpectedInstancesToRemove []fdbv1beta2.ProcessGroupID
					ExpectedProcessCounts     fdbv1beta2.ProcessCounts
				}
				type testCase struct {
					podNames          []string
					clusterNameFilter string // if used, then cross-cluster will not work
					clusterLabel      string
					clusterDataMap    map[string]clusterData
					wantErrorContains string
				}

				DescribeTable("should remove specified processes via clusterLabel and podName(s)",
					func(tc testCase) {
						cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
						err := replaceProcessGroups(cmd, k8sClient, tc.clusterNameFilter, tc.podNames, namespace, replaceProcessGroupsOptions{
							clusterLabel:      tc.clusterLabel,
							processClass:      "",
							withExclusion:     true,
							wait:              false,
							removeAllFailed:   false,
							useProcessGroupID: false,
						})
						if tc.wantErrorContains != "" {
							Expect(err).To(Not(BeNil()))
							Expect(err.Error()).To(ContainSubstring(tc.wantErrorContains))
						} else {
							Expect(err).NotTo(HaveOccurred())
						}

						for clusterName, clusterData := range tc.clusterDataMap {
							var resCluster fdbv1beta2.FoundationDBCluster
							err = k8sClient.Get(context.Background(), client.ObjectKey{
								Namespace: namespace,
								Name:      clusterName,
							}, &resCluster)
							Expect(err).NotTo(HaveOccurred())
							Expect(clusterData.ExpectedInstancesToRemove).To(ContainElements(resCluster.Spec.ProcessGroupsToRemove))
							Expect(len(clusterData.ExpectedInstancesToRemove)).To(BeNumerically("==", len(resCluster.Spec.ProcessGroupsToRemove)))
							Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", 0))
							Expect(clusterData.ExpectedProcessCounts.Storage).To(Equal(resCluster.Spec.ProcessCounts.Storage))
						}
					},
					Entry("errors when process group IDs are provided instead of pod names",
						testCase{
							podNames:          []string{"instance-1"},
							clusterLabel:      fdbv1beta2.FDBClusterLabel,
							wantErrorContains: "could not get pod: test/instance-1",
							clusterDataMap: map[string]clusterData{
								clusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
							},
						},
					),
					Entry("errors when no cluster found with given label",
						testCase{
							podNames:          []string{fmt.Sprintf("%s-instance-1", clusterName)},
							clusterLabel:      "invalid-cluster-label",
							wantErrorContains: fmt.Sprintf("no cluster-label 'invalid-cluster-label' found for pod '%s-instance-1'", clusterName),
							clusterDataMap: map[string]clusterData{
								clusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
							},
						},
					),
					Entry("removes valid 1 process group referred to by pod name and cluster-label",
						testCase{
							podNames:     []string{fmt.Sprintf("%s-instance-1", clusterName)},
							clusterLabel: fdbv1beta2.FDBClusterLabel,
							clusterDataMap: map[string]clusterData{
								clusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", clusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
								secondClusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
							},
						},
					),
					Entry("removes two process groups, each on a different cluster",
						testCase{
							podNames:     []string{fmt.Sprintf("%s-instance-1", clusterName), fmt.Sprintf("%s-instance-1", secondClusterName)},
							clusterLabel: fdbv1beta2.FDBClusterLabel,
							clusterDataMap: map[string]clusterData{
								clusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", clusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
								secondClusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", secondClusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
							},
						},
					),
					Entry("removes 3 process groups, on 2 different clusters",
						testCase{
							podNames:     []string{fmt.Sprintf("%s-instance-1", clusterName), fmt.Sprintf("%s-instance-1", secondClusterName), fmt.Sprintf("%s-instance-2", secondClusterName)},
							clusterLabel: fdbv1beta2.FDBClusterLabel,
							clusterDataMap: map[string]clusterData{
								clusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", clusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
								secondClusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", secondClusterName)),
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-2", secondClusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
							},
						},
					),
					Entry("removes 4 process groups, on 2 different clusters",
						testCase{
							podNames: []string{fmt.Sprintf("%s-instance-1", clusterName), fmt.Sprintf("%s-instance-2", clusterName),
								fmt.Sprintf("%s-instance-1", secondClusterName), fmt.Sprintf("%s-instance-2", secondClusterName)},
							clusterLabel: fdbv1beta2.FDBClusterLabel,
							clusterDataMap: map[string]clusterData{
								clusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", clusterName)),
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-2", clusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
								secondClusterName: {
									ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", secondClusterName)),
										fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-2", secondClusterName)),
									},
									ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
										Storage: 1,
									},
								},
							},
						},
					),
				)
			})
			When("processes are specified via processClass", func() {
				BeforeEach(func() {
					cluster = generateClusterStruct(clusterName, namespace) // the status is overwritten by prior tests
					Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

				})
				type testCase struct {
					ids                       []string // should be ignored when processClass is specified
					clusterName               string
					processClass              string
					wantErrorContains         string
					ExpectedInstancesToRemove []fdbv1beta2.ProcessGroupID
				}
				DescribeTable("should remove specified processes via clusterLabel and podName(s)",
					func(tc testCase) {
						cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
						err := replaceProcessGroups(cmd, k8sClient, tc.clusterName, tc.ids, namespace, replaceProcessGroupsOptions{
							clusterLabel:      "",
							processClass:      tc.processClass,
							withExclusion:     true,
							wait:              false,
							removeAllFailed:   false,
							useProcessGroupID: false,
						})
						if tc.wantErrorContains != "" {
							Expect(err).To(Not(BeNil()))
							Expect(err.Error()).To(ContainSubstring(tc.wantErrorContains))
						} else {
							Expect(err).NotTo(HaveOccurred())
						}

						var resCluster fdbv1beta2.FoundationDBCluster
						err = k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(tc.ExpectedInstancesToRemove).To(ContainElements(resCluster.Spec.ProcessGroupsToRemove))
						Expect(len(tc.ExpectedInstancesToRemove)).To(BeNumerically("==", len(resCluster.Spec.ProcessGroupsToRemove)))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", 0))
					},
					Entry("errors when no process groups are found of the given class",
						testCase{
							processClass:              "non-existent",
							wantErrorContains:         fmt.Sprintf("found no processGroups of processClass 'non-existent' in cluster %s", clusterName),
							clusterName:               clusterName,
							ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{},
						},
					),
					Entry("errors when ids are provided along with processClass",
						testCase{
							ids:                       []string{"ignored", "also-ignored"},
							wantErrorContains:         "provided along with a processClass and would be ignored",
							processClass:              string(fdbv1beta2.ProcessClassStateless),
							clusterName:               clusterName,
							ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{},
						},
					),
					Entry("removes singular matching process",
						testCase{
							processClass: string(fdbv1beta2.ProcessClassStateless),
							clusterName:  clusterName,
							ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-3", clusterName)),
							},
						},
					),
					Entry("removes multiple processes that match",
						testCase{
							processClass: string(fdbv1beta2.ProcessClassStorage),
							clusterName:  clusterName,
							ExpectedInstancesToRemove: []fdbv1beta2.ProcessGroupID{
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-1", clusterName)),
								fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-instance-2", clusterName)),
							},
						},
					),
				)
			})
		})
	})
})
