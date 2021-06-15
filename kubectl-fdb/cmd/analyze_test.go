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
	"fmt"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func getCluster(clusterName string, namespace string, available bool, healthy bool, fullReplication bool, reconciled int64, processGroups []*fdbtypes.ProcessGroupStatus) *fdbtypes.FoundationDBCluster {
	return &fdbtypes.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Generation: 0,
		},
		Spec: fdbtypes.FoundationDBClusterSpec{
			ProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
		Status: fdbtypes.FoundationDBClusterStatus{
			Health: fdbtypes.ClusterHealth{
				Available:       available,
				Healthy:         healthy,
				FullReplication: fullReplication,
			},
			Generations: fdbtypes.ClusterGenerationStatus{
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
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						fdbtypes.FDBClusterLabel:      clusterName,
					},
					DeletionTimestamp: deletionTimestamp,
				},
				Status: status,
			},
		},
	}

}

var _ = Describe("[plugin] analyze cluster", func() {
	clusterName := "test"
	namespace := "test"

	When("analyzing the cluster", func() {
		type testCase struct {
			cluster          *fdbtypes.FoundationDBCluster
			podList          *corev1.PodList
			ExpectedErrMsg   string
			ExpectedStdouMsg string
			AutoFix          bool
			Force            bool
			HasErrors        bool
		}

		DescribeTable("return all successful and failed checks",
			func(tc testCase) {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.cluster, tc.podList).Build()

				// We use these buffers to check the input/output
				outBuffer := bytes.Buffer{}
				errBuffer := bytes.Buffer{}
				inBuffer := bytes.Buffer{}

				cmd := newAnalyzeCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
				err := analyzeCluster(cmd, kubeClient, clusterName, namespace, tc.AutoFix, tc.Force)

				if err != nil && !tc.HasErrors {
					Expect(err).To(HaveOccurred())
				}

				Expect(strings.TrimSpace(tc.ExpectedErrMsg)).To(Equal(strings.TrimSpace(errBuffer.String())))
				Expect(strings.TrimSpace(tc.ExpectedStdouMsg)).To(Equal(strings.TrimSpace(outBuffer.String())))
			},
			Entry("Cluster is fine",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{
						{ProcessGroupID: "instance-1"},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: false,
				}),
			Entry("Cluster is unavailable",
				testCase{
					cluster: getCluster(clusterName, namespace, false, true, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Cluster is not available",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("Cluster is unhealthy",
				testCase{
					cluster: getCluster(clusterName, namespace, true, false, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("Cluster is not fully replicated",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, false, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Cluster is not fully replicated",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("Cluster is not reconciled",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 1, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ Cluster is not reconciled",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("ProcessGroup has a missing process",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{
						{
							ProcessGroupID: "instance-1",
							ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
								fdbtypes.NewProcessGroupCondition(fdbtypes.MissingProcesses),
							},
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: fmt.Sprintf("✖ ProcessGroup: instance-1 has the following condition: MissingProcesses since %s", time.Unix(time.Now().Unix(), 0).String()),
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("ProcessGroup has a missing process but is marked for removal",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{
						{
							ProcessGroupID: "instance-1",
							ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
								fdbtypes.NewProcessGroupCondition(fdbtypes.MissingProcesses),
							},
							Remove: true,
						},
					}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
					}, nil),
					ExpectedErrMsg: "✖ ProcessGroup: instance-1 is marked for removal, excluded state: false",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: false,
				}),
			Entry("Pod is in Pending phase",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodPending,
					}, nil),
					ExpectedErrMsg: "✖ Pod test/instance-1 has unexpected Phase Pending with Reason:",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("Container is in terminated state",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "foundationdb",
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
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("Container is in ready state",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "foundationdb",
								Ready: true,
							},
						},
					}, nil),
					ExpectedErrMsg: "",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: false,
				}),
			Entry("Pod is stuck in terminating",
				testCase{
					cluster: getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList: getPodList(clusterName, namespace, corev1.PodStatus{
						Phase:             corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{},
					}, &metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
					ExpectedErrMsg: fmt.Sprintf("✖ Pod test/instance-1 has been stuck in terminating since %s", time.Unix(time.Now().Add(-1*time.Hour).Unix(), 0).String()),
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition`,
					AutoFix:   false,
					HasErrors: true,
				}),
			Entry("Missing Pods",
				testCase{
					cluster:        getCluster(clusterName, namespace, true, true, true, 0, []*fdbtypes.ProcessGroupStatus{}),
					podList:        &corev1.PodList{},
					ExpectedErrMsg: "✖ Found no Pods for this cluster",
					ExpectedStdouMsg: `Checking cluster: test/test
✔ Cluster is available
✔ Cluster is fully replicated
✔ Cluster is reconciled
✔ ProcessGroups are all in ready condition
✔ Pods are all running and available`,
					AutoFix:   false,
					HasErrors: true,
				}),
			// TODO: test cases for auto-fix
		)
	})

	When("running analyze without arguments", func() {
		It("should printout the help text", func() {
			outBuffer := bytes.Buffer{}
			errBuffer := bytes.Buffer{}
			inBuffer := bytes.Buffer{}

			rootCmd := NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
			rootCmd.SetArgs([]string{"analyze"})

			err := rootCmd.Execute()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
