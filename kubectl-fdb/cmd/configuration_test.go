/*
 * configuration_test.go
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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("[plugin] configuration command", func() {
	When("getting the configuration string", func() {
		When("using a single region cluster", func() {
			var cluster *fdbtypes.FoundationDBCluster

			BeforeEach(func() {
				cluster = &fdbtypes.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Spec: fdbtypes.FoundationDBClusterSpec{
						DatabaseConfiguration: fdb.DatabaseConfiguration{
							Regions: []fdb.Region{
								{
									DataCenters: []fdb.DataCenter{
										{
											ID:       "test",
											Priority: 1,
										},
									},
								},
							},
						},
					},
				}
			})

			It("should return the configuration string", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", false)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"test\\\",\\\"priority\\\":1}]}]"))
			})

			It("should return the same configuration string with failover", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"test\\\",\\\"priority\\\":1}]}]"))
			})
		})

		When("using a multi region cluster", func() {
			var cluster *fdbtypes.FoundationDBCluster

			BeforeEach(func() {
				cluster = &fdbtypes.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Spec: fdbtypes.FoundationDBClusterSpec{
						DatabaseConfiguration: fdb.DatabaseConfiguration{
							Regions: []fdb.Region{
								{
									DataCenters: []fdb.DataCenter{
										{
											ID:       "primary",
											Priority: 1,
										},
										{
											ID:        "primary-sat",
											Priority:  1,
											Satellite: 1,
										},
										{
											ID:        "remote-sat",
											Priority:  0,
											Satellite: 1,
										},
									},
								},
								{
									DataCenters: []fdb.DataCenter{
										{
											ID:       "remote",
											Priority: 0,
										},
										{
											ID:        "remote-sat",
											Priority:  1,
											Satellite: 1,
										},
										{
											ID:        "primary-sat",
											Priority:  0,
											Satellite: 1,
										},
									},
								},
							},
						},
					},
				}

			})

			It("should return the configuration string", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", false)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"satellite\\\":1}]},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\"},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"satellite\\\":1}]}]"))
			})

			It("should return the configuration string with the modified priority", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbtypes.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\"},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"satellite\\\":1}]},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"satellite\\\":1}]}]"))
			})
		})
	})
})
