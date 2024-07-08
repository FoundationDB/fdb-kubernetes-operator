/*
 * admin_client_test.go
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

package fdbclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"time"

	"k8s.io/utils/pointer"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("admin_client_test", func() {
	When("parsing the connection string", func() {
		DescribeTable("it should return the correct connection string",
			func(input string, expected string) {
				connectingString, err := fdbv1beta2.ParseConnectionString(cleanConnectionStringOutput(input))
				Expect(err).NotTo(HaveOccurred())
				Expect(connectingString.String()).To(Equal(expected))
			},
			Entry("with a correct response from FDB",
				">>> option on ACCESS_SYSTEM_KEYS\\nOption enabled for all transactions\\n>>> get \\\\xff/coordinators\\n`\\\\xff/coordinators' is `fdb_cluster_52v1bpr8:rhUbBjrtyweZBQO1U3Td81zyP9d46yEh@100.82.81.253:4500:tls,100.82.71.5:4500:tls,100.82.119.151:4500:tls,100.82.122.125:4500:tls,100.82.76.240:4500:tls'\\n",
				"fdb_cluster_52v1bpr8:rhUbBjrtyweZBQO1U3Td81zyP9d46yEh@100.82.81.253:4500:tls,100.82.71.5:4500:tls,100.82.119.151:4500:tls,100.82.122.125:4500:tls,100.82.76.240:4500:tls",
			),

			Entry("without the byte string response",
				"fdb_cluster_52v1bpr8:rhUbBjrtyweZBQO1U3Td81zyP9d46yEh@100.82.81.253:4500:tls,100.82.71.5:4500:tls,100.82.119.151:4500:tls,100.82.122.125:4500:tls,100.82.76.240:4500:tls",
				"fdb_cluster_52v1bpr8:rhUbBjrtyweZBQO1U3Td81zyP9d46yEh@100.82.81.253:4500:tls,100.82.71.5:4500:tls,100.82.119.151:4500:tls,100.82.122.125:4500:tls,100.82.76.240:4500:tls",
			),
		)
	})

	When("getting the log dir parameter", func() {
		DescribeTable("it should return the correct format of the log dir paramater",
			func(cmd cliCommand, expected string) {
				Expect(cmd.getLogDirParameter()).To(Equal(expected))
			},
			Entry("no binary set",
				cliCommand{
					binary: "",
				},
				"--log-dir",
			),
			Entry("fdbcli binary set",
				cliCommand{
					binary: fdbcliStr,
				},
				"--log-dir",
			),
			Entry("fdbbackup binary set",
				cliCommand{
					binary: fdbbackupStr,
				},
				"--logdir",
			),
			Entry("fdbrestore binary set",
				cliCommand{
					binary: fdbrestoreStr,
				},
				"--logdir",
			),
		)
	})

	When("getting the binary name", func() {
		DescribeTable("it should return the correct binary name",
			func(cmd cliCommand, expected string) {
				Expect(cmd.getBinary()).To(Equal(expected))
			},
			Entry("no binary set",
				cliCommand{
					binary: "",
				},
				fdbcliStr,
			),
			Entry("fdbcli binary set",
				cliCommand{
					binary: fdbcliStr,
				},
				fdbcliStr,
			),
			Entry("fdbbackup binary set",
				cliCommand{
					binary: fdbbackupStr,
				},
				fdbbackupStr,
			),
			Entry("fdbrestore binary set",
				cliCommand{
					binary: fdbrestoreStr,
				},
				fdbrestoreStr,
			),
		)
	})

	When("checking if the binary is fdbcli", func() {
		DescribeTable("it should return the correct output",
			func(cmd cliCommand, expected bool) {
				Expect(cmd.isFdbCli()).To(Equal(expected))
			},
			Entry("no binary set",
				cliCommand{
					binary: "",
				},
				true,
			),
			Entry("fdbcli binary set",
				cliCommand{
					binary: fdbcliStr,
				},
				true,
			),
			Entry("fdbbackup binary set",
				cliCommand{
					binary: fdbbackupStr,
				},
				false,
			),
			Entry("fdbrestore binary set",
				cliCommand{
					binary: fdbrestoreStr,
				},
				false,
			),
		)
	})

	When("getting the version to run the command", func() {
		DescribeTable("it should return the correct output",
			func(cmd cliCommand, cluster *fdbv1beta2.FoundationDBCluster, expected string) {
				Expect(cmd.getVersion(cluster)).To(Equal(expected))
			},
			Entry("version is set set",
				cliCommand{
					version: "7.1.15",
				},
				nil,
				"7.1.15",
			),
			Entry("version is not set and running version is defined",
				cliCommand{},
				&fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						Version: "7.1.25",
					},
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "7.1.15",
					},
				},
				"7.1.15",
			),
			Entry("version is not set and running version is  not defined",
				cliCommand{},
				&fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						Version: "7.1.25",
					},
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: "",
					},
				},
				"7.1.25",
			),
		)
	})

	DescribeTable("getting the args for the command", func(command cliCommand, client *cliAdminClient, traceOption string, traceFormat string, expectedArgs []string, expectedTimeout time.Duration) {
		Expect(client).NotTo(BeNil())
		if traceOption != "" {
			GinkgoT().Setenv("FDB_NETWORK_OPTION_TRACE_ENABLE", traceOption)
		}
		if traceFormat != "" {
			GinkgoT().Setenv("FDB_NETWORK_OPTION_TRACE_FORMAT", traceFormat)
		}

		args, timeout := client.getArgsAndTimeout(command)
		Expect(timeout).To(Equal(expectedTimeout))
		Expect(args).To(HaveExactElements(expectedArgs))
	},
		Entry("using fdbcli and trace options are disabled",
			cliCommand{
				args:    []string{"--version"},
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"",
			"",
			[]string{
				"--version",
				"--timeout",
				"1",
			},
			2*time.Second,
		),
		Entry("using fdbcli and trace options are disabled",
			cliCommand{
				command: "maintenance off",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"",
			"",
			[]string{
				"--exec",
				"maintenance off",
				"-C",
				"test",
				"--timeout",
				"1",
			},
			2*time.Second,
		),
		Entry("using fdbcli and trace options are enabled",
			cliCommand{
				command: "maintenance off",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"/tmp",
			"",
			[]string{
				"--exec",
				"maintenance off",
				"-C",
				"test",
				"--log",
				"--trace_format",
				"xml",
				"--log-dir",
				"/tmp",
				"--timeout",
				"1",
			},
			2*time.Second,
		),
		Entry("using fdbcli and trace options are enabled with a different format",
			cliCommand{
				command: "maintenance off",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"/tmp",
			"json",
			[]string{
				"--exec",
				"maintenance off",
				"-C",
				"test",
				"--log",
				"--trace_format",
				"json",
				"--log-dir",
				"/tmp",
				"--timeout",
				"1",
			},
			2*time.Second,
		),
		Entry("using fdbcli and trace options are disabled and client knobs are passed",
			cliCommand{
				command: "maintenance off",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
				knobs:           []string{"--knob-testing=1"},
			},
			"",
			"",
			[]string{
				"--exec",
				"maintenance off",
				"-C",
				"test",
				"--timeout",
				"1",
			},
			2*time.Second,
		),
		// Tests for fdbrestore
		Entry("using fdbrestore and trace options are disabled",
			cliCommand{
				binary:  fdbrestoreStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"",
			"",
			[]string{
				"--exec",
				"status",
				"--dest_cluster_file",
				"test",
			},
			1*time.Second,
		),
		Entry("using fdbrestore and trace options are enabled",
			cliCommand{
				binary:  fdbrestoreStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"/tmp",
			"",
			[]string{
				"--exec",
				"status",
				"--dest_cluster_file",
				"test",
				"--log",
				"--logdir",
				"/tmp",
			},
			1*time.Second,
		),
		Entry("using fdbrestore and trace options are enabled and a different format is set",
			cliCommand{
				binary:  fdbrestoreStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"/tmp",
			"json",
			[]string{
				"--exec",
				"status",
				"--dest_cluster_file",
				"test",
				"--log",
				"--logdir",
				"/tmp",
			},
			1*time.Second,
		),
		Entry("using fdbrestore and trace options are disabled and client knobs are set",
			cliCommand{
				binary:  fdbrestoreStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
				knobs:           []string{"--knob-testing=1"},
			},
			"",
			"",
			[]string{
				"--exec",
				"status",
				"--dest_cluster_file",
				"test",
				"--knob-testing=1",
			},
			1*time.Second,
		),
		// Tests for fdbbackup
		Entry("using fdbbackup and trace options are disabled",
			cliCommand{
				binary:  fdbbackupStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"",
			"",
			[]string{
				"--exec",
				"status",
				"-C",
				"test",
			},
			1*time.Second,
		),
		Entry("using fdbbackup and trace options are enabled",
			cliCommand{
				binary:  fdbbackupStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"/tmp",
			"",
			[]string{
				"--exec",
				"status",
				"-C",
				"test",
				"--log",
				"--logdir",
				"/tmp",
			},
			1*time.Second,
		),
		Entry("using fdbbackup and trace options are enabled and a different format is set",
			cliCommand{
				binary:  fdbbackupStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
			},
			"/tmp",
			"json",
			[]string{
				"--exec",
				"status",
				"-C",
				"test",
				"--log",
				"--logdir",
				"/tmp",
			},
			1*time.Second,
		),
		Entry("using fdbbackup and trace options are disabled and client knobs are set",
			cliCommand{
				binary:  fdbbackupStr,
				command: "status",
				version: "7.1.25",
				timeout: 1 * time.Second,
			},
			&cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
				knobs:           []string{"--knob-testing=1"},
			},
			"",
			"",
			[]string{
				"--exec",
				"status",
				"-C",
				"test",
				"--knob-testing=1",
			},
			1*time.Second,
		),
	)

	When("getting the protocol version from fdbcli", func() {
		var mockRunner *mockCommandRunner
		var protocolVersion string
		var err error

		JustBeforeEach(func() {
			cliClient := &cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
				cmdRunner:       mockRunner,
			}

			protocolVersion, err = cliClient.GetProtocolVersion("7.1.21")
		})

		When("the fdbcli call returns the expected output", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: nil,
					mockedOutput: []string{`fdbcli --version
FoundationDB CLI 7.1 (v7.1.21)
source version e9f38c7169d21dde901b7b9408e1c5a8df182d64
protocol fdb00b071010000`,
					},
				}
			})

			It("should report the protocol version", func() {
				Expect(protocolVersion).To(Equal("fdb00b071010000"))
				Expect(err).NotTo(HaveOccurred())
				Expect(mockRunner.receivedBinary[0]).To(Equal("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedArgs[0]).To(ContainElements("--version"))
			})
		})

		When("an error is returned", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: []error{
						errors.New("boom"),
					},
					mockedOutput: []string{`fdbcli --version
FoundationDB CLI 7.1 (v7.1.21)
source version e9f38c7169d21dde901b7b9408e1c5a8df182d64
protocol fdb00b071010000`,
					},
				}
			})

			It("should report the error", func() {
				Expect(protocolVersion).To(Equal(""))
				Expect(err).To(HaveOccurred())
				Expect(mockRunner.receivedBinary[0]).To(Equal("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedArgs[0]).To(ContainElements("--version"))
			})
		})

		When("the protocol version is missing", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: []error{
						errors.New("boom"),
					},
					mockedOutput: []string{""},
				}
			})

			It("should report the error", func() {
				Expect(protocolVersion).To(Equal(""))
				Expect(err).To(HaveOccurred())
				Expect(mockRunner.receivedBinary[0]).To(Equal("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedArgs[0]).To(ContainElements("--version"))
			})
		})
	})

	When("validating if the version is supported", func() {
		var mockRunner *mockCommandRunner
		var supported bool
		var err error

		JustBeforeEach(func() {
			cliClient := &cliAdminClient{
				Cluster:         nil,
				clusterFilePath: "test",
				log:             logr.Discard(),
				cmdRunner:       mockRunner,
			}

			supported, err = cliClient.VersionSupported(fdbv1beta2.Versions.Default.String())
		})

		When("the binary does not exist", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError:  nil,
					mockedOutput: []string{""},
				}
			})

			It("should return an error", func() {
				Expect(supported).To(BeFalse())
				Expect(err).To(HaveOccurred())
			})
		})

		When("the binary exists", func() {
			BeforeEach(func() {
				tmpDir := GinkgoT().TempDir()
				GinkgoT().Setenv("FDB_BINARY_DIR", tmpDir)

				binaryDir := path.Join(tmpDir, fdbv1beta2.Versions.Default.GetBinaryVersion())
				Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
				_, err := os.Create(path.Join(binaryDir, fdbcliStr))
				Expect(err).NotTo(HaveOccurred())

				mockRunner = &mockCommandRunner{
					mockedError:  nil,
					mockedOutput: []string{""},
				}
			})

			It("should return that the version is supported", func() {
				Expect(supported).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	When("getting the status from a cluster that is being upgraded", func() {
		var mockRunner *mockCommandRunner
		var status *fdbv1beta2.FoundationDBStatus
		var err error
		var oldBinary, newBinary string

		JustBeforeEach(func() {
			cliClient := &cliAdminClient{
				Cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						Version: fdbv1beta2.Versions.NextMajorVersion.String(),
					},
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: fdbv1beta2.Versions.Default.String(),
					},
				},
				clusterFilePath: "test",
				log:             logr.Discard(),
				cmdRunner:       mockRunner,
			}

			status, err = cliClient.getStatus()
		})

		BeforeEach(func() {
			tmpDir := GinkgoT().TempDir()
			GinkgoT().Setenv("FDB_BINARY_DIR", tmpDir)

			binaryDir := path.Join(tmpDir, fdbv1beta2.Versions.Default.GetBinaryVersion())
			Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
			oldBinary = path.Join(binaryDir, fdbcliStr)
			_, err := os.Create(oldBinary)
			Expect(err).NotTo(HaveOccurred())

			binaryDir = path.Join(tmpDir, fdbv1beta2.Versions.NextMajorVersion.GetBinaryVersion())
			Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
			newBinary = path.Join(binaryDir, fdbcliStr)
			_, err = os.Create(newBinary)
			Expect(err).NotTo(HaveOccurred())
		})

		When("the old version returns the correct result", func() {
			BeforeEach(func() {
				inputStatus := &fdbv1beta2.FoundationDBStatus{
					Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
						DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
							Available: true,
						},
					},
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {},
						},
					},
				}

				out, err := json.Marshal(inputStatus)
				Expect(err).NotTo(HaveOccurred())

				mockRunner = &mockCommandRunner{
					mockedError:  nil,
					mockedOutput: []string{string(out)},
				}
			})

			It("should return the correct status", func() {
				Expect(status).NotTo(BeNil())
				Expect(status.Cluster.Processes).To(HaveLen(1))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("the old version returns the wrong result", func() {
			emptyStatus := &fdbv1beta2.FoundationDBStatus{}

			emptyOut, err := json.Marshal(emptyStatus)
			Expect(err).NotTo(HaveOccurred())

			inputStatus := &fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": {},
					},
				},
			}

			statusOut, err := json.Marshal(inputStatus)
			Expect(err).NotTo(HaveOccurred())

			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: nil,
					mockedOutputPerBinary: map[string]string{
						oldBinary: string(emptyOut),
						newBinary: string(statusOut),
					},
				}
			})

			It("should return the correct status", func() {
				Expect(status).NotTo(BeNil())
				Expect(status.Cluster.Processes).To(HaveLen(1))
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	When("excluding a set of processes", func() {
		var mockRunner *mockCommandRunner
		var useNonBlockingExcludes bool

		JustBeforeEach(func() {
			cluster := &fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "6.3.25",
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						UseNonBlockingExcludes: pointer.Bool(useNonBlockingExcludes),
					},
				},
			}

			cliClient := &cliAdminClient{
				Cluster:         cluster,
				clusterFilePath: "test",
				log:             logr.Discard(),
				cmdRunner:       mockRunner,
			}

			Expect(cliClient.ExcludeProcesses([]fdbv1beta2.ProcessAddress{{
				IPAddress: net.ParseIP("127.0.0.1"),
				Port:      4500,
			}})).NotTo(HaveOccurred())
		})

		BeforeEach(func() {
			tmpDir := GinkgoT().TempDir()
			GinkgoT().Setenv("FDB_BINARY_DIR", tmpDir)

			binaryDir := path.Join(tmpDir, "6.3")
			Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
			_, err := os.Create(path.Join(binaryDir, fdbcliStr))
			Expect(err).NotTo(HaveOccurred())

			mockRunner = &mockCommandRunner{
				mockedError:  nil,
				mockedOutput: []string{""},
			}
		})

		When("the cluster specifies that blocking exclusions should be used", func() {
			It("should return that the exclusion command is called without no_wait", func() {
				Expect(mockRunner.receivedArgs[0]).To(ContainElement("exclude 127.0.0.1:4500"))
			})
		})

		When("the cluster specifies that non-blocking exclusions should be used", func() {
			BeforeEach(func() {
				useNonBlockingExcludes = true
			})

			It("should return that the exclusion command is called with no_wait", func() {
				Expect(mockRunner.receivedArgs[0]).To(ContainElement("exclude no_wait 127.0.0.1:4500"))
			})
		})
	})

	When("checking if processes can safely be removed", func() {
		var mockRunner *mockCommandRunner
		var mockFdbClient *mockFdbLibClient
		var addressesToCheck []fdbv1beta2.ProcessAddress
		var result []fdbv1beta2.ProcessAddress
		var err error

		JustBeforeEach(func() {
			cluster := &fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.Default.String(),
				},
			}

			cliClient := &cliAdminClient{
				Cluster:         cluster,
				clusterFilePath: "test",
				log:             logr.Discard(),
				cmdRunner:       mockRunner,
				fdbLibClient:    mockFdbClient,
			}

			result, err = cliClient.CanSafelyRemove(addressesToCheck)
			Expect(mockFdbClient.requestedKey).To(Equal("\xff\xff/status/json"))
		})

		BeforeEach(func() {
			tmpDir := GinkgoT().TempDir()
			GinkgoT().Setenv("FDB_BINARY_DIR", tmpDir)

			binaryDir := path.Join(tmpDir, fdbv1beta2.Versions.Default.GetBinaryVersion())
			Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
			_, err := os.Create(path.Join(binaryDir, fdbcliStr))
			Expect(err).NotTo(HaveOccurred())

			status := &fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
				},
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"1": { // This process is fully excluded
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("192.168.0.1"),
								Port:      4500,
							},
							Excluded: true,
						},
						"2": { // This process is fully excluded
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("192.168.0.2"),
								Port:      4500,
							},
							Excluded: true,
						},
						"3": { // This process is not excluded
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("192.168.0.3"),
								Port:      4500,
							},
							Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
								{
									Role: "test",
								},
							},
						},
						"4": { // This process is marked as excluded but has a role
							Address: fdbv1beta2.ProcessAddress{
								IPAddress: net.ParseIP("192.168.0.4"),
								Port:      4500,
							},
							Excluded: true,
							Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
								{
									Role: "test",
								},
							},
						},
					},
				},
			}

			statusBytes, err := json.Marshal(status)
			Expect(err).NotTo(HaveOccurred())

			mockFdbClient = &mockFdbLibClient{
				mockedOutput: statusBytes,
			}

			mockRunner = &mockCommandRunner{
				mockedError:  nil,
				mockedOutput: []string{""},
			}
		})

		When("all provided processes are fully excluded", func() {
			BeforeEach(func() {
				addressesToCheck = []fdbv1beta2.ProcessAddress{
					{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
				}
			})

			It("should return an empty list and no error", func() {
				Expect(result).To(HaveLen(0))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should issue an exclude command to verify the exclusion", func() {
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix(fdbcliStr))
				Expect(mockRunner.receivedArgs[0]).To(ContainElements("exclude 192.168.0.1:4500 192.168.0.2:4500"))
			})
		})

		When("all provided processes are fully excluded in the status but the exclude command returns an error", func() {
			BeforeEach(func() {
				addressesToCheck = []fdbv1beta2.ProcessAddress{
					{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
				}

				mockRunner = &mockCommandRunner{
					mockedError: []error{
						fdbv1beta2.TimeoutError{Err: fmt.Errorf("timed out")},
					},
					mockedOutput: []string{""},
				}
			})

			It("should return an empty list and an error", func() {
				Expect(result).To(HaveLen(0))
				Expect(err).To(HaveOccurred())
			})

			It("should issue an exclude command to verify the exclusion", func() {
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix(fdbcliStr))
				Expect(mockRunner.receivedArgs[0]).To(ContainElements("exclude 192.168.0.1:4500 192.168.0.2:4500"))
			})
		})

		When("one process is not marked as excluded", func() {
			BeforeEach(func() {
				addressesToCheck = []fdbv1beta2.ProcessAddress{
					{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.3"),
						Port:      4500,
					},
				}

				mockRunner = &mockCommandRunner{
					mockedError: []error{
						fdbv1beta2.TimeoutError{Err: fmt.Errorf("timed out")},
					},
					mockedOutput: []string{""},
				}
			})

			It("should return the one process that is not excluded", func() {
				Expect(result).To(ConsistOf(fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("192.168.0.3"),
					Port:      4500,
				}))
				Expect(err).To(HaveOccurred())
			})

			It("should issue an exclude command to verify the exclusion", func() {
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix(fdbcliStr))
			})
		})

		When("one process is marked as excluded but still serves a role", func() {
			BeforeEach(func() {
				addressesToCheck = []fdbv1beta2.ProcessAddress{
					{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.4"),
						Port:      4500,
					},
				}
			})

			It("should return the one process that is still serving a role", func() {
				Expect(result).To(ConsistOf(fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("192.168.0.4"),
					Port:      4500,
				}))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should issue an exclude command to verify the exclusion", func() {
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix(fdbcliStr))
			})
		})

		When("one process is marked as excluded but still serves a role and one process is not excluded", func() {
			BeforeEach(func() {
				addressesToCheck = []fdbv1beta2.ProcessAddress{
					{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.3"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.4"),
						Port:      4500,
					},
				}
			})

			It("should return the one process that is still serving a role and the one that is not excluded", func() {
				Expect(result).To(ConsistOf(
					fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.3"),
						Port:      4500,
					},
					fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.4"),
						Port:      4500,
					}))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should issue an exclude command to verify the exclusion", func() {
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix(fdbcliStr))
			})
		})

		When("one process is missing in the cluster status and the exclude command times out", func() {
			BeforeEach(func() {
				addressesToCheck = []fdbv1beta2.ProcessAddress{
					{
						IPAddress: net.ParseIP("192.168.0.1"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.2"),
						Port:      4500,
					},
					{
						IPAddress: net.ParseIP("192.168.0.5"),
						Port:      4500,
					},
				}

				mockRunner = &mockCommandRunner{
					mockedError: []error{
						fdbv1beta2.TimeoutError{Err: fmt.Errorf("timed out")},
						nil,
					},
					mockedOutput: []string{"", ""},
				}
			})

			It("should return the missing address list and no error", func() {
				Expect(result).To(ConsistOf(fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("192.168.0.5"),
					Port:      4500,
				}))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should issue an exclude command", func() {
				Expect(mockRunner.receivedBinary[0]).NotTo(BeEmpty())
				Expect(mockRunner.receivedArgs[0]).To(ContainElements("exclude 192.168.0.5:4500"))
				Expect(mockRunner.receivedArgs[1]).To(ContainElements("exclude 192.168.0.1:4500 192.168.0.2:4500"))
			})

			When("the exclude command returns an error different from the timeout error", func() {
				BeforeEach(func() {
					mockRunner.mockedError[0] = fmt.Errorf("unit test")
				})

				It("should return the missing address and no error", func() {
					Expect(result).To(ConsistOf(fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("192.168.0.5"),
						Port:      4500,
					}))
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the missing process is fully excluded or doesn't have any data", func() {
				BeforeEach(func() {
					mockRunner.mockedError = nil
				})

				It("should return an empty result", func() {
					Expect(result).To(HaveLen(0))
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})

		When("the get cluster status returns an error", func() {
			BeforeEach(func() {
				mockFdbClient.mockedError = fmt.Errorf("unit test")
			})

			It("should return an error", func() {
				Expect(result).To(HaveLen(0))
				Expect(err).To(HaveOccurred())
			})
		})
	})

	When("getting the version from the reachable coordinators", func() {
		var mockRunner *mockCommandRunner
		var version, previousBinary, newBinary, quorumReachableStatus, quorumNotReachableStatus string
		var err error
		previousVersion := "7.1.55"
		newVersion := "7.3.33"

		JustBeforeEach(func() {
			cliClient := &cliAdminClient{
				Cluster: &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						Version: newVersion,
					},
					Status: fdbv1beta2.FoundationDBClusterStatus{
						RunningVersion: previousVersion,
					},
				},
				clusterFilePath: "test",
				log:             logr.Discard(),
				cmdRunner:       mockRunner,
			}

			version = cliClient.GetVersionFromReachableCoordinators()
		})

		BeforeEach(func() {
			tmpDir := GinkgoT().TempDir()
			GinkgoT().Setenv("FDB_BINARY_DIR", tmpDir)

			binaryDir := path.Join(tmpDir, "7.1")
			Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
			previousBinary = path.Join(binaryDir, fdbcliStr)
			_, err := os.Create(previousBinary)
			Expect(err).NotTo(HaveOccurred())

			binaryDir = path.Join(tmpDir, "7.3")
			Expect(os.MkdirAll(binaryDir, 0700)).NotTo(HaveOccurred())
			newBinary = path.Join(binaryDir, fdbcliStr)
			_, err = os.Create(newBinary)
			Expect(err).NotTo(HaveOccurred())

			quorumReachable := &fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
						Available: true,
					},
					Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
						QuorumReachable: true,
					},
				},
			}
			quorumReachableStatusOut, err := json.Marshal(quorumReachable)
			Expect(err).NotTo(HaveOccurred())
			quorumReachableStatus = string(quorumReachableStatusOut)

			quorumNotReachable := &fdbv1beta2.FoundationDBStatus{
				Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
					Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
						QuorumReachable: false,
					},
				},
			}
			quorumNotReachableStatusOut, err := json.Marshal(quorumNotReachable)
			Expect(err).NotTo(HaveOccurred())
			quorumNotReachableStatus = string(quorumNotReachableStatusOut)
		})

		When("the fdbcli call for the previous version returns that the quorum is reachable", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: nil,
					mockedOutputPerBinary: map[string]string{
						previousBinary: quorumReachableStatus,
						newBinary:      quorumNotReachableStatus,
					},
				}
			})

			It("should report the previous version", func() {
				Expect(version).To(Equal(previousVersion))
				Expect(err).NotTo(HaveOccurred())
				Expect(mockRunner.receivedBinary).To(HaveLen(1))
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix("7.1/" + fdbcliStr))
			})
		})

		When("the fdbcli call for the new version returns that the quorum is reachable", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: nil,
					mockedOutputPerBinary: map[string]string{
						previousBinary: quorumNotReachableStatus,
						newBinary:      quorumReachableStatus,
					},
				}
			})

			It("should report the new version", func() {
				Expect(version).To(Equal(newVersion))
				Expect(err).NotTo(HaveOccurred())
				Expect(mockRunner.receivedBinary).To(HaveLen(2))
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedBinary[1]).To(HaveSuffix("7.3/" + fdbcliStr))
			})
		})

		When("none of the fdbcli calls returns that the quorum is reachable", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: nil,
					mockedOutputPerBinary: map[string]string{
						previousBinary: quorumNotReachableStatus,
						newBinary:      quorumNotReachableStatus,
					},
				}
			})

			It("should report the previous version", func() {
				Expect(version).To(Equal(previousVersion))
				Expect(err).NotTo(HaveOccurred())
				Expect(mockRunner.receivedBinary).To(HaveLen(2))
				Expect(mockRunner.receivedBinary[0]).To(HaveSuffix("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedBinary[1]).To(HaveSuffix("7.3/" + fdbcliStr))
			})
		})
	})
})
