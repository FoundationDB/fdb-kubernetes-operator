/*
Copyright 2020 FoundationDB project authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta2

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] FoundationDBCluster", func() {
	log := logf.Log.WithName("controller")

	When("getting the default role counts", func() {
		It("should return the default role counts", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: RedundancyModeDouble,
					},
				},
			}

			counts := cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:       3,
				Logs:          3,
				Proxies:       3,
				CommitProxies: 2,
				GrvProxies:    1,
				Resolvers:     1,
				RemoteLogs:    -1,
				LogRouters:    -1,
			}))
			Expect(counts.Map()).To(Equal(map[ProcessClass]int{
				"logs":           3,
				"proxies":        3,
				"resolvers":      1,
				"remote_logs":    -1,
				"log_routers":    -1,
				"commit_proxies": 2,
				"grv_proxies":    1,
			}))
			Expect(cluster.Spec.DatabaseConfiguration.RoleCounts).To(Equal(RoleCounts{}))

			cluster.Spec.DatabaseConfiguration.UsableRegions = 2
			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:       3,
				Logs:          3,
				CommitProxies: 2,
				GrvProxies:    1,
				Proxies:       3,
				Resolvers:     1,
				RemoteLogs:    3,
				LogRouters:    3,
			}))
			Expect(counts.Map()).To(Equal(map[ProcessClass]int{
				"logs":           3,
				"proxies":        3,
				"resolvers":      1,
				"remote_logs":    3,
				"log_routers":    3,
				"commit_proxies": 2,
				"grv_proxies":    1,
			}))

			cluster.Spec.DatabaseConfiguration.RoleCounts = RoleCounts{
				Storage: 5,
			}

			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:       5,
				Logs:          3,
				Proxies:       3,
				GrvProxies:    1,
				CommitProxies: 2,
				Resolvers:     1,
				RemoteLogs:    3,
				LogRouters:    3,
			}))

			cluster.Spec.DatabaseConfiguration.RoleCounts = RoleCounts{
				Logs: 8,
			}
			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:       3,
				Logs:          8,
				CommitProxies: 2,
				GrvProxies:    1,
				Proxies:       3,
				Resolvers:     1,
				RemoteLogs:    8,
				LogRouters:    8,
			}))

			cluster.Spec.DatabaseConfiguration.RoleCounts = RoleCounts{
				Logs:       4,
				RemoteLogs: 5,
				LogRouters: 6,
			}
			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:       3,
				Logs:          4,
				Proxies:       3,
				GrvProxies:    1,
				CommitProxies: 2,
				Resolvers:     1,
				RemoteLogs:    5,
				LogRouters:    6,
			}))
		})
	})

	When("getting the default process counts", func() {
		It("should return the default process counts", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					Version: Versions.Default.String(),
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: RedundancyModeDouble,
						RoleCounts: RoleCounts{
							Storage:   5,
							Logs:      3,
							Proxies:   3,
							Resolvers: 1,
						},
					},
				},
			}

			counts, err := cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       4,
				Stateless: 9,
			}))
			Expect(counts.Map()).To(Equal(map[ProcessClass]int{
				ProcessClassStorage:   5,
				ProcessClassLog:       4,
				ProcessClassStateless: 9,
			}))
			Expect(cluster.Spec.ProcessCounts).To(Equal(ProcessCounts{}))

			cluster.Spec.ProcessCounts = ProcessCounts{
				Storage: 10,
			}
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Storage).To(Equal(10))

			cluster.Spec.ProcessCounts = ProcessCounts{
				ClusterController: 3,
			}
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Stateless).To(Equal(8))
			Expect(counts.ClusterController).To(Equal(3))
			Expect(counts.Map()).To(Equal(map[ProcessClass]int{
				ProcessClassStorage:           5,
				ProcessClassLog:               4,
				ProcessClassStateless:         8,
				ProcessClassClusterController: 3,
			}))

			cluster.Spec.ProcessCounts = ProcessCounts{
				Log: 2,
			}
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Log).To(Equal(2))

			cluster.Spec.ProcessCounts = ProcessCounts{}
			cluster.Spec.DatabaseConfiguration.RoleCounts.RemoteLogs = 4
			cluster.Spec.DatabaseConfiguration.RoleCounts.LogRouters = 8

			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       5,
				Stateless: 9,
			}))
		})
	})

	When("getting the default process counts with cross cluster replication", func() {
		It("should return the default process counts", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					Version: Versions.Default.String(),
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: RedundancyModeDouble,
						RoleCounts: RoleCounts{
							Storage:   5,
							Logs:      3,
							Proxies:   5,
							Resolvers: 1,
						},
					},
					FaultDomain: FoundationDBClusterFaultDomain{
						Key: "foundationdb.org/kubernetes-cluster",
					},
				},
			}

			counts, err := cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   2,
				Log:       2,
				Stateless: 4,
			}))

			cluster.Spec.ProcessCounts = ProcessCounts{}
			cluster.Spec.FaultDomain.ZoneIndex = 2
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   1,
				Log:       1,
				Stateless: 3,
			}))

			cluster.Spec.ProcessCounts = ProcessCounts{}
			cluster.Spec.FaultDomain.ZoneIndex = 1
			cluster.Spec.FaultDomain.ZoneCount = 5
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   1,
				Log:       1,
				Stateless: 2,
			}))
		})
	})

	When("getting the default process counts with satellites", func() {
		It("should return the default process counts", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					Version: Versions.Default.String(),
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: RedundancyModeDouble,
						RoleCounts: RoleCounts{
							Storage:   5,
							Logs:      3,
							Proxies:   5,
							Resolvers: 1,
						},
						Regions: []Region{
							{
								DataCenters: []DataCenter{
									{ID: "dc1", Satellite: 0, Priority: 1},
									{ID: "dc2", Satellite: 1, Priority: 1},
								},
								SatelliteLogs:           2,
								SatelliteRedundancyMode: "one_satellite_double",
							},
							{
								DataCenters: []DataCenter{
									{ID: "dc3", Satellite: 0, Priority: 1},
									{ID: "dc4", Satellite: 1, Priority: 1},
								},
								SatelliteLogs:           2,
								SatelliteRedundancyMode: "one_satellite_double",
							},
						},
					},
				},
			}

			cluster.Spec.DataCenter = "dc1"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       4,
				Stateless: 11,
			}))

			cluster.Spec.DataCenter = "dc2"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Log: 3,
			}))

			cluster.Spec.DataCenter = "dc3"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       4,
				Stateless: 11,
			}))

			cluster.Spec.DataCenter = "dc4"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Log: 3,
			}))

			cluster.Spec.DataCenter = "dc5"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       4,
				Stateless: 11,
			}))

			cluster.Spec.DatabaseConfiguration.Regions = []Region{
				{
					DataCenters: []DataCenter{
						{ID: "dc1", Satellite: 0, Priority: 1},
						{ID: "dc2", Satellite: 1, Priority: 2},
						{ID: "dc3", Satellite: 1, Priority: 1},
					},
					SatelliteLogs:           4,
					SatelliteRedundancyMode: "one_satellite_double",
				},
				{
					DataCenters: []DataCenter{
						{ID: "dc3", Satellite: 0, Priority: 1},
						{ID: "dc2", Satellite: 1, Priority: 1},
						{ID: "dc1", Satellite: 1, Priority: 2},
					},
					SatelliteLogs:           3,
					SatelliteRedundancyMode: "one_satellite_double",
				},
			}

			cluster.Spec.DataCenter = "dc1"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       7,
				Stateless: 11,
			}))

			cluster.Spec.DataCenter = "dc2"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Log: 5,
			}))

			cluster.Spec.DataCenter = "dc3"
			Expect(cluster.GetProcessCountsWithDefaults()).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       8,
				Stateless: 11,
			}))
		})
	})

	When("checking if the process counts are satisfied", func() {
		It("should return if the process counts are satisfied", func() {
			counts := ProcessCounts{Stateless: 5}
			Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 5})).To(BeTrue())
			Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 6})).To(BeFalse())
			Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 0})).To(BeFalse())
			counts = ProcessCounts{Stateless: -1}
			Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 0})).To(BeTrue())
			Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 5})).To(BeFalse())
		})
	})

	When("setting the process count by name", func() {
		It("should set the process counts by name", func() {
			counts := ProcessCounts{}
			counts.IncreaseCount(ProcessClassStorage, 2)
			Expect(counts.Storage).To(Equal(2))
			Expect(counts.ClusterController).To(Equal(0))
			counts.IncreaseCount(ProcessClassStorage, 3)
			Expect(counts.Storage).To(Equal(5))
			Expect(counts.ClusterController).To(Equal(0))
			counts.IncreaseCount(ProcessClassClusterController, 1)
			Expect(counts.Storage).To(Equal(5))
			Expect(counts.ClusterController).To(Equal(1))
		})
	})

	When("getting desired fault tolerance", func() {
		It("should return the desired fault tolerance", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
			}

			Expect(cluster.DesiredFaultTolerance()).To(Equal(1))
			Expect(cluster.MinimumFaultDomains()).To(Equal(2))
			Expect(cluster.DesiredCoordinatorCount()).To(Equal(3))

			cluster.Spec.DatabaseConfiguration.RedundancyMode = RedundancyModeSingle
			Expect(cluster.DesiredFaultTolerance()).To(Equal(0))
			Expect(cluster.MinimumFaultDomains()).To(Equal(1))
			Expect(cluster.DesiredCoordinatorCount()).To(Equal(1))

			cluster.Spec.DatabaseConfiguration.RedundancyMode = RedundancyModeDouble
			Expect(cluster.DesiredFaultTolerance()).To(Equal(1))
			Expect(cluster.MinimumFaultDomains()).To(Equal(2))
			Expect(cluster.DesiredCoordinatorCount()).To(Equal(3))

			cluster.Spec.DatabaseConfiguration.UsableRegions = 2
			Expect(cluster.DesiredFaultTolerance()).To(Equal(1))
			Expect(cluster.MinimumFaultDomains()).To(Equal(2))
			Expect(cluster.DesiredCoordinatorCount()).To(Equal(9))
		})
	})

	When("parsing the backup status for 6.2", func() {
		It("should be parsed correctly", func() {
			statusFile, err := os.OpenFile(filepath.Join("testdata", "fdbbackup_status_6_2.json"), os.O_RDONLY, os.ModePerm)
			Expect(err).NotTo(HaveOccurred())
			defer statusFile.Close()
			statusDecoder := json.NewDecoder(statusFile)
			status := FoundationDBLiveBackupStatus{}
			err = statusDecoder.Decode(&status)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(Equal(FoundationDBLiveBackupStatus{
				DestinationURL:          "blobstore://minio@minio-service:9000/sample-cluster?bucket=fdb-backups",
				SnapshotIntervalSeconds: 864000,
				Status: FoundationDBLiveBackupStatusState{
					Running: true,
				},
			}))
		})
	})

	coordinators := []ProcessAddress{
		{
			IPAddress: net.ParseIP("127.0.0.1"),
			Port:      4500,
		},
		{
			IPAddress: net.ParseIP("127.0.0.2"),
			Port:      4500,
		},
		{
			IPAddress: net.ParseIP("127.0.0.3"),
			Port:      4500,
		},
	}

	coordinatorsStr := []string{
		"127.0.0.1:4500",
		"127.0.0.2:4500",
		"127.0.0.3:4500",
	}

	When("parsing the connection string", func() {
		It("should be parsed correctly", func() {
			str, err := ParseConnectionString("test:abcd@127.0.0.1:4500,127.0.0.2:4500,127.0.0.3:4500")
			Expect(err).NotTo(HaveOccurred())
			Expect(str.DatabaseName).To(Equal("test"))
			Expect(str.GenerationID).To(Equal("abcd"))
			Expect(str.Coordinators).To(Equal(coordinatorsStr))

			str, err = ParseConnectionString("test:abcd")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("invalid connection string test:abcd"))
		})
	})

	When("formatting the connection string", func() {
		It("should be formatted correctly", func() {
			str := ConnectionString{
				DatabaseName: "test",
				GenerationID: "abcd",
				Coordinators: coordinatorsStr,
			}
			Expect(str.String()).To(Equal("test:abcd@127.0.0.1:4500,127.0.0.2:4500,127.0.0.3:4500"))
		})
	})

	When("generating the connection ID from the connection string", func() {
		It("should be formatted correctly", func() {
			str := ConnectionString{
				DatabaseName: "test",
				GenerationID: "abcd",
				Coordinators: coordinatorsStr,
			}
			err := str.GenerateNewGenerationID()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(str.GenerationID)).To(Equal(32))
		})
	})

	When("checking the coordinators for the connection string", func() {
		It("should return the correct list of coordinators", func() {
			str := ConnectionString{
				DatabaseName: "test",
				GenerationID: "abcd",
				Coordinators: coordinatorsStr,
			}
			Expect(str.HasCoordinators(coordinators)).To(BeTrue())
			// We have to copy the slice to prevent to modify the original slice
			// See: https://golang.org/ref/spec#Appending_and_copying_slices
			newCoord := make([]ProcessAddress, len(coordinators))
			copy(newCoord, coordinators)
			rand.Shuffle(len(newCoord), func(i, j int) {
				newCoord[i], newCoord[j] = newCoord[j], newCoord[i]
			})
			Expect(str.HasCoordinators(newCoord)).To(BeTrue())
			newCoord = make([]ProcessAddress, len(coordinators))
			copy(newCoord, coordinators)
			newCoord = append(newCoord, ProcessAddress{IPAddress: net.ParseIP("127.0.0.4"), Port: 4500})
			Expect(str.HasCoordinators(newCoord)).To(BeFalse())
			newCoord = make([]ProcessAddress, len(coordinators))
			copy(newCoord, coordinators)
			newCoord = append(newCoord[:2], ProcessAddress{IPAddress: net.ParseIP("127.0.0.4"), Port: 4500})
			Expect(str.HasCoordinators(newCoord)).To(BeFalse())
			Expect(str.HasCoordinators(newCoord[:2])).To(BeFalse())
		})
	})

	When("getting the cluster database configuration", func() {
		It("should be parsed correctly", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: RedundancyModeDouble,
						StorageEngine:  "ssd",
						RoleCounts: RoleCounts{
							Storage: 5,
							Logs:    4,
							Proxies: 5,
						},
					},
				},
			}

			Expect(cluster.DesiredDatabaseConfiguration()).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				StorageEngine:  "ssd-2",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:          4,
					Proxies:       5,
					CommitProxies: 2,
					GrvProxies:    1,
					Resolvers:     1,
					LogRouters:    -1,
					RemoteLogs:    -1,
				},
			}))

			cluster.Spec = FoundationDBClusterSpec{}

			Expect(cluster.DesiredDatabaseConfiguration()).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				StorageEngine:  "ssd-2",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:          3,
					Proxies:       3,
					CommitProxies: 2,
					GrvProxies:    1,
					Resolvers:     1,
					LogRouters:    -1,
					RemoteLogs:    -1,
				},
			}))
		})
	})

	When("getting the  configuration string", func() {
		It("should be parsed correctly", func() {
			configuration := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				StorageEngine:  "ssd",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs: 5,
				},
			}
			Expect(configuration.GetConfigurationString("6.3.24")).To(Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[]"))

			configuration.Regions = []Region{{
				DataCenters: []DataCenter{{
					ID:        "iad",
					Priority:  1,
					Satellite: 0,
				}},
				SatelliteLogs: 2,
			}}
			Expect(configuration.GetConfigurationString("6.3.24")).To(Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"iad\\\",\\\"priority\\\":1}],\\\"satellite_logs\\\":2}]"))
			configuration.Regions = nil

			configuration.VersionFlags.LogSpill = 3
			Expect(configuration.GetConfigurationString("6.3.24")).To(Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 log_spill:=3 regions=[]"))
		})
	})

	When("changing the redundancy mode", func() {
		It("should return the new redundancy mode", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
			}
			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeTriple,
			}
			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeTriple,
			}))
		})
	})

	When("enabling fearless DR", func() {
		It("should return the new fearless config", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("changing tto fearless dr without initial regions", func() {
		It("should return the fearless config", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("enabling a single region without initial regions", func() {
		It("should return the new redundancy mode", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("disabling fearless DR", func() {
		It("should return new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("disabling fearless DR and switch the dc", func() {
		It("should return the new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc2",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("disabling and clearing regions", func() {
		It("should return the new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 0,
							},
							{
								ID:        "dc4",
								Priority:  1,
								Satellite: 1,
							},
						},
						SatelliteLogs:           3,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("changing the primary DC with a single region", func() {
		It("should return the new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("changing the primary DC with multiple regions", func() {
		It("should return the new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 0,
							},
						},
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 0,
							},
						},
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 0,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 0,
							},
						},
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("changing multiple DCs", func() {
		It("should return the new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 0,
							},
						},
					},
				},
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc4",
								Priority: 0,
							},
						},
					},
				},
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: -1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  1,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc4",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc4",
								Priority: -1,
							},
						},
					},
				},
			}))
			Expect(nextConfig).NotTo(Equal(finalConfig))

			nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: RedundancyModeDouble,
				UsableRegions:  2,
				Regions: []Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc3",
								Priority: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc4",
								Priority: 0,
							},
						},
					},
				},
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	Context("Normalize cluster spec", func() {
		When("log routers are missing", func() {
			It("should set the correct value (-1) for og routers", func() {
				spec := DatabaseConfiguration{}
				spec.RemoteLogs = 9
				normalized := spec.NormalizeConfiguration()
				Expect(normalized.LogRouters).To(Equal(-1))
				Expect(normalized.RemoteLogs).To(Equal(9))
			})
		})

		When("the DC order is incorrect", func() {
			It("should correct the order", func() {
				spec := DatabaseConfiguration{
					Regions: []Region{
						{
							DataCenters: []DataCenter{
								{
									ID:       "dc1",
									Priority: 1,
								},
								{
									ID:        "dc1a",
									Priority:  1,
									Satellite: 1,
								},
								{
									ID:        "dc1b",
									Priority:  2,
									Satellite: 1,
								},
							},
						},
						{
							DataCenters: []DataCenter{
								{
									ID:        "dc2a",
									Priority:  2,
									Satellite: 1,
								},
								{
									ID:       "dc2",
									Priority: 1,
								},
								{
									ID:        "dc2b",
									Priority:  0,
									Satellite: 1,
								},
							},
						},
					},
				}
				normalized := spec.NormalizeConfiguration()
				Expect(normalized.Regions).To(Equal([]Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 1,
							},
							{
								ID:        "dc1b",
								Priority:  2,
								Satellite: 1,
							},
							{
								ID:        "dc1a",
								Priority:  1,
								Satellite: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
							{
								ID:        "dc2a",
								Priority:  2,
								Satellite: 1,
							},
							{
								ID:        "dc2b",
								Priority:  0,
								Satellite: 1,
							},
						},
					},
				}))
			})
		})

		When("the region order is incorrect", func() {
			It("should return the correct order", func() {
				spec := DatabaseConfiguration{
					Regions: []Region{
						{
							DataCenters: []DataCenter{
								{
									ID:       "dc1",
									Priority: 0,
								},
								{
									ID:        "dc1a",
									Priority:  2,
									Satellite: 1,
								},
								{
									ID:        "dc1b",
									Priority:  1,
									Satellite: 1,
								},
							},
						},
						{
							DataCenters: []DataCenter{
								{
									ID:       "dc2",
									Priority: 1,
								},
								{
									ID:        "dc2a",
									Priority:  1,
									Satellite: 1,
								},
								{
									ID:        "dc2b",
									Priority:  0,
									Satellite: 1,
								},
							},
						},
					},
				}
				normalized := spec.NormalizeConfiguration()
				Expect(normalized.Regions).To(Equal([]Region{
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc2",
								Priority: 1,
							},
							{
								ID:        "dc2a",
								Priority:  1,
								Satellite: 1,
							},
							{
								ID:        "dc2b",
								Priority:  0,
								Satellite: 1,
							},
						},
					},
					{
						DataCenters: []DataCenter{
							{
								ID:       "dc1",
								Priority: 0,
							},
							{
								ID:        "dc1a",
								Priority:  2,
								Satellite: 1,
							},
							{
								ID:        "dc1b",
								Priority:  1,
								Satellite: 1,
							},
						},
					},
				}))
			})
		})
	})

	When("the process address is parsed", func() {
		type testCase struct {
			input        string
			expectedAddr ProcessAddress
			expectedStr  string
			err          error
		}

		DescribeTable("should print the correct string",
			func(tc testCase) {
				address, err := ParseProcessAddress(tc.input)
				if err == nil {
					Expect(err).NotTo(HaveOccurred())
					Expect(address).To(Equal(tc.expectedAddr))
					Expect(address.String()).To(Equal(tc.expectedStr))
				} else {
					// When an error has happened we don't have to check the result
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal(tc.err.Error()))
				}
			},
			Entry("IPv4 with TLS flag",
				testCase{
					input: "127.0.0.1:4500:tls",
					expectedAddr: ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
						Port:      4500,
						Flags:     map[string]bool{"tls": true},
					},
					expectedStr: "127.0.0.1:4500:tls",
					err:         nil,
				}),
			Entry("IPv4 without TLS flag",
				testCase{
					input: "127.0.0.1:4500",
					expectedAddr: ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
						Port:      4500,
						Flags:     nil,
					},
					expectedStr: "127.0.0.1:4500",
					err:         nil,
				}),
			Entry("IPv4 without port and TLS flag",
				testCase{
					input: "127.0.0.1",
					expectedAddr: ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
						Port:      0,
						Flags:     nil,
					},
					expectedStr: "127.0.0.1",
					err:         nil,
				}),
			Entry("IPv6 with TLS flag",
				testCase{
					input: "[::1]:4500:tls",
					expectedAddr: ProcessAddress{
						IPAddress: net.ParseIP("::1"),
						Port:      4500,
						Flags:     map[string]bool{"tls": true},
					},
					expectedStr: "[::1]:4500:tls",
					err:         nil,
				}),
			Entry("IPv6 without TLS flag",
				testCase{
					input: "[::1]:4500",
					expectedAddr: ProcessAddress{
						IPAddress: net.ParseIP("::1"),
						Port:      4500,
						Flags:     nil,
					},
					expectedStr: "[::1]:4500",
					err:         nil,
				}),
			Entry("IPv6 without port and TLS flag",
				testCase{
					input: "::1",
					expectedAddr: ProcessAddress{
						IPAddress: net.ParseIP("::1"),
						Port:      0,
						Flags:     nil,
					},
					expectedStr: "::1",
					err:         nil,
				}),
			Entry("IPv6 with bad port",
				testCase{
					input: "[::1]:bad",
					err:   fmt.Errorf("strconv.Atoi: parsing \"bad\": invalid syntax"),
				}),
			Entry("IPv4 with bad port",
				testCase{
					input: "127.0.0.1:bad",
					err:   fmt.Errorf("strconv.Atoi: parsing \"bad\": invalid syntax"),
				}),
			Entry("host name",
				testCase{
					input: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local:4500",
					expectedAddr: ProcessAddress{
						StringAddress: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local",
						Port:          4500,
						Flags:         nil,
					},
					expectedStr: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local:4500",
					err:         nil,
				}),
			Entry("IP from host name",
				testCase{
					input: "127.0.0.1:4501(fromHostname)",
					expectedAddr: ProcessAddress{
						IPAddress:    net.ParseIP("127.0.0.1"),
						Port:         4501,
						Flags:        nil,
						FromHostname: true,
					},
					expectedStr: "127.0.0.1:4501(fromHostname)",
					err:         nil,
				}),
		)
	})

	When("printing a process address", func() {
		type testCase struct {
			processAddr ProcessAddress
			expected    string
		}

		DescribeTable("should print the correct string",
			func(tc testCase) {
				Expect(tc.processAddr.String()).To(Equal(tc.expected))

			},
			Entry("IPv6 with TLS flag",
				testCase{
					processAddr: ProcessAddress{
						IPAddress: net.ParseIP("::1"),
						Port:      4500,
						Flags:     map[string]bool{"tls": true},
					},
					expected: "[::1]:4500:tls",
				}),
			Entry("IPv6 with without TLS flag",
				testCase{
					processAddr: ProcessAddress{
						IPAddress: net.ParseIP("::1"),
						Port:      4500,
						Flags:     map[string]bool{},
					},
					expected: "[::1]:4500",
				}),
			Entry("IPv6 with without port and TLS flag",
				testCase{
					processAddr: ProcessAddress{
						IPAddress: net.ParseIP("::1"),
						Port:      0,
						Flags:     map[string]bool{},
					},
					expected: "::1",
				}),
			Entry("IPv4 with TLS flag",
				testCase{
					processAddr: ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
						Port:      4500,
						Flags:     map[string]bool{"tls": true},
					},
					expected: "127.0.0.1:4500:tls",
				}),
			Entry("IPv4 with without TLS flag",
				testCase{
					processAddr: ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
						Port:      4500,
						Flags:     map[string]bool{},
					},
					expected: "127.0.0.1:4500",
				}),
			Entry("IPv4 with without port and TLS flag",
				testCase{
					processAddr: ProcessAddress{
						IPAddress: net.ParseIP("127.0.0.1"),
						Port:      0,
						Flags:     map[string]bool{},
					},
					expected: "127.0.0.1",
				}),
			Entry("With a placeholder",
				testCase{
					processAddr: ProcessAddress{
						StringAddress: "$POD_IP",
						Port:          4500,
						Flags:         map[string]bool{},
					},
					expected: "$POD_IP:4500",
				}),
		)
	})

	When("a process group is being removed", func() {
		It("should remove the process group", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sample-cluster",
				},
			}
			Expect(cluster.ProcessGroupIsBeingRemoved("storage-1")).To(BeFalse())

			cluster.Spec.ProcessGroupsToRemove = []string{"log-1"}
			Expect(cluster.ProcessGroupIsBeingRemoved("storage-1")).To(BeFalse())
			Expect(cluster.ProcessGroupIsBeingRemoved("log-1")).To(BeTrue())
			cluster.Spec.ProcessGroupsToRemove = nil

			cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []string{"log-1"}
			Expect(cluster.ProcessGroupIsBeingRemoved("storage-1")).To(BeFalse())
			Expect(cluster.ProcessGroupIsBeingRemoved("log-1")).To(BeTrue())
			cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = nil
		})
	})

	When("checking the reconciliation for a cluster", func() {
		It("should return the correct status", func() {
			createCluster := func() *FoundationDBCluster {
				return &FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "sample-cluster",
						Namespace:  "default",
						Generation: 2,
					},
					Spec: FoundationDBClusterSpec{
						Version:               Versions.Default.String(),
						DatabaseConfiguration: DatabaseConfiguration{},
					},
					Status: FoundationDBClusterStatus{
						Health: ClusterHealth{
							Available: true,
							Healthy:   true,
						},
						RequiredAddresses: RequiredAddressSet{
							NonTLS: true,
						},
						DatabaseConfiguration: DatabaseConfiguration{
							RedundancyMode: RedundancyModeDouble,
							StorageEngine:  "ssd-2",
							UsableRegions:  1,
							RoleCounts: RoleCounts{
								Logs:          3,
								Proxies:       3,
								GrvProxies:    1,
								CommitProxies: 2,
								Resolvers:     1,
								LogRouters:    -1,
								RemoteLogs:    -1,
							},
						},
						Generations: ClusterGenerationStatus{
							Reconciled: 1,
						},
						ProcessGroups: []*ProcessGroupStatus{
							{ProcessGroupID: "storage-1", ProcessClass: "storage"},
							{ProcessGroupID: "storage-2", ProcessClass: "storage"},
							{ProcessGroupID: "storage-3", ProcessClass: "storage"},
							{ProcessGroupID: "stateless-1", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-2", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-3", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-4", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-5", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-6", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-7", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-8", ProcessClass: "stateless"},
							{ProcessGroupID: "stateless-9", ProcessClass: "stateless"},
							{ProcessGroupID: "log-1", ProcessClass: "log"},
							{ProcessGroupID: "log-2", ProcessClass: "log"},
							{ProcessGroupID: "log-3", ProcessClass: "log"},
							{ProcessGroupID: "log-4", ProcessClass: "log"},
						},
						Configured: true,
					},
				}
			}

			cluster := createCluster()

			result, err := cluster.CheckReconciliation(log)
			Expect(result).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 2,
			}))

			cluster = createCluster()
			cluster.Status.Configured = false
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:               1,
				NeedsConfigurationChange: 2,
			}))

			cluster = createCluster()
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, &ProcessGroupStatus{ProcessGroupID: "storage-5", ProcessClass: "storage"})
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:  1,
				NeedsShrink: 2,
			}))

			cluster = createCluster()
			cluster.Status.ProcessGroups = cluster.Status.ProcessGroups[1:]
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 1,
				NeedsGrow:  2,
			}))

			cluster = createCluster()
			cluster.Status.Health.Available = false
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:          1,
				DatabaseUnavailable: 2,
			}))

			cluster = createCluster()
			cluster.Spec.DatabaseConfiguration.StorageEngine = "ssd-1"
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:               1,
				NeedsConfigurationChange: 2,
			}))

			cluster = createCluster()
			cluster.Status.HasIncorrectConfigMap = true
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:             1,
				NeedsMonitorConfUpdate: 2,
			}))

			cluster = createCluster()
			cluster.Status.RequiredAddresses.TLS = true
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:        1,
				HasExtraListeners: 2,
			}))

			cluster = createCluster()
			cluster.Spec.ProcessCounts.Storage = 2
			cluster.Status.ProcessGroups[0].MarkForRemoval()
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:  1,
				NeedsShrink: 2,
			}))

			cluster = createCluster()
			cluster.Spec.ProcessCounts.Storage = 2
			cluster.Status.ProcessGroups[0].MarkForRemoval()
			cluster.Status.ProcessGroups[0].UpdateCondition(ResourcesTerminating, true, nil, "")
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:        2,
				HasPendingRemoval: 2,
			}))

			cluster = createCluster()
			cluster.Spec.ProcessCounts.Storage = 2
			cluster.Status.ProcessGroups[0].MarkForRemoval()
			cluster.Status.ProcessGroups[0].SetExclude()
			cluster.Status.ProcessGroups[0].UpdateCondition(IncorrectCommandLine, true, nil, "")
			cluster.Status.ProcessGroups[0].UpdateCondition(ResourcesTerminating, true, nil, "")
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:        2,
				HasPendingRemoval: 2,
			}))

			cluster = createCluster()
			cluster.Status.HasIncorrectServiceConfig = true
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:         1,
				NeedsServiceUpdate: 2,
			}))

			cluster = createCluster()
			cluster.Status.NeedsNewCoordinators = true
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:             1,
				NeedsCoordinatorChange: 2,
			}))

			cluster = createCluster()
			cluster.Status.ProcessGroups[0].UpdateCondition(IncorrectCommandLine, true, nil, "")
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:          1,
				HasUnhealthyProcess: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1"})
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:                    1,
				NeedsLockConfigurationChanges: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1"})
			cluster.Status.Locks.DenyList = []string{"dc1", "dc2"}
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1", Allow: true})
			cluster.Status.Locks.DenyList = []string{"dc1", "dc2"}
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:                    1,
				NeedsLockConfigurationChanges: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1", Allow: true})
			result, err = cluster.CheckReconciliation(log)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 2,
			}))
		})
	})

	When("getting the process settings", func() {
		It("should return the correct settings", func() {
			cluster := &FoundationDBCluster{
				Spec: FoundationDBClusterSpec{
					Processes: map[ProcessClass]ProcessSettings{
						ProcessClassGeneral: {
							PodTemplate: &corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"test-label": "label1"},
								},
							},
							CustomParameters: FoundationDBCustomParameters{"test_knob=value1"},
						},
						ProcessClassStorage: {
							PodTemplate: &corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"test-label": "label2"},
								},
							},
						},
						ProcessClassStateless: {
							PodTemplate: &corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"test-label": "label3"},
								},
							},
						},
					},
				},
			}
			settings := cluster.GetProcessSettings(ProcessClassStorage)
			Expect(settings.PodTemplate.ObjectMeta.Labels).To(Equal(map[string]string{"test-label": "label2"}))
			Expect(settings.CustomParameters).To(Equal(FoundationDBCustomParameters{"test_knob=value1"}))
		})
	})

	When("getting the lock options", func() {
		It("should return the correct lock options", func() {
			cluster := &FoundationDBCluster{}

			Expect(cluster.GetLockPrefix()).To(Equal("\xff\x02/org.foundationdb.kubernetes-operator"))
			Expect(cluster.GetLockDuration()).To(Equal(10 * time.Minute))

			var disabled = true
			cluster.Spec.LockOptions.DisableLocks = &disabled
			Expect(cluster.ShouldUseLocks()).To(BeFalse())
			disabled = false
			Expect(cluster.ShouldUseLocks()).To(BeTrue())
			cluster.Spec.LockOptions.DisableLocks = nil

			cluster.Spec.LockOptions.LockKeyPrefix = "\xfe/locks"
			Expect(cluster.GetLockPrefix()).To(Equal("\xfe/locks"))

			cluster.Spec.LockOptions.DisableLocks = nil
			Expect(cluster.ShouldUseLocks()).To(BeFalse())

			cluster.Spec.FaultDomain.ZoneCount = 3
			Expect(cluster.ShouldUseLocks()).To(BeTrue())

			cluster.Spec.FaultDomain.ZoneCount = 0
			cluster.Spec.DatabaseConfiguration.Regions = []Region{
				{},
				{},
			}
			Expect(cluster.ShouldUseLocks()).To(BeTrue())

			duration := 60
			cluster.Spec.LockOptions.LockDurationMinutes = &duration
			Expect(cluster.GetLockDuration()).To(Equal(60 * time.Minute))
		})
	})

	When("getting the condition timestamp", func() {
		It("should return the correct timestamp", func() {
			status := &ProcessGroupStatus{}

			timestamp := time.Now().Unix()
			status.ProcessGroupConditions = append(status.ProcessGroupConditions, &ProcessGroupCondition{ProcessGroupConditionType: MissingProcesses, Timestamp: timestamp})

			result := status.GetConditionTime(MissingProcesses)
			Expect(result).NotTo(BeNil())
			Expect(*result).To(Equal(timestamp))
			Expect(status.GetConditionTime(IncorrectConfigMap)).To(BeNil())
		})
	})

	Describe("filter by condition", func() {
		var status []*ProcessGroupStatus
		BeforeEach(func() {
			status = nil
			status = append(status, &ProcessGroupStatus{
				ProcessGroupID: "storage-1",
			})
			status = append(status, &ProcessGroupStatus{
				ProcessGroupID:         "storage-2",
				ProcessGroupConditions: []*ProcessGroupCondition{NewProcessGroupCondition(IncorrectCommandLine)},
			})
			status = append(status, &ProcessGroupStatus{
				ProcessGroupID:         "storage-3",
				ProcessGroupConditions: []*ProcessGroupCondition{NewProcessGroupCondition(IncorrectPodSpec)},
			})
			status = append(status, &ProcessGroupStatus{
				ProcessGroupID:         "storage-4",
				ProcessGroupConditions: []*ProcessGroupCondition{NewProcessGroupCondition(IncorrectCommandLine), NewProcessGroupCondition(IncorrectPodSpec)},
			})
			status = append(status, &ProcessGroupStatus{
				ProcessGroupID:         "storage-5",
				ProcessGroupConditions: []*ProcessGroupCondition{NewProcessGroupCondition(IncorrectCommandLine)},
				RemovalTimestamp:       &metav1.Time{Time: time.Now()},
			})
		})

		Context("with a single condition", func() {
			It("should return the process groups with that condition", func() {
				groups := FilterByCondition(status, IncorrectCommandLine, false)
				Expect(groups).To(Equal([]string{"storage-2", "storage-4", "storage-5"}))
			})
		})

		When("ignoring removed processes", func() {
			It("should return the process groups with that condition", func() {
				groups := FilterByCondition(status, IncorrectCommandLine, true)
				Expect(groups).To(Equal([]string{"storage-2", "storage-4"}))
			})
		})

		Context("with a mix of required and forbidden conditions", func() {
			It("should return the process groups that match all rules", func() {
				groups := FilterByConditions(status, map[ProcessGroupConditionType]bool{IncorrectCommandLine: true, IncorrectPodSpec: false}, false)
				Expect(groups).To(Equal([]string{"storage-2", "storage-5"}))
			})
		})
	})

	When("getting the process address and port", func() {
		type testCase struct {
			processNumber int
			tls           bool
			expectedPort  int
		}

		DescribeTable("Generate the port correctly",
			func(tc testCase) {
				Expect(GetProcessPort(tc.processNumber, tc.tls)).To(Equal(tc.expectedPort))
			},
			Entry("test first process no tls",
				testCase{
					1,
					true,
					4500,
				}),
			Entry("test first process with tls",
				testCase{
					1,
					false,
					4501,
				}),
			Entry("test second process no tls",
				testCase{
					2,
					true,
					4502,
				}),
			Entry("test third process no tls",
				testCase{
					2,
					false,
					4503,
				}),
		)
	})

	When("adding StorageServerPerDisk", func() {
		type testCase struct {
			ValuesToAdd                   []int
			ExpectedLen                   int
			ExpectedStorageServersPerDisk []int
		}

		DescribeTable("should generate the status correctly",
			func(tc testCase) {
				status := FoundationDBClusterStatus{
					StorageServersPerDisk: []int{},
				}

				for _, val := range tc.ValuesToAdd {
					status.AddStorageServerPerDisk(val)
				}

				Expect(len(status.StorageServersPerDisk)).To(BeNumerically("==", tc.ExpectedLen))
				Expect(status.StorageServersPerDisk).To(Equal(tc.ExpectedStorageServersPerDisk))
			},
			Entry("Add missing element",
				testCase{
					ValuesToAdd:                   []int{1},
					ExpectedLen:                   1,
					ExpectedStorageServersPerDisk: []int{1},
				}),
			Entry("Duplicates should only inserted once",
				testCase{
					ValuesToAdd:                   []int{1, 1},
					ExpectedLen:                   1,
					ExpectedStorageServersPerDisk: []int{1},
				}),
			Entry("Multiple elements should be added",
				testCase{
					ValuesToAdd:                   []int{1, 2},
					ExpectedLen:                   2,
					ExpectedStorageServersPerDisk: []int{1, 2},
				}),
		)
	})

	When("adding addresses to a process group", func() {
		type testCase struct {
			initialProcessGroup  ProcessGroupStatus
			inputAddresses       []string
			keepOldAddresses     bool
			expectedProcessGroup ProcessGroupStatus
		}

		DescribeTable("should add or ignore the addresses",
			func(tc testCase) {
				tc.initialProcessGroup.AddAddresses(tc.inputAddresses, tc.keepOldAddresses)
				Expect(tc.expectedProcessGroup).To(Equal(tc.initialProcessGroup))

			},
			Entry("Empty input address",

				testCase{
					initialProcessGroup: ProcessGroupStatus{Addresses: []string{
						"1.1.1.1",
					}},
					inputAddresses: nil,
					expectedProcessGroup: ProcessGroupStatus{Addresses: []string{
						"1.1.1.1",
					}},
				}),
			Entry("New Pod IP",
				testCase{
					initialProcessGroup: ProcessGroupStatus{Addresses: []string{
						"1.1.1.1",
					}},
					inputAddresses: []string{
						"2.2.2.2",
					},
					expectedProcessGroup: ProcessGroupStatus{Addresses: []string{
						"2.2.2.2",
					}},
				}),
			Entry("New Pod IP and keep old addresses",
				testCase{
					initialProcessGroup: ProcessGroupStatus{
						Addresses: []string{
							"1.1.1.1",
						},
					},
					inputAddresses: []string{
						"2.2.2.2",
					},
					keepOldAddresses: true,
					expectedProcessGroup: ProcessGroupStatus{Addresses: []string{
						"1.1.1.1",
						"2.2.2.2",
					}},
				}),
		)
	})

	When("parsing the addresses from the process commandline", func() {
		type testCase struct {
			cmdline  string
			expected []ProcessAddress
		}

		DescribeTable("should add or ignore the addresses",
			func(tc testCase) {
				res, err := ParseProcessAddressesFromCmdline(tc.cmdline)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(res)).To(BeNumerically("==", len(tc.expected)))
				Expect(res).To(Equal(tc.expected))
			},
			Entry("Only no-tls",
				testCase{
					cmdline: "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-9 --locality_machineid=machine1 --locality_zoneid=zone1 --logdir=/var/log/fdb-trace-logs --loggroup=test --public_address=1.2.3.4:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					expected: []ProcessAddress{
						{
							IPAddress: net.ParseIP("1.2.3.4"),
							Port:      4501,
						},
					},
				}),
			Entry("Only TLS",
				testCase{
					cmdline: "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-9 --locality_machineid=machine1 --locality_zoneid=zone1 --logdir=/var/log/fdb-trace-logs --loggroup=test --public_address=1.2.3.4:4500:tls --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					expected: []ProcessAddress{
						{
							IPAddress: net.ParseIP("1.2.3.4"),
							Port:      4500,
							Flags:     map[string]bool{"tls": true},
						},
					},
				}),
			Entry("TLS IPv6",
				testCase{
					cmdline: "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-9 --locality_machineid=machine1 --locality_zoneid=zone1 --logdir=/var/log/fdb-trace-logs --loggroup=test --public_address=[::1]:4500:tls --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					expected: []ProcessAddress{
						{
							IPAddress: net.ParseIP("::1"),
							Port:      4500,
							Flags: map[string]bool{
								"tls": true,
							},
						},
					},
				}),
			Entry("TLS and no-TLS",
				testCase{
					cmdline: "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-9 --locality_machineid=machine1 --locality_zoneid=zone1 --logdir=/var/log/fdb-trace-logs --loggroup=test --public_address=1.2.3.4:4501,1.2.3.4:4500:tls --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					expected: []ProcessAddress{
						{
							IPAddress: net.ParseIP("1.2.3.4"),
							Port:      4501,
						},
						{
							IPAddress: net.ParseIP("1.2.3.4"),
							Port:      4500,
							Flags: map[string]bool{
								"tls": true,
							},
						},
					},
				}),
		)
	})

	When("checking if a process is eligible as coordinator candidate", func() {
		type testCase struct {
			cluster  *FoundationDBCluster
			pClass   ProcessClass
			expected bool
		}

		DescribeTable("should return if the process class is eligible",
			func(tc testCase) {
				Expect(tc.cluster.IsEligibleAsCandidate(tc.pClass)).To(Equal(tc.expected))
			},
			Entry("storage class without any configuration is eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStorage,
					expected: true,
				}),
			Entry("log class without any configuration is eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassLog,
					expected: true,
				}),
			Entry("transaction class without any configuration is eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassTransaction,
					expected: true,
				}),
			Entry("stateless class without any configuration is not eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStateless,
					expected: false,
				}),
			Entry("cluster controller class without any configuration is not eligible",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassClusterController,
					expected: false,
				}),
			Entry("storage class with only storage classes is eligible",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassStorage,
					expected: true,
				}),
			Entry("log class with only storage classes is not eligible",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassLog,
					expected: false,
				}),
		)
	})

	When("getting the priority of a process class", func() {
		type testCase struct {
			cluster  *FoundationDBCluster
			pClass   ProcessClass
			expected int
		}

		DescribeTable("should return the expected process class",
			func(tc testCase) {
				Expect(tc.cluster.GetClassCandidatePriority(tc.pClass)).To(Equal(tc.expected))
			},
			Entry("storage class without any configuration returns highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStorage,
					expected: math.MinInt64,
				}),
			Entry("log class without any configuration highest prioritye",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassLog,
					expected: math.MinInt64,
				}),
			Entry("transaction class without any configuration highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassTransaction,
					expected: math.MinInt64,
				}),
			Entry("stateless class without any configuration highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassStateless,
					expected: math.MinInt64,
				}),
			Entry("cluster controller class without any configuration highest priority",
				testCase{
					cluster:  &FoundationDBCluster{},
					pClass:   ProcessClassClusterController,
					expected: math.MinInt64,
				}),
			Entry("storage class with only storage classes returns 1 as priority",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassStorage,
					expected: 1,
				}),
			Entry("log class with only storage classes returns highest priority",
				testCase{
					cluster: &FoundationDBCluster{
						Spec: FoundationDBClusterSpec{
							CoordinatorSelection: []CoordinatorSelectionSetting{
								{
									ProcessClass: ProcessClassStorage,
									Priority:     1,
								},
							},
						},
					},
					pClass:   ProcessClassLog,
					expected: math.MinInt64,
				}),
		)
	})

	Describe("checking for explicit listen address", func() {
		var cluster *FoundationDBCluster

		BeforeEach(func() {
			cluster = &FoundationDBCluster{}
		})

		It("is not required for a default cluster", func() {
			Expect(cluster.NeedsExplicitListenAddress()).To(BeTrue())
		})

		It("is required with a service as the public IP", func() {
			source := PublicIPSourceService
			cluster.Spec.Routing.PublicIPSource = &source
			Expect(cluster.NeedsExplicitListenAddress()).To(BeTrue())
		})

		It("is required with a pod as the public IP", func() {
			source := PublicIPSourcePod
			cluster.Spec.Routing.PublicIPSource = &source
			Expect(cluster.NeedsExplicitListenAddress()).To(BeTrue())
		})

		It("is required with the flag set to true", func() {
			cluster.Spec.UseExplicitListenAddress = pointer.Bool(true)
			Expect(cluster.NeedsExplicitListenAddress()).To(BeTrue())
		})

		It("is not required with the flag set to false", func() {
			cluster.Spec.UseExplicitListenAddress = pointer.Bool(false)
			Expect(cluster.NeedsExplicitListenAddress()).To(BeFalse())
		})
	})

	When("checking whether the process group should be skipped or not", func() {
		type testCase struct {
			cluster  *FoundationDBCluster
			pStatus  *ProcessGroupStatus
			expected bool
		}

		DescribeTable("should return the expected result",
			func(tc testCase) {
				Expect(tc.cluster.SkipProcessGroup(tc.pStatus)).To(Equal(tc.expected))
			},
			Entry("nil process group should be skipped",
				testCase{
					cluster:  &FoundationDBCluster{},
					pStatus:  nil,
					expected: true,
				}),
			Entry("process group without condition should not be skipped",
				testCase{
					cluster:  &FoundationDBCluster{},
					pStatus:  &ProcessGroupStatus{},
					expected: false,
				}),
			Entry("process group with a different condition should not be skipped",
				testCase{
					cluster: &FoundationDBCluster{},
					pStatus: &ProcessGroupStatus{
						ProcessGroupConditions: []*ProcessGroupCondition{
							{
								ProcessGroupConditionType: PodFailing,
								Timestamp:                 time.Now().Unix(),
							},
						},
					},
					expected: false,
				}),
			Entry("process group with a pending condition for only a few seconds should not be skipped",
				testCase{
					cluster: &FoundationDBCluster{},
					pStatus: &ProcessGroupStatus{
						ProcessGroupConditions: []*ProcessGroupCondition{
							{
								ProcessGroupConditionType: PodPending,
								Timestamp:                 time.Now().Unix(),
							},
						},
					},
					expected: false,
				}),
			Entry("process group with a pending condition for multiple minutes should be skipped",
				testCase{
					cluster: &FoundationDBCluster{},
					pStatus: &ProcessGroupStatus{
						ProcessGroupConditions: []*ProcessGroupCondition{
							{
								ProcessGroupConditionType: PodPending,
								Timestamp:                 time.Now().Add(-15 * time.Minute).Unix(),
							},
						},
					},
					expected: true,
				}),
		)
	})

	When("merging image configs", func() {
		It("applies chooses the first value for each field", func() {
			configs := []ImageConfig{
				{
					BaseImage: "foundationdb/foundationdb",
					Version:   Versions.Default.String(),
				},
				{
					BaseImage: "foundationdb/foundationdb-slim",
					Version:   Versions.Default.String(),
					Tag:       "abcdef",
					TagSuffix: "-1",
				},
			}

			finalConfig := SelectImageConfig(configs, Versions.Default.String())
			Expect(finalConfig).To(Equal(ImageConfig{
				BaseImage: "foundationdb/foundationdb",
				Version:   Versions.Default.String(),
				Tag:       "abcdef",
				TagSuffix: "-1",
			}))
		})

		It("ignores configs that are for different versions", func() {
			configs := []ImageConfig{
				{
					BaseImage: "foundationdb/foundationdb",
					Version:   Versions.Default.String(),
				},
				{
					Version: Versions.NextMajorVersion.String(),
					Tag:     "abcdef",
				},
				{
					TagSuffix: "-1",
				},
			}

			finalConfig := SelectImageConfig(configs, Versions.Default.String())
			Expect(finalConfig).To(Equal(ImageConfig{
				BaseImage: "foundationdb/foundationdb",
				Version:   Versions.Default.String(),
				TagSuffix: "-1",
			}))
		})
	})

	When("building image names", func() {
		It("applies the fields", func() {
			config := ImageConfig{
				BaseImage: "foundationdb/foundationdb-kubernetes-sidecar",
				Version:   Versions.Default.String(),
				TagSuffix: "-2",
			}
			image := config.Image()
			Expect(image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-2", Versions.Default)))
		})

		It("uses the tag to override the version and tag suffix", func() {
			config := ImageConfig{
				BaseImage: "foundationdb/foundationdb-kubernetes-sidecar",
				Version:   Versions.Default.String(),
				Tag:       "abcdef",
				TagSuffix: "-2",
			}
			image := config.Image()
			Expect(image).To(Equal("foundationdb/foundationdb-kubernetes-sidecar:abcdef"))
		})
	})

	When("checking if the process group needs a replacement", func() {
		var processGroup *ProcessGroupStatus
		var needsReplacement bool
		var timestamp int64
		var oldTimestamp int64

		BeforeEach(func() {
			processGroup = &ProcessGroupStatus{ProcessGroupID: "storage-1", ProcessClass: "storage"}
			oldTimestamp = time.Now().Add(-1 * time.Hour).Unix()
		})

		JustBeforeEach(func() {
			needsReplacement, timestamp = processGroup.NeedsReplacement(60)
		})

		Context("with no conditions", func() {
			It("should not need replacement", func() {
				Expect(needsReplacement).To(BeFalse())
			})
		})

		Context("with a process group that went missing after the window", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(MissingProcesses, true, nil, "")
			})

			It("should not need replacement", func() {
				Expect(needsReplacement).To(BeFalse())
			})
		})

		Context("with a process group that went missing before the window", func() {
			var targetTimestamp int64
			BeforeEach(func() {
				processGroup.UpdateCondition(MissingProcesses, true, nil, "")
				targetTimestamp = time.Now().Add(-1 * time.Hour).Unix()
				processGroup.ProcessGroupConditions[0].Timestamp = targetTimestamp
			})

			It("should need replacement", func() {
				Expect(needsReplacement).To(BeTrue())
				Expect(timestamp).To(Equal(targetTimestamp))
			})
		})

		Context("with multiple conditions that could trigger a replacement", func() {
			var targetTimestamp int64
			BeforeEach(func() {
				processGroup.UpdateCondition(MissingProcesses, true, nil, "")
				processGroup.UpdateCondition(PodFailing, true, nil, "")
				targetTimestamp = time.Now().Add(-1 * time.Hour).Unix()
				processGroup.ProcessGroupConditions[0].Timestamp = targetTimestamp
				processGroup.ProcessGroupConditions[1].Timestamp = targetTimestamp - 60
			})

			It("should use the older timestamp", func() {
				Expect(needsReplacement).To(BeTrue())
				Expect(timestamp).To(Equal(targetTimestamp - 60))
			})
		})

		Context("with a process group that failed", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(PodFailing, true, nil, "")
				processGroup.ProcessGroupConditions[0].Timestamp = oldTimestamp
			})

			It("should need replacement", func() {
				Expect(needsReplacement).To(BeTrue())
				Expect(timestamp).To(Equal(oldTimestamp))
			})
		})

		Context("with a process group that had the wrong command line", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(IncorrectCommandLine, true, nil, "")
				processGroup.ProcessGroupConditions[0].Timestamp = oldTimestamp
			})

			It("should not need replacement", func() {
				Expect(needsReplacement).To(BeFalse())
			})
		})
	})

	When("using the database configuration  to fail over", func() {
		When("using a single region cluster", func() {
			var cluster *FoundationDBCluster

			BeforeEach(func() {
				cluster = &FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						DatabaseConfiguration: DatabaseConfiguration{
							Regions: []Region{
								{
									DataCenters: []DataCenter{
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

			It("should return the same configuration", func() {
				config := cluster.Spec.DatabaseConfiguration.FailOver()
				Expect(config).To(Equal(cluster.Spec.DatabaseConfiguration))
			})
		})

		When("using a multi region cluster", func() {
			var cluster *FoundationDBCluster

			BeforeEach(func() {
				cluster = &FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						DatabaseConfiguration: DatabaseConfiguration{
							Regions: []Region{
								{
									DataCenters: []DataCenter{
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
									DataCenters: []DataCenter{
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

			It("should change the priority", func() {
				expected := DatabaseConfiguration{
					Regions: []Region{
						{
							DataCenters: []DataCenter{
								{
									ID:       "primary",
									Priority: 0,
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
							DataCenters: []DataCenter{
								{
									ID:       "remote",
									Priority: 1,
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
				}

				config := cluster.Spec.DatabaseConfiguration.FailOver()
				Expect(config).To(Equal(expected))
				// The cluster config should not be changed
				Expect(cluster.Spec.DatabaseConfiguration).NotTo(Equal(expected))
			})
		})
	})

	Describe("routing configuration", func() {
		var cluster *FoundationDBCluster
		BeforeEach(func() {
			cluster = &FoundationDBCluster{}
		})

		When("checking whether we need a headless service", func() {
			It("respects the headless service setting", func() {
				Expect(cluster.NeedsHeadlessService()).To(BeFalse())

				cluster.Spec.Routing.HeadlessService = pointer.Bool(true)
				Expect(cluster.NeedsHeadlessService()).To(BeTrue())
			})

			It("can be overridden by the DNS setting", func() {
				cluster.Spec.Routing.HeadlessService = pointer.Bool(false)
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				Expect(cluster.NeedsHeadlessService()).To(BeTrue())
			})
		})

		When("checking whether we use DNS in the cluster file", func() {
			It("respects the value in the flag", func() {
				Expect(cluster.UseDNSInClusterFile()).To(BeFalse())

				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				Expect(cluster.UseDNSInClusterFile()).To(BeTrue())
			})
		})

		When("getting the DNS domain", func() {
			It("allows overrides in the spec", func() {
				Expect(cluster.GetDNSDomain()).To(Equal("cluster.local"))
				suffix := "cluster.example"
				cluster.Spec.Routing.DNSDomain = &suffix
				Expect(cluster.GetDNSDomain()).To(Equal(suffix))
			})
		})
	})

	When("checking if the process group must be replaced", func() {
		DescribeTable("it should return to correct update strategy",
			func(cluster *FoundationDBCluster, processGroup *ProcessGroupStatus, expected bool) {
				Expect(cluster.NeedsReplacement(processGroup)).To(Equal(expected))
			},
			Entry("Default update strategy storage process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: "",
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassStorage,
				},
				false,
			),
			Entry("Default update strategy log process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: "",
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassTransaction,
				},
				true,
			),
			Entry("Update strategy all storage process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: PodUpdateStrategyReplacement,
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassStorage,
				},
				true,
			),
			Entry("Update strategy all log process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: PodUpdateStrategyReplacement,
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassTransaction,
				},
				true,
			),
			Entry("Update strategy transaction system storage process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: PodUpdateStrategyTransactionReplacement,
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassStorage,
				},
				false,
			),
			Entry("Update strategy transaction system log process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: PodUpdateStrategyTransactionReplacement,
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassTransaction,
				},
				true,
			),
			Entry("Update strategy delete storage process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: PodUpdateStrategyDelete,
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassStorage,
				},
				false,
			),
			Entry("Update strategy delete log process",
				&FoundationDBCluster{
					Spec: FoundationDBClusterSpec{
						AutomationOptions: FoundationDBClusterAutomationOptions{
							PodUpdateStrategy: PodUpdateStrategyDelete,
						},
					},
				},
				&ProcessGroupStatus{
					ProcessClass: ProcessClassTransaction,
				},
				false,
			),
		)
	})

	When("adding a process to the removal list", func() {
		var cluster *FoundationDBCluster

		BeforeEach(func() {
			cluster = &FoundationDBCluster{}
		})

		When("removing with exclusion", func() {
			When("a np process group is already included in the list", func() {
				It("should add the process group to the removal list", func() {
					removals := []string{"test1"}
					cluster.AddProcessGroupsToRemovalList(removals)
					Expect(cluster.Spec.ProcessGroupsToRemove).To(ContainElements(removals))
					Expect(len(cluster.Spec.ProcessGroupsToRemove)).To(Equal(len(removals)))
					Expect(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(Equal(0))
				})
			})

			When("a process group is already included in the list", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = append(cluster.Spec.ProcessGroupsToRemove, "test1")
				})

				It("should only add the missing process groups", func() {
					Expect(cluster.Spec.ProcessGroupsToRemove).To(ContainElements("test1"))
					removals := []string{"test1", "test2"}
					cluster.AddProcessGroupsToRemovalList(removals)
					Expect(cluster.Spec.ProcessGroupsToRemove).To(ContainElements(removals))
					Expect(len(cluster.Spec.ProcessGroupsToRemove)).To(Equal(len(removals)))
					Expect(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(Equal(0))
				})
			})
		})

		When("removing without exclusion", func() {
			When("a process group is not already included in the list", func() {
				It("should add the process group to the removal list", func() {
					removals := []string{"test1"}
					cluster.AddProcessGroupsToRemovalWithoutExclusionList(removals)
					Expect(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements(removals))
					Expect(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(Equal(len(removals)))
					Expect(len(cluster.Spec.ProcessGroupsToRemove)).To(Equal(0))
				})
			})

			When("a process group is already included in the list", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = append(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion, "test1")
					Expect(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements("test1"))
				})

				It("should only add the missing process groups", func() {
					removals := []string{"test1", "test2"}
					cluster.AddProcessGroupsToRemovalWithoutExclusionList(removals)
					Expect(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements(removals))
					Expect(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(Equal(len(removals)))
					Expect(len(cluster.Spec.ProcessGroupsToRemove)).To(Equal(0))
				})
			})

			When("a process group is already included in the with exclusion list", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = append(cluster.Spec.ProcessGroupsToRemove, "test1")
					Expect(cluster.Spec.ProcessGroupsToRemove).To(ContainElements("test1"))
					Expect(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(Equal(0))
				})

				It("should only add the missing process groups", func() {
					removals := []string{"test1", "test2"}
					cluster.AddProcessGroupsToRemovalWithoutExclusionList(removals)
					Expect(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(ContainElements(removals))
					Expect(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(Equal(len(removals)))
					Expect(len(cluster.Spec.ProcessGroupsToRemove)).To(Equal(1))
				})
			})
		})
	})
})
