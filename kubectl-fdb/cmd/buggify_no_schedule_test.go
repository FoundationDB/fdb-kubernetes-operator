/*
 * buggify_no_schedule_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] buggify no-schedule instances command", func() {
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

	When("running buggify no-schedule instances command", func() {
		When("adding instances to no-schedule list from a cluster", func() {
			type testCase struct {
				Instances                     []string
				ExpectedInstancesInNoSchedule []string
			}

			DescribeTable("should add all targeted processes to no-schedule list",
				func(tc testCase) {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

					err := updateNoScheduleList(kubeClient, clusterName, tc.Instances, namespace, false, false, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesInNoSchedule).To(ContainElements(resCluster.Spec.Buggify.NoSchedule))
					Expect(len(tc.ExpectedInstancesInNoSchedule)).To(BeNumerically("==", len(resCluster.Spec.Buggify.NoSchedule)))
				},
				Entry("Adding single instance.",
					testCase{
						Instances:                     []string{"test-storage-1"},
						ExpectedInstancesInNoSchedule: []string{"storage-1"},
					}),
				Entry("Adding multiple instances.",
					testCase{
						Instances:                     []string{"test-storage-1", "test-storage-2"},
						ExpectedInstancesInNoSchedule: []string{"storage-1", "storage-2"},
					}),
			)

			When("a process group is already in no-schedule", func() {
				var kubeClient client.Client

				BeforeEach(func() {
					cluster.Spec.Buggify.NoSchedule = []string{"storage-1"}
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()
				})

				type testCase struct {
					Instances                     []string
					ExpectedInstancesInNoSchedule []string
				}

				DescribeTable("should add all targeted processes to no-schedule list",
					func(tc testCase) {
						err := updateNoScheduleList(kubeClient, clusterName, tc.Instances, namespace, false, false, false)
						Expect(err).NotTo(HaveOccurred())

						var resCluster fdbv1beta2.FoundationDBCluster
						err = kubeClient.Get(ctx.Background(), client.ObjectKey{
							Namespace: namespace,
							Name:      clusterName,
						}, &resCluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(tc.ExpectedInstancesInNoSchedule).To(ContainElements(resCluster.Spec.Buggify.NoSchedule))
						Expect(len(tc.ExpectedInstancesInNoSchedule)).To(BeNumerically("==", len(resCluster.Spec.Buggify.NoSchedule)))
					},
					Entry("Adding the same instance.",
						testCase{
							Instances:                     []string{"test-storage-1"},
							ExpectedInstancesInNoSchedule: []string{"storage-1"},
						}),
					Entry("Adding different instance.",
						testCase{
							Instances:                     []string{"test-storage-2"},
							ExpectedInstancesInNoSchedule: []string{"storage-1", "storage-2"},
						}),
					Entry("Adding multiple instances.",
						testCase{
							Instances:                     []string{"test-storage-2", "test-storage-3"},
							ExpectedInstancesInNoSchedule: []string{"storage-1", "storage-2", "storage-3"},
						}),
				)
			})
		})

		When("removing process group from no-schedule list from a cluster", func() {
			var kubeClient client.Client

			BeforeEach(func() {
				cluster.Spec.Buggify.NoSchedule = []string{"storage-1", "storage-2", "storage-3"}
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()
			})

			type testCase struct {
				Instances                     []string
				ExpectedInstancesInNoSchedule []string
			}

			DescribeTable("should remove all targeted processes from the no-schedule list",
				func(tc testCase) {
					err := updateNoScheduleList(kubeClient, clusterName, tc.Instances, namespace, false, true, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.ExpectedInstancesInNoSchedule).To(Equal(resCluster.Spec.Buggify.NoSchedule))
					Expect(len(tc.ExpectedInstancesInNoSchedule)).To(BeNumerically("==", len(resCluster.Spec.Buggify.NoSchedule)))
				},
				Entry("Removing single instance.",
					testCase{
						Instances:                     []string{"test-storage-1"},
						ExpectedInstancesInNoSchedule: []string{"storage-2", "storage-3"},
					}),
				Entry("Removing multiple instances.",
					testCase{
						Instances:                     []string{"test-storage-2", "test-storage-3"},
						ExpectedInstancesInNoSchedule: []string{"storage-1"},
					}),
			)

		})

		When("clearing no-schedule list", func() {
			var kubeClient client.Client

			BeforeEach(func() {
				cluster.Spec.Buggify.NoSchedule = []string{"storage-1", "storage-2", "storage-3"}
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()
			})

			It("should clear the no-schedule list", func() {
				err := updateNoScheduleList(kubeClient, clusterName, nil, namespace, false, false, true)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = kubeClient.Get(ctx.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resCluster.Spec.Buggify.NoSchedule)).To(Equal(0))
			})
		})
	})
})
