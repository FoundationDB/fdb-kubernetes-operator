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
	ctx "context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("[plugin] configuration command", func() {
	When("getting the configuration string", func() {
		When("using a single region cluster", func() {
			var cluster *fdbv1beta2.FoundationDBCluster

			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							Regions: []fdbv1beta2.Region{
								{
									DataCenters: []fdbv1beta2.DataCenter{
										{
											ID:       "test",
											Priority: 1,
										},
									},
								},
							},
						},
						Version: fdbv1beta2.Versions.Default.String(),
					},
				}
			})

			It("should return the configuration string", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", false)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 resolvers=0 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"test\\\",\\\"priority\\\":1}]}]"))
			})

			It("should return the same configuration string with fail-over", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 resolvers=0 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"test\\\",\\\"priority\\\":1}]}]"))
			})
		})

		When("using a multi region cluster", func() {
			var cluster *fdbv1beta2.FoundationDBCluster

			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
							Regions: []fdbv1beta2.Region{
								{
									DataCenters: []fdbv1beta2.DataCenter{
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
									SatelliteLogs:           3,
									SatelliteRedundancyMode: fdbv1beta2.RedundancyModeOneSatelliteSingle,
								},
								{
									DataCenters: []fdbv1beta2.DataCenter{
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
									SatelliteLogs:           3,
									SatelliteRedundancyMode: fdbv1beta2.RedundancyModeOneSatelliteDouble,
								},
							},
						},
						Version: fdbv1beta2.Versions.Default.String(),
					},
				}

			})

			It("should return the configuration string", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", false)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 resolvers=0 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_single\\\"},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\"},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_double\\\"}]"))
			})

			It("should return the configuration string with the modified priority", func() {
				scheme := runtime.NewScheme()
				_ = clientgoscheme.AddToScheme(scheme)
				_ = fdbv1beta2.AddToScheme(scheme)
				kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

				configuration, err := getConfigurationString(kubeClient, "test", "test", true)
				Expect(err).NotTo(HaveOccurred())
				Expect(configuration).To(Equal("  usable_regions=0 logs=0 resolvers=0 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\"},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_single\\\"},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_double\\\"}]"))
			})

			When("Updating the config for the cluster", func() {
				It("should update the config with the modified priority", func() {
					scheme := runtime.NewScheme()
					_ = clientgoscheme.AddToScheme(scheme)
					_ = fdbv1beta2.AddToScheme(scheme)
					kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cluster).Build()

					err := updateConfig(kubeClient, "test", "test", true, false)
					Expect(err).NotTo(HaveOccurred())

					var resCluster fdbv1beta2.FoundationDBCluster
					err = kubeClient.Get(ctx.Background(), client.ObjectKey{
						Namespace: "test",
						Name:      "test",
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())

					config := resCluster.Spec.DatabaseConfiguration
					Expect(len(config.Regions)).To(BeNumerically("==", 2))

					Expect(config.Regions[0].SatelliteRedundancyMode).To(Equal(fdbv1beta2.RedundancyModeOneSatelliteSingle))
					Expect(config.Regions[0].SatelliteLogs).To(Equal(3))

					Expect(config.Regions[0].DataCenters[0].ID).To(Equal("primary"))
					Expect(config.Regions[0].DataCenters[0].Satellite).To(Equal(0))
					Expect(config.Regions[0].DataCenters[0].Priority).To(Equal(0))
					Expect(config.Regions[0].DataCenters[1].ID).To(Equal("primary-sat"))
					Expect(config.Regions[0].DataCenters[1].Satellite).To(Equal(1))
					Expect(config.Regions[0].DataCenters[1].Priority).To(Equal(1))
					Expect(config.Regions[0].DataCenters[2].ID).To(Equal("remote-sat"))
					Expect(config.Regions[0].DataCenters[2].Satellite).To(Equal(1))
					Expect(config.Regions[0].DataCenters[2].Priority).To(Equal(0))

					Expect(config.Regions[1].DataCenters[0].ID).To(Equal("remote"))
					Expect(config.Regions[1].DataCenters[0].Satellite).To(Equal(0))
					Expect(config.Regions[1].DataCenters[0].Priority).To(Equal(1))
					Expect(config.Regions[1].DataCenters[1].ID).To(Equal("remote-sat"))
					Expect(config.Regions[1].DataCenters[1].Satellite).To(Equal(1))
					Expect(config.Regions[1].DataCenters[1].Priority).To(Equal(1))
					Expect(config.Regions[1].DataCenters[2].ID).To(Equal("primary-sat"))
					Expect(config.Regions[1].DataCenters[2].Satellite).To(Equal(1))
					Expect(config.Regions[1].DataCenters[2].Priority).To(Equal(0))

					Expect(config.GetConfigurationString(fdbv1beta2.Versions.Default.String())).To(Equal("  usable_regions=0 logs=0 resolvers=0 log_routers=0 remote_logs=0 proxies=3 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"primary\\\"},{\\\"id\\\":\\\"primary-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_single\\\"},{\\\"datacenters\\\":[{\\\"id\\\":\\\"remote\\\",\\\"priority\\\":1},{\\\"id\\\":\\\"remote-sat\\\",\\\"priority\\\":1,\\\"satellite\\\":1},{\\\"id\\\":\\\"primary-sat\\\",\\\"satellite\\\":1}],\\\"satellite_logs\\\":3,\\\"satellite_redundancy_mode\\\":\\\"one_satellite_double\\\"}]"))
				})
			})
		})
	})
})
