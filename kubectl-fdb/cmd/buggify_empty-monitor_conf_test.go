/*
 * buggify_empty_monitor_conf_test.go
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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] buggify empty-monitor-conf instances command", func() {
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

	When("running buggify empty-monitor-conf instances command", func() {
		type testCase struct {
			set                      bool
			ExpectedEmptyMonitorConf bool
		}

		DescribeTable("should update empty-monitor-conf",
			func(tc testCase) {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster, &corev1.PodList{}).Build()

				err := updateMonitorConf(kubeClient, clusterName, namespace, false, tc.set)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = kubeClient.Get(ctx.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(tc.ExpectedEmptyMonitorConf).To(Equal(resCluster.Spec.Buggify.EmptyMonitorConf))
			},
			Entry("setting monitor config to true",
				testCase{
					set:                      true,
					ExpectedEmptyMonitorConf: true,
				}),
			Entry("setting monitor config to false",
				testCase{
					set:                      false,
					ExpectedEmptyMonitorConf: false,
				}),
		)
	})

})
