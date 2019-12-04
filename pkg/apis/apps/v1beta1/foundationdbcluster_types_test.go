/*
Copyright 2019 FoundationDB project authors.

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
	"fmt"
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
		LogRouters: 9,
	}))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
		"logs":        3,
		"proxies":     3,
		"resolvers":   1,
		"remote_logs": 3,
		"log_routers": 9,
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
		LogRouters: 9,
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
		LogRouters: 24,
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

	counts := cluster.GetProcessCountsWithDefaults()
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       4,
		Stateless: 7,
	}))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
		"storage":   5,
		"log":       4,
		"stateless": 7,
	}))
	g.Expect(cluster.Spec.ProcessCounts).To(gomega.Equal(ProcessCounts{}))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Storage: 10,
	}
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts.Storage).To(gomega.Equal(10))

	cluster.Spec.ProcessCounts = ProcessCounts{
		ClusterController: 3,
	}
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts.Stateless).To(gomega.Equal(6))
	g.Expect(counts.ClusterController).To(gomega.Equal(3))
	g.Expect(counts.Map()).To(gomega.Equal(map[string]int{
		"storage":            5,
		"log":                4,
		"stateless":          6,
		"cluster_controller": 3,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Resolver: 1,
	}
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts.Stateless).To(gomega.Equal(6))
	g.Expect(counts.Resolver).To(gomega.Equal(1))
	g.Expect(counts.Resolution).To(gomega.Equal(0))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Resolution: 1,
	}
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts.Stateless).To(gomega.Equal(6))
	g.Expect(counts.Resolution).To(gomega.Equal(1))
	g.Expect(counts.Resolver).To(gomega.Equal(0))

	cluster.Spec.ProcessCounts = ProcessCounts{
		Log: 2,
	}
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts.Log).To(gomega.Equal(2))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.RoleCounts.RemoteLogs = 4
	cluster.Spec.RoleCounts.LogRouters = 8

	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   5,
		Log:       5,
		Stateless: 9,
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

	counts := cluster.GetProcessCountsWithDefaults()
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   2,
		Log:       2,
		Stateless: 3,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.FaultDomain.ZoneIndex = 2
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   1,
		Log:       1,
		Stateless: 3,
	}))

	cluster.Spec.ProcessCounts = ProcessCounts{}
	cluster.Spec.FaultDomain.ZoneIndex = 1
	cluster.Spec.FaultDomain.ZoneCount = 5
	counts = cluster.GetProcessCountsWithDefaults()
	g.Expect(counts).To(gomega.Equal(ProcessCounts{
		Storage:   1,
		Log:       1,
		Stateless: 2,
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

	count := cluster.DesiredCoordinatorCount()
	if count != 3 {
		t.Errorf("Incorrect coordinator count. Expected=%d, actual=%d", 3, count)
	}

	cluster.Spec.RedundancyMode = "single"
	count = cluster.DesiredCoordinatorCount()
	if count != 1 {
		t.Errorf("Incorrect coordinator count. Expected=%d, actual=%d", 1, count)
	}
}

func TestParsingClusterStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_0.json"), os.O_RDONLY, os.ModePerm)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer statusFile.Close()
	statusDecoder := json.NewDecoder(statusFile)
	status := FoundationDBStatus{}
	err = statusDecoder.Decode(&status)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(status).To(gomega.Equal(FoundationDBStatus{
		Client: FoundationDBStatusClientInfo{
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
				},
				"d532d8cb1c23d002c4b97742f5195fdb": {
					Address:      "172.17.0.7:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-3 --locality_zoneid=foundationdbcluster-sample-3 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.7:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				},
				"f7058e8bed0618a0533f6188e9e35cdb": {
					Address:      "172.17.0.9:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-2 --locality_zoneid=foundationdbcluster-sample-2 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.9:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				},
				"6a5d5735fc8a58add63cceba1da46421": {
					Address:      "172.17.0.8:4500",
					ProcessClass: "storage",
					CommandLine:  "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-1 --locality_zoneid=foundationdbcluster-sample-1 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.8:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
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
	fmt.Println(configuration.GetConfigurationString())
	g.Expect(configuration.GetConfigurationString()).To(gomega.Equal("double ssd usable_regions=1 logs=5 proxies=0 resolvers=0 log_routers=0 remote_logs=0 regions=[{\\\"datacenters\\\":[{\\\"id\\\":\\\"iad\\\",\\\"priority\\\":1}],\\\"satellite_logs\\\":2}]"))
}

func TestGettingSidecarVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := &FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: FoundationDBClusterSpec{
			Version: "6.1.8",
			DatabaseConfiguration: DatabaseConfiguration{
				RedundancyMode: "double",
			},
		},
	}

	g.Expect(cluster.GetFullSidecarVersion()).To(gomega.Equal("6.1.8-1"))

	cluster.Spec.SidecarVersion = 2
	g.Expect(cluster.GetFullSidecarVersion()).To(gomega.Equal("6.1.8-2"))
}
