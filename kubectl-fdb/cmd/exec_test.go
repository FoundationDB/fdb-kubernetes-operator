/*
 * exec_tests.go
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
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] exec command", func() {
	When("running exec command", func() {
		clusterName := "test"
		namespace := "test"

		var cluster fdbtypes.FoundationDBCluster
		var podList corev1.PodList

		type testCase struct {
			ClusterName   string
			Context       string
			Command       []string
			ExpectedArgs  []string
			ExpectedError string
		}

		BeforeEach(func() {
			cluster = fdbtypes.FoundationDBCluster{
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

			podList = corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "instance-1",
							Namespace: namespace,
							Labels: map[string]string{
								fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
								fdbtypes.FDBClusterLabel:      clusterName,
							},
						},
					},
				},
			}
		})

		DescribeTable("should execute the provided command",
			func(input testCase) {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&cluster, &podList).Build()

				command, err := buildCommand(kubeClient, input.ClusterName, input.Context, namespace, input.Command)

				if input.ExpectedError != "" {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal(input.ExpectedError))
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(command).NotTo(Equal(""))
					expectedArgs := []string{command.Path}
					expectedArgs = append(expectedArgs, input.ExpectedArgs...)
					Expect(command.Args).To(Equal(expectedArgs))
				}
			},
			Entry("Exec into instance with valid pod",
				testCase{
					ClusterName:  "test",
					ExpectedArgs: []string{"--namespace", "test", "exec", "-it", "instance-1", "--", "bash"},
				}),
			Entry("Exec into instance with explicit context",
				testCase{
					ClusterName:  "test",
					Context:      "remote-kc",
					ExpectedArgs: []string{"--context", "remote-kc", "--namespace", "test", "exec", "-it", "instance-1", "--", "bash"},
				}),
			Entry("Exec into instance with missing pod",
				testCase{
					ClusterName:   "test-2",
					ExpectedError: "no usable pods found for cluster test-2",
				}),
		)
	})
})
