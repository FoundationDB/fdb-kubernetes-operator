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
	"bytes"
	"context"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] using the Kubernetes client", func() {
	When("fetching processes with conditions", func() {
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
						{
							ProcessGroupID: "instance-5",
							Addresses:      []string{"1.2.3.7"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.SidecarUnreachable),
							},
						},
					},
				},
			}

			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "instance-1",
						Namespace: namespace,
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "instance-1",
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
							fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "instance-2",
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
							fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "instance-3",
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
							fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "instance-4",
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			}

			for _, pod := range pods {
				Expect(k8sClient.Create(context.TODO(), &pod)).NotTo(HaveOccurred())
			}
		})

		type testCase struct {
			conditions           []fdbv1beta2.ProcessGroupConditionType
			expected             []string
			expectedOutputBuffer string
		}

		// The length of the expectedPodNamesMultipleConditions should be equal to the number of processes with the given conditions
		// and not marked for removal
		expectedPodNamesMultipleConditions := make([]string, 0, 3)

		DescribeTable("should show all deprecations",
			func(tc testCase) {
				outBuffer := bytes.Buffer{}
				pods, err := getAllPodsFromClusterWithCondition(&outBuffer, k8sClient, clusterName, namespace, tc.conditions)
				Expect(err).NotTo(HaveOccurred())
				Expect(pods).To(Equal(tc.expected))
				Expect(outBuffer.String()).To(Equal(tc.expectedOutputBuffer))
			},
			Entry("No conditions",
				testCase{
					conditions:           []fdbv1beta2.ProcessGroupConditionType{},
					expected:             []string{},
					expectedOutputBuffer: "",
				}),
			Entry("Single condition",
				testCase{
					conditions:           []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.MissingProcesses},
					expected:             []string{"instance-1"},
					expectedOutputBuffer: "Skipping Process Group: instance-3, Pod is not running, current phase: Failed",
				}),
			Entry("Multiple conditions",
				testCase{
					conditions:           []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.MissingProcesses, fdbv1beta2.IncorrectCommandLine},
					expected:             append(expectedPodNamesMultipleConditions, "instance-2", "instance-1"),
					expectedOutputBuffer: "Skipping Process Group: instance-3, Pod is not running, current phase: Failed",
				}),
			Entry("Single condition and missing pod",
				testCase{
					conditions:           []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.SidecarUnreachable},
					expected:             []string{},
					expectedOutputBuffer: "Process Group: instance-5, is missing pods.",
				}),
		)
	})

	When("getting the process groups IDs from Pods", func() {
		When("the cluster doesn't have a prefix", func() {
			DescribeTable("should get all process groups IDs",
				func(podNames []string, expected []fdbv1beta2.ProcessGroupID) {
					instances, err := getProcessGroupIDsFromPodName(cluster, podNames)
					Expect(err).NotTo(HaveOccurred())
					Expect(instances).To(ContainElements(expected))
					Expect(len(instances)).To(BeNumerically("==", len(expected)))
				},
				Entry("Filter one instance",
					[]string{"test-storage-1"},
					[]fdbv1beta2.ProcessGroupID{"storage-1"},
				),
				Entry("Filter two instances",
					[]string{"test-storage-1", "test-storage-2"},
					[]fdbv1beta2.ProcessGroupID{"storage-1", "storage-2"},
				),
				Entry("Filter no instance",
					[]string{""},
					[]fdbv1beta2.ProcessGroupID{},
				),
			)
		})

		When("the cluster has a prefix", func() {
			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName,
						Namespace: namespace,
					},
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						ProcessGroupIDPrefix: "banana",
						ProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
					},
				}
			})

			DescribeTable("should get all process groups IDs",
				func(podNames []string, expected []fdbv1beta2.ProcessGroupID) {
					instances, err := getProcessGroupIDsFromPodName(cluster, podNames)
					Expect(err).NotTo(HaveOccurred())
					Expect(instances).To(ContainElements(expected))
					Expect(len(instances)).To(BeNumerically("==", len(expected)))
				},
				Entry("Filter one instance",
					[]string{"test-storage-1"},
					[]fdbv1beta2.ProcessGroupID{"banana-storage-1"},
				),
				Entry("Filter two instances",
					[]string{"test-storage-1", "test-storage-2"},
					[]fdbv1beta2.ProcessGroupID{"banana-storage-1", "banana-storage-2"},
				),
				Entry("Filter no instance",
					[]string{""},
					[]fdbv1beta2.ProcessGroupID{},
				),
			)
		})
	})
})
