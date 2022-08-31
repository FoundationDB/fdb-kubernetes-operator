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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("profile analyser", func() {
	When("running profile analyzer", func() {
		clusterName := "test"
		namespace := "test"
		scheme := runtime.NewScheme()
		var kubeClient client.Client

		BeforeEach(func() {
			var cluster *fdbv1beta2.FoundationDBCluster
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbv1beta2.AddToScheme(scheme)
			cluster = &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}
			kubeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()
		})
		It("should not throw an error", func() {
			err := runProfileAnalyzer(kubeClient, namespace, clusterName, "21:30 08/24/2022 BST", "22:30 08/24/2022 BST", 100, "../../sample-apps/fdb-profile-analyzer/sample_template.yaml")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
