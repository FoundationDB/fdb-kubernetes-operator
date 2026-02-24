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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func getCluster(
	clusterName string,
	namespace string,
	available bool,
	healthy bool,
	fullReplication bool,
	reconciled int64,
	processGroups []*fdbv1beta2.ProcessGroupStatus,
) *fdbv1beta2.FoundationDBCluster {
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

func getPodList(
	clusterName string,
	namespace string,
	status corev1.PodStatus,
	deletionTimestamp *metav1.Time,
) *corev1.PodList {
	return &corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-1",
					Namespace: namespace,
					Labels: map[string]string{
						fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
						fdbv1beta2.FDBClusterLabel:        clusterName,
						fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
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

				cmd := newAnalyzeCmd(
					genericiooptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer},
				)
				err := analyzeCluster(
					cmd,
					k8sClient,
					tc.cluster,
					tc.AutoFix,
					tc.NoWait,
					tc.IgnoredConditions,
					tc.IgnoreRemovals,
				)

				if err != nil && !tc.HasErrors {
					Expect(err).To(HaveOccurred())
				}

				Expect(
					strings.TrimSpace(tc.ExpectedErrMsg),
				).To(Equal(strings.TrimSpace(errBuffer.String())))
				Expect(
					strings.TrimSpace(tc.ExpectedStdoutMsg),
				).To(Equal(strings.TrimSpace(outBuffer.String())))
			},
			Entry("Cluster is fine",
				testCase{
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "⚠ Could not fetch fault domain information for cluster",
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
					cluster: getCluster(
						clusterName,
						namespace,
						false,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: `✖ Cluster is not available
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						false,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "⚠ Could not fetch fault domain information for cluster",
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						false,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: `✖ Cluster is not fully replicated
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						0,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: `✖ Cluster is not reconciled
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
									fdbv1beta2.NewProcessGroupCondition(
										fdbv1beta2.MissingProcesses,
									),
								},
							},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: fmt.Sprintf(
						"✖ ProcessGroup: storage-1 has the following condition: MissingProcesses since %s\n⚠ Could not fetch fault domain information for cluster",
						time.Unix(time.Now().Unix(), 0).String(),
					),
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
									fdbv1beta2.NewProcessGroupCondition(
										fdbv1beta2.MissingProcesses,
									),
								},
								RemovalTimestamp: &metav1.Time{Time: time.Now()},
							},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: `⚠ ProcessGroup: storage-1 is marked for removal, excluded state: false
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodPending,
					}, nil),
					ExpectedErrMsg: `✖ Pod test/storage-1 has unexpected Phase Pending with Reason: , found events: [(no events)]
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  fdbv1beta2.MainContainerName,
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode: 137,
										FinishedAt: metav1.Time{
											Time: time.Now().Add(-1 * time.Hour),
										},
									},
								},
							},
						},
					}, nil),
					ExpectedErrMsg: `✖ Pod test/storage-1 has an unready container: foundationdb
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  fdbv1beta2.MainContainerName,
								Ready: true,
							},
						},
					}, nil),
					ExpectedErrMsg: "⚠ Could not fetch fault domain information for cluster",
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
			Entry("Pod is stuck in terminating and marked for removal",
				testCase{
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID:     "storage-1",
								RemovalTimestamp:   &metav1.Time{Time: time.Now()},
								ExclusionTimestamp: &metav1.Time{Time: time.Now()},
							},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "test",
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode: 127,
										FinishedAt: metav1.Time{
											Time: time.Now().Add(-1 * time.Hour),
										},
									},
								},
							},
						},
					}, &metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
					ExpectedErrMsg: `⚠ ProcessGroup: storage-1 is marked for removal, excluded state: true
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID:     "storage-1",
								RemovalTimestamp:   &metav1.Time{Time: time.Now()},
								ExclusionTimestamp: &metav1.Time{Time: time.Now()},
							},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "test",
								Ready: false,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										ExitCode: 127,
										FinishedAt: metav1.Time{
											Time: time.Now().Add(-1 * time.Hour),
										},
									},
								},
							},
						},
					}, &metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
					ExpectedErrMsg: `⚠ Ignored 1 process groups marked for removal
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{ProcessGroupID: "storage-1"},
						},
					),
					podList: &corev1.PodList{},
					ExpectedErrMsg: `✖ Found no Pods for this cluster
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: `✖ Pod test/storage-1 with the ID storage-1 is not part of the cluster spec status
⚠ Could not fetch fault domain information for cluster`,
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
					cluster: getCluster(
						clusterName,
						namespace,
						true,
						true,
						true,
						1,
						[]*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
									fdbv1beta2.NewProcessGroupCondition(
										fdbv1beta2.MissingProcesses,
									),
									fdbv1beta2.NewProcessGroupCondition(
										fdbv1beta2.IncorrectPodSpec,
									),
								},
							},
						},
					),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: fmt.Sprintf(
						"✖ ProcessGroup: storage-1 has the following condition: MissingProcesses since %s\n⚠ Ignored 1 conditions\n⚠ Could not fetch fault domain information for cluster",
						time.Unix(time.Now().Unix(), 0).String(),
					),
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

			rootCmd := NewRootCmd(
				genericiooptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer},
				&MockVersionChecker{},
			)
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

	// Helper function to create ProcessGroupStatus with fault domain
	getProcessGroupWithFaultDomain := func(
		id fdbv1beta2.ProcessGroupID,
		processClass fdbv1beta2.ProcessClass,
		faultDomain fdbv1beta2.FaultDomain,
		markedForRemoval bool,
	) *fdbv1beta2.ProcessGroupStatus {
		pg := &fdbv1beta2.ProcessGroupStatus{
			ProcessGroupID: id,
			ProcessClass:   processClass,
			FaultDomain:    faultDomain,
		}
		if markedForRemoval {
			pg.RemovalTimestamp = &metav1.Time{Time: time.Now()}
		}
		return pg
	}

	When("testing generateFaultDomainSummary", func() {
		DescribeTable(
			"should generate correct fault domain summary",
			func(processGroups []*fdbv1beta2.ProcessGroupStatus, expectedSummary FaultDomainSummary) {
				cluster := getCluster("test-cluster", "test", true, true, true, 1, processGroups)
				summary := generateFaultDomainSummary(cluster)
				Expect(summary).To(Equal(expectedSummary))
			},
			Entry("empty cluster",
				[]*fdbv1beta2.ProcessGroupStatus{},
				FaultDomainSummary{},
			),
			Entry("single process class with multiple fault domains",
				[]*fdbv1beta2.ProcessGroupStatus{
					getProcessGroupWithFaultDomain(
						"storage-1",
						fdbv1beta2.ProcessClassStorage,
						"zone-a",
						false,
					),
					getProcessGroupWithFaultDomain(
						"storage-2",
						fdbv1beta2.ProcessClassStorage,
						"zone-b",
						false,
					),
					getProcessGroupWithFaultDomain(
						"storage-3",
						fdbv1beta2.ProcessClassStorage,
						"zone-a",
						false,
					),
				},
				FaultDomainSummary{
					fdbv1beta2.ProcessClassStorage: {
						"zone-a": 2,
						"zone-b": 1,
					},
				},
			),
			Entry("multiple process classes",
				[]*fdbv1beta2.ProcessGroupStatus{
					getProcessGroupWithFaultDomain(
						"storage-1",
						fdbv1beta2.ProcessClassStorage,
						"zone-a",
						false,
					),
					getProcessGroupWithFaultDomain(
						"storage-2",
						fdbv1beta2.ProcessClassStorage,
						"zone-b",
						false,
					),
					getProcessGroupWithFaultDomain(
						"log-1",
						fdbv1beta2.ProcessClassLog,
						"zone-a",
						false,
					),
					getProcessGroupWithFaultDomain(
						"stateless-1",
						fdbv1beta2.ProcessClassStateless,
						"zone-c",
						false,
					),
				},
				FaultDomainSummary{
					fdbv1beta2.ProcessClassStorage: {
						"zone-a": 1,
						"zone-b": 1,
					},
					fdbv1beta2.ProcessClassLog: {
						"zone-a": 1,
					},
					fdbv1beta2.ProcessClassStateless: {
						"zone-c": 1,
					},
				},
			),
			Entry("process groups marked for removal should be ignored",
				[]*fdbv1beta2.ProcessGroupStatus{
					getProcessGroupWithFaultDomain(
						"storage-1",
						fdbv1beta2.ProcessClassStorage,
						"zone-a",
						false,
					),
					getProcessGroupWithFaultDomain(
						"storage-2",
						fdbv1beta2.ProcessClassStorage,
						"zone-b",
						true,
					), // marked for removal
					getProcessGroupWithFaultDomain(
						"storage-3",
						fdbv1beta2.ProcessClassStorage,
						"zone-a",
						false,
					),
				},
				FaultDomainSummary{
					fdbv1beta2.ProcessClassStorage: {
						"zone-a": 2,
					},
				},
			),
			Entry("empty fault domain names",
				[]*fdbv1beta2.ProcessGroupStatus{
					getProcessGroupWithFaultDomain(
						"storage-1",
						fdbv1beta2.ProcessClassStorage,
						"",
						false,
					),
					getProcessGroupWithFaultDomain(
						"storage-2",
						fdbv1beta2.ProcessClassStorage,
						"zone-a",
						false,
					),
				},
				FaultDomainSummary{
					fdbv1beta2.ProcessClassStorage: {
						"zone-a": 1,
					},
				},
			),
		)
	})

	When("testing faultDomainDistributionIsValid", func() {
		DescribeTable(
			"should print correct fault domain summary",
			func(cluster *fdbv1beta2.FoundationDBCluster, expectedStdOut string, expectedStdErr string, expectedResult bool) {
				outBuffer := bytes.Buffer{}
				errBuffer := bytes.Buffer{}
				inBuffer := bytes.Buffer{}

				cmd := newAnalyzeCmd(
					genericiooptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer},
				)

				Expect(faultDomainDistributionIsValid(cmd, cluster)).To(Equal(expectedResult))
				Expect(outBuffer.String()).To(Equal(expectedStdOut))
				Expect(errBuffer.String()).To(Equal(expectedStdErr))
			},
			Entry("empty summary",
				&fdbv1beta2.FoundationDBCluster{},
				"",
				"⚠ Could not fetch fault domain information for cluster\n",
				false,
			),
			Entry("single process class",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-3",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-4",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-b",
							},
							{
								ProcessGroupID: "storage-5",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-b",
							},
							{
								ProcessGroupID: "storage-6",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-c",
							},
						},
					},
				},
				`Fault Domain Summary for cluster:
✔ storage: Total: 3 fault domains, 6 process groups top 3 fault domains: zone-a: 3 zone-b: 2 zone-c: 1
`,
				"",
				true,
			),
			Entry("multiple process classes",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-3",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-4",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-b",
							},
							{
								ProcessGroupID: "storage-5",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-b",
							},
							{
								ProcessGroupID: "storage-6",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-c",
							},
							{
								ProcessGroupID: "log-1",
								ProcessClass:   fdbv1beta2.ProcessClassLog,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "log-2",
								ProcessClass:   fdbv1beta2.ProcessClassLog,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "log-3",
								ProcessClass:   fdbv1beta2.ProcessClassLog,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "log-4",
								ProcessClass:   fdbv1beta2.ProcessClassLog,
								FaultDomain:    "zone-b",
							},
							{
								ProcessGroupID: "log-5",
								ProcessClass:   fdbv1beta2.ProcessClassLog,
								FaultDomain:    "zone-b",
							},
							{
								ProcessGroupID: "log-6",
								ProcessClass:   fdbv1beta2.ProcessClassLog,
								FaultDomain:    "zone-c",
							},
						},
					},
				},
				`Fault Domain Summary for cluster:
✔ storage: Total: 3 fault domains, 6 process groups top 3 fault domains: zone-a: 3 zone-b: 2 zone-c: 1
✔ log: Total: 3 fault domains, 6 process groups top 3 fault domains: zone-a: 3 zone-b: 2 zone-c: 1
`,
				"",
				true,
			),
			Entry(
				"process class with empty fault domain",
				&fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "zone-a",
							},
							{
								ProcessGroupID: "storage-3",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
								FaultDomain:    "",
							},
						},
					},
				},
				"Fault Domain Summary for cluster:\n",
				"✖ storage: Total: 1 fault domains, 2 process groups top 3 fault domains: zone-a: 2\n",
				false,
			),
		)
	})

	When("testing printTopNFaultDomains", func() {
		DescribeTable("should print top N fault domains correctly",
			func(faultDomains map[fdbv1beta2.FaultDomain]int, n int, expectedOutput string) {
				Expect(
					strings.TrimSpace(printTopNFaultDomains(faultDomains, n)),
				).To(Equal(strings.TrimSpace(expectedOutput)))
			},
			Entry("top 3 with more than 3 domains",
				map[fdbv1beta2.FaultDomain]int{
					"zone-a": 5,
					"zone-b": 3,
					"zone-c": 2,
					"zone-d": 4,
					"zone-e": 1,
				},
				3,
				"top 3 fault domains: zone-a: 5 zone-d: 4 zone-b: 3",
			),
			Entry("top 3 with exactly 3 domains",
				map[fdbv1beta2.FaultDomain]int{
					"zone-a": 3,
					"zone-b": 2,
					"zone-c": 1,
				},
				3,
				"top 3 fault domains: zone-a: 3 zone-b: 2 zone-c: 1",
			),
			Entry("top 3 with less than 3 domains",
				map[fdbv1beta2.FaultDomain]int{
					"zone-a": 5,
					"zone-b": 2,
				},
				3,
				"top 3 fault domains: zone-a: 5 zone-b: 2",
			),
			Entry("empty fault domains map",
				map[fdbv1beta2.FaultDomain]int{},
				3,
				"top 3 fault domains:",
			),
			Entry("single fault domain",
				map[fdbv1beta2.FaultDomain]int{
					"zone-a": 10,
				},
				3,
				"top 3 fault domains: zone-a: 10",
			),
			Entry("top 0 domains",
				map[fdbv1beta2.FaultDomain]int{
					"zone-a": 5,
					"zone-b": 3,
				},
				0,
				"top 0 fault domains:",
			),
		)
	})
})
