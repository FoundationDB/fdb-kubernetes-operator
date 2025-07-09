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
	"fmt"
	"strings"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
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
							ProcessGroupID: "storage-1",
							Addresses:      []string{"1.2.3.4"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
						},
						{
							ProcessGroupID: "storage-2",
							Addresses:      []string{"1.2.3.5"},
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(
									fdbv1beta2.IncorrectCommandLine,
								),
							},
						},
						{
							ProcessGroupID: "stateless-3",
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
								fdbv1beta2.NewProcessGroupCondition(
									fdbv1beta2.IncorrectCommandLine,
								),
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
						Name:      "storage-1",
						Namespace: namespace,
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStorage,
							),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "storage-2",
						Namespace: namespace,
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStorage,
							),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "storage-2",
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "stateless-3",
						Namespace: namespace,
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStorage,
							),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "stateless-3",
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
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStorage,
							),
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

		// The length of the expected slices should be equal to the number of processes with the given conditions
		// and not marked for removal
		expectedPodNamesMultipleConditions := make([]string, 0, 3)
		expectedPodNamesMultipleConditionsMissingPods := make([]string, 0, 3)

		DescribeTable("should show all deprecations",
			func(tc testCase) {
				outBuffer := bytes.Buffer{}
				pods, err := getAllPodsFromClusterWithCondition(
					context.Background(),
					&outBuffer,
					k8sClient,
					clusterName,
					namespace,
					tc.conditions,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(pods).Should(ConsistOf(tc.expected))
				Expect(
					strings.Split(outBuffer.String(), "\n"),
				).Should(ConsistOf(strings.Split(tc.expectedOutputBuffer, "\n")))
			},
			Entry("No conditions",
				testCase{
					conditions:           []fdbv1beta2.ProcessGroupConditionType{},
					expected:             []string{},
					expectedOutputBuffer: "",
				}),
			Entry("Single condition",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{
						fdbv1beta2.MissingProcesses,
					},
					expected:             []string{"storage-1"},
					expectedOutputBuffer: "Skipping Process Group: stateless-3, Pod is not running, current phase: Failed\n",
				}),
			Entry("Multiple conditions",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{
						fdbv1beta2.MissingProcesses,
						fdbv1beta2.IncorrectCommandLine,
					},
					expected: append(
						expectedPodNamesMultipleConditions,
						"storage-1",
						"storage-2",
					),
					expectedOutputBuffer: "Skipping Process Group: stateless-3, Pod is not running, current phase: Failed\n",
				}),
			Entry("Single condition and missing pod",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{
						fdbv1beta2.SidecarUnreachable,
					},
					expected:             []string{},
					expectedOutputBuffer: "Skipping Process Group: instance-5, because it does not have a corresponding Pod.\n",
				}),
			Entry("Multiple conditions and missing pod",
				testCase{
					conditions: []fdbv1beta2.ProcessGroupConditionType{
						fdbv1beta2.MissingProcesses,
						fdbv1beta2.SidecarUnreachable,
					},
					expected: append(
						expectedPodNamesMultipleConditionsMissingPods,
						"storage-1",
					),
					expectedOutputBuffer: "Skipping Process Group: instance-5, because it does not have a corresponding Pod.\nSkipping Process Group: stateless-3, Pod is not running, current phase: Failed\n",
				}),
		)
	})

	When("getting the process groups IDs from Pods", func() {
		When("the cluster doesn't have a prefix", func() {
			BeforeEach(func() {
				previousPrefix := cluster.Spec.ProcessGroupIDPrefix + "-"
				cluster.Spec.ProcessGroupIDPrefix = ""

				// Remove the process group prefix from the ID.
				for idx, processGroup := range cluster.Status.ProcessGroups {
					newID := strings.TrimPrefix(string(processGroup.ProcessGroupID), previousPrefix)
					cluster.Status.ProcessGroups[idx].ProcessGroupID = fdbv1beta2.ProcessGroupID(
						newID,
					)
				}
			})

			DescribeTable("should get all process groups IDs",
				func(podNames []string, expected []fdbv1beta2.ProcessGroupID) {
					instances, err := getProcessGroupIDsFromPodName(GinkgoWriter, cluster, podNames)
					Expect(err).NotTo(HaveOccurred())
					Expect(instances).To(ConsistOf(expected))
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
						ProcessGroupIDPrefix: "test",
						ProcessCounts: fdbv1beta2.ProcessCounts{
							Storage: 1,
						},
					},
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "test-storage-1",
							},
							{
								ProcessGroupID: "test-storage-2",
							},
						},
					},
				}
			})

			DescribeTable("should get all process groups IDs",
				func(podNames []string, expected []fdbv1beta2.ProcessGroupID) {
					instances, err := getProcessGroupIDsFromPodName(GinkgoWriter, cluster, podNames)
					Expect(err).NotTo(HaveOccurred())
					Expect(instances).To(ContainElements(expected))
					Expect(len(instances)).To(BeNumerically("==", len(expected)))
				},
				Entry("Filter one instance",
					[]string{"test-storage-1"},
					[]fdbv1beta2.ProcessGroupID{"test-storage-1"},
				),
				Entry("Filter two instances",
					[]string{"test-storage-1", "test-storage-2"},
					[]fdbv1beta2.ProcessGroupID{"test-storage-1", "test-storage-2"},
				),
				Entry("Filter no instance",
					[]string{""},
					[]fdbv1beta2.ProcessGroupID{},
				),
			)
		})
	})
	When("calling getProcessGroupsByCluster", func() {
		BeforeEach(func() {
			// creating Pods for first cluster.
			cluster = generateClusterStruct(
				clusterName,
				namespace,
			) // the status is overwritten by prior tests
			Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

			// creating a second cluster
			secondCluster = generateClusterStruct(secondClusterName, namespace)
			Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
			Expect(createPods(secondClusterName, namespace)).NotTo(HaveOccurred())
		})
		type testCase struct {
			opts            processGroupSelectionOptions
			wantResult      map[string][]fdbv1beta2.ProcessGroupID // cluster-name to processGroups in the cluster
			wantErrContains string
		}
		DescribeTable("correctly follow various options",
			func(tc testCase) {
				tc.opts.namespace = namespace
				cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
				result, err := getProcessGroupsByCluster(cmd, k8sClient, tc.opts)
				if tc.wantErrContains == "" {
					Expect(err).To(BeNil())
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(tc.wantErrContains))
				}
				// ensure that the maps are equivalent, unfortunately involved due to passing cluster by-pointer
				Expect(len(tc.wantResult)).To(Equal(len(result)))
				for wantClusterName, wantProcessGroups := range tc.wantResult {
					foundInResult := false
					for cluster, processGroups := range result {
						if cluster.Name != wantClusterName {
							continue
						}
						foundInResult = true
						Expect(processGroups).To(ContainElements(wantProcessGroups))
						Expect(wantProcessGroups).To(ContainElements(processGroups))
					}
					Expect(foundInResult).To(BeTrue())
				}
			},
			Entry("errors when neither clusterName nor clusterLabel are passed",
				testCase{
					opts:            processGroupSelectionOptions{ids: []string{"something"}},
					wantErrContains: "processGroups will not be selected without cluster specification",
				},
			),
			Entry("errors when ids are passed along with processClass selector",
				testCase{
					opts: processGroupSelectionOptions{
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
						},
						processClass: string(fdbv1beta2.ProcessClassStateless),
						clusterName:  clusterName,
					},
					wantErrContains: "process identifiers were provided along with a processClass (or processConditions) and would be ignored, please only provide one or the other",
				},
			),
			Entry("errors when conditions are passed along with processClass selector",
				testCase{
					opts: processGroupSelectionOptions{
						conditions:   []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.PodFailing},
						processClass: string(fdbv1beta2.ProcessClassStateless),
						clusterName:  clusterName,
					},
					wantErrContains: "selection of processes by both processClass and conditions is not supported at this time",
				},
			),
			Entry("errors when no processGroups are found with the given processClass",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName:  clusterName,
						processClass: string(fdbv1beta2.ProcessClassProxy),
					},
					wantErrContains: "found no processGroups meeting the selection criteria",
				},
			),
			Entry("gets processGroups from podNames and clusterLabel",
				testCase{
					opts: processGroupSelectionOptions{
						clusterLabel: fdbv1beta2.FDBClusterLabel,
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-3",
									clusterName,
									fdbv1beta2.ProcessClassStateless,
								),
							),
						},
					},
				},
			),
			Entry("gets processGroups across clusters from podNames and clusterLabel",
				testCase{
					opts: processGroupSelectionOptions{
						clusterLabel: fdbv1beta2.FDBClusterLabel,
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf(
								"%s-%s-1",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
							fmt.Sprintf(
								"%s-%s-2",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
						},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							),
						},
						secondClusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-1",
									secondClusterName,
									fdbv1beta2.ProcessClassStorage,
								),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-2",
									secondClusterName,
									fdbv1beta2.ProcessClassStorage,
								),
							),
						},
					},
				},
			),
			Entry("gets processGroups from podNames and cluster name",
				testCase{
					opts: processGroupSelectionOptions{
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
						},
						clusterName: clusterName,
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
							),
						},
					},
				},
			),
			Entry("gets processGroups matching processClassStorage",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName:  clusterName,
						processClass: string(fdbv1beta2.ProcessClassStorage),
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
							),
						},
					},
				},
			),
			Entry("gets processGroups matching processClassStateless",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName:  clusterName,
						processClass: string(fdbv1beta2.ProcessClassStateless),
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-3",
									clusterName,
									fdbv1beta2.ProcessClassStateless,
								),
							),
						},
					},
				},
			),
			Entry("gets 2 processGroups matching PodFailing condition",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						conditions:  []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.PodFailing},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
							),
						},
					},
				},
			),
			Entry("gets 1 processGroups matching MissingProcesses condition",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						conditions: []fdbv1beta2.ProcessGroupConditionType{
							fdbv1beta2.MissingProcesses,
						},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
							),
						},
					},
				},
			),
			Entry("gets 0 processGroups matching IncorrectPodSpec condition",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						conditions: []fdbv1beta2.ProcessGroupConditionType{
							fdbv1beta2.IncorrectPodSpec,
						},
					},
					wantResult:      nil,
					wantErrContains: "found no processGroups meeting the selection criteria",
				},
			),
			Entry("gets all processGroups with given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{fdbv1beta2.FDBClusterLabel: clusterName},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-3",
									clusterName,
									fdbv1beta2.ProcessClassStateless,
								),
							),
						},
					},
				},
			),
			Entry("gets 1 processGroup matching given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStateless,
							),
						},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-3",
									clusterName,
									fdbv1beta2.ProcessClassStateless,
								),
							),
						},
					},
				},
			),
			Entry("gets 1 processGroup matching given labels",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStateless,
							),
							fdbv1beta2.FDBProcessGroupIDLabel: fmt.Sprintf(
								"%s-%s-3",
								clusterName,
								fdbv1beta2.ProcessClassStateless,
							),
						},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						clusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-3",
									clusterName,
									fdbv1beta2.ProcessClassStateless,
								),
							),
						},
					},
				},
			),
			Entry("gets 2 processGroups with given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: secondClusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
					wantResult: map[string][]fdbv1beta2.ProcessGroupID{
						secondClusterName: {
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-1",
									secondClusterName,
									fdbv1beta2.ProcessClassStorage,
								),
							),
							fdbv1beta2.ProcessGroupID(
								fmt.Sprintf(
									"%s-%s-2",
									secondClusterName,
									fdbv1beta2.ProcessClassStorage,
								),
							),
						},
					},
				},
			),
			Entry("gets 0 processGroups with given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassProxy),
						},
					},
					wantErrContains: "found no processGroups meeting the selection criteria",
				},
			),
		)
	})
	When("calling getPodNamesByCluster", func() {
		BeforeEach(func() {
			// creating Pods for first cluster.
			cluster = generateClusterStruct(
				clusterName,
				namespace,
			) // the status is overwritten by prior tests
			Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

			// creating a second cluster
			secondCluster = generateClusterStruct(secondClusterName, namespace)
			Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
			Expect(createPods(secondClusterName, namespace)).NotTo(HaveOccurred())
		})
		type testCase struct {
			opts            processGroupSelectionOptions
			wantResult      map[string][]string // cluster-name to pod names in the cluster
			wantErrContains string
		}
		DescribeTable("correctly follow various options",
			func(tc testCase) {
				tc.opts.namespace = namespace
				cmd := newRemoveProcessGroupCmd(genericclioptions.IOStreams{})
				result, err := getPodNamesByCluster(cmd, k8sClient, tc.opts)
				if tc.wantErrContains == "" {
					Expect(err).To(BeNil())
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(tc.wantErrContains))
				}
				// ensure that the maps are equivalent, unfortunately involved due to passing cluster by-pointer
				Expect(len(tc.wantResult)).To(Equal(len(result)))
				for wantClusterName, wantProcessGroups := range tc.wantResult {
					foundInResult := false
					for cluster, processGroups := range result {
						if cluster.Name != wantClusterName {
							continue
						}
						foundInResult = true
						Expect(processGroups).To(ContainElements(wantProcessGroups))
						Expect(wantProcessGroups).To(ContainElements(processGroups))
					}
					Expect(foundInResult).To(BeTrue())
				}
			},
			Entry("errors when neither clusterName nor clusterLabel are passed",
				testCase{
					opts:            processGroupSelectionOptions{ids: []string{"something"}},
					wantErrContains: "podNames will not be selected without cluster specification",
				},
			),
			Entry("errors when ids are passed along with processClass selector",
				testCase{
					opts: processGroupSelectionOptions{
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
						},
						processClass: string(fdbv1beta2.ProcessClassStateless),
						clusterName:  clusterName,
					},
					wantErrContains: "process identifiers were provided along with a processClass (or processConditions) and would be ignored, please only provide one or the other",
				},
			),
			Entry("errors when conditions are passed along with processClass selector",
				testCase{
					opts: processGroupSelectionOptions{
						conditions:   []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.PodFailing},
						processClass: string(fdbv1beta2.ProcessClassStateless),
						clusterName:  clusterName,
					},
					wantErrContains: "by both processClass and conditions is not supported at this time",
				},
			),
			Entry("errors when no pods are found with the given processClass",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName:  clusterName,
						processClass: string(fdbv1beta2.ProcessClassProxy),
					},
					wantErrContains: "found no pods meeting the selection criteria",
				},
			),
			Entry("gets pods from podNames and clusterLabel",
				testCase{
					opts: processGroupSelectionOptions{
						clusterLabel: fdbv1beta2.FDBClusterLabel,
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
				},
			),
			Entry("gets pods across clusters from podNames and clusterLabel",
				testCase{
					opts: processGroupSelectionOptions{
						clusterLabel: fdbv1beta2.FDBClusterLabel,
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf(
								"%s-%s-1",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
							fmt.Sprintf(
								"%s-%s-2",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
						},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
						},
						secondClusterName: {
							fmt.Sprintf(
								"%s-%s-1",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
							fmt.Sprintf(
								"%s-%s-2",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
						},
					},
				},
			),
			Entry("gets pods from podNames and cluster name",
				testCase{
					opts: processGroupSelectionOptions{
						ids: []string{
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
						},
						clusterName: clusterName,
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
						},
					},
				},
			),
			Entry("gets pods matching processClassStorage",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName:  clusterName,
						processClass: string(fdbv1beta2.ProcessClassStorage),
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
						},
					},
				},
			),
			Entry("gets pods matching processClassStateless",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName:  clusterName,
						processClass: string(fdbv1beta2.ProcessClassStateless),
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
				},
			),
			Entry("gets 2 pods matching PodFailing condition",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						conditions:  []fdbv1beta2.ProcessGroupConditionType{fdbv1beta2.PodFailing},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
						},
					},
				},
			),
			Entry("gets 1 pods matching MissingProcesses condition",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						conditions: []fdbv1beta2.ProcessGroupConditionType{
							fdbv1beta2.MissingProcesses,
						},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
						},
					},
				},
			),
			Entry("gets 0 pods matching IncorrectPodSpec condition",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						conditions: []fdbv1beta2.ProcessGroupConditionType{
							fdbv1beta2.IncorrectPodSpec,
						},
					},
					wantResult:      nil,
					wantErrContains: "found no pods meeting the selection criteria",
				},
			),
			Entry("gets all processGroups with given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{fdbv1beta2.FDBClusterLabel: clusterName},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-1", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-2", clusterName, fdbv1beta2.ProcessClassStorage),
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
				},
			),
			Entry("gets 1 processGroup matching given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStateless,
							),
						},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
				},
			),
			Entry("gets 1 processGroup matching given labels",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(
								fdbv1beta2.ProcessClassStateless,
							),
							fdbv1beta2.FDBProcessGroupIDLabel: fmt.Sprintf(
								"%s-%s-3",
								clusterName,
								fdbv1beta2.ProcessClassStateless,
							),
						},
					},
					wantResult: map[string][]string{
						clusterName: {
							fmt.Sprintf("%s-%s-3", clusterName, fdbv1beta2.ProcessClassStateless),
						},
					},
				},
			),
			Entry("gets 2 processGroups with given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: secondClusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
						},
					},
					wantResult: map[string][]string{
						secondClusterName: {
							fmt.Sprintf(
								"%s-%s-1",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
							fmt.Sprintf(
								"%s-%s-2",
								secondClusterName,
								fdbv1beta2.ProcessClassStorage,
							),
						},
					},
				},
			),
			Entry("gets 0 processGroups with given label",
				testCase{
					opts: processGroupSelectionOptions{
						clusterName: clusterName,
						matchLabels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassProxy),
						},
					},
					wantErrContains: "found no pods meeting the selection criteria",
				},
			),
		)
	})
})
