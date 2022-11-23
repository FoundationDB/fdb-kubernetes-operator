/*
 * deprecation_test.go
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
	"strings"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[plugin] deprecation command", func() {
	When("running the deprecation command", func() {
		type testCase struct {
			cluster            fdbv1beta2.FoundationDBCluster
			inputClusters      []string
			deprecationOptions internal.DeprecationOptions
			showClusterSpec    bool
			checkOutput        bool
			expectedOutput     string
			expectedError      string
		}

		DescribeTable("should show all deprecations",
			func(tc testCase) {
				// We use these buffers to check the input/output
				outBuffer := bytes.Buffer{}
				errBuffer := bytes.Buffer{}
				inBuffer := bytes.Buffer{}

				Expect(k8sClient.Delete(context.TODO(), &tc.cluster)).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.TODO(), &tc.cluster)).NotTo(HaveOccurred())

				cmd := newDeprecationCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
				err := checkDeprecation(cmd, k8sClient, tc.inputClusters, namespace, tc.deprecationOptions, tc.showClusterSpec)
				if tc.expectedError != "" {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal(tc.expectedError))
					// We don't compare the diff output since it includes some special
					// whitespace characters
					if tc.checkOutput {
						Expect(strings.TrimSpace(outBuffer.String())).To(Equal(strings.TrimSpace(tc.expectedOutput)))
					}
					return
				}

				Expect(err).NotTo(HaveOccurred())
				Expect(outBuffer.String()).To(Equal(tc.expectedOutput))
			},
			Entry("Cluster doesn't match",
				testCase{
					cluster: fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbv1beta2.FoundationDBClusterSpec{},
					},
					inputClusters: []string{"no-match"},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   false,
					},
					expectedOutput:  "0 cluster(s) without deprecations\n",
					expectedError:   "",
					showClusterSpec: false,
				}),
			Entry("Cluster has no deprecations",
				testCase{
					cluster: fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbv1beta2.FoundationDBClusterSpec{
							MinimumUptimeSecondsForBounce: 600,
							MainContainer: fdbv1beta2.ContainerOverrides{
								ImageConfigs: []fdbv1beta2.ImageConfig{
									{
										BaseImage: "foundationdb/foundationdb",
									},
								},
							},
							UseExplicitListenAddress: pointer.Bool(true),
							Processes: map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
								fdbv1beta2.ProcessClassGeneral: {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: fdbv1beta2.SidecarContainerName,
													Resources: corev1.ResourceRequirements{
														Limits: corev1.ResourceList{
															"org.foundationdb/empty": *resource.NewQuantity(0, ""),
														},
														Requests: corev1.ResourceList{
															"org.foundationdb/empty": *resource.NewQuantity(0, ""),
														},
													},
												},
												{
													Name: fdbv1beta2.MainContainerName,
													Resources: corev1.ResourceRequirements{
														Limits: corev1.ResourceList{
															"org.foundationdb/empty": *resource.NewQuantity(0, ""),
														},
														Requests: corev1.ResourceList{
															"org.foundationdb/empty": *resource.NewQuantity(0, ""),
														},
													},
												},
											},
											InitContainers: []corev1.Container{
												{
													Name: fdbv1beta2.InitContainerName,
													Resources: corev1.ResourceRequirements{
														Limits: corev1.ResourceList{
															"org.foundationdb/empty": *resource.NewQuantity(0, ""),
														},
														Requests: corev1.ResourceList{
															"org.foundationdb/empty": *resource.NewQuantity(0, ""),
														},
													},
												},
											},
										},
									},
								},
							},
							SidecarContainer: fdbv1beta2.ContainerOverrides{
								EnableLivenessProbe:  pointer.Bool(true),
								EnableReadinessProbe: pointer.Bool(false),
								ImageConfigs: []fdbv1beta2.ImageConfig{
									{
										BaseImage: "foundationdb/foundationdb-kubernetes-sidecar",
										TagSuffix: "-1",
									},
								},
							},
							AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
								Replacements: fdbv1beta2.AutomaticReplacementOptions{
									Enabled: pointer.Bool(true),
								},
							},
							LabelConfig: fdbv1beta2.LabelConfig{
								FilterOnOwnerReferences: pointer.Bool(true),
								MatchLabels: map[string]string{
									fdbv1beta2.FDBClusterLabel: clusterName,
								},
								ResourceLabels: map[string]string{
									fdbv1beta2.FDBClusterLabel: clusterName,
								},
								ProcessGroupIDLabels: []string{fdbv1beta2.FDBProcessGroupIDLabel},
								ProcessClassLabels:   []string{fdbv1beta2.FDBProcessClassLabel},
							},
						},
					},
					inputClusters: []string{},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   true,
					},
					expectedOutput: `Cluster test has no deprecation
1 cluster(s) without deprecations
`,
					expectedError:   "",
					showClusterSpec: false,
				}),
		)
	})

	When("print the help command", func() {
		var cmd *cobra.Command

		BeforeEach(func() {
			// We use these buffers to check the input/output
			outBuffer := bytes.Buffer{}
			errBuffer := bytes.Buffer{}
			inBuffer := bytes.Buffer{}

			cmd = NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
		})

		It("should print out the help information", func() {
			args := []string{"deprecation", "--help"}
			cmd.SetArgs(args)

			err := cmd.Execute()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
