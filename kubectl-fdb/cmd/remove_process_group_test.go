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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
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

	var cluster *fdbtypes.FoundationDBCluster

	BeforeEach(func() {
		cluster = &fdbtypes.FoundationDBCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Spec: fdbtypes.FoundationDBClusterSpec{
				ProcessCounts: fdbtypes.ProcessCounts{
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
									fdbtypes.FDBProcessClassLabel:      string(fdbtypes.ProcessClassStorage),
									internal.OldFDBProcessClassLabel:   string(fdbtypes.ProcessClassStorage),
									fdbtypes.FDBClusterLabel:           clusterName,
									internal.OldFDBClusterLabel:        clusterName,
									fdbtypes.FDBProcessGroupIDLabel:    "storage-1",
									internal.OldFDBProcessGroupIDLabel: "storage-1",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "instance-2",
								Namespace: namespace,
								Labels: map[string]string{
									fdbtypes.FDBProcessClassLabel:      string(fdbtypes.ProcessClassStorage),
									internal.OldFDBProcessClassLabel:   string(fdbtypes.ProcessClassStorage),
									fdbtypes.FDBClusterLabel:           clusterName,
									internal.OldFDBClusterLabel:        clusterName,
									fdbtypes.FDBProcessGroupIDLabel:    "storage-2",
									internal.OldFDBProcessGroupIDLabel: "storage-2",
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
					_ = fdbtypes.AddToScheme(scheme)
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
				cluster.Status = fdbtypes.FoundationDBClusterStatus{
					ProcessGroups: []*fdbtypes.ProcessGroupStatus{
						{
							ProcessGroupID: "failed",
							Addresses:      []string{"1.2.3.4"},
							ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
								fdbtypes.NewProcessGroupCondition(fdbtypes.MissingProcesses),
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
									fdbtypes.FDBProcessClassLabel:    string(fdbtypes.ProcessClassStorage),
									internal.OldFDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
									fdbtypes.FDBClusterLabel:         clusterName,
									internal.OldFDBClusterLabel:      clusterName,
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
				ExpectedProcessCounts                     fdbtypes.ProcessCounts
				RemoveAllFailed                           bool
			}

			DescribeTable("should cordon all targeted processes",
				func(tc testCase) {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbtypes.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()

					err := replaceProcessGroups(kubeClient, clusterName, tc.Instances, namespace, tc.WithExclusion, tc.WithShrink, true, tc.RemoveAllFailed, true)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbtypes.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesToRemove).To(ContainElements(resCluster.Spec.InstancesToRemove))
					Expect(len(tc.ExpectedInstancesToRemove)).To(BeNumerically("==", len(resCluster.Spec.InstancesToRemove)))
					Expect(tc.ExpectedInstancesToRemoveWithoutExclusion).To(ContainElements(resCluster.Spec.InstancesToRemoveWithoutExclusion))
					Expect(len(tc.ExpectedInstancesToRemoveWithoutExclusion)).To(BeNumerically("==", len(resCluster.Spec.InstancesToRemoveWithoutExclusion)))
					Expect(tc.ExpectedProcessCounts.Storage).To(Equal(resCluster.Spec.ProcessCounts.Storage))
				},
				Entry("Remove instance with exclusion",
					testCase{
						Instances:                 []string{"instance-1"},
						WithExclusion:             true,
						WithShrink:                false,
						ExpectedInstancesToRemove: []string{"instance-1"},
						ExpectedInstancesToRemoveWithoutExclusion: []string{},
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: true,
					}),
			)

			DescribeTable("should cordon all targeted processes",
				func(tc testCase) {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbtypes.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()

					err := replaceProcessGroups(kubeClient, clusterName, tc.Instances, namespace, tc.WithExclusion, tc.WithShrink, true, tc.RemoveAllFailed, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbtypes.FoundationDBCluster
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
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
						ExpectedProcessCounts: fdbtypes.ProcessCounts{
							Storage: 1,
						},
						RemoveAllFailed: true,
					}),
			)
		})
	})
})
