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
	"testing"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetDefaultRoleCounts(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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
	g.Expect(counts).To(gomega.Equal(RoleCounts{
		Storage:    3,
		Logs:       3,
		Proxies:    3,
		Resolvers:  1,
		RemoteLogs: -1,
		LogRouters: -1,
	}))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
		"logs":        3,
		"proxies":     3,
		"resolvers":   1,
		"remote_logs": -1,
		"log_routers": -1,
	}))
	g.Expect(cluster.Spec.RoleCounts).To(gomega.Equal(RoleCounts{}))

	cluster.Spec.UsableRegions = 2
	counts = cluster.GetRoleCountsWithDefaults()
	g.Expect(counts).To(gomega.Equal(RoleCounts{
		Storage:    3,
		Logs:       3,
		Proxies:    3,
		Resolvers:  1,
		RemoteLogs: 3,
		LogRouters: 3,
	}))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
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
	g.Expect(counts).To(gomega.Equal(RoleCounts{
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
	g.Expect(counts).To(gomega.Equal(RoleCounts{
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
	g.Expect(counts).To(gomega.Equal(RoleCounts{
		Storage:    3,
		Logs:       4,
		Proxies:    3,
		Resolvers:  1,
		RemoteLogs: 5,
		LogRouters: 6,
	}))
}

func TestGettingDefaultProcessCounts(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       4,
		Stateless: 9,
	}))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
		"storage":   5,
		"log":       4,
		"stateless": 9,
	}))
	g.Expect(cluster.Spec.ProcessCounts).To(gomega.Equal(ProcessCounts{}))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Storage: 10,
	}
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts.Storage).To(gomega.Equal(10))

	cluster.Spec.ProcessCounts = ProcessCounts{
		ClusterController: 3,
	}
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts.Stateless).To(gomega.Equal(8))
	g.Expect(counts.ClusterController).To(gomega.Equal(3))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
		"storage":            5,
		"log":                4,
		"stateless":          8,
		"cluster_controller": 3,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Resolver: 1,
	}
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts.Stateless).To(gomega.Equal(8))
	g.Expect(counts.Resolver).To(gomega.Equal(1))
	g.Expect(counts.Resolution).To(gomega.Equal(0))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Resolution: 1,
	}
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts.Stateless).To(gomega.Equal(8))
	g.Expect(counts.Resolution).To(gomega.Equal(1))
	g.Expect(counts.Resolver).To(gomega.Equal(0))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Log: 2,
	}
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts.Log).To(gomega.Equal(2))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.RoleCounts.RemoteLogs = 4
	cluster.Spec.RoleCounts.LogRouters = 8

	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       5,
		Stateless: 9,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.RoleCounts = RoleCounts{}
	cluster.Spec.Version = Versions.WithoutRatekeeperRole.String()

	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   3,
		Log:       4,
		Stateless: 7,
	}))
}

func TestGettingDefaultProcessCountsWithCrossClusterReplication(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   2,
		Log:       2,
		Stateless: 4,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.FaultDomain.ZoneIndex = 2
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   1,
		Log:       1,
		Stateless: 3,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.FaultDomain.ZoneIndex = 1
	cluster.Spec.FaultDomain.ZoneCount = 5
	counts, err = cluster.GetProcessCountsWithDefaults()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   1,
		Log:       1,
		Stateless: 2,
	}))
}

func TestGettingDefaultProcessCountsWithSatellites(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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
					Region{
						DataCenters: []DataCenter{
							DataCenter{ID: "dc1", Satellite: 0, Priority: 1},
							DataCenter{ID: "dc2", Satellite: 1, Priority: 1},
						},
						SatelliteLogs:           2,
						SatelliteRedundancyMode: "one_satellite_double",
					},
					Region{
						DataCenters: []DataCenter{
							DataCenter{ID: "dc3", Satellite: 0, Priority: 1},
							DataCenter{ID: "dc4", Satellite: 1, Priority: 1},
						},
						SatelliteLogs:           2,
						SatelliteRedundancyMode: "one_satellite_double",
					},
				},
			},
		},
	}

	cluster.Spec.DataCenter = "dc1"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       4,
		Stateless: 11,
	}))

	cluster.Spec.DataCenter = "dc2"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Log: 3,
	}))

	cluster.Spec.DataCenter = "dc3"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       4,
		Stateless: 11,
	}))

	cluster.Spec.DataCenter = "dc4"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Log: 3,
	}))

	cluster.Spec.DataCenter = "dc5"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       4,
		Stateless: 11,
	}))

	cluster.Spec.DatabaseConfiguration.Regions = []Region{
		Region{
			DataCenters: []DataCenter{
				DataCenter{ID: "dc1", Satellite: 0, Priority: 1},
				DataCenter{ID: "dc2", Satellite: 1, Priority: 2},
				DataCenter{ID: "dc3", Satellite: 1, Priority: 1},
			},
			SatelliteLogs:           4,
			SatelliteRedundancyMode: "one_satellite_double",
		},
		Region{
			DataCenters: []DataCenter{
				DataCenter{ID: "dc3", Satellite: 0, Priority: 1},
				DataCenter{ID: "dc2", Satellite: 1, Priority: 1},
				DataCenter{ID: "dc1", Satellite: 1, Priority: 2},
			},
			SatelliteLogs:           3,
			SatelliteRedundancyMode: "one_satellite_double",
		},
	}

	cluster.Spec.DataCenter = "dc1"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       7,
		Stateless: 11,
	}))

	cluster.Spec.DataCenter = "dc2"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Log: 5,
	}))

	cluster.Spec.DataCenter = "dc3"
	g.Expect(cluster.GetProcessCountsWithDefaults()).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       8,
		Stateless: 11,
	}))
}

func TestCheckingWhetherProcessCountsAreSatisfied(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	counts := ProcessCounts{Stateless: 5}
	g.Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 5})).To(gomega.BeTrue())
	g.Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 6})).To(gomega.BeFalse())
	g.Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 0})).To(gomega.BeFalse())
	counts = ProcessCounts{Stateless: -1}
	g.Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 0})).To(gomega.BeTrue())
	g.Expect(counts.CountsAreSatisfied(ProcessCounts{Stateless: 5})).To(gomega.BeFalse())
}

func TestSettingProcessCountByName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	counts := ProcessCounts{}
	counts.IncreaseCount("storage", 2)
	g.Expect(counts.Storage).To(gomega.Equal(2))
	g.Expect(counts.ClusterController).To(gomega.Equal(0))
	counts.IncreaseCount("storage", 3)
	g.Expect(counts.Storage).To(gomega.Equal(5))
	g.Expect(counts.ClusterController).To(gomega.Equal(0))
	counts.IncreaseCount("cluster_controller", 1)
	g.Expect(counts.Storage).To(gomega.Equal(5))
	g.Expect(counts.ClusterController).To(gomega.Equal(1))
}

func TestClusterDesiredCoordinatorCounts(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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

	g.Expect(cluster.DesiredCoordinatorCount()).To(gomega.Equal(3))

	cluster.Spec.RedundancyMode = "single"
	g.Expect(cluster.DesiredCoordinatorCount()).To(gomega.Equal(1))

	cluster.Spec.DatabaseConfiguration.UsableRegions = 2
	g.Expect(cluster.DesiredCoordinatorCount()).To(gomega.Equal(9))
}

func TestParsingClusterStatusWithSixZeroCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_0.json"), os.O_RDONLY, os.ModePerm)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer statusFile.Close()
	statusDecoder := json.NewDecoder(statusFile)
	status := FoundationDBStatus{}
	err = statusDecoder.Decode(&status)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(status).To(gomega.Equal(FoundationDBStatus{
		Client: FoundationDBStatusLocalClientInfo{
			Coordinators: FoundationDBStatusCoordinatorInfo{
				Coordinators: []FoundationDBStatusCoordinator{
					FoundationDBStatusCoordinator{Address: "172.17.0.6:4500", Reachable: false},
					FoundationDBStatusCoordinator{Address: "172.17.0.7:4500", Reachable: true},
					FoundationDBStatusCoordinator{Address: "172.17.0.9:4500", Reachable: true},
				},
			},
			DatabaseStatus: FoundationDBStatusClientDBStatus{
				Available: true,
				Healthy:   true,
			},
		},
		Cluster: FoundationDBStatusClusterInfo{
			Clients: FoundationDBStatusClusterClientInfo{
				Count: 1,
				SupportedVersions: []FoundationDBStatusSupportedVersion{
					FoundationDBStatusSupportedVersion{
						ClientVersion: "5.1.7",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							FoundationDBStatusConnectedClient{
								Address:  "172.17.0.5:38260",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						ProtocolVersion: "fdb00a551040001",
						SourceVersion:   "9ad8d02386d4a6a5efecf898df80f2747695c627",
					},
					FoundationDBStatusSupportedVersion{
						ClientVersion: "5.2.5",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							FoundationDBStatusConnectedClient{
								Address:  "172.17.0.5:38260",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						ProtocolVersion: "fdb00a552000001",
						SourceVersion:   "4e48018437df4506aa5ed0c7f5976b9412b0145f",
					},
					FoundationDBStatusSupportedVersion{
						ClientVersion: "6.0.18",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							FoundationDBStatusConnectedClient{
								Address:  "172.17.0.5:38260",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						ProtocolVersion: "fdb00a570010001",
						SourceVersion:   "48d84faa3e6174deb7f852ef4d314f7bad1dfa57",
					},
				},
			},
			DatabaseConfiguration: DatabaseConfiguration{
				RedundancyMode: "double",
				StorageEngine:  "memory",
				UsableRegions:  1,
				RoleCounts: RoleCounts{
					Logs:    3,
					Proxies: 3,
				},
			},
			Processes: map[string]FoundationDBStatusProcessInfo{
				"c9eb35e25a364910fd77fdeec5c3a1f6": {
					Address:      "172.17.0.6:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-4 --locality_zoneid=foundationdbcluster-sample-4 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.6:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     true,
					Locality: map[string]string{
						"machineid": "foundationdbcluster-sample-4",
						"processid": "c9eb35e25a364910fd77fdeec5c3a1f6",
						"zoneid":    "foundationdbcluster-sample-4",
					},
					Version: "6.0.18",
				},
				"d532d8cb1c23d002c4b97742f5195fdb": {
					Address:      "172.17.0.7:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-3 --locality_zoneid=foundationdbcluster-sample-3 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.7:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Locality: map[string]string{
						"machineid": "foundationdbcluster-sample-3",
						"processid": "d532d8cb1c23d002c4b97742f5195fdb",
						"zoneid":    "foundationdbcluster-sample-3",
					},
					Version: "6.0.18",
				},
				"f7058e8bed0618a0533f6188e9e35cdb": {
					Address:      "172.17.0.9:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-2 --locality_zoneid=foundationdbcluster-sample-2 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.9:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Locality: map[string]string{
						"machineid": "foundationdbcluster-sample-2",
						"processid": "f7058e8bed0618a0533f6188e9e35cdb",
						"zoneid":    "foundationdbcluster-sample-2",
					},
					Version: "6.0.18",
				},
				"6a5d5735fc8a58add63cceba1da46421": {
					Address:      "172.17.0.8:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-1 --locality_zoneid=foundationdbcluster-sample-1 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.8:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Locality: map[string]string{
						"machineid": "foundationdbcluster-sample-1",
						"processid": "6a5d5735fc8a58add63cceba1da46421",
						"zoneid":    "foundationdbcluster-sample-1",
					},
					Version: "6.0.18",
				},
			},
			Data: FoundationDBStatusDataStatistics{
				MovingData: FoundationDBStatusMovingData{
					HighestPriority: 1,
					InFlightBytes:   100,
					InQueueBytes:    500,
				},
				KVBytes: 215250,
			},
			FullReplication: true,
			Layers: FoundationDBStatusLayerInfo{
				Backup: FoundationDBStatusBackupInfo{
					Tags: map[string]FoundationDBStatusBackupTag{
						"default": FoundationDBStatusBackupTag{
							CurrentContainer: "blobstore://minio@minio-service:9000/sample-cluster-test-backup?bucket=fdb-backups",
							RunningBackup:    true,
							Restorable:       false,
						},
					},
				},
			},
		},
	}))
}

func TestParsingClusterStatusWithSixOneCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_1.json"), os.O_RDONLY, os.ModePerm)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer statusFile.Close()
	statusDecoder := json.NewDecoder(statusFile)
	status := FoundationDBStatus{}
	err = statusDecoder.Decode(&status)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(status).To(gomega.Equal(FoundationDBStatus{
		Client: FoundationDBStatusLocalClientInfo{
			Coordinators: FoundationDBStatusCoordinatorInfo{
				Coordinators: []FoundationDBStatusCoordinator{
					{
						Address:   "10.1.38.82:4501",
						Reachable: true,
					},
					{
						Address:   "10.1.38.86:4501",
						Reachable: true,
					},
					{
						Address:   "10.1.38.91:4501",
						Reachable: true,
					},
				},
			},
			DatabaseStatus: FoundationDBStatusClientDBStatus{Available: true, Healthy: true},
		},
		Cluster: FoundationDBStatusClusterInfo{
			DatabaseConfiguration: DatabaseConfiguration{
				RedundancyMode: "double",
				StorageEngine:  "ssd-2",
				UsableRegions:  1,
				Regions:        nil,
				RoleCounts:     RoleCounts{Storage: 0, Logs: 3, Proxies: 3, Resolvers: 1, LogRouters: 0, RemoteLogs: 0},
				VersionFlags:   VersionFlags{LogSpill: 1},
			},
			Processes: map[string]FoundationDBStatusProcessInfo{
				"c813e585043a7ab55a4905f465c4aa52": {
					Address:      "10.1.38.91:4501",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-3 --locality_machineid=sample-cluster-storage-3 --locality_zoneid=sample-cluster-storage-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.91:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-3",
						"machineid":   "sample-cluster-storage-3",
						"processid":   "c813e585043a7ab55a4905f465c4aa52",
						"zoneid":      "sample-cluster-storage-3",
					},
					Version: "6.1.12",
				},
				"f9efa90fc104f4e277b140baf89aab66": {
					Address:      "10.1.38.82:4501",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-1 --locality_machineid=sample-cluster-storage-1 --locality_zoneid=sample-cluster-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.82:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-1",
						"machineid":   "sample-cluster-storage-1",
						"processid":   "f9efa90fc104f4e277b140baf89aab66",
						"zoneid":      "sample-cluster-storage-1",
					},
					Version: "6.1.12",
				},
				"5a633d7f4e98a6c938c84b97ec4aedbf": {
					Address:      "10.1.38.89:4501",
					ProcessClass: "log",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-2 --locality_machineid=sample-cluster-log-2 --locality_zoneid=sample-cluster-log-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.89:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "log-2",
						"machineid":   "sample-cluster-log-2",
						"processid":   "5a633d7f4e98a6c938c84b97ec4aedbf",
						"zoneid":      "sample-cluster-log-2",
					},
					Version: "6.1.12",
				},
				"5c1b68147a0ef34ce005a38245851270": {
					Address:      "10.1.38.88:4501",
					ProcessClass: "log",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-4 --locality_machineid=sample-cluster-log-4 --locality_zoneid=sample-cluster-log-4 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.88:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"machineid":   "sample-cluster-log-4",
						"processid":   "5c1b68147a0ef34ce005a38245851270",
						"zoneid":      "sample-cluster-log-4",
						"instance_id": "log-4",
					},
					Version: "6.1.12",
				},
				"653defde43cf1fdef131e2fb82bd192d": {
					Address:      "10.1.38.87:4501",
					ProcessClass: "log",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-1 --locality_machineid=sample-cluster-log-1 --locality_zoneid=sample-cluster-log-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.87:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "log-1",
						"machineid":   "sample-cluster-log-1",
						"processid":   "653defde43cf1fdef131e2fb82bd192d",
						"zoneid":      "sample-cluster-log-1",
					},
					Version: "6.1.12",
				},
				"9c93d3b70118f16c72f7cb3f53e49f4c": {
					Address:      "10.1.38.86:4501",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-2 --locality_machineid=sample-cluster-storage-2 --locality_zoneid=sample-cluster-storage-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.86:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"processid":   "9c93d3b70118f16c72f7cb3f53e49f4c",
						"zoneid":      "sample-cluster-storage-2",
						"instance_id": "storage-2",
						"machineid":   "sample-cluster-storage-2",
					},
					Version: "6.1.12",
				},
				"b9c25278c0fa207bc2a73bda2300d0a9": {
					Address:      "10.1.38.90:4501",
					ProcessClass: "log",
					CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-3 --locality_machineid=sample-cluster-log-3 --locality_zoneid=sample-cluster-log-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.90:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"processid":   "b9c25278c0fa207bc2a73bda2300d0a9",
						"zoneid":      "sample-cluster-log-3",
						"instance_id": "log-3",
						"machineid":   "sample-cluster-log-3",
					},
					Version: "6.1.12",
				},
			},
			Data: FoundationDBStatusDataStatistics{
				KVBytes:    0,
				MovingData: FoundationDBStatusMovingData{HighestPriority: 0, InFlightBytes: 0, InQueueBytes: 0},
			},
			FullReplication: true,
			Clients: FoundationDBStatusClusterClientInfo{
				Count: 6,
				SupportedVersions: []FoundationDBStatusSupportedVersion{
					{
						ClientVersion: "6.1.8",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.38.83:47846",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.83:47958",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.84:43094",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.84:43180",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.85:53594",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.85:53626",
								LogGroup: "sample-cluster-client",
							},
						},
						ProtocolVersion: "fdb00b061060001",
						SourceVersion:   "bd6b10cbcee08910667194e6388733acd3b80549",
					},
					{
						ClientVersion: "6.2.15",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.38.83:47846",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.83:47958",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.84:43094",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.84:43180",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.85:53594",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.85:53626",
								LogGroup: "sample-cluster-client",
							},
						},
						ProtocolVersion: "fdb00b062010001",
						SourceVersion:   "20566f2ff06a7e822b30e8cfd91090fbd863a393",
					},
				},
			},
			Layers: FoundationDBStatusLayerInfo{
				Backup: FoundationDBStatusBackupInfo{
					Tags: map[string]FoundationDBStatusBackupTag{
						"default": FoundationDBStatusBackupTag{
							CurrentContainer: "blobstore://minio@minio-service:9000/sample-cluster-test-backup?bucket=fdb-backups",
							RunningBackup:    true,
							Restorable:       false,
						},
					},
				},
			},
		},
	}))
}

func TestParsingClusterStatusWithSixTwoCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_2.json"), os.O_RDONLY, os.ModePerm)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer statusFile.Close()
	statusDecoder := json.NewDecoder(statusFile)
	status := FoundationDBStatus{}
	err = statusDecoder.Decode(&status)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(status).To(gomega.Equal(FoundationDBStatus{
		Client: FoundationDBStatusLocalClientInfo{
			Coordinators: FoundationDBStatusCoordinatorInfo{
				Coordinators: []FoundationDBStatusCoordinator{
					{
						Address:   "10.1.38.94:4501",
						Reachable: true,
					},
					{
						Address:   "10.1.38.102:4501",
						Reachable: true,
					},
					{
						Address:   "10.1.38.104:4501",
						Reachable: true,
					},
				},
			},
			DatabaseStatus: FoundationDBStatusClientDBStatus{Available: true, Healthy: true},
		},
		Cluster: FoundationDBStatusClusterInfo{
			DatabaseConfiguration: DatabaseConfiguration{
				RedundancyMode: "double",
				StorageEngine:  "ssd-2",
				UsableRegions:  1,
				Regions:        nil,
				RoleCounts:     RoleCounts{Storage: 0, Logs: 3, Proxies: 3, Resolvers: 1, LogRouters: 0, RemoteLogs: 0},
				VersionFlags:   VersionFlags{LogSpill: 2},
			},
			Processes: map[string]FoundationDBStatusProcessInfo{
				"b9c25278c0fa207bc2a73bda2300d0a9": {
					Address:      "10.1.38.93:4501",
					ProcessClass: "log",
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-3 --locality_machineid=sample-cluster-log-3 --locality_zoneid=sample-cluster-log-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.93:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"processid":   "b9c25278c0fa207bc2a73bda2300d0a9",
						"zoneid":      "sample-cluster-log-3",
						"instance_id": "log-3",
						"machineid":   "sample-cluster-log-3",
					},
					Version: "6.2.15",
				},
				"c813e585043a7ab55a4905f465c4aa52": {
					Address:      "10.1.38.95:4501",
					ProcessClass: "storage",
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-3 --locality_machineid=sample-cluster-storage-3 --locality_zoneid=sample-cluster-storage-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.95:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-3",
						"machineid":   "sample-cluster-storage-3",
						"processid":   "c813e585043a7ab55a4905f465c4aa52",
						"zoneid":      "sample-cluster-storage-3",
					},
					Version: "6.2.15",
				},
				"f9efa90fc104f4e277b140baf89aab66": {
					Address:      "10.1.38.92:4501",
					ProcessClass: "storage",
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-1 --locality_machineid=sample-cluster-storage-1 --locality_zoneid=sample-cluster-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.92:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-1",
						"machineid":   "sample-cluster-storage-1",
						"processid":   "f9efa90fc104f4e277b140baf89aab66",
						"zoneid":      "sample-cluster-storage-1",
					},
					Version: "6.2.15",
				},
				"5a633d7f4e98a6c938c84b97ec4aedbf": {
					Address:      "10.1.38.105:4501",
					ProcessClass: "log",
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-2 --locality_machineid=sample-cluster-log-2 --locality_zoneid=sample-cluster-log-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.105:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "log-2",
						"machineid":   "sample-cluster-log-2",
						"processid":   "5a633d7f4e98a6c938c84b97ec4aedbf",
						"zoneid":      "sample-cluster-log-2",
					},
					Version: "6.2.15",
				},
				"5c1b68147a0ef34ce005a38245851270": {
					Address:      "10.1.38.102:4501",
					ProcessClass: "log",
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-4 --locality_machineid=sample-cluster-log-4 --locality_zoneid=sample-cluster-log-4 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.102:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"zoneid":      "sample-cluster-log-4",
						"instance_id": "log-4",
						"machineid":   "sample-cluster-log-4",
						"processid":   "5c1b68147a0ef34ce005a38245851270",
					},
					Version: "6.2.15",
				},
				"653defde43cf1fdef131e2fb82bd192d": {
					Address:      "10.1.38.104:4501",
					ProcessClass: "log",
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-1 --locality_machineid=sample-cluster-log-1 --locality_zoneid=sample-cluster-log-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.104:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "log-1",
						"machineid":   "sample-cluster-log-1",
						"processid":   "653defde43cf1fdef131e2fb82bd192d",
						"zoneid":      "sample-cluster-log-1",
					},
					Version: "6.2.15",
				},
				"9c93d3b70118f16c72f7cb3f53e49f4c": {
					Address:      "10.1.38.94:4501",
					ProcessClass: "storage",
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-2 --locality_machineid=sample-cluster-storage-2 --locality_zoneid=sample-cluster-storage-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.94:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-2",
						"machineid":   "sample-cluster-storage-2",
						"processid":   "9c93d3b70118f16c72f7cb3f53e49f4c",
						"zoneid":      "sample-cluster-storage-2",
					},
					Version: "6.2.15",
				},
			},
			Data: FoundationDBStatusDataStatistics{
				KVBytes:    0,
				MovingData: FoundationDBStatusMovingData{HighestPriority: 0, InFlightBytes: 0, InQueueBytes: 0},
			},
			FullReplication: true,
			Clients: FoundationDBStatusClusterClientInfo{
				Count: 8,
				SupportedVersions: []FoundationDBStatusSupportedVersion{
					{
						ClientVersion: "Unknown",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.38.92:52762",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.92:56406",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:43346",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:43354",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:51458",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:51472",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:59442",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:59942",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:60222",
								LogGroup: "default",
							},
							{
								Address:  "10.1.38.103:60230",
								LogGroup: "default",
							},
						},
						MaxProtocolClients: nil,
						ProtocolVersion:    "Unknown",
						SourceVersion:      "Unknown",
					},
					{
						ClientVersion: "6.1.8",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.38.106:35640",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.106:36128",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.106:36802",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.107:42234",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.107:49684",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:47320",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:47388",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:58734",
								LogGroup: "sample-cluster-client",
							},
						},
						MaxProtocolClients: nil,
						ProtocolVersion:    "fdb00b061060001",
						SourceVersion:      "bd6b10cbcee08910667194e6388733acd3b80549",
					},
					{
						ClientVersion: "6.2.15",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.38.106:35640",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.106:36128",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.106:36802",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.107:42234",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.107:49684",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:47320",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:47388",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:58734",
								LogGroup: "sample-cluster-client",
							},
						},
						MaxProtocolClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.38.106:35640",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.106:36128",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.106:36802",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.107:42234",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.107:49684",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:47320",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:47388",
								LogGroup: "sample-cluster-client",
							},
							{
								Address:  "10.1.38.108:58734",
								LogGroup: "sample-cluster-client",
							},
						},
						ProtocolVersion: "fdb00b062010001",
						SourceVersion:   "20566f2ff06a7e822b30e8cfd91090fbd863a393",
					},
				},
			},
			Layers: FoundationDBStatusLayerInfo{
				Backup: FoundationDBStatusBackupInfo{
					Tags: map[string]FoundationDBStatusBackupTag{
						"default": FoundationDBStatusBackupTag{
							CurrentContainer: "blobstore://minio@minio-service:9000/sample-cluster-test-backup?bucket=fdb-backups",
							RunningBackup:    true,
							Restorable:       false,
						},
					},
				},
			},
		},
	}))
}

func TestParsingConnectionString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	str, err := ParseConnectionString("test:abcd@127.0.0.1:4500,127.0.0.2:4500,127.0.0.3:4500")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(str.DatabaseName).To(gomega.Equal("test"))
	g.Expect(str.GenerationID).To(gomega.Equal("abcd"))
	g.Expect(str.Coordinators).To(gomega.Equal([]string{
		"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500",
	}))

	str, err = ParseConnectionString("test:abcd")
	g.Expect(err.Error()).To(gomega.Equal("Invalid connection string test:abcd"))
}

func TestFormattingConnectionString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	str := ConnectionString{
		DatabaseName: "test",
		GenerationID: "abcd",
		Coordinators: []string{
			"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500",
		},
	}
	g.Expect(str.String()).To(gomega.Equal("test:abcd@127.0.0.1:4500,127.0.0.2:4500,127.0.0.3:4500"))
}

func TestGeneratingConnectionIDForConnectionString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	str := ConnectionString{
		DatabaseName: "test",
		GenerationID: "abcd",
		Coordinators: []string{
			"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500",
		},
	}
	err := str.GenerateNewGenerationID()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(str.GenerationID)).To(gomega.Equal(32))
}

func TestCheckingCoordinatorsForConnectionString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	str := ConnectionString{
		DatabaseName: "test",
		GenerationID: "abcd",
		Coordinators: []string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500"},
	}
	g.Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500"})).To(gomega.BeTrue())
	g.Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.3:4500", "127.0.0.2:4500"})).To(gomega.BeTrue())
	g.Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.3:4500", "127.0.0.4:4500"})).To(gomega.BeFalse())
	g.Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500", "127.0.0.4:4500"})).To(gomega.BeFalse())
	g.Expect(str.HasCoordinators([]string{"127.0.0.1:4500", "127.0.0.2:4500"})).To(gomega.BeFalse())
}

func TestGettingClusterDatabaseConfiguration(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

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

	g.Expect(cluster.DesiredDatabaseConfiguration()).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		StorageEngine:  "ssd-2",
		RoleCounts: RoleCounts{
			Logs:       4,
			Proxies:    5,
			Resolvers:  1,
			LogRouters: -1,
			RemoteLogs: -1,
		},
	}))
}

func TestGettingConfigurationString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configuration := DatabaseConfiguration{
		RedundancyMode: "double",
		StorageEngine:  "ssd",
		UsableRegions:  1,
		RoleCounts: RoleCounts{
			Logs: 5,
		},
	}
	g.Expect(configuration.GetConfigurationString()).To(gomega.Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[]"))

	configuration.Regions = []Region{Region{
		DataCenters: []DataCenter{DataCenter{
			ID:        "iad",
			Priority:  1,
			Satellite: 0,
		}},
		SatelliteLogs: 2,
	}}
	g.Expect(configuration.GetConfigurationString()).To(gomega.Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"iad\\\",\\\"priority\\\":1}],\\\"satellite_logs\\\":2}]"))
	configuration.Regions = nil

	configuration.VersionFlags.LogSpill = 3
	g.Expect(configuration.GetConfigurationString()).To(gomega.Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 log_spill:=3 regions=[]"))
}

func TestGettingSidecarVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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

	g.Expect(cluster.GetFullSidecarVersion(false)).To(gomega.Equal("6.2.15-1"))

	cluster.Spec.SidecarVersion = 2
	g.Expect(cluster.GetFullSidecarVersion(false)).To(gomega.Equal("6.2.15-2"))
}

func TestParsingFdbVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	version, err := ParseFdbVersion("6.2.11")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(version).To(gomega.Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

	version, err = ParseFdbVersion("6.2")
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.Equal("Could not parse FDB version from 6.2"))
}

func TestFormattingFdbVersionAsString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	version := FdbVersion{Major: 6, Minor: 2, Patch: 11}
	g.Expect(version.String()).To(gomega.Equal("6.2.11"))
}

func TestFeatureFlagsForFdbVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	version := FdbVersion{Major: 6, Minor: 2, Patch: 0}
	g.Expect(version.HasInstanceIDInSidecarSubstitutions()).To(gomega.BeFalse())
	g.Expect(version.PrefersCommandLineArgumentsInSidecar()).To(gomega.BeFalse())

	version = FdbVersion{Major: 7, Minor: 0, Patch: 0}
	g.Expect(version.HasInstanceIDInSidecarSubstitutions()).To(gomega.BeTrue())
	g.Expect(version.PrefersCommandLineArgumentsInSidecar()).To(gomega.BeTrue())
}

func TestGetNextConfigurationChangeWithSimpleChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
	}
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "triple",
	}
	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "triple",
	}))
}

func TestGetNextConfigurationChangeWhenEnablingFearlessDR(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
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
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
					DataCenter{
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
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
					DataCenter{
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
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenEnablingFearlessDRWithNoInitialRegions(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
	}

	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
					DataCenter{
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
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
					DataCenter{
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
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenEnablingSingleRegionWithNoInitialRegions(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
	}

	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
		},
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenDisablingFearlessDR(t *testing.T) {
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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

	g := gomega.NewGomegaWithT(t)
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
		},
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenDisablingFearlessDRAndSwitching(t *testing.T) {
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2",
						Priority:  1,
						Satellite: 1,
					},
				},
				SatelliteLogs:           3,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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

	g := gomega.NewGomegaWithT(t)
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenDisablingAndClearingRegions(t *testing.T) {
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 0,
					},
					DataCenter{
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

	g := gomega.NewGomegaWithT(t)
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenChangingPrimaryDataCenterWithSingleRegion(t *testing.T) {
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
		},
	}

	g := gomega.NewGomegaWithT(t)
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenChangingPrimaryDataCenterWithMultipleRegions(t *testing.T) {
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 0,
					},
				},
			},
		},
	}

	g := gomega.NewGomegaWithT(t)
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 0,
					},
				},
			},
		},
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 0,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 0,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestGetNextConfigurationChangeWhenChangingMultipleDataCenters(t *testing.T) {
	currentConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 0,
					},
				},
			},
		},
	}

	g := gomega.NewGomegaWithT(t)
	finalConfig := DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc4",
						Priority: 0,
					},
				},
			},
		},
	}

	nextConfig := currentConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: -1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  1,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc4",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc4",
						Priority: -1,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).NotTo(gomega.Equal(finalConfig))

	nextConfig = nextConfig.GetNextConfigurationChange(finalConfig)
	g.Expect(nextConfig).To(gomega.Equal(DatabaseConfiguration{
		RedundancyMode: "double",
		UsableRegions:  2,
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc3",
						Priority: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc4",
						Priority: 0,
					},
				},
			},
		},
	}))
	g.Expect(nextConfig).To(gomega.Equal(finalConfig))
}

func TestNormalizeConfigurationWithMissingLogRouters(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	spec := DatabaseConfiguration{}
	spec.RemoteLogs = 9
	normalized := spec.NormalizeConfiguration()
	g.Expect(normalized.LogRouters).To(gomega.Equal(-1))
	g.Expect(normalized.RemoteLogs).To(gomega.Equal(9))
}

func TestNormalizeConfigurationWithIncorrectDataCenterOrder(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	spec := DatabaseConfiguration{
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc1a",
						Priority:  1,
						Satellite: 1,
					},
					DataCenter{
						ID:        "dc1b",
						Priority:  2,
						Satellite: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:        "dc2a",
						Priority:  2,
						Satellite: 1,
					},
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2b",
						Priority:  0,
						Satellite: 1,
					},
				},
			},
		},
	}
	normalized := spec.NormalizeConfiguration()
	g.Expect(normalized.Regions).To(gomega.Equal([]Region{
		Region{
			DataCenters: []DataCenter{
				DataCenter{
					ID:       "dc1",
					Priority: 1,
				},
				DataCenter{
					ID:        "dc1b",
					Priority:  2,
					Satellite: 1,
				},
				DataCenter{
					ID:        "dc1a",
					Priority:  1,
					Satellite: 1,
				},
			},
		},
		Region{
			DataCenters: []DataCenter{
				DataCenter{
					ID:       "dc2",
					Priority: 1,
				},
				DataCenter{
					ID:        "dc2a",
					Priority:  2,
					Satellite: 1,
				},
				DataCenter{
					ID:        "dc2b",
					Priority:  0,
					Satellite: 1,
				},
			},
		},
	}))
}

func TestNormalizeConfigurationWithIncorrectRegionOrder(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	spec := DatabaseConfiguration{
		Regions: []Region{
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc1",
						Priority: 0,
					},
					DataCenter{
						ID:        "dc1a",
						Priority:  2,
						Satellite: 1,
					},
					DataCenter{
						ID:        "dc1b",
						Priority:  1,
						Satellite: 1,
					},
				},
			},
			Region{
				DataCenters: []DataCenter{
					DataCenter{
						ID:       "dc2",
						Priority: 1,
					},
					DataCenter{
						ID:        "dc2a",
						Priority:  1,
						Satellite: 1,
					},
					DataCenter{
						ID:        "dc2b",
						Priority:  0,
						Satellite: 1,
					},
				},
			},
		},
	}
	normalized := spec.NormalizeConfiguration()
	g.Expect(normalized.Regions).To(gomega.Equal([]Region{
		Region{
			DataCenters: []DataCenter{
				DataCenter{
					ID:       "dc2",
					Priority: 1,
				},
				DataCenter{
					ID:        "dc2a",
					Priority:  1,
					Satellite: 1,
				},
				DataCenter{
					ID:        "dc2b",
					Priority:  0,
					Satellite: 1,
				},
			},
		},
		Region{
			DataCenters: []DataCenter{
				DataCenter{
					ID:       "dc1",
					Priority: 0,
				},
				DataCenter{
					ID:        "dc1a",
					Priority:  2,
					Satellite: 1,
				},
				DataCenter{
					ID:        "dc1b",
					Priority:  1,
					Satellite: 1,
				},
			},
		},
	}))
}

func TestParseProcessAddress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	address, err := ParseProcessAddress("127.0.0.1:4500:tls")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(address).To(gomega.Equal(ProcessAddress{
		IPAddress: "127.0.0.1",
		Port:      4500,
		Flags:     map[string]bool{"tls": true},
	}))
	g.Expect(address.String()).To(gomega.Equal("127.0.0.1:4500:tls"))

	address, err = ParseProcessAddress("127.0.0.1:4501")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(address).To(gomega.Equal(ProcessAddress{
		IPAddress: "127.0.0.1",
		Port:      4501,
	}))
	g.Expect(address.String()).To(gomega.Equal("127.0.0.1:4501"))

	address, err = ParseProcessAddress("127.0.0.1:bad")
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.Equal("strconv.Atoi: parsing \"bad\": invalid syntax"))
}

func TestInstanceIsBeingRemoved(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := &FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sample-cluster",
		},
	}
	g.Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(gomega.BeFalse())

	cluster.Spec.PendingRemovals = map[string]string{
		"sample-cluster-storage-1": "127.0.0.1",
	}
	g.Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(gomega.BeTrue())
	g.Expect(cluster.InstanceIsBeingRemoved("log-1")).To(gomega.BeFalse())
	cluster.Spec.PendingRemovals = nil

	cluster.Spec.InstancesToRemove = []string{"log-1"}
	g.Expect(cluster.InstanceIsBeingRemoved("storage-1")).To(gomega.BeFalse())
	g.Expect(cluster.InstanceIsBeingRemoved("log-1")).To(gomega.BeTrue())
}

func TestCheckingReconciliationForCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	createCluster := func() *FoundationDBCluster {
		return &FoundationDBCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sample-cluster",
				Namespace:  "default",
				Generation: 2,
			},
			Spec: FoundationDBClusterSpec{
				Configured: true,
				Version:    Versions.Default.String(),
				DatabaseConfiguration: DatabaseConfiguration{
					RedundancyMode: "double",
				},
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
			},
		}
	}

	cluster := createCluster()

	result, err := cluster.CheckReconciliation()
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled: 2,
	}))

	cluster = createCluster()
	cluster.Spec.Configured = false
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:               1,
		NeedsConfigurationChange: 2,
	}))

	cluster = createCluster()
	cluster.Spec.PendingRemovals = map[string]string{
		"sample-cluster-storage-1": "17.1.1.1",
	}
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:  1,
		NeedsShrink: 2,
	}))

	cluster = createCluster()
	cluster.Status.ProcessCounts.Storage = 4
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:  1,
		NeedsShrink: 2,
	}))

	cluster = createCluster()
	cluster.Status.ProcessCounts.Log = 3
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled: 1,
		NeedsGrow:  2,
	}))

	cluster = createCluster()
	cluster.Status.IncorrectProcesses = map[string]int64{
		"sample-cluster-storage-1": 123,
	}
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:             1,
		NeedsMonitorConfUpdate: 2,
	}))

	cluster = createCluster()
	cluster.Status.IncorrectPods = []string{
		"sample-cluster-storage-1",
	}
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:       1,
		NeedsPodDeletion: 2,
	}))

	cluster = createCluster()
	cluster.Status.Health.Available = false
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:          1,
		DatabaseUnavailable: 2,
	}))

	cluster = createCluster()
	cluster.Spec.DatabaseConfiguration.StorageEngine = "ssd-1"
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:               1,
		NeedsConfigurationChange: 2,
	}))

	cluster = createCluster()
	cluster.Status.HasIncorrectConfigMap = true
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:             1,
		NeedsMonitorConfUpdate: 2,
	}))

	cluster = createCluster()
	cluster.Status.RequiredAddresses.TLS = true
	result, err = cluster.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(cluster.Status.Generations).To(gomega.Equal(ClusterGenerationStatus{
		Reconciled:        1,
		HasExtraListeners: 2,
	}))
}

func TestCheckingReconciliationForBackup(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var agentCount = 3

	createBackup := func() *FoundationDBBackup {
		return &FoundationDBBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sample-cluster",
				Namespace:  "default",
				Generation: 2,
			},
			Spec: FoundationDBBackupSpec{
				AgentCount: &agentCount,
			},
			Status: FoundationDBBackupStatus{
				Generations: BackupGenerationStatus{
					Reconciled: 1,
				},
				AgentCount:           3,
				DeploymentConfigured: true,
				BackupDetails: &FoundationDBBackupStatusBackupDetails{
					URL:     "blobstore://test@test-service/sample-cluster?bucket=fdb-backups",
					Running: true,
				},
			},
		}
	}

	backup := createBackup()

	result, err := backup.CheckReconciliation()
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Status.AgentCount = 5
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupAgentUpdate: 2,
	}))

	backup = createBackup()
	agentCount = 0
	backup.Status.AgentCount = 0
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	agentCount = 3
	backup.Status.BackupDetails = nil
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:       1,
		NeedsBackupStart: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Stopped"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:      1,
		NeedsBackupStop: 2,
	}))

	backup = createBackup()
	backup.Status.BackupDetails = nil
	backup.Spec.BackupState = "Stopped"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Status.BackupDetails.Running = false
	backup.Spec.BackupState = "Stopped"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Paused"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupPauseToggle: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Paused"
	backup.Status.BackupDetails = nil
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupStart:       2,
		NeedsBackupPauseToggle: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Paused"
	backup.Status.BackupDetails.Paused = true
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Status.BackupDetails.Paused = true
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupPauseToggle: 2,
	}))
}

func TestCheckingBackupStates(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{},
	}

	g.Expect(backup.ShouldRun()).To(gomega.BeTrue())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

	backup.Spec.BackupState = "Running"
	g.Expect(backup.ShouldRun()).To(gomega.BeTrue())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

	backup.Spec.BackupState = "Stopped"
	g.Expect(backup.ShouldRun()).To(gomega.BeFalse())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

	backup.Spec.BackupState = "Paused"
	g.Expect(backup.ShouldRun()).To(gomega.BeTrue())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeTrue())
}

func TestGettingBucketName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{},
	}

	g.Expect(backup.Bucket()).To(gomega.Equal("fdb-backups"))
	backup.Spec.Bucket = "fdb-backup-v2"
	g.Expect(backup.Bucket()).To(gomega.Equal("fdb-backup-v2"))
}

func TestGettingBackupName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{},
	}

	g.Expect(backup.BackupName()).To(gomega.Equal("sample-cluster"))
	backup.Spec.BackupName = "sample_cluster_2020_03_22"
	g.Expect(backup.BackupName()).To(gomega.Equal("sample_cluster_2020_03_22"))
}

func TestBuildingBackupURL(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{
			AccountName: "test@test-service",
		},
	}

	g.Expect(backup.BackupURL()).To(gomega.Equal("blobstore://test@test-service/sample-cluster?bucket=fdb-backups"))
}

func TestGettingSnapshotTime(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{
			AccountName: "test@test-service",
		},
	}

	g.Expect(backup.SnapshotPeriodSeconds()).To(gomega.Equal(864000))

	period := 60
	backup.Spec.SnapshotPeriodSeconds = &period
	g.Expect(backup.SnapshotPeriodSeconds()).To(gomega.Equal(60))
}
