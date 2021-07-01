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

package v1beta1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] FoundationDBCluster", func() {
	When("getting the default role counts", func() {
		It("should return the default role counts", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: "double",
					},
				},
			}

			counts := cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:    3,
				Logs:       3,
				Proxies:    3,
				Resolvers:  1,
				RemoteLogs: -1,
				LogRouters: -1,
			}))
			Expect(counts.Map()).To(Equal(map[ProcessClass]int{
				"logs":        3,
				"proxies":     3,
				"resolvers":   1,
				"remote_logs": -1,
				"log_routers": -1,
			}))
			Expect(cluster.Spec.RoleCounts).To(Equal(RoleCounts{}))

			cluster.Spec.UsableRegions = 2
			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:    3,
				Logs:       3,
				Proxies:    3,
				Resolvers:  1,
				RemoteLogs: 3,
				LogRouters: 3,
			}))
			Expect(counts.Map()).To(Equal(map[ProcessClass]int{
				"logs":        3,
				"proxies":     3,
				"resolvers":   1,
				"remote_logs": 3,
				"log_routers": 3,
			}))

			cluster.Spec.RoleCounts = RoleCounts{
				Storage: 5,
			}

			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:    5,
				Logs:       3,
				Proxies:    3,
				Resolvers:  1,
				RemoteLogs: 3,
				LogRouters: 3,
			}))

			cluster.Spec.RoleCounts = RoleCounts{
				Logs: 8,
			}
			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:    3,
				Logs:       8,
				Proxies:    3,
				Resolvers:  1,
				RemoteLogs: 8,
				LogRouters: 8,
			}))

			cluster.Spec.RoleCounts = RoleCounts{
				Logs:       4,
				RemoteLogs: 5,
				LogRouters: 6,
			}
			counts = cluster.GetRoleCountsWithDefaults()
			Expect(counts).To(Equal(RoleCounts{
				Storage:    3,
				Logs:       4,
				Proxies:    3,
				Resolvers:  1,
				RemoteLogs: 5,
				LogRouters: 6,
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
						RedundancyMode: "double",
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
				Resolver: 1,
			}
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Stateless).To(Equal(8))
			Expect(counts.Resolver).To(Equal(1))
			Expect(counts.Resolution).To(Equal(0))

			cluster.Spec.ProcessCounts = ProcessCounts{
				Resolution: 1,
			}
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Stateless).To(Equal(8))
			Expect(counts.Resolution).To(Equal(1))
			Expect(counts.Resolver).To(Equal(0))

			cluster.Spec.ProcessCounts = ProcessCounts{
				Log: 2,
			}
			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Log).To(Equal(2))

			cluster.Spec.ProcessCounts = ProcessCounts{}
			cluster.Spec.RoleCounts.RemoteLogs = 4
			cluster.Spec.RoleCounts.LogRouters = 8

			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   5,
				Log:       5,
				Stateless: 9,
			}))

			cluster.Spec.ProcessCounts = ProcessCounts{}
			cluster.Spec.RoleCounts = RoleCounts{}
			cluster.Spec.Version = Versions.WithoutRatekeeperRole.String()

			counts, err = cluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(ProcessCounts{
				Storage:   3,
				Log:       4,
				Stateless: 7,
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
						RedundancyMode: "double",
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
						RedundancyMode: "double",
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

			cluster.Spec.RedundancyMode = "single"
			Expect(cluster.DesiredFaultTolerance()).To(Equal(0))
			Expect(cluster.MinimumFaultDomains()).To(Equal(1))
			Expect(cluster.DesiredCoordinatorCount()).To(Equal(1))

			cluster.Spec.RedundancyMode = "double"
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

	When("parsing the connection string", func() {
		It("should be parsed correctly", func() {
			str, err := ParseConnectionString("test:abcd@127.0.0.1:4500,127.0.0.2:4500,127.0.0.3:4500")
			Expect(err).NotTo(HaveOccurred())
			Expect(str.DatabaseName).To(Equal("test"))
			Expect(str.GenerationID).To(Equal("abcd"))
			Expect(str.Coordinators).To(Equal([]string{
				"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500",
			}))

			str, err = ParseConnectionString("test:abcd")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("Invalid connection string test:abcd"))
		})
	})

	When("formatting the connection string", func() {
		It("should be formatted correctly", func() {
			str := ConnectionString{
				DatabaseName: "test",
				GenerationID: "abcd",
				Coordinators: []string{
					"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500",
				},
			}
			Expect(str.String()).To(Equal("test:abcd@127.0.0.1:4500,127.0.0.2:4500,127.0.0.3:4500"))
		})
	})

	When("generating the connection ID from the connection string", func() {
		It("should be formatted correctly", func() {
			str := ConnectionString{
				DatabaseName: "test",
				GenerationID: "abcd",
				Coordinators: []string{
					"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500",
				},
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
				Coordinators: []string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500"},
			}
			Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500"})).To(BeTrue())
			Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.3:4500", "127.0.0.2:4500"})).To(BeTrue())
			Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500", "127.0.0.4:4500"})).To(BeFalse())
			Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.4:4500"})).To(BeFalse())
			Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500"})).To(BeFalse())
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
						RedundancyMode: "double",
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
				RedundancyMode: "double",
				StorageEngine:  "ssd-2",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:       4,
					Proxies:    5,
					Resolvers:  1,
					LogRouters: -1,
					RemoteLogs: -1,
				},
			}))

			cluster.Spec = FoundationDBClusterSpec{}

			Expect(cluster.DesiredDatabaseConfiguration()).To(Equal(DatabaseConfiguration{
				RedundancyMode: "double",
				StorageEngine:  "ssd-2",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:       3,
					Proxies:    3,
					Resolvers:  1,
					LogRouters: -1,
					RemoteLogs: -1,
				},
			}))
		})
	})

	When("getting the  configuration string", func() {
		It("should be parsed correctly", func() {
			configuration := DatabaseConfiguration{
				RedundancyMode: "double",
				StorageEngine:  "ssd",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs: 5,
				},
			}
			Expect(configuration.GetConfigurationString()).To(Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[]"))

			configuration.Regions = []Region{{
				DataCenters: []DataCenter{{
					ID:        "iad",
					Priority:  1,
					Satellite: 0,
				}},
				SatelliteLogs: 2,
			}}
			Expect(configuration.GetConfigurationString()).To(Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"iad\\\",\\\"priority\\\":1}],\\\"satellite_logs\\\":2}]"))
			configuration.Regions = nil

			configuration.VersionFlags.LogSpill = 3
			Expect(configuration.GetConfigurationString()).To(Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 log_spill:=3 regions=[]"))
		})
	})

	When("getting the version for the sidecar", func() {
		It("should return the correct sidecar version", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: FoundationDBClusterSpec{
					Version: "6.2.15",
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode: "double",
					},
				},
			}

			Expect(cluster.GetFullSidecarVersion(false)).To(Equal("6.2.15-1"))

			cluster.Spec.SidecarVersions = map[string]int{
				"6.2.14": 3,
				"6.2.15": 2,
			}

			Expect(cluster.GetFullSidecarVersion(false)).To(Equal("6.2.15-2"))
		})
	})

	Context("Using the fdb version", func() {
		It("should return the fdb version struct", func() {
			version, err := ParseFdbVersion("6.2.11")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

			version, err = ParseFdbVersion("prerelease-6.2.11")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

			version, err = ParseFdbVersion("test-6.2.11-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

			_, err = ParseFdbVersion("6.2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("could not parse FDB version from 6.2"))
		})

		It("should format the version correctly", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 11}
			Expect(version.String()).To(Equal("6.2.11"))
		})

		It("should validate the flags for the version correct", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 0}
			Expect(version.HasInstanceIDInSidecarSubstitutions()).To(BeFalse())
			Expect(version.PrefersCommandLineArgumentsInSidecar()).To(BeFalse())

			version = FdbVersion{Major: 7, Minor: 0, Patch: 0}
			Expect(version.HasInstanceIDInSidecarSubstitutions()).To(BeTrue())
			Expect(version.PrefersCommandLineArgumentsInSidecar()).To(BeTrue())
		})
	})

	When("changing the redundancy mode", func() {
		It("should return the new redundancy mode", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: "double",
			}
			finalConfig := DatabaseConfiguration{
				RedundancyMode: "triple",
			}
			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: "triple",
			}))
		})
	})

	When("enabling fearless DR", func() {
		It("should return the new fearless config", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
				UsableRegions:  1,
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
				UsableRegions:  1,
			}

			finalConfig := DatabaseConfiguration{
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
				UsableRegions:  1,
			}

			nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
			Expect(nextConfig).To(Equal(DatabaseConfiguration{
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
				UsableRegions:  1,
			}))
			Expect(nextConfig).To(Equal(finalConfig))
		})
	})

	When("changing the primary DC with a single region", func() {
		It("should return the new configuration", func() {
			currentConfig := DatabaseConfiguration{
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
				RedundancyMode: "double",
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
		It("should parse the address correctly", func() {
			address, err := ParseProcessAddress("127.0.0.1:4500:tls")
			Expect(err).NotTo(HaveOccurred())
			Expect(address).To(Equal(ProcessAddress{
				IPAddress: "127.0.0.1",
				Port:      4500,
				Flags:     map[string]bool{"tls": true},
			}))
			Expect(address.String()).To(Equal("127.0.0.1:4500:tls"))

			address, err = ParseProcessAddress("127.0.0.1:4501")
			Expect(err).NotTo(HaveOccurred())
			Expect(address).To(Equal(ProcessAddress{
				IPAddress: "127.0.0.1",
				Port:      4501,
			}))
			Expect(address.String()).To(Equal("127.0.0.1:4501"))

			address, err = ParseProcessAddress("127.0.0.1:bad")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("strconv.Atoi: parsing \"bad\": invalid syntax"))
		})
	})

	When("an instance is being removed", func() {
		It("should remove the instance", func() {
			cluster := &FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sample-cluster",
				},
			}
			Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(BeFalse())

			cluster.Spec.PendingRemovals = map[string]string{
				"sample-cluster-storage-1": "127.0.0.1",
			}
			Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(BeTrue())
			Expect(cluster.InstanceIsBeingRemoved("log-1")).To(BeFalse())
			cluster.Spec.PendingRemovals = nil

			cluster.Status.PendingRemovals = map[string]PendingRemovalState{
				"log-1": {
					PodName: "sample-cluster-log-1",
					Address: "127.0.0.2",
				},
			}
			Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(BeFalse())
			Expect(cluster.InstanceIsBeingRemoved("log-1")).To(BeTrue())
			cluster.Status.PendingRemovals = nil

			cluster.Spec.InstancesToRemove = []string{"log-1"}
			Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(BeFalse())
			Expect(cluster.InstanceIsBeingRemoved("log-1")).To(BeTrue())
			cluster.Spec.InstancesToRemove = nil
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
							RedundancyMode: "double",
							StorageEngine:  "ssd-2",
							UsableRegions:  1,
							RoleCounts: RoleCounts{
								Logs:       3,
								Proxies:    3,
								Resolvers:  1,
								LogRouters: -1,
								RemoteLogs: -1,
							},
						},
						Generations: ClusterGenerationStatus{
							Reconciled: 1,
						},
						ProcessCounts: ProcessCounts{
							Storage:   3,
							Stateless: 9,
							Log:       4,
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

			result, err := cluster.CheckReconciliation()
			Expect(result).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 2,
			}))

			cluster = createCluster()
			cluster.Status.Configured = false
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:               1,
				NeedsConfigurationChange: 2,
			}))

			cluster = createCluster()
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, &ProcessGroupStatus{ProcessGroupID: "storage-5", ProcessClass: "storage"})
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:  1,
				NeedsShrink: 2,
			}))

			cluster = createCluster()
			cluster.Status.ProcessGroups = cluster.Status.ProcessGroups[1:]
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 1,
				NeedsGrow:  2,
			}))

			cluster = createCluster()
			cluster.Status.Health.Available = false
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:          1,
				DatabaseUnavailable: 2,
			}))

			cluster = createCluster()
			cluster.Spec.DatabaseConfiguration.StorageEngine = "ssd-1"
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:               1,
				NeedsConfigurationChange: 2,
			}))

			cluster = createCluster()
			cluster.Status.HasIncorrectConfigMap = true
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:             1,
				NeedsMonitorConfUpdate: 2,
			}))

			cluster = createCluster()
			cluster.Status.RequiredAddresses.TLS = true
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:        1,
				HasExtraListeners: 2,
			}))

			cluster = createCluster()
			cluster.Spec.ProcessCounts.Storage = 2
			cluster.Status.ProcessGroups[0].Remove = true
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:  1,
				NeedsShrink: 2,
			}))

			cluster = createCluster()
			cluster.Spec.ProcessCounts.Storage = 2
			cluster.Status.ProcessGroups[0].Remove = true
			cluster.Status.ProcessGroups[0].Excluded = true
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:        2,
				HasPendingRemoval: 2,
			}))

			cluster = createCluster()
			cluster.Spec.ProcessCounts.Storage = 2
			cluster.Status.ProcessGroups[0].Remove = true
			cluster.Status.ProcessGroups[0].Excluded = true
			cluster.Status.ProcessGroups[0].UpdateCondition(IncorrectCommandLine, true, nil, "")
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:        2,
				HasPendingRemoval: 2,
			}))

			cluster = createCluster()
			cluster.Status.HasIncorrectServiceConfig = true
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:         1,
				NeedsServiceUpdate: 2,
			}))

			cluster = createCluster()
			cluster.Status.NeedsNewCoordinators = true
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:             1,
				NeedsCoordinatorChange: 2,
			}))

			cluster = createCluster()
			cluster.Status.ProcessGroups[0].UpdateCondition(IncorrectCommandLine, true, nil, "")
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:          1,
				HasUnhealthyProcess: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1"})
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:                    1,
				NeedsLockConfigurationChanges: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1"})
			cluster.Status.Locks.DenyList = []string{"dc1", "dc2"}
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1", Allow: true})
			cluster.Status.Locks.DenyList = []string{"dc1", "dc2"}
			result, err = cluster.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(cluster.Status.Generations).To(Equal(ClusterGenerationStatus{
				Reconciled:                    1,
				NeedsLockConfigurationChanges: 2,
			}))

			cluster = createCluster()
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, LockDenyListEntry{ID: "dc1", Allow: true})
			result, err = cluster.CheckReconciliation()
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
							CustomParameters: &[]string{"test_knob=value1"},
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
			Expect(settings.CustomParameters).To(Equal(&[]string{"test_knob=value1"}))
		})
	})

	When("checking if the protocol and the version are compatible", func() {
		It("should return the correct compatibility", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 20}
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 2, Patch: 20})).To(BeTrue())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 2, Patch: 22})).To(BeTrue())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 3, Patch: 0})).To(BeFalse())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 3, Patch: 20})).To(BeFalse())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 7, Minor: 2, Patch: 20})).To(BeFalse())
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
			expectedProcessGroup ProcessGroupStatus
		}

		DescribeTable("should add or ignore the addresses",
			func(tc testCase) {
				tc.initialProcessGroup.AddAddresses(tc.inputAddresses)
				Expect(tc.expectedProcessGroup).To(Equal(tc.initialProcessGroup))
			},
			Entry("Empty input address",
				testCase{
					initialProcessGroup:  ProcessGroupStatus{Addresses: []string{"1.1.1.1"}},
					inputAddresses:       []string{""},
					expectedProcessGroup: ProcessGroupStatus{Addresses: []string{"1.1.1.1"}},
				}),
			Entry("New Pod IP",
				testCase{
					initialProcessGroup:  ProcessGroupStatus{Addresses: []string{"1.1.1.1"}},
					inputAddresses:       []string{"2.2.2.2"},
					expectedProcessGroup: ProcessGroupStatus{Addresses: []string{"2.2.2.2"}},
				}),
			Entry("New Pod IP and process group is marked for removal",
				testCase{
					initialProcessGroup:  ProcessGroupStatus{Addresses: []string{"1.1.1.1"}, Remove: true},
					inputAddresses:       []string{"2.2.2.2"},
					expectedProcessGroup: ProcessGroupStatus{Addresses: []string{"1.1.1.1", "2.2.2.2"}, Remove: true},
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
							IPAddress: "1.2.3.4",
							Port:      4501,
						},
					},
				}),
			Entry("Only TLS",
				testCase{
					cmdline: "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-9 --locality_machineid=machine1 --locality_zoneid=zone1 --logdir=/var/log/fdb-trace-logs --loggroup=test --public_address=1.2.3.4:4500:tls --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					expected: []ProcessAddress{
						{
							IPAddress: "1.2.3.4",
							Port:      4500,
							Flags:     map[string]bool{"tls": true},
						},
					},
				}),
			Entry("TLS and no-TLS",
				testCase{
					cmdline: "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-9 --locality_machineid=machine1 --locality_zoneid=zone1 --logdir=/var/log/fdb-trace-logs --loggroup=test --public_address=1.2.3.4:4501,1.2.3.4:4500:tls --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					expected: []ProcessAddress{
						{
							IPAddress: "1.2.3.4",
							Port:      4501,
						},
						{
							IPAddress: "1.2.3.4",
							Port:      4500,
							Flags: map[string]bool{
								"tls": true,
							},
						},
					},
				}),
		)
	})
})
