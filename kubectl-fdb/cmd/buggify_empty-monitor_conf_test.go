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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		var kubeClient client.Client

		When("should update empty-monitor-conf", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.EmptyMonitorConf = false
				kubeClient = getFakeKubeClientWithRuntime(cluster)
			})

			It("setting monitor config to true", func() {
				err := updateMonitorConf(kubeClient, clusterName, namespace, false, true)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = kubeClient.Get(ctx.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(resCluster.Spec.Buggify.EmptyMonitorConf).To(BeTrue())
			})
		})

		When("should update empty-monitor-conf", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.EmptyMonitorConf = true
				kubeClient = getFakeKubeClientWithRuntime(cluster)
			})

			It("setting monitor config to false", func() {
				err := updateMonitorConf(kubeClient, clusterName, namespace, false, false)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = kubeClient.Get(ctx.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(resCluster.Spec.Buggify.EmptyMonitorConf).To(BeFalse())
			})
		})
	})
})
