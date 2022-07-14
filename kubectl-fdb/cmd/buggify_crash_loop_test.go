/*
 * buggify_crash_loop_test.go
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
	"sort"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] buggify crash-loop instances command", func() {
	clusterName := "test"
	namespace := "test"

	var cluster *fdbv1beta2.FoundationDBCluster

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
		}
	})

	When("running buggify crash-loop instances command", func() {
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
									fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
									fdbv1beta2.FDBClusterLabel:        clusterName,
									fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "instance-2",
								Namespace: namespace,
								Labels: map[string]string{
									fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
									fdbv1beta2.FDBClusterLabel:        clusterName,
									fdbv1beta2.FDBProcessGroupIDLabel: "storage-2",
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
					_ = fdbv1beta2.AddToScheme(scheme)
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

		When("adding instances to crash-loop list from a cluster", func() {
			var podList corev1.PodList

			type testCase struct {
				Instances                    []string
				ExpectedInstancesInCrashLoop []string
			}

			DescribeTable("should add all targeted processes to crash-loop list",
				func(tc testCase) {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()

					err := updateCrashLoopList(kubeClient, clusterName, tc.Instances, namespace, false, false, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					crashLoopList := resCluster.Spec.Buggify.CrashLoop
					sort.Strings(crashLoopList)
					Expect(tc.ExpectedInstancesInCrashLoop).To(Equal(crashLoopList))
					Expect(len(tc.ExpectedInstancesInCrashLoop)).To(BeNumerically("==", len(resCluster.Spec.Buggify.CrashLoop)))
				},
				Entry("Adding single instance.",
					testCase{
						Instances:                    []string{"instance-1"},
						ExpectedInstancesInCrashLoop: []string{"instance-1"},
					}),
				Entry("Adding multiple instances.",
					testCase{
						Instances:                    []string{"instance-1", "instance-2"},
						ExpectedInstancesInCrashLoop: []string{"instance-1", "instance-2"},
					}),
			)

			When("a process group was already in crash-loop", func() {
				var kubeClient client.Client

				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoop = []string{"instance-1"}
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()
				})

				type testCase struct {
					Instances                    []string
					ExpectedInstancesInCrashLoop []string
				}

				DescribeTable("should add all targeted processes to crash-loop list",
					func(tc testCase) {
						err := updateCrashLoopList(kubeClient, clusterName, tc.Instances, namespace, false, false, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = kubeClient.Get(ctx.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)
						Expect(err).NotTo(HaveOccurred())
						crashLoopList := resCluster.Spec.Buggify.CrashLoop
						sort.Strings(crashLoopList)
						Expect(tc.ExpectedInstancesInCrashLoop).To(Equal(crashLoopList))
						Expect(len(tc.ExpectedInstancesInCrashLoop)).To(BeNumerically("==", len(resCluster.Spec.Buggify.CrashLoop)))
					},
					Entry("Adding the same instance.",
						testCase{
							Instances:                    []string{"instance-1"},
							ExpectedInstancesInCrashLoop: []string{"instance-1"},
						}),
					Entry("Adding different instance.",
						testCase{
							Instances:                    []string{"instance-2"},
							ExpectedInstancesInCrashLoop: []string{"instance-1", "instance-2"},
						}),
					Entry("Adding multiple instances.",
						testCase{
							Instances:                    []string{"instance-2", "instance-3"},
							ExpectedInstancesInCrashLoop: []string{"instance-1", "instance-2", "instance-3"},
						}),
				)
			})
		})

		When("removing instances from crash-loop list from a cluster", func() {
			var podList corev1.PodList
			var kubeClient client.Client

			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []string{"instance-1", "instance-2", "instance-3"}
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()
			})

			type testCase struct {
				Instances                    []string
				ExpectedInstancesInCrashLoop []string
			}

			DescribeTable("should remove all targeted processes from the crash-loop list",
				func(tc testCase) {
					err := updateCrashLoopList(kubeClient, clusterName, tc.Instances, namespace, false, true, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					crashLoopList := resCluster.Spec.Buggify.CrashLoop
					sort.Strings(crashLoopList)
					Expect(tc.ExpectedInstancesInCrashLoop).To(Equal(crashLoopList))
					Expect(len(tc.ExpectedInstancesInCrashLoop)).To(BeNumerically("==", len(resCluster.Spec.Buggify.CrashLoop)))
				},
				Entry("Removing single instance.",
					testCase{
						Instances:                    []string{"instance-1"},
						ExpectedInstancesInCrashLoop: []string{"instance-2", "instance-3"},
					}),
				Entry("Removing multiple instances.",
					testCase{
						Instances:                    []string{"instance-2", "instance-3"},
						ExpectedInstancesInCrashLoop: []string{"instance-1"},
					}),
			)

		})

		When("clearing crash-loop list", func() {
			var podList corev1.PodList
			var kubeClient client.Client

			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []string{"instance-1", "instance-2", "instance-3"}
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &podList).Build()
			})

			It("should clear the crash-loop list", func() {
				err := updateCrashLoopList(kubeClient, clusterName, []string{}, namespace, false, false, true)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = kubeClient.Get(ctx.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resCluster.Spec.Buggify.CrashLoop)).To(Equal(0))
			})
		})
	})
})
