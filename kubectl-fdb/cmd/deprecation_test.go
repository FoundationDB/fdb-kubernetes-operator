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
	"strings"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] deprecation command", func() {
	var trueValue = true

	When("running the deprecation command", func() {
		clusterName := "test"
		namespace := "test"

		type testCase struct {
			cluster            fdbtypes.FoundationDBCluster
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

				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&tc.cluster).Build()

				cmd := newDeprecationCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
				err := checkDeprecation(cmd, kubeClient, tc.inputClusters, namespace, tc.deprecationOptions, tc.showClusterSpec)
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
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{},
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
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							MinimumUptimeSecondsForBounce: 600,
							Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
								"general": {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "foundationdb-kubernetes-sidecar",
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
													Name: "foundationdb-kubernetes-init",
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
							AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
								Replacements: fdbtypes.AutomaticReplacementOptions{
									Enabled: &trueValue,
								},
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
			Entry("Cluster has no deprecations but unset defaults",
				testCase{
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							MinimumUptimeSecondsForBounce: 600,
							Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
								"general": {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "foundationdb-kubernetes-sidecar",
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
													Name: "foundationdb-kubernetes-init",
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
						},
					},
					inputClusters: []string{},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   false,
					},
					expectedOutput:  `Cluster test has deprecations`,
					expectedError:   "1/1 cluster(s) with deprecations",
					showClusterSpec: false,
				}),
			Entry("Cluster has deprecations",
				testCase{
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							PodLabels:                     map[string]string{"test": "test"},
							MinimumUptimeSecondsForBounce: 600,
							Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
								"general": {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "foundationdb-kubernetes-sidecar",
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
													Name: "foundationdb-kubernetes-init",
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
						},
					},
					inputClusters: []string{},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   true,
					},
					expectedOutput:  "Cluster test has deprecations",
					expectedError:   "1/1 cluster(s) with deprecations",
					showClusterSpec: false,
				}),
			Entry("Cluster has no deprecations and set show cluster spec",
				testCase{
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							MinimumUptimeSecondsForBounce: 600,
							Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
								"general": {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "foundationdb-kubernetes-sidecar",
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
													Name: "foundationdb-kubernetes-init",
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
							AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
								Replacements: fdbtypes.AutomaticReplacementOptions{
									Enabled: &trueValue,
								},
							},
						},
					},
					inputClusters: []string{},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   true,
					},
					expectedOutput:  "1 cluster(s) without deprecations\n",
					expectedError:   "",
					showClusterSpec: true,
				}),
			Entry("Cluster has no deprecations but unset defaults and show cluster spec",
				testCase{
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							MinimumUptimeSecondsForBounce: 600,
							Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
								"general": {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "foundationdb-kubernetes-sidecar",
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
													Name: "foundationdb-kubernetes-init",
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
						},
					},
					inputClusters: []string{},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   false,
					},
					expectedOutput: `---
automationOptions:
  replacements:
    enabled: false
    failureDetectionTimeSeconds: 1800
buggify: {}
databaseConfiguration: {}
faultDomain: {}
lockOptions: {}
mainContainer: {}
minimumUptimeSecondsForBounce: 600
partialConnectionString: {}
processCounts: {}
processes:
  general:
    podTemplate:
      metadata:
        creationTimestamp: null
      spec:
        containers:
        - name: foundationdb
          resources:
            limits:
              cpu: "1"
              memory: 1Gi
            requests:
              cpu: "1"
              memory: 1Gi
        - name: foundationdb-kubernetes-sidecar
          resources:
            limits:
              org.foundationdb/empty: "0"
            requests:
              org.foundationdb/empty: "0"
        initContainers:
        - name: foundationdb-kubernetes-init
          resources:
            limits:
              org.foundationdb/empty: "0"
            requests:
              org.foundationdb/empty: "0"
services:
  publicIPSource: pod
sidecarContainer: {}
version: ""`,
					expectedError:   "1/1 cluster(s) with deprecations",
					showClusterSpec: true,
					checkOutput:     true,
				}),
			Entry("Cluster has deprecations and show cluster spec flag",
				testCase{
					cluster: fdbtypes.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
						Spec: fdbtypes.FoundationDBClusterSpec{
							PodLabels:                     map[string]string{"test": "test"},
							MinimumUptimeSecondsForBounce: 600,
							Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
								"general": {
									PodTemplate: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name: "foundationdb-kubernetes-sidecar",
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
													Name: "foundationdb-kubernetes-init",
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
						},
					},
					inputClusters: []string{},
					deprecationOptions: internal.DeprecationOptions{
						UseFutureDefaults: false,
						OnlyShowChanges:   true,
					},
					expectedOutput: `---
automationOptions:
  replacements:
    enabled: false
buggify: {}
configMap:
  metadata:
    creationTimestamp: null
    labels:
      test: test
databaseConfiguration: {}
faultDomain: {}
lockOptions: {}
mainContainer: {}
minimumUptimeSecondsForBounce: 600
partialConnectionString: {}
processCounts: {}
processes:
  general:
    podTemplate:
      metadata:
        creationTimestamp: null
        labels:
          test: test
      spec:
        containers:
        - name: foundationdb-kubernetes-sidecar
          resources:
            limits:
              org.foundationdb/empty: "0"
            requests:
              org.foundationdb/empty: "0"
        initContainers:
        - name: foundationdb-kubernetes-init
          resources:
            limits:
              org.foundationdb/empty: "0"
            requests:
              org.foundationdb/empty: "0"
    volumeClaimTemplate:
      metadata:
        creationTimestamp: null
        labels:
          test: test
      spec:
        resources: {}
      status: {}
services: {}
sidecarContainer: {}
version: ""`,
					expectedError:   "1/1 cluster(s) with deprecations",
					showClusterSpec: true,
					checkOutput:     true,
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
