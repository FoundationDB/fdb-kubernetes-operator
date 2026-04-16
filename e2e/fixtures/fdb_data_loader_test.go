/*
 * cluster_config_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/v2/mock-kubernetes-client/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("FDB Data Loader", func() {
	When("generating the data loader config", func() {
		var factory *Factory
		var k8sClient *mockclient.MockClient

		BeforeEach(func() {
			Expect(scheme.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
			Expect(fdbv1beta2.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

			k8sClient = mockclient.NewMockClientWithHooksAndIndexes(
				scheme.Scheme,
				nil, /* create hooks */
				nil, /* update hooks */
				true /* enable indexing by nodeName */)

			factory = &Factory{
				options: &FactoryOptions{
					dataLoaderImage:             "test",
					enableDataLoading:           true,
					featureOperatorUnifiedImage: true,
				},
				certificate: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cert",
					},
				},
				controllerRuntimeClient: k8sClient,
			}

		})

		When("passing empty arguments options", func() {
			It("should generate a decoder without errors", func() {
				factory.CreateDataLoaderIfAbsentWithOptions(&FdbCluster{
					cluster: &fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cluster",
							Namespace: "test",
						},
					},
				}, &DataLoaderOptions{
					Wait: false,
				})
			})
		})

		When("passing custom arguments options", func() {
			It("should generate a decoder without errors", func() {
				factory.CreateDataLoaderIfAbsentWithOptions(&FdbCluster{
					cluster: &fdbv1beta2.FoundationDBCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cluster",
							Namespace: "test",
						},
					},
				}, &DataLoaderOptions{
					Wait: false,
					DataLoaderArguments: []string{
						"--test=123",
					},
				})
			})
		})
	})

})
