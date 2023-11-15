/*
 * analyze_test.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func getCluster(clusterName string, namespace string, available bool, healthy bool, fullReplication bool, reconciled int64, processGroups []*fdbv1beta2.ProcessGroupStatus) *fdbv1beta2.FoundationDBCluster {
	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			ProcessCounts: fdbv1beta2.ProcessCounts{
				Storage: 1,
			},
		},
		Status: fdbv1beta2.FoundationDBClusterStatus{
			Health: fdbv1beta2.ClusterHealth{
				Available:       available,
				Healthy:         healthy,
				FullReplication: fullReplication,
			},
			Generations: fdbv1beta2.ClusterGenerationStatus{
				Reconciled: reconciled,
			},
			ProcessGroups: processGroups,
		},
	}
}

func getPodList(clusterName string, namespace string, status corev1.PodStatus, deletionTimestamp *metav1.Time) *corev1.PodList {
	return &corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-1",
					Namespace: namespace,
					Labels: map[string]string{
						fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
						fdbv1beta2.FDBClusterLabel:        clusterName,
						fdbv1beta2.FDBProcessGroupIDLabel: "instance-1",
					},
					DeletionTimestamp: deletionTimestamp,
				},
				Status: status,
			},
		},
	}
}

var _ = Describe("[plugin] analyze cluster", func() {
	When("analyzing the cluster", func() {
		type testCase struct {
			cluster           *fdbv1beta2.FoundationDBCluster
			podList           *corev1.PodList
			ExpectedErrMsg    string
			ExpectedStdoutMsg string
			AutoFix           bool
			NoWait            bool
			HasErrors         bool
			IgnoredConditions []string
			IgnoreRemovals    bool
		}

		DescribeTable("return all successful and failed checks",
			func(tc testCase) {
				for _, pod := range tc.podList.Items {
					Expect(k8sClient.Create(context.TODO(), &pod)).NotTo(HaveOccurred())
				}

				// We use these buffers to check the input/output
				outBuffer := bytes.Buffer{}
				errBuffer := bytes.Buffer{}
				inBuffer := bytes.Buffer{}

				cmd := newAnalyzeCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
				err := analyzeCluster(cmd, k8sClient, tc.cluster, tc.AutoFix, tc.NoWait, tc.IgnoredConditions, tc.IgnoreRemovals)

				if err != nil && !tc.HasErrors {
					Expect(err).To(HaveOccurred())
				}

				Expect(strings.TrimSpace(tc.ExpectedErrMsg)).To(Equal(strings.TrimSpace(errBuffer.String())))
				Expect(strings.TrimSpace(tc.ExpectedStdoutMsg)).To(Equal(strings.TrimSpace(outBuffer.String())))
			},
			Entry("Cluster is fine",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      false,
					IgnoreRemovals: true,
				}),
			Entry("Cluster is unavailable",
				testCase{
					cluster: getCluster(clusterName, namespace, false, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Cluster is not available",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Cluster is unhealthy",
				testCase{
					cluster: getCluster(clusterName, namespace, true, false, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Cluster is not fully replicated",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, false, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Cluster is not fully replicated",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Cluster is not reconciled",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Cluster is not reconciled",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("ProcessGroup has a missing process",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "instance-1",
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: fmt.Sprintf("✖ ProcessGroup: instance-1 has the following condition: MissingProcesses since %s", time.Unix(time.Now().Unix(), 0).String()),
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("ProcessGroup has a missing process but is marked for removal",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "instance-1",
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
							},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "⚠ ProcessGroup: instance-1 is marked for removal, excluded state: false",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      false,
					IgnoreRemovals: false,
				}),
			Entry("Pod is in Pending phase",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodPending,
					}, nil),
					ExpectedErrMsg: "✖ Pod test/instance-1 has unexpected Phase Pending with Reason:",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:        false,
					IgnoreRemovals: true,
					HasErrors:      true,
				}),
			Entry("Container is in terminated state",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  fdbv1beta2.MainContainerName,
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode:   137,
										FinishedAt: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
									},
								},
							},
						},
					}, nil),
					ExpectedErrMsg: "✖ Pod test/instance-1 has an unready container: foundationdb",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Container is in ready state",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  fdbv1beta2.MainContainerName,
								Ready: true,
							},
						},
					}, nil),
					ExpectedErrMsg: "",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      false,
					IgnoreRemovals: true,
				}),
			Entry("Pod is stuck in terminating",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase:             corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{},
					}, &metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
					ExpectedErrMsg: fmt.Sprintf("✖ Pod test/instance-1 has been stuck in terminating since %s", time.Unix(time.Now().Add(-1*time.Hour).Unix(), 0).String()),
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Pod is stuck in terminating and marked for removal",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID:     "instance-1",
							RemovalTimestamp:   &metav1.Time{Time: time.Now()},
							ExclusionTimestamp: &metav1.Time{Time: time.Now()},
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "test",
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode:   127,
										FinishedAt: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
									},
								},
							},
						},
					}, &metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
					ExpectedErrMsg: "⚠ ProcessGroup: instance-1 is marked for removal, excluded state: true",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: false,
				}),
			Entry("Pod is stuck in terminating and marked for removal and removals are ignored",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID:     "instance-1",
							RemovalTimestamp:   &metav1.Time{Time: time.Now()},
							ExclusionTimestamp: &metav1.Time{Time: time.Now()},
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "test",
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode:   127,
										FinishedAt: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
									},
								},
							},
						},
					}, &metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
					ExpectedErrMsg: "⚠ Ignored 1 process groups marked for removal",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Missing Pods",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList:        &corev1.PodList{},
					ExpectedErrMsg: "✖ Found no Pods for this cluster",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("Missing entry in status",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Pod test/instance-1 with the ID instance-1 is not part of the cluster spec status",
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:        false,
					HasErrors:      true,
					IgnoreRemovals: true,
				}),
			Entry("ProcessGroup has a two conditions and we ignore one.",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "instance-1",
							ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
								fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.IncorrectPodSpec),
							},
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: fmt.Sprintf("✖ ProcessGroup: instance-1 has the following condition: MissingProcesses since %s\n⚠ Ignored 1 conditions", time.Unix(time.Now().Unix(), 0).String()),
					ExpectedStdoutMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ Pods are all running and available`,
					AutoFix:           false,
					HasErrors:         true,
					IgnoredConditions: []string{string(fdbv1beta2.IncorrectPodSpec)},
					IgnoreRemovals:    true,
				}),
			// TODO: test cases for auto-fix
		)
	})

	When("running analyze without arguments", func() {
		It("should printout the help text", func() {
			outBuffer := bytes.Buffer{}
			errBuffer := bytes.Buffer{}
			inBuffer := bytes.Buffer{}

			rootCmd := NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer}, &MockVersionChecker{})
			rootCmd.SetArgs([]string{"analyze"})

			err := rootCmd.Execute()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("testing if all conditions are valid", func() {
		DescribeTable("return all successful and failed checks",
			func(input []string, expected string) {
				err := allConditionsValid(input)
				var errString string
				if err != nil {
					errString = err.Error()
				}
				Expect(expected).To(Equal(errString))
			},
			Entry("empty condition list",
				[]string{},
				"",
			),
			Entry("valid condition",
				[]string{string(fdbv1beta2.PodPending)},
				"",
			),
			Entry("valid and invalid condition",
				[]string{string(fdbv1beta2.PodPending), "apple pie"},
				"unknown condition: apple pie\n",
			),
			Entry("invalid condition",
				[]string{"apple pie"},
				"unknown condition: apple pie\n",
			),
			Entry("multiple invalid conditions",
				[]string{"apple pie", "banana pie"},
				"unknown condition: apple pie\nunknown condition: banana pie\n",
			),
		)
	})
})
