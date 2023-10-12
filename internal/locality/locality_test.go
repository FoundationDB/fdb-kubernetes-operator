package locality

/*
 * change_coordinators_test.go
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

import (
	"fmt"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient/mock"
	"math"
	"net"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func generateDummyProcessInfo(id string, dcID string, port int, tls bool) fdbv1beta2.FoundationDBStatusProcessInfo {
	ipString := "1.1.1." + strings.Split(id, "-")[1]

	var tlsSuffix string
	var flags map[string]bool
	if tls {
		flags = map[string]bool{"tls": true}
		tlsSuffix = ":tls"
	}

	info := fdbv1beta2.FoundationDBStatusProcessInfo{
		ProcessClass: fdbv1beta2.ProcessClassStorage,
		Address: fdbv1beta2.ProcessAddress{
			IPAddress: net.ParseIP(ipString),
			Port:      port,
			Flags:     flags,
		},
		CommandLine: fmt.Sprintf("... --public_address=%s:%d%s ...", ipString, port, tlsSuffix),
		Locality: map[string]string{
			fdbv1beta2.FDBLocalityInstanceIDKey: id,
			fdbv1beta2.FDBLocalityZoneIDKey:     id,
			fdbv1beta2.FDBLocalityDCIDKey:       dcID,
		},
	}

	return info
}

func generateDefaultStatus(tls bool) *fdbv1beta2.FoundationDBStatus {
	var flags map[string]bool

	port := 4501
	if tls {
		port = 4500
		flags = map[string]bool{"tls": true}
	}

	return &fdbv1beta2.FoundationDBStatus{
		Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
			Coordinators: fdbv1beta2.FoundationDBStatusCoordinatorInfo{
				Coordinators: []fdbv1beta2.FoundationDBStatusCoordinator{
					{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.1"),
							Port:      port,
							Flags:     flags,
						},
						Reachable: true,
					},
					{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.2"),
							Port:      port,
							Flags:     flags,
						},
						Reachable: true,
					},
					{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.3"),
							Port:      port,
							Flags:     flags,
						},
						Reachable: true,
					},
				},
			},
		},
		Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
			Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
				"1": generateDummyProcessInfo("test-1", "dc1", port, tls),
				"2": generateDummyProcessInfo("test-2", "dc1", port, tls),
				"3": generateDummyProcessInfo("test-3", "dc1", port, tls),
			},
		},
	}
}

var _ = Describe("Change coordinators", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
			{
				ProcessClass: fdbv1beta2.ProcessClassStorage,
				Priority:     math.MaxInt32,
			},
			{
				ProcessClass: fdbv1beta2.ProcessClassLog,
				Priority:     0,
			},
		}
	})

	When("Sorting the localities", func() {
		var localities []Info

		BeforeEach(func() {
			localities = []Info{
				{
					ID:    "storage-1",
					Class: fdbv1beta2.ProcessClassStorage,
				},
				{
					ID:    "tlog-1",
					Class: fdbv1beta2.ProcessClassTransaction,
				},
				{
					ID:    "log-1",
					Class: fdbv1beta2.ProcessClassLog,
				},
				{
					ID:    "storage-51",
					Class: fdbv1beta2.ProcessClassStorage,
				},
			}
		})

		When("no other preferences are defined", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{}
			})

			It("should sort the localities based on the IDs", func() {
				sortLocalities(cluster, localities)

				Expect(localities[0].Class).To(Equal(fdbv1beta2.ProcessClassLog))
				Expect(localities[0].ID).To(Equal("log-1"))
				Expect(localities[1].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[1].ID).To(Equal("storage-1"))
				Expect(localities[2].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[2].ID).To(Equal("storage-51"))
				Expect(localities[3].Class).To(Equal(fdbv1beta2.ProcessClassTransaction))
				Expect(localities[3].ID).To(Equal("tlog-1"))
			})
		})

		When("when the storage class is preferred", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     0,
					},
				}
			})

			It("should sort the localities based on the provided config", func() {
				sortLocalities(cluster, localities)

				Expect(localities[0].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[0].ID).To(Equal("storage-1"))
				Expect(localities[1].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[1].ID).To(Equal("storage-51"))
				Expect(localities[2].Class).To(Equal(fdbv1beta2.ProcessClassLog))
				Expect(localities[2].ID).To(Equal("log-1"))
				Expect(localities[3].Class).To(Equal(fdbv1beta2.ProcessClassTransaction))
				Expect(localities[3].ID).To(Equal("tlog-1"))
			})
		})

		When("when the storage class is preferred over transaction class", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     1,
					},
					{
						ProcessClass: fdbv1beta2.ProcessClassTransaction,
						Priority:     0,
					},
				}
			})

			It("should sort the localities based on the provided config", func() {
				sortLocalities(cluster, localities)

				Expect(localities[0].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[0].ID).To(Equal("storage-1"))
				Expect(localities[1].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[1].ID).To(Equal("storage-51"))
				Expect(localities[2].Class).To(Equal(fdbv1beta2.ProcessClassTransaction))
				Expect(localities[2].ID).To(Equal("tlog-1"))
				Expect(localities[3].Class).To(Equal(fdbv1beta2.ProcessClassLog))
				Expect(localities[3].ID).To(Equal("log-1"))
			})
		})
	})

	Describe("chooseDistributedProcesses", func() {
		var candidates []Info
		var result []Info
		var err error

		Context("with a flat set of processes", func() {
			BeforeEach(func() {
				candidates = []Info{
					{ID: "p1", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z1"}},
					{ID: "p2", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z1"}},
					{ID: "p3", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z2"}},
					{ID: "p4", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z3"}},
					{ID: "p5", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z2"}},
					{ID: "p6", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z4"}},
					{ID: "p7", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z5"}},
				}
				result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should recruit the processes across multiple zones", func() {
				Expect(len(result)).To(Equal(5))
				Expect(result[0].ID).To(Equal("p1"))
				Expect(result[1].ID).To(Equal("p3"))
				Expect(result[2].ID).To(Equal("p4"))
				Expect(result[3].ID).To(Equal("p6"))
				Expect(result[4].ID).To(Equal("p7"))
			})
		})

		Context("with fewer zones than desired processes", func() {
			BeforeEach(func() {
				candidates = []Info{
					{ID: "p1", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z1"}},
					{ID: "p2", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z1"}},
					{ID: "p3", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z2"}},
					{ID: "p4", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z3"}},
					{ID: "p5", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z2"}},
					{ID: "p6", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z4"}},
				}
			})

			Context("with no hard limit", func() {
				It("should only re-use zones as necessary", func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{})
					Expect(err).NotTo(HaveOccurred())

					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p3"))
					Expect(result[2].ID).To(Equal("p4"))
					Expect(result[3].ID).To(Equal("p6"))
					Expect(result[4].ID).To(Equal("p2"))
				})
			})

			Context("with a hard limit", func() {
				It("should give an error", func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{
						HardLimits: map[string]int{fdbv1beta2.FDBLocalityZoneIDKey: 1},
					})
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("Could only select 4 processes, but 5 are required"))
				})
			})
		})

		Context("with multiple data centers", func() {
			BeforeEach(func() {
				candidates = []Info{
					{ID: "p1", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z1", "dcid": "dc1"}},
					{ID: "p2", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z1", "dcid": "dc1"}},
					{ID: "p3", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z2", "dcid": "dc1"}},
					{ID: "p4", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z3", "dcid": "dc1"}},
					{ID: "p5", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z2", "dcid": "dc1"}},
					{ID: "p6", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z4", "dcid": "dc1"}},
					{ID: "p7", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z5", "dcid": "dc1"}},
					{ID: "p8", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z6", "dcid": "dc2"}},
					{ID: "p9", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z7", "dcid": "dc2"}},
					{ID: "p10", LocalityData: map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "z8", "dcid": "dc2"}},
				}
			})

			Context("with the default constraints", func() {
				BeforeEach(func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should recruit the processes across multiple zones and data centers", func() {
					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p10"))
					Expect(result[2].ID).To(Equal("p3"))
					Expect(result[3].ID).To(Equal("p8"))
					Expect(result[4].ID).To(Equal("p4"))
				})
			})

			Context("when only distributing across data centers", func() {
				BeforeEach(func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{
						Fields: []string{"dcid"},
					})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should recruit the processes across data centers", func() {
					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p10"))
					Expect(result[2].ID).To(Equal("p2"))
					Expect(result[3].ID).To(Equal("p8"))
					Expect(result[4].ID).To(Equal("p3"))
				})
			})
		})
	})

	DescribeTable("when getting the hard limits", func(cluster *fdbv1beta2.FoundationDBCluster, expected map[string]int) {
		Expect(GetHardLimits(cluster)).To(Equal(expected))
	},
		Entry("default cluster with one usable region",
			&fdbv1beta2.FoundationDBCluster{},
			map[string]int{
				fdbv1beta2.FDBLocalityZoneIDKey: 1,
			},
		),
		Entry("default cluster with two usable regiosn",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					DatabaseConfiguration: fdbv1beta2.DatabaseConfiguration{
						UsableRegions: 2,
					},
				},
			},
			map[string]int{
				fdbv1beta2.FDBLocalityZoneIDKey: 1,
				fdbv1beta2.FDBLocalityDCIDKey:   4,
			},
		),
	)

	DescribeTable("when getting the locality info from a process", func(process fdbv1beta2.FoundationDBStatusProcessInfo, mainContainerTLS bool, expected Info, expectedError bool) {
		info, err := InfoForProcess(process, mainContainerTLS)
		if expectedError {
			Expect(err).To(HaveOccurred())
			return
		}

		Expect(err).NotTo(HaveOccurred())
		Expect(info).To(Equal(expected))
	},
		Entry("a process info without tls",
			fdbv1beta2.FoundationDBStatusProcessInfo{
				CommandLine: "... --public_address=1.1.1.1:4501,1.1.1.1:4500:tls ...",
				Locality: map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: "test",
				},
				ProcessClass: fdbv1beta2.ProcessClassStorage,
			},
			false,
			Info{
				ID: "test",
				Address: fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("1.1.1.1"),
					Port:      4501,
				},
				Class: fdbv1beta2.ProcessClassStorage,
				LocalityData: map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: "test",
				},
			},
			false,
		),
		Entry("a process info with tls",
			fdbv1beta2.FoundationDBStatusProcessInfo{
				CommandLine: "... --public_address=1.1.1.1:4501,1.1.1.1:4500:tls ...",
				Locality: map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: "test",
				},
				ProcessClass: fdbv1beta2.ProcessClassStorage,
			},
			true,
			Info{
				ID: "test",
				Address: fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("1.1.1.1"),
					Port:      4500,
					Flags: map[string]bool{
						"tls": true,
					},
				},
				Class: fdbv1beta2.ProcessClassStorage,
				LocalityData: map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: "test",
				},
			},
			false,
		),
		Entry("a process info with missing public address",
			fdbv1beta2.FoundationDBStatusProcessInfo{
				CommandLine: "",
				Locality: map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: "test",
				},
				ProcessClass: fdbv1beta2.ProcessClassStorage,
			},
			true,
			Info{
				ID: "test",
				Address: fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("1.1.1.1"),
					Port:      4500,
					Flags: map[string]bool{
						"tls": true,
					},
				},
				Class: fdbv1beta2.ProcessClassStorage,
				LocalityData: map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: "test",
				},
			},
			true,
		),
	)

	DescribeTable("when getting the locality info from a sidecar", func(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, expected Info, expectedError bool) {
		client, err := mock.NewMockFdbPodClient(cluster, pod)
		Expect(err).NotTo(HaveOccurred())

		info, err := InfoFromSidecar(cluster, client)
		if expectedError {
			Expect(err).To(HaveOccurred())
			return
		}

		Expect(err).NotTo(HaveOccurred())
		Expect(info).To(Equal(expected))
	},
		Entry("a Pod with all fields set",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					FaultDomain: fdbv1beta2.FoundationDBClusterFaultDomain{
						Key: "foundationdb.org/none",
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					RequiredAddresses: fdbv1beta2.RequiredAddressSet{
						NonTLS: true,
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						fdbv1beta2.FDBProcessGroupIDLabel: "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foundationdb-kubernetes-sidecar",
							Args: []string{
								"--public-ip-family",
								"4",
							},
						},
					},
				},
				Status: corev1.PodStatus{
					PodIPs: []corev1.PodIP{
						{IP: "1.1.1.1"},
					},
				},
			},
			Info{
				ID: "test",
				Address: fdbv1beta2.ProcessAddress{
					IPAddress: net.ParseIP("1.1.1.1"),
					Port:      4501,
				},
				LocalityData: map[string]string{
					fdbv1beta2.FDBLocalityZoneIDKey:  "test",
					fdbv1beta2.FDBLocalityDNSNameKey: "",
				},
			},
			false,
		),
		Entry("the sidecar is not reachable",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					FaultDomain: fdbv1beta2.FoundationDBClusterFaultDomain{
						Key: "foundationdb.org/none",
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					RequiredAddresses: fdbv1beta2.RequiredAddressSet{
						NonTLS: true,
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						internal.MockUnreachableAnnotation: "true",
					},
				},
			},
			Info{},
			true,
		),
	)

	Describe("checkCoordinatorValidity", func() {
		var status *fdbv1beta2.FoundationDBStatus
		var coordinatorStatus map[string]bool

		BeforeEach(func() {
			cluster.Status.ProcessGroups = []*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "test-1",
				},
				{
					ProcessGroupID: "test-2",
				},
				{
					ProcessGroupID: "test-3",
				},
			}

			status = generateDefaultStatus(false)
		})

		JustBeforeEach(func() {
			coordinatorStatus = make(map[string]bool, len(status.Client.Coordinators.Coordinators))
			for _, coordinator := range status.Client.Coordinators.Coordinators {
				coordinatorStatus[coordinator.Address.String()] = false
			}
		})

		Context("with the default configuration", func() {
			It("should report the coordinators as valid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeTrue())
				Expect(addressesValid).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("an empty coordinator status is passed down", func() {
			It("should report the coordinators as invalid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, nil)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeFalse())
				Expect(err).To(HaveOccurred())
			})
		})

		When("changing the coordinator selection setting", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassLog,
					},
				}
			})

			It("should report the coordinators as invalid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		When("one process is marked for exclusion", func() {
			BeforeEach(func() {
				process := status.Cluster.Processes["1"]
				process.Excluded = true
				status.Cluster.Processes["1"] = process
			})

			It("should report the coordinators as invalid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("a process has an empty address", func() {
			BeforeEach(func() {
				status.Cluster.Processes["4"] = fdbv1beta2.FoundationDBStatusProcessInfo{}
			})

			It("should ignore the process", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeTrue())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		When("the coordinators are listening on TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
			})

			It("should report the coordinators as invalid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("a process misses the public-address flag", func() {
			BeforeEach(func() {
				process := status.Cluster.Processes["3"]
				process.CommandLine = ""
				status.Cluster.Processes["3"] = process

				Expect(status.Cluster.Processes["3"].CommandLine).To(BeEmpty())
			})

			It("should report that not all addresses and coordinators are valid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeFalse())
				Expect(err).To(BeNil())
			})
		})

		When("a process is missing localities", func() {
			BeforeEach(func() {
				status.Cluster.Processes["4"] = fdbv1beta2.FoundationDBStatusProcessInfo{
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
					},
					ProcessClass: fdbv1beta2.ProcessClassLog,
				}
			})

			It("should be ignored", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeTrue())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with a tester process without a command line field", func() {
			BeforeEach(func() {
				testProcess := fdbv1beta2.FoundationDBStatusProcessInfo{
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("9.9.9.9"),
					},
					ProcessClass: fdbv1beta2.ProcessClassTest,
				}
				Expect(testProcess.Address.IsEmpty()).To(BeFalse())
				status.Cluster.Processes[fdbv1beta2.ProcessGroupID(fdbv1beta2.ProcessClassTest)] = testProcess
			})

			It("should ignore the test process and report the coordinators as valid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeTrue())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with too few coordinators", func() {
			BeforeEach(func() {
				status.Client.Coordinators.Coordinators = status.Client.Coordinators.Coordinators[0:2]
			})

			It("should report the coordinators as not valid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with too few zones", func() {
			BeforeEach(func() {
				zone := ""
				for _, process := range status.Cluster.Processes {
					if process.Address.Equal(status.Client.Coordinators.Coordinators[0].Address) {
						zone = process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
					}
				}
				for _, process := range status.Cluster.Processes {
					if process.Address.Equal(status.Client.Coordinators.Coordinators[1].Address) {
						process.Locality[fdbv1beta2.FDBLocalityZoneIDKey] = zone
					}
				}
			})

			It("should report the coordinators as not valid", func() {
				coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with multiple regions", func() {
			BeforeEach(func() {
				cluster.Spec.DatabaseConfiguration.UsableRegions = 2

				status.Cluster.Processes["4"] = generateDummyProcessInfo("test-4", "dc2", 4501, false)
				status.Cluster.Processes["5"] = generateDummyProcessInfo("test-5", "dc2", 4501, false)
				status.Cluster.Processes["6"] = generateDummyProcessInfo("test-6", "dc2", 4501, false)
				status.Cluster.Processes["7"] = generateDummyProcessInfo("test-7", "dc3", 4501, false)
				status.Cluster.Processes["8"] = generateDummyProcessInfo("test-8", "dc3", 4501, false)
				status.Cluster.Processes["9"] = generateDummyProcessInfo("test-9", "dc3", 4501, false)

				status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators,
					fdbv1beta2.FoundationDBStatusCoordinator{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.4"),
							Port:      4501,
						},
						Reachable: true,
					},
					fdbv1beta2.FoundationDBStatusCoordinator{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.5"),
							Port:      4501,
						},
						Reachable: true,
					},
					fdbv1beta2.FoundationDBStatusCoordinator{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.6"),
							Port:      4501,
						},
						Reachable: true,
					},
					fdbv1beta2.FoundationDBStatusCoordinator{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.7"),
							Port:      4501,
						},
						Reachable: true,
					},
					fdbv1beta2.FoundationDBStatusCoordinator{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.8"),
							Port:      4501,
						},
						Reachable: true,
					},
					fdbv1beta2.FoundationDBStatusCoordinator{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP("1.1.1.9"),
							Port:      4501,
						},
						Reachable: true,
					},
				)
			})

			Context("with coordinators divided across three DCs", func() {
				It("should report the coordinators as valid", func() {
					coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(coordinatorsValid).To(BeTrue())
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})
			})

			Context("with coordinators divided across two DCs", func() {
				BeforeEach(func() {
					for _, process := range status.Cluster.Processes {
						if process.Locality["dcid"] == "dc3" {
							process.Locality["dcid"] = "dc1"
						}
					}
				})
				It("should report the coordinators as not valid", func() {
					coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(coordinatorsValid).To(BeFalse())
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})
			})
		})

		When("changing the TLS setting", func() {
			BeforeEach(func() {
				cluster.Status.RequiredAddresses = fdbv1beta2.RequiredAddressSet{
					TLS:    true,
					NonTLS: true,
				}
			})

			When("TLS is disabled", func() {
				BeforeEach(func() {
					cluster.Spec.MainContainer = fdbv1beta2.ContainerOverrides{
						EnableTLS: false,
					}
					cluster.Spec.SidecarContainer = fdbv1beta2.ContainerOverrides{
						EnableTLS: false,
					}
				})

				It("should report the coordinators addresses as valid", func() {
					_, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})

				When("converting back to TLS", func() {
					BeforeEach(func() {
						cluster.Spec.MainContainer = fdbv1beta2.ContainerOverrides{
							EnableTLS: true,
						}
						cluster.Spec.SidecarContainer = fdbv1beta2.ContainerOverrides{
							EnableTLS: true,
						}

						status = generateDefaultStatus(true)
					})

					It("should report the coordinators addresses as valid", func() {
						_, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
						Expect(addressesValid).To(BeTrue())
						Expect(err).To(BeNil())
					})
				})
			})

			When("TLS is enabled", func() {
				BeforeEach(func() {
					cluster.Spec.MainContainer = fdbv1beta2.ContainerOverrides{
						EnableTLS: true,
					}
					cluster.Spec.SidecarContainer = fdbv1beta2.ContainerOverrides{
						EnableTLS: true,
					}

					status = generateDefaultStatus(true)
				})

				It("should report the coordinators addresses as valid", func() {
					_, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})

				When("converting back to non-TLS", func() {
					BeforeEach(func() {
						cluster.Spec.MainContainer = fdbv1beta2.ContainerOverrides{
							EnableTLS: false,
						}

						cluster.Spec.SidecarContainer = fdbv1beta2.ContainerOverrides{
							EnableTLS: false,
						}

						status = generateDefaultStatus(false)
					})

					It("should report the coordinators addresses as valid", func() {
						_, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
						Expect(addressesValid).To(BeTrue())
						Expect(err).To(BeNil())
					})
				})
			})
		})

		When("enabling DNS names in the cluster file", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
			})

			When("the pods do not have DNS names assigned", func() {
				It("should report valid coordinators", func() {
					coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(err).NotTo(HaveOccurred())
					Expect(coordinatorsValid).To(BeTrue())
					Expect(addressesValid).To(BeTrue())
				})
			})

			When("the pods have DNS names assigned", func() {
				BeforeEach(func() {
					for _, process := range status.Cluster.Processes {
						process.Locality[fdbv1beta2.FDBLocalityDNSNameKey] = process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
					}
				})

				It("should reject coordinators based on IP addresses", func() {
					coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(err).NotTo(HaveOccurred())
					Expect(coordinatorsValid).To(BeFalse())
					Expect(addressesValid).To(BeTrue())
				})
			})

			When("the pods have DNS names assigned and the coordinator have DNS names", func() {
				BeforeEach(func() {
					status.Client.Coordinators.Coordinators = nil

					for _, process := range status.Cluster.Processes {
						process.Locality[fdbv1beta2.FDBLocalityDNSNameKey] = process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
						status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators,
							fdbv1beta2.FoundationDBStatusCoordinator{
								Address: fdbv1beta2.ProcessAddress{
									StringAddress: process.Locality[fdbv1beta2.FDBLocalityZoneIDKey],
									Port:          4501,
								},
							})
					}
				})

				It("should return that the coordinators are valid", func() {
					coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(coordinatorsValid).To(BeTrue())
					Expect(addressesValid).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the pods have DNS names assigned and the coordinator have DNS names but DNS is disabled", func() {
				BeforeEach(func() {
					status.Client.Coordinators.Coordinators = nil

					for _, process := range status.Cluster.Processes {
						process.Locality[fdbv1beta2.FDBLocalityDNSNameKey] = process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
						status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators,
							fdbv1beta2.FoundationDBStatusCoordinator{
								Address: fdbv1beta2.ProcessAddress{
									StringAddress: process.Locality[fdbv1beta2.FDBLocalityZoneIDKey],
									Port:          4501,
								},
							})
					}

					cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(false)
				})

				It("should return that the coordinators are invalid", func() {
					coordinatorsValid, addressesValid, err := CheckCoordinatorValidity(logr.Discard(), cluster, status, coordinatorStatus)
					Expect(coordinatorsValid).To(BeFalse())
					Expect(addressesValid).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})
	})
})
