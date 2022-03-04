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
	"fmt"
	"strings"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("monitor_conf", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var fakeConnectionString string
	var err error

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		err = NormalizeClusterSpec(cluster, DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())
		fakeConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
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
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.RunServers).NotTo(BeNil())
				Expect(*config.RunServers).To(BeFalse())
				Expect(config.Version).To(Equal(fdb.Versions.Default.String()))
			})
		})

		When("running a storage instance", func() {
			It("generates the conf", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Version).To(Equal(fdb.Versions.Default.String()))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.RunServers).To(BeNil())

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[0]).To(Equal(monitorapi.Argument{Value: "--cluster_file=/var/fdb/data/fdb.cluster"}))
				Expect(config.Arguments[1]).To(Equal(monitorapi.Argument{Value: "--seed_cluster_file=/var/dynamic-conf/fdb.cluster"}))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
				Expect(config.Arguments[3]).To(Equal(monitorapi.Argument{Value: "--class=storage"}))
				Expect(config.Arguments[4]).To(Equal(monitorapi.Argument{Value: "--logdir=/var/log/fdb-trace-logs"}))
				Expect(config.Arguments[5]).To(Equal(monitorapi.Argument{Value: "--loggroup=" + cluster.Name}))
				Expect(config.Arguments[6]).To(Equal(monitorapi.Argument{Value: "--datadir=/var/fdb/data"}))
				Expect(config.Arguments[7]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_instance_id="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
				}}))
				Expect(config.Arguments[8]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_machineid="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_MACHINE_ID"},
				}}))
				Expect(config.Arguments[9]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_zoneid="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_ZONE_ID"},
				}}))
			})
		})

		When("running a log instance", func() {
			It("generates the conf", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassLog, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Version).To(Equal(fdb.Versions.Default.String()))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.RunServers).To(BeNil())

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[3]).To(Equal(monitorapi.Argument{Value: "--class=log"}))
			})
		})

		When("using the split image type", func() {
			It("generates the conf", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeSplit, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Version).To(Equal(fdb.Versions.Default.String()))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.RunServers).To(BeNil())

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[0]).To(Equal(monitorapi.Argument{Value: "--cluster_file=/var/fdb/data/fdb.cluster"}))
				Expect(config.Arguments[1]).To(Equal(monitorapi.Argument{Value: "--seed_cluster_file=/var/dynamic-conf/fdb.cluster"}))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: ":"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
				Expect(config.Arguments[3]).To(Equal(monitorapi.Argument{Value: "--class=storage"}))
				Expect(config.Arguments[4]).To(Equal(monitorapi.Argument{Value: "--logdir=/var/log/fdb-trace-logs"}))
				Expect(config.Arguments[5]).To(Equal(monitorapi.Argument{Value: "--loggroup=" + cluster.Name}))
				Expect(config.Arguments[6]).To(Equal(monitorapi.Argument{Value: "--datadir=/var/fdb/data"}))
				Expect(config.Arguments[7]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_instance_id="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
				}}))
				Expect(config.Arguments[8]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_machineid="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_MACHINE_ID"},
				}}))
				Expect(config.Arguments[9]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_zoneid="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_ZONE_ID"},
				}}))
			})
		})

		When("running multiple processes", func() {
			It("adds a process ID argument", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 2, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[7]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_process_id="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
					{Value: "-"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType},
				}}))
				Expect(config.Arguments[8]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_instance_id="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
				}}))
			})

			It("includes the process number in the data directory", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 2, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments[6]).To(Equal(monitorapi.Argument{
					ArgumentType: monitorapi.ConcatenateArgumentType,
					Values: []monitorapi.Argument{
						{Value: "--datadir=/var/fdb/data/"},
						{ArgumentType: monitorapi.ProcessNumberArgumentType},
					},
				}))
			})
		})

		When("the public IP comes from the pod", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
			})

			It("does not have a listen address", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})
		})

		When("the public IP comes from the service", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Status.HasListenIPsForAllPods = true
			})

			It("adds a separate listen address", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
				Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--listen_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_POD_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})

			When("some pods do not have the listen IP environment variable", func() {
				BeforeEach(func() {
					cluster.Status.HasListenIPsForAllPods = false
				})

				It("does not have a listen address", func() {
					config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Arguments).To(HaveLen(baseArgumentLength))
					Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
						{Value: "--public_address=["},
						{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
						{Value: "]:"},
						{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
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
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4498, Multiplier: 2},
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
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4498, Multiplier: 2},
					{Value: ":tls"},
					{Value: ",["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
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
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[2]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4498, Multiplier: 2},
					{Value: ":tls"},
					{Value: ",["},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_PUBLIC_IP"},
					{Value: "]:"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: 4499, Multiplier: 2},
				}}))
			})
		})

		When("the cluster has custom parameters", func() {
			When("there are parameters in the general section", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdb.ProcessClass]fdbv1beta2.ProcessSettings{fdb.ProcessClassGeneral: {CustomParameters: fdb.FoundationDBCustomParameters{
						"knob_disable_posix_kernel_aio = 1",
					}}}
				})

				It("includes the custom parameters", func() {
					config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{Value: "--knob_disable_posix_kernel_aio=1"}))
				})
			})

			When("there are parameters on different process classes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdb.ProcessClass]fdbv1beta2.ProcessSettings{
						fdb.ProcessClassGeneral: {CustomParameters: fdb.FoundationDBCustomParameters{
							"knob_disable_posix_kernel_aio = 1",
						}},
						fdb.ProcessClassStorage: {CustomParameters: fdb.FoundationDBCustomParameters{
							"knob_test = test1",
						}},
						fdb.ProcessClassStateless: {CustomParameters: fdb.FoundationDBCustomParameters{
							"knob_test = test2",
						}},
					}
				})

				It("includes the custom parameters for that class", func() {
					config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{Value: "--knob_test=test1"}))
				})
			})
		})

		When("the cluster has an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
			})

			It("uses the variable as the zone ID", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))

				Expect(config.Arguments[9]).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_zoneid="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "RACK"},
				}}))
			})
		})

		When("the spec has custom peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
			})

			It("includes the verification rules", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{Value: "--tls_verify_peers=S.CN=foundationdb.org"}))
			})
		})

		When("the spec has a custom log group", func() {
			BeforeEach(func() {
				cluster.Spec.LogGroup = "test-fdb-cluster"
			})

			It("includes the log group", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[5]).To(Equal(monitorapi.Argument{Value: "--loggroup=test-fdb-cluster"}))
			})
		})

		When("the spec has a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
			})

			It("adds an argument for the data center", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{Value: "--locality_dcid=dc01"}))
			})
		})

		When("the spec has a data hall", func() {
			BeforeEach(func() {
				cluster.Spec.DataHall = "dh01"
			})

			It("adds an argument for the data hall", func() {
				config, err := GetMonitorProcessConfiguration(cluster, fdb.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{Value: "--locality_data_hall=dh01"}))
			})
		})
	})

	Describe("GetStartCommand", func() {
		var pod *corev1.Pod
		var command string
		var address string
		var processClass = fdb.ProcessClassStorage
		var processGroupID = "storage-1"

		BeforeEach(func() {
			pod, err = GetPod(cluster, fdb.ProcessClassStorage, 1)
			Expect(err).NotTo(HaveOccurred())
			address = pod.Status.PodIP
		})

		Context("for a basic storage process", func() {
			It("should substitute the variables in the start command", func() {
				podClient, err := NewMockFdbPodClient(cluster, pod)
				Expect(err).NotTo(HaveOccurred())
				command, err = GetStartCommand(cluster, processClass, podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())

				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", processGroupID),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4501", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})

			Context("with custom parameters with substitutions", func() {
				It("should substitute the variables in the custom parameters", func() {
					settings := cluster.Spec.Processes["general"]
					settings.CustomParameters = []fdb.FoundationDBCustomParameter{"locality_disk_id=$FDB_INSTANCE_ID"}
					cluster.Spec.Processes["general"] = settings

					podClient, err := NewMockFdbPodClient(cluster, pod)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommand(cluster, processClass, podClient, 1, 1)
					Expect(err).NotTo(HaveOccurred())

					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data",
						fmt.Sprintf("--locality_disk_id=%s", processGroupID),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})
		})

		When("using the unified image", func() {
			BeforeEach(func() {
				cluster.Spec.UseUnifiedImage = pointer.Bool(true)
			})

			It("should generate the unsorted command-line", func() {
				podClient, err := NewMockFdbPodClient(cluster, pod)
				Expect(err).NotTo(HaveOccurred())
				command, err = GetStartCommand(cluster, processClass, podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())

				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("--public_address=[%s]:4501", address),
					"--class=storage",
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", processGroupID),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
				}, " ")))
			})

			When("the pod has multiple processes", func() {
				It("should fill in the process number", func() {
					podClient, err := NewMockFdbPodClient(cluster, pod)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommand(cluster, processClass, podClient, 2, 3)
					Expect(err).NotTo(HaveOccurred())

					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("--public_address=[%s]:4503", address),
						"--class=storage",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						"--datadir=/var/fdb/data/2",
						fmt.Sprintf("--locality_process_id=%s-2", processGroupID),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
					}, " ")))
				})
			})
		})

		When("using the split image", func() {
			BeforeEach(func() {
				cluster.Spec.UseUnifiedImage = pointer.Bool(false)
			})

			It("should generate the sorted command-line", func() {
				podClient, err := NewMockFdbPodClient(cluster, pod)
				Expect(err).NotTo(HaveOccurred())
				command, err = GetStartCommand(cluster, processClass, podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())

				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", processGroupID),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4501", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("for a basic storage process with multiple storage servers per Pod", func() {
			It("should substitute the variables in the start command", func() {
				podClient, err := NewMockFdbPodClient(cluster, pod)
				Expect(err).NotTo(HaveOccurred())
				command, err = GetStartCommand(cluster, processClass, podClient, 1, 2)
				Expect(err).NotTo(HaveOccurred())

				id := "storage-1"
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data/1",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_process_id=%s-1", id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4501", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))

				command, err = GetStartCommand(cluster, processClass, podClient, 2, 2)
				Expect(err).NotTo(HaveOccurred())
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data/2",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_process_id=%s-2", id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4503", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with host replication", func() {
			BeforeEach(func() {
				pod.Spec.NodeName = "machine1"
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{}

				podClient, _ := NewMockFdbPodClient(cluster, pod)
				command, err = GetStartCommand(cluster, fdb.ProcessClassStorage, podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should provide the host information in the start command", func() {
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					"--locality_instance_id=storage-1",
					"--locality_machineid=machine1",
					"--locality_zoneid=machine1",
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4501", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with cross-Kubernetes replication", func() {
			BeforeEach(func() {
				pod.Spec.NodeName = "machine1"

				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:   "foundationdb.org/kubernetes-cluster",
					Value: "kc2",
				}

				podClient, _ := NewMockFdbPodClient(cluster, pod)
				command, err = GetStartCommand(cluster, fdb.ProcessClassStorage, podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the zone ID in the start command", func() {
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					"--locality_instance_id=storage-1",
					"--locality_machineid=machine1",
					"--locality_zoneid=kc2",
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4501", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with binaries from the main container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdb.Versions.Default.String()
				cluster.Status.RunningVersion = fdb.Versions.Default.String()
				podClient, _ := NewMockFdbPodClient(cluster, pod)

				command, err = GetStartCommand(cluster, fdb.ProcessClassStorage, podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("includes the binary path in the start command", func() {
				id := pod.Labels[fdb.FDBProcessGroupIDLabel]
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					fmt.Sprintf("--public_address=%s:4501", address),
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})
	})

	Describe("GetMonitorConf", func() {
		var conf string
		var err error

		BeforeEach(func() {
			cluster.Status.ConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
		})

		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with DNS names enabled", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"locality_dns_name = $FDB_DNS_NAME",
				}, "\n")))
			})
		})

		Context("with a basic storage instance with multiple storage servers per Pod", func() {
			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf with two processes", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/1",
					"locality_process_id = $FDB_INSTANCE_ID-1",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"[fdbserver.2]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4503",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/2",
					"locality_process_id = $FDB_INSTANCE_ID-2",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with the public IP from the pod", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with the public IP from the service", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Status.HasListenIPsForAllPods = true
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"listen_address = $FDB_POD_IP:4501",
				}, "\n")))
			})

			Context("with pods without the listen IP environment variable", func() {
				BeforeEach(func() {
					cluster.Status.HasListenIPsForAllPods = false
					conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, 1)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should generate the storage conf", func() {
					Expect(conf).To(Equal(strings.Join([]string{
						"[general]",
						"kill_on_configuration_change = false",
						"restart_delay = 60",
						"[fdbserver.1]",
						"command = $BINARY_DIR/fdbserver",
						"cluster_file = /var/fdb/data/fdb.cluster",
						"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
						"public_address = $FDB_PUBLIC_IP:4501",
						"class = storage",
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						"locality_instance_id = $FDB_INSTANCE_ID",
						"locality_machineid = $FDB_MACHINE_ID",
						"locality_zoneid = $FDB_ZONE_ID",
					}, "\n")))
				})
			})
		})

		Context("with TLS enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the TLS flag in the address", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4500:tls",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a transition to TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true

				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include both addresses", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4500:tls,$FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a transition to non-TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = false
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true

				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include both addresses", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4500:tls,$FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with custom parameters", func() {
			Context("with general parameters", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdb.ProcessClass]fdbv1beta2.ProcessSettings{fdb.ProcessClassGeneral: {CustomParameters: fdb.FoundationDBCustomParameters{
						"knob_disable_posix_kernel_aio = 1",
					}}}
					conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
					Expect(err).NotTo(HaveOccurred())
				})

				It("should include the custom parameters", func() {
					Expect(conf).To(Equal(strings.Join([]string{
						"[general]",
						"kill_on_configuration_change = false",
						"restart_delay = 60",
						"[fdbserver.1]",
						"command = $BINARY_DIR/fdbserver",
						"cluster_file = /var/fdb/data/fdb.cluster",
						"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
						"public_address = $FDB_PUBLIC_IP:4501",
						"class = storage",
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						"locality_instance_id = $FDB_INSTANCE_ID",
						"locality_machineid = $FDB_MACHINE_ID",
						"locality_zoneid = $FDB_ZONE_ID",
						"knob_disable_posix_kernel_aio = 1",
					}, "\n")))
				})
			})

			Context("with process-class parameters", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdb.ProcessClass]fdbv1beta2.ProcessSettings{
						fdb.ProcessClassGeneral: {CustomParameters: fdb.FoundationDBCustomParameters{
							"knob_disable_posix_kernel_aio = 1",
						}},
						fdb.ProcessClassStorage: {CustomParameters: fdb.FoundationDBCustomParameters{
							"knob_test = test1",
						}},
						fdb.ProcessClassStateless: {CustomParameters: fdb.FoundationDBCustomParameters{
							"knob_test = test2",
						}},
					}
					conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
					Expect(err).NotTo(HaveOccurred())
				})

				It("should include the custom parameters", func() {
					Expect(conf).To(Equal(strings.Join([]string{
						"[general]",
						"kill_on_configuration_change = false",
						"restart_delay = 60",
						"[fdbserver.1]",
						"command = $BINARY_DIR/fdbserver",
						"cluster_file = /var/fdb/data/fdb.cluster",
						"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
						"public_address = $FDB_PUBLIC_IP:4501",
						"class = storage",
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						"locality_instance_id = $FDB_INSTANCE_ID",
						"locality_machineid = $FDB_MACHINE_ID",
						"locality_zoneid = $FDB_ZONE_ID",
						"knob_test = test1",
					}, "\n")))
				})
			})
		})

		Context("with an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should use the variable as the zone ID", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $RACK",
				}, "\n")))
			})
		})

		Context("with peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the verification rules", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"tls_verify_peers = S.CN=foundationdb.org",
				}, "\n")))
			})
		})

		Context("with a custom log group", func() {
			BeforeEach(func() {
				cluster.Spec.LogGroup = "test-fdb-cluster"
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the log group", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = test-fdb-cluster",
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
				conf, err = GetMonitorConf(cluster, fdb.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the log group", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"locality_dcid = dc01",
				}, "\n")))
			})
		})
	})

})
