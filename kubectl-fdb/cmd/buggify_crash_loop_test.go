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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[plugin] buggify crash-loop instances command", func() {
	When("running buggify crash-loop instances command", func() {
		When("adding instances to crash-loop list from a cluster", func() {
			type testCase struct {
				Instances                    []string
				ExpectedInstancesInCrashLoop []fdbv1beta2.ProcessGroupID
			}

			DescribeTable("should add all targeted processes to crash-loop list",
				func(tc testCase) {
					err := updateCrashLoopList(k8sClient, clusterName, tc.Instances, namespace, false, false, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesInCrashLoop).To(ContainElements(resCluster.Spec.Buggify.CrashLoop))
					Expect(len(tc.ExpectedInstancesInCrashLoop)).To(BeNumerically("==", len(resCluster.Spec.Buggify.CrashLoop)))
				},
				Entry("Adding single instance.",
					testCase{
						Instances:                    []string{"test-storage-1"},
						ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-1"},
					}),
				Entry("Adding multiple instances.",
					testCase{
						Instances:                    []string{"test-storage-1", "test-storage-2"},
						ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2"},
					}),
			)

			When("a process group was already in crash-loop", func() {
				type testCase struct {
					Instances                    []string
					ExpectedInstancesInCrashLoop []fdbv1beta2.ProcessGroupID
				}

				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1"}
				})

				DescribeTable("should add all targeted processes to crash-loop list",
					func(tc testCase) {
						err := updateCrashLoopList(k8sClient, clusterName, tc.Instances, namespace, false, false, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = k8sClient.Get(context.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(tc.ExpectedInstancesInCrashLoop).To(ContainElements(resCluster.Spec.Buggify.CrashLoop))
						Expect(len(tc.ExpectedInstancesInCrashLoop)).To(BeNumerically("==", len(resCluster.Spec.Buggify.CrashLoop)))
					},
					Entry("Adding the same instance.",
						testCase{
							Instances:                    []string{"test-storage-1"},
							ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-1"},
						}),
					Entry("Adding different instance.",
						testCase{
							Instances:                    []string{"test-storage-2"},
							ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2"},
						}),
					Entry("Adding multiple instances.",
						testCase{
							Instances:                    []string{"test-storage-2", "test-storage-3"},
							ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2", "storage-3"},
						}),
				)
			})
		})

		When("removing instances from crash-loop list from a cluster", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2", "storage-3"}
			})

			type testCase struct {
				Instances                    []string
				ExpectedInstancesInCrashLoop []fdbv1beta2.ProcessGroupID
			}

			DescribeTable("should remove all targeted processes from the crash-loop list",
				func(tc testCase) {
					err := updateCrashLoopList(k8sClient, clusterName, tc.Instances, namespace, false, true, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesInCrashLoop).To(ContainElements(resCluster.Spec.Buggify.CrashLoop))
					Expect(len(tc.ExpectedInstancesInCrashLoop)).To(BeNumerically("==", len(resCluster.Spec.Buggify.CrashLoop)))
				},
				Entry("Removing single instance.",
					testCase{
						Instances:                    []string{"test-storage-1"},
						ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-2", "storage-3"},
					}),
				Entry("Removing multiple instances.",
					testCase{
						Instances:                    []string{"test-storage-2", "test-storage-3"},
						ExpectedInstancesInCrashLoop: []fdbv1beta2.ProcessGroupID{"storage-1"},
					}),
			)
		})

		When("clearing crash-loop list", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2", "storage-3"}
			})

			It("should clear the crash-loop list", func() {
				err := updateCrashLoopList(k8sClient, clusterName, []string{}, namespace, false, false, true)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resCluster.Spec.Buggify.CrashLoop)).To(Equal(0))
			})
		})
	})
})
