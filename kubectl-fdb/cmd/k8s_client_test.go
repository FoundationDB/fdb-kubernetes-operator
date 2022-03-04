/*
 * k8s_client_test.go
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
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] using the Kubernetes client", func() {
	When("fetching processes with conditions", func() {
		clusterName := "test"
		namespace := "test"
		var cluster fdbv1beta2.FoundationDBCluster
		var podList corev1.PodList

		BeforeEach(func() {
			cluster = fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					ProcessCounts: fdb.ProcessCounts{
						Storage: 1,
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "instance-1",
							Addresses:      []string{"1.2.3.4"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
						},
						{
							ProcessGroupID: "instance-2",
							Addresses:      []string{"1.2.3.5"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.IncorrectCommandLine),
							},
						},
						{
							ProcessGroupID: "instance-3",
							Addresses:      []string{"1.2.3.6"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
						},
						{
							ProcessGroupID:   "instance-4",
							Addresses:        []string{"1.2.3.7"},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.IncorrectCommandLine),
							},
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
								fdb.FDBProcessClassLabel:   string(fdb.ProcessClassStorage),
								fdb.FDBClusterLabel:        clusterName,
								fdb.FDBProcessGroupIDLabel: "instance-1",
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "instance-2",
							Namespace: namespace,
							Labels: map[string]string{
								fdb.FDBProcessClassLabel:   string(fdb.ProcessClassStorage),
								fdb.FDBClusterLabel:        clusterName,
								fdb.FDBProcessGroupIDLabel: "instance-2",
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "instance-3",
							Namespace: namespace,
							Labels: map[string]string{
								fdb.FDBProcessClassLabel:   string(fdb.ProcessClassStorage),
								fdb.FDBClusterLabel:        clusterName,
								fdb.FDBProcessGroupIDLabel: "instance-3",
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodFailed,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "instance-4",
							Namespace: namespace,
							Labels: map[string]string{
								fdb.FDBProcessClassLabel:   string(fdb.ProcessClassStorage),
								fdb.FDBClusterLabel:        clusterName,
								fdb.FDBProcessGroupIDLabel: "instance-4",
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
				},
			}
		})

		type testCase struct {
			conditions []fdbv1beta2.ProcessGroupConditionType
			expected   []string
		}

		DescribeTable("should show all deprecations",
			func(tc testCase) {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&cluster, &podList).Build()

				pods, err := getAllPodsFromClusterWithCondition(kubeClient, clusterName, namespace, tc.conditions)
				Expect(err).NotTo(HaveOccurred())
				Expect(pods).To(Equal(tc.expected))
			},
			Entry("No conditions",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{},
					expected:   []string{},
				}),
			Entry("Single condition",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.MissingProcesses},
					expected:   []string{"instance-1"},
				}),
			Entry("Multiple conditions",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.MissingProcesses, fdbv1beta2.IncorrectCommandLine},
					expected:   []string{"instance-1", "instance-2"},
				}),
		)
	})
})
