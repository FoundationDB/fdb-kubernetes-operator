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
	ctx "context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] remove process groups command", func() {
	clusterName := "test"
	namespace := "test"

	var cluster *fdbv1beta2.FoundationDBCluster

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
		}
	})

	When("running remove process groups command", func() {
		When("removing process groups from a cluster", func() {
			BeforeEach(func() {
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
				ExpectedInstancesToRemove                 []string
				ExpectedInstancesToRemoveWithoutExclusion []string
				ExpectedProcessCounts                     fdbv1beta2.ProcessCounts
				RemoveAllFailed                           bool
			}

			DescribeTable("should cordon all targeted processes",
				func(tc testCase) {
					kubeClient := getFakeKubeClientWithRuntime(cluster)

					err := replaceProcessGroups(kubeClient, clusterName, tc.Instances, namespace, tc.WithExclusion, false, tc.RemoveAllFailed, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
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
						ExpectedInstancesToRemove: []string{"storage-1"},
						ExpectedInstancesToRemoveWithoutExclusion: []string{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: false,
					}),
				Entry("Remove process group without exclusion",
					testCase{
						Instances:                 []string{"test-storage-1"},
						WithExclusion:             false,
						ExpectedInstancesToRemove: []string{},
						ExpectedInstancesToRemoveWithoutExclusion: []string{"storage-1"},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
					}),
				Entry("Remove all failed instances",
					testCase{
						Instances:                 []string{"test-storage-42"},
						WithExclusion:             true,
						ExpectedInstancesToRemove: []string{"storage-42"},
						ExpectedInstancesToRemoveWithoutExclusion: []string{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: true,
					}),
			)

			When("a process group was already marked for removal", func() {
				var kubeClient client.Client

				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []string{"storage-1"}
					kubeClient = getFakeKubeClientWithRuntime(cluster)
				})

				When("adding the same process group to the removal list without exclusion", func() {
					It("should add the process group to the removal without exclusion list", func() {
						removals := []string{"test-storage-1"}
						err := replaceProcessGroups(kubeClient, clusterName, removals, namespace, false, false, false, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = kubeClient.Get(ctx.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)

						Expect(err).NotTo(HaveOccurred())
						Expect(resCluster.Spec.ProcessGroupsToRemove).To(ContainElements("storage-1"))
						Expect(len(resCluster.Spec.ProcessGroupsToRemove)).To(BeNumerically("==", len(removals)))
						Expect(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements("storage-1"))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", len(removals)))
					})
				})

				When("adding the same process group to the removal list", func() {
					It("should add the process group to the removal without exclusion list", func() {
						removals := []string{"test-storage-1"}
						err := replaceProcessGroups(kubeClient, clusterName, removals, namespace, true, false, false, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = kubeClient.Get(ctx.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)

						Expect(err).NotTo(HaveOccurred())
						Expect(resCluster.Spec.ProcessGroupsToRemove).To(ContainElements("storage-1"))
						Expect(len(resCluster.Spec.ProcessGroupsToRemove)).To(BeNumerically("==", len(removals)))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", 0))
					})
				})
			})
		})
	})
})
