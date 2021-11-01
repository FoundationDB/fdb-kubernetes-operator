/*
 * monitor_conf_test.go
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

package internal

import (
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("pod_models", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var fakeConnectionString string
	var err error

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		err = NormalizeClusterSpec(cluster, DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())
		fakeConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
		print(fakeConnectionString)
	})

	Context("GetUnifedMonitorConf", func() {
		var baseArgumentLength = 10
		BeforeEach(func() {
			cluster.Status.ConnectionString = fakeConnectionString
		})

		When("there is no connection string", func() {
			It("generates conf with an no processes", func() {
				Expect(cluster).NotTo(BeNil())
				cluster.Status.ConnectionString = ""
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.ServerCount).To(Equal(0))
			})
		})

		When("running a storage instance", func() {
			It("generates the conf", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Version).To(Equal(fdbtypes.Versions.Default.String()))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.ServerCount).To(Equal(1))

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[0]).To(Equal(KubernetesMonitorArgument{Value: "--cluster_file=/var/fdb/data/fdb.cluster"}))
				Expect(config.Arguments[1]).To(Equal(KubernetesMonitorArgument{Value: "--seed_cluster_file=/var/dynamic-conf/fdb.cluster"}))
				Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--public_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
				Expect(config.Arguments[3]).To(Equal(KubernetesMonitorArgument{Value: "--class=storage"}))
				Expect(config.Arguments[4]).To(Equal(KubernetesMonitorArgument{Value: "--logdir=/var/log/fdb-trace-logs"}))
				Expect(config.Arguments[5]).To(Equal(KubernetesMonitorArgument{Value: "--loggroup=" + cluster.Name}))
				Expect(config.Arguments[6]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--datadir=/var/fdb/data/"},
					{ArgumentType: ProcessNumberArgumentType},
				}}))
				Expect(config.Arguments[7]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--locality_instance_id="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
				}}))
				Expect(config.Arguments[8]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--locality_machineid="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_MACHINE_ID"},
				}}))
				Expect(config.Arguments[9]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--locality_zoneid="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_ZONE_ID"},
				}}))
			})
		})

		When("the public IP comes from the pod", func() {
			BeforeEach(func() {
				source := fdbtypes.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
			})

			It("does not havce a listen address", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--public_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})
		})

		When("the public IP comes from the service", func() {
			BeforeEach(func() {
				source := fdbtypes.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Status.HasListenIPsForAllPods = true
			})

			It("adds a separate listen address", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--public_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
				Expect(config.Arguments[10]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--listen_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_POD_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})

			When("some pods do not have the listen IP environment variable", func() {
				BeforeEach(func() {
					cluster.Status.HasListenIPsForAllPods = false
				})

				It("does not havce a listen address", func() {
					config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Arguments).To(HaveLen(baseArgumentLength))
					Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
						{Value: "--public_address="},
						{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
						{Value: ":"},
						{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
					}}))
				})
			})
		})

		When("TLS is enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
			})

			It("includes the TLS flag in the address", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--public_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4498, Multiplier: 2},
					{Value: ":tls"},
				}}))
			})
		})

		Context("with a transition to TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true
			})

			It("includes both addresses", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--public_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4498, Multiplier: 2},
					{Value: ":tls"},
					{Value: ","},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})
		})

		Context("with a transition to non-TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = false
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true
			})

			It("includes both addresses", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--public_address="},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4498, Multiplier: 2},
					{Value: ":tls"},
					{Value: ","},
					{ArgumentType: EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})
		})

		When("the cluster has custom parameters", func() {
			When("there are parameters in the general section", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{
						"knob_disable_posix_kernel_aio = 1",
					}}}
				})

				It("includes the custom parameters", func() {
					config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(KubernetesMonitorArgument{Value: "--knob_disable_posix_kernel_aio=1"}))
				})
			})

			When("there are parameters on different process classes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
						fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{
							"knob_disable_posix_kernel_aio = 1",
						}},
						fdbtypes.ProcessClassStorage: {CustomParameters: &[]string{
							"knob_test = test1",
						}},
						fdbtypes.ProcessClassStateless: {CustomParameters: &[]string{
							"knob_test = test2",
						}},
					}
				})

				It("includes the custom parameters for that class", func() {
					config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(KubernetesMonitorArgument{Value: "--knob_test=test1"}))
				})
			})
		})

		When("the cluster has an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
			})

			It("uses the variable as the zone ID", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))

				Expect(config.Arguments[9]).To(Equal(KubernetesMonitorArgument{ArgumentType: ConcatenateArgumentType, Values: []KubernetesMonitorArgument{
					{Value: "--locality_zoneid="},
					{ArgumentType: EnvironmentArgumentType, Source: "RACK"},
				}}))
			})
		})

		When("the spec has custom peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
			})

			It("includes the verification rules", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[10]).To(Equal(KubernetesMonitorArgument{Value: "--tls_verify_peers=S.CN=foundationdb.org"}))
			})
		})

		When("the spec has a custom log group", func() {
			BeforeEach(func() {
				cluster.Spec.LogGroup = "test-fdb-cluster"
			})

			It("includes the log group", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[5]).To(Equal(KubernetesMonitorArgument{Value: "--loggroup=test-fdb-cluster"}))
			})
		})

		When("the spec has a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
			})

			It("adds an argument for the data center", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[10]).To(Equal(KubernetesMonitorArgument{Value: "--locality_dcid=dc01"}))
			})
		})

		When("the spec has a data hall", func() {
			BeforeEach(func() {
				cluster.Spec.DataHall = "dh01"
			})

			It("adds an argument for the data hall", func() {
				config, err := GetUnifiedMonitorConf(cluster, fdbtypes.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[10]).To(Equal(KubernetesMonitorArgument{Value: "--locality_data_hall=dh01"}))
			})
		})
	})
})
