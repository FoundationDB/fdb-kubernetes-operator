/*
 * profile_analyzer_test.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ctx "context"
)

var _ = Describe("profile analyser", func() {
	When("running profile analyzer with 6.3", func() {
		clusterName := "test"
		namespace := "test"
		scheme := runtime.NewScheme()
		var kubeClient client.Client

		BeforeEach(func() {
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbv1beta2.AddToScheme(scheme)
			cluster := &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "6.3.24",
				},
			}
			kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()
		})
		It("should match the command args", func() {
			expectedEnv := v1.EnvVar{
				Name:      "FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY",
				Value:     "/usr/bin/fdb/6.3.24/lib/",
				ValueFrom: nil,
			}
			err := runProfileAnalyzer(kubeClient, namespace, clusterName, "21:30 08/24/2022 BST", "22:30 08/24/2022 BST", 100, "../../sample-apps/fdb-profile-analyzer/sample_template.yaml")
			Expect(err).NotTo(HaveOccurred())
			job := &batchv1.Job{}
			err = kubeClient.Get(ctx.Background(), client.ObjectKey{
				Namespace: namespace,
				Name:      "test-hot-shard-tool",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"-c",
				"python3 ./transaction_profiling_analyzer.py  -C /var/dynamic-conf/fdb.cluster -s \"21:30 08/24/2022 BST\" -e \"22:30 08/24/2022 BST\" --filter-get-range --top-requests  100"}))
			Expect(job.Spec.Template.Spec.Containers[0].Env).To(ContainElements(expectedEnv))
		})
	})
	When("running profile analyzer with 7.1", func() {
		clusterName := "test"
		namespace := "test"
		scheme := runtime.NewScheme()
		var kubeClient client.Client

		BeforeEach(func() {
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbv1beta2.AddToScheme(scheme)
			cluster := &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.19",
				},
			}
			kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()
		})
		It("should match the command args", func() {
			expectedEnv := v1.EnvVar{
				Name:      "FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY",
				Value:     "/usr/bin/fdb/7.1.19/lib/",
				ValueFrom: nil,
			}
			err := runProfileAnalyzer(kubeClient, namespace, clusterName, "21:30 08/24/2022 BST", "22:30 08/24/2022 BST", 100, "../../sample-apps/fdb-profile-analyzer/sample_template.yaml")
			Expect(err).NotTo(HaveOccurred())
			job := &batchv1.Job{}
			err = kubeClient.Get(ctx.Background(), client.ObjectKey{
				Namespace: namespace,
				Name:      "test-hot-shard-tool",
			}, job)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"-c",
				"python3 ./transaction_profiling_analyzer.py  -C /var/dynamic-conf/fdb.cluster -s \"21:30 08/24/2022 BST\" -e \"22:30 08/24/2022 BST\" --filter-get-range --top-requests  100"}))
			Expect(job.Spec.Template.Spec.Containers[0].Env).To(ContainElements(expectedEnv))
		})
	})
})
