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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] remove instances command", func() {
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

	When("running remove instances command", func() {
		When("getting the instance IDs from Pods", func() {
			var podList corev1.PodList

			BeforeEach(func() {
				podList = corev1.PodList{
					Items: []corev1.Pod{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "instance-1",
								Namespace: namespace,
								Labels: map[string]string{
									fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
									fdbv1beta2.FDBClusterLabel:        clusterName,
									fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "instance-2",
								Namespace: namespace,
								Labels: map[string]string{
									fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
									fdbv1beta2.FDBClusterLabel:        clusterName,
									fdbv1beta2.FDBProcessGroupIDLabel: "storage-2",
								},
							},
						},
					},
				}
			})

			type testCase struct {
				Instances         []string
				ExpectedInstances []string
			}

			DescribeTable("should get all instance IDs",
				func(input testCase) {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()

					instances, err := getProcessGroupIDsFromPod(kubeClient, clusterName, input.Instances, namespace)
					Expect(err).NotTo(HaveOccurred())
					Expect(input.ExpectedInstances).To(Equal(instances))
				},
				Entry("Filter one instance",
					testCase{
						Instances:         []string{"instance-1"},
						ExpectedInstances: []string{"storage-1"},
					}),
				Entry("Filter two instances",
					testCase{
						Instances:         []string{"instance-1", "instance-2"},
						ExpectedInstances: []string{"storage-1", "storage-2"},
					}),
				Entry("Filter no instance",
					testCase{
						Instances:         []string{""},
						ExpectedInstances: []string{},
					}),
			)
		})

		When("removing instances from a cluster", func() {
			var podList corev1.PodList

			BeforeEach(func() {
				cluster.Status = fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "failed",
							Addresses:      []string{"1.2.3.4"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
						},
					},
				}
				podList = corev1.PodList{
					Items: []corev1.Pod{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "instance-1",
								Namespace: namespace,
								Labels: map[string]string{
									fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
									fdbv1beta2.FDBClusterLabel:      clusterName,
								},
							},
						},
					},
				}
			})

			type testCase struct {
				Instances                                 []string
				WithExclusion                             bool
				WithShrink                                bool
				ExpectedInstancesToRemove                 []string
				ExpectedInstancesToRemoveWithoutExclusion []string
				ExpectedProcessCounts                     fdbv1beta2.ProcessCounts
				RemoveAllFailed                           bool
			}

			DescribeTable("should cordon all targeted processes",
				func(tc testCase) {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()

					err := replaceProcessGroups(kubeClient, clusterName, tc.Instances, namespace, tc.WithExclusion, tc.WithShrink, true, tc.RemoveAllFailed)
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
				Entry("Remove instance with exclusion",
					testCase{
						Instances:                 []string{"instance-1"},
						WithExclusion:             true,
						WithShrink:                false,
						ExpectedInstancesToRemove: []string{"instance-1"},
						ExpectedInstancesToRemoveWithoutExclusion: []string{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: false,
					}),
				Entry("Remove instance without exclusion",
					testCase{
						Instances:                 []string{"instance-1"},
						WithExclusion:             false,
						WithShrink:                false,
						ExpectedInstancesToRemove: []string{},
						ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
					}),
				Entry("Remove instance with exclusion and shrink",
					testCase{
						Instances:                 []string{"instance-1"},
						WithExclusion:             true,
						WithShrink:                true,
						ExpectedInstancesToRemove: []string{"instance-1"},
						ExpectedInstancesToRemoveWithoutExclusion: []string{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 0,
						},
						RemoveAllFailed: false,
					}),

				Entry("Remove instance without exclusion and shrink",
					testCase{
						Instances:                 []string{"instance-1"},
						WithExclusion:             false,
						WithShrink:                true,
						ExpectedInstancesToRemove: []string{},
						ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 0,
						},
						RemoveAllFailed: false,
					}),
				Entry("Remove all failed instances",
					testCase{
						Instances:                 []string{"failed"},
						WithExclusion:             true,
						WithShrink:                false,
						ExpectedInstancesToRemove: []string{"failed"},
						ExpectedInstancesToRemoveWithoutExclusion: []string{},
						ExpectedProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: true,
					}),
			)

			When("a procress group was already marked for removal", func() {
				var kubeClient client.Client

				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []string{"instance-1"}
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()
				})

				When("adding the same process group to the removal list without exclusion", func() {
					It("should add the process group to the removal without exclusion list", func() {
						removals := []string{"instance-1"}
						err := replaceProcessGroups(kubeClient, clusterName, removals, namespace, false, false, true, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = kubeClient.Get(ctx.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)

						Expect(err).NotTo(HaveOccurred())
						Expect(resCluster.Spec.ProcessGroupsToRemove).To(ContainElements(removals))
						Expect(len(resCluster.Spec.ProcessGroupsToRemove)).To(BeNumerically("==", len(removals)))
						Expect(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements(removals))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", len(removals)))
					})
				})

				When("adding the same process group to the removal list", func() {
					It("should add the process group to the removal without exclusion list", func() {
						removals := []string{"instance-1"}
						err := replaceProcessGroups(kubeClient, clusterName, removals, namespace, true, false, true, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = kubeClient.Get(ctx.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)

						Expect(err).NotTo(HaveOccurred())
						Expect(resCluster.Spec.ProcessGroupsToRemove).To(ContainElements(removals))
						Expect(len(resCluster.Spec.ProcessGroupsToRemove)).To(BeNumerically("==", len(removals)))
						Expect(len(resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeNumerically("==", 0))
					})
				})
			})
		})
	})
})
