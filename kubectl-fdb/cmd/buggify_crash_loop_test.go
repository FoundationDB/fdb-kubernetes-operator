/*
 * buggify_crash_loop_test.go
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

	"k8s.io/cli-runtime/pkg/genericclioptions"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[plugin] buggify crash-loop process groups command", func() {
	When("running buggify crash-loop process groups command", func() {
		When("adding process groups to the crash-loop container list", func() {
			type testCase struct {
				ProcessGroups                    []string
				ExpectedProcessGroupsInCrashLoop []fdbv1beta2.ProcessGroupID
			}

			type processGroupOptionsTestCase struct {
				ProcessGroupOpts                 processGroupSelectionOptions
				ExpectedProcessGroupsInCrashLoop map[string][]fdbv1beta2.ProcessGroupID // keyed by cluster-name
			}

			When("the crash-loop container list is empty", func() {
				BeforeEach(func() {
					cluster = generateClusterStruct(clusterName, namespace)
					Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

					secondCluster = generateClusterStruct(secondClusterName, namespace)
					Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
					Expect(createPods(secondClusterName, namespace)).NotTo(HaveOccurred())
				})
				DescribeTable("should add all targeted process groups to crash-loop container list",
					func(tc processGroupOptionsTestCase) {
						Expect(cluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(0))
						cmd := newBuggifyCrashLoop(genericclioptions.IOStreams{})
						opts := buggifyProcessGroupOptions{
							containerName: fdbv1beta2.MainContainerName,
							wait:          false,
							clear:         false,
							clean:         false,
						}
						tc.ProcessGroupOpts.namespace = namespace
						Expect(updateCrashLoopContainerList(cmd, k8sClient, opts, tc.ProcessGroupOpts)).NotTo(HaveOccurred())

						for cluster, processGroupsInCrashLoop := range tc.ExpectedProcessGroupsInCrashLoop {
							var resCluster fdbv1beta2.FoundationDBCluster
							Expect(k8sClient.Get(context.Background(), client.ObjectKey{
								Namespace: namespace,
								Name:      cluster,
							}, &resCluster)).NotTo(HaveOccurred())
							Expect(resCluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
							for _, crashLoopContainerObj := range resCluster.Spec.Buggify.CrashLoopContainers {
								if crashLoopContainerObj.ContainerName != fdbv1beta2.MainContainerName {
									continue
								}
								Expect(processGroupsInCrashLoop).To(ContainElements(crashLoopContainerObj.Targets))
								Expect(crashLoopContainerObj.Targets).To(ContainElements(processGroupsInCrashLoop))
								Expect(processGroupsInCrashLoop).To(HaveLen(len(crashLoopContainerObj.Targets)))
							}
						}
					},
					Entry("Adding single process group.",
						processGroupOptionsTestCase{
							ProcessGroupOpts: processGroupSelectionOptions{
								ids:         []string{"test-storage-1"},
								clusterName: clusterName,
							},
							ExpectedProcessGroupsInCrashLoop: map[string][]fdbv1beta2.ProcessGroupID{
								clusterName: {"test-storage-1"},
							},
						}),
					Entry("Adding multiple process groups.",
						processGroupOptionsTestCase{
							ProcessGroupOpts: processGroupSelectionOptions{
								ids:         []string{"test-storage-1", "test-storage-2"},
								clusterName: clusterName,
							},
							ExpectedProcessGroupsInCrashLoop: map[string][]fdbv1beta2.ProcessGroupID{
								clusterName: {"test-storage-1", "test-storage-2"},
							},
						}),
					Entry("Adding multiple process groups across clusters.",
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
							ExpectedProcessGroupsInCrashLoop: map[string][]fdbv1beta2.ProcessGroupID{
								clusterName: {
									fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage)),
									fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage)),
								},
								secondClusterName: {
									fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", secondClusterName, fdbv1beta2.ProcessClassStorage)),
								},
							},
						}),
					Entry("Adding process groups with ProcessClassStorage.",
						processGroupOptionsTestCase{
							ProcessGroupOpts: processGroupSelectionOptions{
								processClass: string(fdbv1beta2.ProcessClassStorage),
								clusterName:  clusterName,
							},
							ExpectedProcessGroupsInCrashLoop: map[string][]fdbv1beta2.ProcessGroupID{
								clusterName: {
									// the helper function to create pods for the k8s client uses "instance" not "storage" processGroup names
									fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage)),
									fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage)),
								},
							},
						}),
					Entry("Adding process groups with condition MissingProcesses.",
						processGroupOptionsTestCase{
							ProcessGroupOpts: processGroupSelectionOptions{
								clusterName: clusterName,
								conditions:  []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.MissingProcesses},
							},
							ExpectedProcessGroupsInCrashLoop: map[string][]fdbv1beta2.ProcessGroupID{
								clusterName: {
									// the helper function to create pods for the k8s client uses "instance" not "storage" processGroup names
									fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage)),
								},
							},
						}),
				)

			})

			When("the crash-loop container list contains the input container", func() {
				BeforeEach(func() {
					crashLoopContainerObj := fdbv1beta2.CrashLoopContainerObject{
						ContainerName: fdbv1beta2.MainContainerName,
						Targets:       []fdbv1beta2.ProcessGroupID{"test-storage-1"},
					}
					cluster.Spec.Buggify.CrashLoopContainers = append(cluster.Spec.Buggify.CrashLoopContainers, crashLoopContainerObj)
				})

				DescribeTable("should add all targeted process groups to crash-loop container list",
					func(tc testCase) {
						Expect(cluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
						cmd := newBuggifyCrashLoop(genericclioptions.IOStreams{})
						opts := buggifyProcessGroupOptions{
							containerName: fdbv1beta2.MainContainerName,
							wait:          false,
							clear:         false,
							clean:         false,
						}
						processGroupOpts := processGroupSelectionOptions{
							ids:         tc.ProcessGroups,
							clusterName: clusterName,
							namespace:   namespace,
						}
						Expect(updateCrashLoopContainerList(cmd, k8sClient, opts, processGroupOpts)).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						Expect(k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)).NotTo(HaveOccurred())
						Expect(resCluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
						for _, crashLoopContainerObj := range resCluster.Spec.Buggify.CrashLoopContainers {
							if crashLoopContainerObj.ContainerName != fdbv1beta2.MainContainerName {
								continue
							}
							Expect(tc.ExpectedProcessGroupsInCrashLoop).To(ContainElements(crashLoopContainerObj.Targets))
							Expect(tc.ExpectedProcessGroupsInCrashLoop).To(HaveLen(len(crashLoopContainerObj.Targets)))
						}
					},
					Entry("Adding same process group.",
						testCase{
							ProcessGroups:                    []string{"test-storage-1"},
							ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-1"},
						}),
					Entry("Adding single but different process group.",
						testCase{
							ProcessGroups:                    []string{"test-storage-2"},
							ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2"},
						}),
					Entry("Adding multiple process groups.",
						testCase{
							ProcessGroups:                    []string{"test-storage-1", "test-storage-2", "test-storage-3"},
							ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2", "test-storage-3"},
						}),
				)
			})

			When("the crash-loop container list doesn't contains the input container", func() {
				BeforeEach(func() {
					crashLoopContainerObj := fdbv1beta2.CrashLoopContainerObject{
						ContainerName: fdbv1beta2.SidecarContainerName,
						Targets:       []fdbv1beta2.ProcessGroupID{"should-be-ignored-storage-1"},
					}
					cluster.Spec.Buggify.CrashLoopContainers = append(cluster.Spec.Buggify.CrashLoopContainers, crashLoopContainerObj)
					secondCluster.Spec.Buggify.CrashLoopContainers = append(cluster.Spec.Buggify.CrashLoopContainers, crashLoopContainerObj)
				})

				DescribeTable("should add all targeted processes to crash-loop container list",
					func(tc testCase) {
						Expect(cluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
						cmd := newBuggifyCrashLoop(genericclioptions.IOStreams{})
						opts := buggifyProcessGroupOptions{
							containerName: fdbv1beta2.MainContainerName,
							wait:          false,
							clear:         false,
							clean:         false,
						}
						processGroupOpts := processGroupSelectionOptions{
							ids:         tc.ProcessGroups,
							clusterName: clusterName,
							namespace:   namespace,
						}
						Expect(updateCrashLoopContainerList(cmd, k8sClient, opts, processGroupOpts)).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						Expect(k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)).NotTo(HaveOccurred())
						Expect(resCluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(2))
						for _, crashLoopContainerObj := range resCluster.Spec.Buggify.CrashLoopContainers {
							if crashLoopContainerObj.ContainerName != fdbv1beta2.MainContainerName {
								continue
							}
							Expect(tc.ExpectedProcessGroupsInCrashLoop).To(ContainElements(crashLoopContainerObj.Targets))
							Expect(tc.ExpectedProcessGroupsInCrashLoop).To(HaveLen(len(crashLoopContainerObj.Targets)))
						}
					},
					Entry("Adding single process group.",
						testCase{
							ProcessGroups:                    []string{"test-storage-1"},
							ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-1"},
						}),
					Entry("Adding multiple process group.",
						testCase{
							ProcessGroups:                    []string{"test-storage-1", "test-storage-2", "test-storage-3"},
							ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2", "test-storage-3"},
						}),
				)

			})
		})

		When("removing process groups from crash-loop container list", func() {
			type testCase struct {
				ProcessGroups                    []string
				ExpectedProcessGroupsInCrashLoop []fdbv1beta2.ProcessGroupID
			}

			BeforeEach(func() {
				crashLoopContainerObj := fdbv1beta2.CrashLoopContainerObject{
					ContainerName: fdbv1beta2.MainContainerName,
					Targets:       []fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2", "test-storage-3"},
				}
				cluster.Spec.Buggify.CrashLoopContainers = append(cluster.Spec.Buggify.CrashLoopContainers, crashLoopContainerObj)
			})

			DescribeTable("should remove all targeted process groups from crash-loop container list",
				func(tc testCase) {
					Expect(cluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
					cmd := newBuggifyCrashLoop(genericclioptions.IOStreams{})
					opts := buggifyProcessGroupOptions{
						containerName: fdbv1beta2.MainContainerName,
						wait:          false,
						clear:         true,
						clean:         false,
					}
					processGroupOpts := processGroupSelectionOptions{
						ids:         tc.ProcessGroups,
						clusterName: clusterName,
						namespace:   namespace,
					}
					Expect(updateCrashLoopContainerList(cmd, k8sClient, opts, processGroupOpts)).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)).NotTo(HaveOccurred())
					Expect(resCluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
					for _, crashLoopContainerObj := range resCluster.Spec.Buggify.CrashLoopContainers {
						if crashLoopContainerObj.ContainerName != fdbv1beta2.MainContainerName {
							continue
						}
						Expect(tc.ExpectedProcessGroupsInCrashLoop).To(ContainElements(crashLoopContainerObj.Targets))
						Expect(tc.ExpectedProcessGroupsInCrashLoop).To(HaveLen(len(crashLoopContainerObj.Targets)))
					}
				},
				Entry("Removing single process group.",
					testCase{
						ProcessGroups:                    []string{"test-storage-1"},
						ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-2", "test-storage-3"},
					}),
				Entry("Removing multiple process groups.",
					testCase{
						ProcessGroups:                    []string{"test-storage-1", "test-storage-2"},
						ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{"test-storage-3"},
					}),
				Entry("Removing all process groups.",
					testCase{
						ProcessGroups:                    []string{"test-storage-1", "test-storage-2", "test-storage-3"},
						ExpectedProcessGroupsInCrashLoop: []fdbv1beta2.ProcessGroupID{},
					}),
			)
		})

		When("clearing the crash-loop container list", func() {
			BeforeEach(func() {
				crashLoopContainerObj := fdbv1beta2.CrashLoopContainerObject{
					ContainerName: fdbv1beta2.MainContainerName,
					Targets:       []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2", "storage-3"},
				}
				cluster.Spec.Buggify.CrashLoopContainers = append(cluster.Spec.Buggify.CrashLoopContainers, crashLoopContainerObj)
			})

			It("should clear everything from the crash-loop container-list", func() {
				Expect(cluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
				cmd := newBuggifyCrashLoop(genericclioptions.IOStreams{})
				opts := buggifyProcessGroupOptions{
					containerName: fdbv1beta2.MainContainerName,
					wait:          false,
					clear:         false,
					clean:         true,
				}
				processGroupOpts := processGroupSelectionOptions{
					clusterName: clusterName,
					namespace:   namespace,
				}
				Expect(updateCrashLoopContainerList(cmd, k8sClient, opts, processGroupOpts)).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				Expect(k8sClient.Get(context.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)).NotTo(HaveOccurred())
				Expect(resCluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
				for _, crashLoopContainerObj := range resCluster.Spec.Buggify.CrashLoopContainers {
					if crashLoopContainerObj.ContainerName != fdbv1beta2.MainContainerName {
						continue
					}
					Expect(crashLoopContainerObj.Targets).To(HaveLen(0))
				}
			})
			It("should error if no cluster name is provided", func() {
				Expect(cluster.Spec.Buggify.CrashLoopContainers).To(HaveLen(1))
				cmd := newBuggifyCrashLoop(genericclioptions.IOStreams{})
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
				err := updateCrashLoopContainerList(cmd, k8sClient, opts, processGroupOpts)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("clean option requires cluster-name argument"))
			})
		})
	})
})
