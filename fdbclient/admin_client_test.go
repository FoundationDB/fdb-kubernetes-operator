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
	"net"
	"os"
	"path"
	"time"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("admin_client_test", func() {
	Describe("helper methods", func() {
		Describe("parseExclusionOutput", func() {
			It("should map the output description to exclusion success", func() {
				output := "  10.1.56.36(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53(Whole machine)  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35(Whole machine)  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36": "Success",
					"10.1.56.43": "Success",
					"10.1.56.52": "Success",
					"10.1.56.53": "Missing",
					"10.1.56.35": "In Progress",
					"10.1.56.56": "Success",
				}))
			})

			It("should handle a lack of suffices in the output", func() {
				output := "  10.1.56.36  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36": "Success",
					"10.1.56.43": "Success",
					"10.1.56.52": "Success",
					"10.1.56.53": "Missing",
					"10.1.56.35": "In Progress",
					"10.1.56.56": "Success",
				}))
			})

			It("should handle ports in the output", func() {
				output := "  10.1.56.36:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53:4500  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35:4500  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36:4500": "Success",
					"10.1.56.43:4500": "Success",
					"10.1.56.52:4500": "Success",
					"10.1.56.53:4500": "Missing",
					"10.1.56.35:4500": "In Progress",
					"10.1.56.56:4500": "Success",
				}))
			})
		})
	})

	When("getting the excluded and remaining processes", func() {
		addr1 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.1"), "", 0, nil)
		addr2 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.2"), "", 0, nil)
		addr3 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.3"), "", 0, nil)
		addr4 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.4"), "", 0, nil)
		addr5 := fdbv1beta2.NewProcessAddress(net.ParseIP("127.0.0.5"), "", 0, nil)
		status := &fdbv1beta2.FoundationDBStatus{
			Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
				Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
					"1": {
						Address:  addr1,
						Excluded: true,
					},
					"2": {
						Address: addr2,
					},
					"3": {
						Address: addr3,
					},
					"4": {
						Address:  addr4,
						Excluded: true,
						Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
							{
								Role: "tester",
							},
						},
					},
				},
			},
		}

		DescribeTable("fetching the excluded and remaining processes from the status",
			func(status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress, expectedExcluded []fdbv1beta2.ProcessAddress, expectedRemaining []fdbv1beta2.ProcessAddress, expectedFullyExcluded []fdbv1beta2.ProcessAddress) {
				exclusions := getRemainingAndExcludedFromStatus(status, addresses)
				Expect(expectedExcluded).To(ContainElements(exclusions.inProgress))
				Expect(len(expectedExcluded)).To(BeNumerically("==", len(exclusions.inProgress)))
				Expect(expectedRemaining).To(ContainElements(exclusions.notExcluded))
				Expect(len(expectedRemaining)).To(BeNumerically("==", len(exclusions.notExcluded)))
				Expect(expectedFullyExcluded).To(ContainElements(exclusions.fullyExcluded))
				Expect(len(expectedFullyExcluded)).To(BeNumerically("==", len(exclusions.fullyExcluded)))
			},
			Entry("with an empty input address slice",
				status,
				[]fdbv1beta2.ProcessAddress{},
				nil,
				nil,
				nil,
			),
			Entry("when the process is excluded",
				status,
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				nil,
				nil,
			),
			Entry("when the process is not excluded",
				status,
				[]fdbv1beta2.ProcessAddress{addr3},
				nil,
				[]fdbv1beta2.ProcessAddress{addr3},
				nil,
			),
			Entry("when some processes are excluded and some not",
				status,
				[]fdbv1beta2.ProcessAddress{addr1, addr2, addr3, addr4},
				[]fdbv1beta2.ProcessAddress{addr4},
				[]fdbv1beta2.ProcessAddress{addr2, addr3},
				[]fdbv1beta2.ProcessAddress{addr1},
			),
			Entry("when a process is missing",
				status,
				[]fdbv1beta2.ProcessAddress{addr5},
				[]fdbv1beta2.ProcessAddress{addr5},
				[]fdbv1beta2.ProcessAddress{},
				nil,
			),
		)
	})

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

	When("getting the args for the command", func() {
		var command cliCommand
		var client *cliAdminClient
		var args, expectedArgs []string
		var timeout, expectedTimeout time.Duration

		JustBeforeEach(func() {
			Expect(client).NotTo(BeNil())
			args, timeout = client.getArgsAndTimeout(command)
			Expect(timeout).To(Equal(expectedTimeout))
			Expect(args).To(ContainElements(expectedArgs))
			Expect(len(args)).To(BeNumerically("==", len(expectedArgs)))
		})

		When("the used command defines args", func() {
			BeforeEach(func() {
				command = cliCommand{
					args:    []string{"--version"},
					version: "7.1.25",
					timeout: 1 * time.Second,
				}

				client = &cliAdminClient{
					Cluster:         nil,
					clusterFilePath: "test",
					log:             logr.Discard(),
				}
			})

			When("trace options are disabled", func() {
				BeforeEach(func() {
					expectedTimeout = 2 * time.Second
					expectedArgs = []string{
						"--version",
						"--timeout",
						"1",
					}
				})
			})
		})

		When("the used command is a fdbcli command", func() {
			BeforeEach(func() {
				command = cliCommand{
					command: "maintenance off",
					timeout: 1 * time.Second,
				}

				client = &cliAdminClient{
					Cluster:         nil,
					clusterFilePath: "test",
					log:             logr.Discard(),
				}
			})

			When("trace options are disabled", func() {
				BeforeEach(func() {
					expectedTimeout = 2 * time.Second
					expectedArgs = []string{
						"--exec",
						"maintenance off",
						"test",
						"--timeout",
						"1",
					}
				})
			})

			When("trace options are enabled", func() {
				BeforeEach(func() {
					GinkgoT().Setenv("FDB_NETWORK_OPTION_TRACE_ENABLE", "/tmp")
					expectedTimeout = 2 * time.Second
					expectedArgs = []string{
						"--exec",
						"maintenance off",
						"test",
						"--log",
						"--trace_format",
						"xml",
						"--log-dir",
						"/tmp",
						"--timeout",
						"1",
					}
				})

				When("a different trace format is defined", func() {
					BeforeEach(func() {
						GinkgoT().Setenv("FDB_NETWORK_OPTION_TRACE_FORMAT", "json")
						expectedTimeout = 2 * time.Second
						expectedArgs = []string{
							"--exec",
							"maintenance off",
							"test",
							"--log",
							"--trace_format",
							"json",
							"--log-dir",
							"/tmp",
							"--timeout",
							"1",
						}
					})
				})
			})
		})

		When("the used command is a fdbrestore command", func() {
			BeforeEach(func() {
				command = cliCommand{
					binary: fdbrestoreStr,
					args: []string{
						"status",
					},
					timeout: 1 * time.Second,
				}

				client = &cliAdminClient{
					Cluster:         nil,
					clusterFilePath: "test",
					log:             logr.Discard(),
				}
			})

			When("trace options are disabled", func() {
				BeforeEach(func() {
					expectedTimeout = 2 * time.Second
					expectedArgs = []string{
						"status",
					}
				})
			})

			When("trace options are enabled", func() {
				BeforeEach(func() {
					GinkgoT().Setenv("FDB_NETWORK_OPTION_TRACE_ENABLE", "/tmp")
					expectedTimeout = 2 * time.Second
					expectedArgs = []string{
						"status",
						"--log",
						"--trace_format",
						"xml",
						"--logdir",
						"/tmp",
					}
				})

				When("a different trace format is defined", func() {
					BeforeEach(func() {
						GinkgoT().Setenv("FDB_NETWORK_OPTION_TRACE_FORMAT", "json")
						expectedTimeout = 2 * time.Second
						expectedArgs = []string{
							"status",
							"--log",
							"--trace_format",
							"json",
							"--logdir",
							"/tmp",
						}
					})
				})
			})
		})
	})

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
					mockedOutput: `fdbcli --version
FoundationDB CLI 7.1 (v7.1.21)
source version e9f38c7169d21dde901b7b9408e1c5a8df182d64
protocol fdb00b071010000`,
				}
			})

			It("should report the protocol version", func() {
				Expect(protocolVersion).To(Equal("fdb00b071010000"))
				Expect(err).NotTo(HaveOccurred())
				Expect(mockRunner.receivedBinary).To(Equal("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedArgs).To(ContainElements("--version"))
			})
		})

		When("an error is returned", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError: errors.New("boom"),
					mockedOutput: `fdbcli --version
FoundationDB CLI 7.1 (v7.1.21)
source version e9f38c7169d21dde901b7b9408e1c5a8df182d64
protocol fdb00b071010000`,
				}
			})

			It("should report the error", func() {
				Expect(protocolVersion).To(Equal(""))
				Expect(err).To(HaveOccurred())
				Expect(mockRunner.receivedBinary).To(Equal("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedArgs).To(ContainElements("--version"))
			})
		})

		When("the protocol version is missing", func() {
			BeforeEach(func() {
				mockRunner = &mockCommandRunner{
					mockedError:  errors.New("boom"),
					mockedOutput: "",
				}
			})

			It("should report the error", func() {
				Expect(protocolVersion).To(Equal(""))
				Expect(err).To(HaveOccurred())
				Expect(mockRunner.receivedBinary).To(Equal("7.1/" + fdbcliStr))
				Expect(mockRunner.receivedArgs).To(ContainElements("--version"))
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
					mockedOutput: ``,
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
					mockedOutput: ``,
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
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
							"1": {},
						},
					},
				}

				out, err := json.Marshal(inputStatus)
				Expect(err).NotTo(HaveOccurred())

				mockRunner = &mockCommandRunner{
					mockedError:  nil,
					mockedOutput: string(out),
				}
			})

			It("should the correct status", func() {
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
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[string]fdbv1beta2.FoundationDBStatusProcessInfo{
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

			It("should the correct status", func() {
				Expect(status).NotTo(BeNil())
				Expect(status.Cluster.Processes).To(HaveLen(1))
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	// TODO(johscheuer): Add test case for timeout.
})
