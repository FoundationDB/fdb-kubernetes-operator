/*
 * foundationdb_status_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2022 Apple Inc. and the FoundationDB project authors
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

package v1beta2

import (
	"encoding/json"
	"k8s.io/utils/pointer"
	"net"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FoundationDBStatus", func() {
	When("parsing the status json with a 7.1.0-rc1 cluster", func() {
		migrationType := StorageMigrationTypeDisabled
		status := FoundationDBStatusClusterInfo{
			Messages:                []FoundationDBStatusMessage{},
			IncompatibleConnections: []string{},
			ConnectionString:        "test_cluster:aHeD9ocNXOUxi0dyzU3k7Bhg53SpyrBV@10.1.18.253:4501,10.1.18.254:4501,10.1.19.0:4501",
			DatabaseConfiguration: DatabaseConfiguration{
				RedundancyMode:                 "double",
				StorageEngine:                  StorageEngineSSD2,
				UsableRegions:                  1,
				Regions:                        nil,
				ExcludedServers:                make([]ExcludedServers, 0),
				RoleCounts:                     RoleCounts{Storage: 0, Logs: 3, Proxies: 3, CommitProxies: 2, GrvProxies: 1, Resolvers: 1, LogRouters: -1, RemoteLogs: -1},
				VersionFlags:                   VersionFlags{LogSpill: 2, LogVersion: 0},
				StorageMigrationType:           &migrationType,
				PerpetualStorageWiggle:         pointer.Int(0),
				PerpetualStorageWiggleLocality: pointer.String("0"),
			},
			Processes: map[ProcessGroupID]FoundationDBStatusProcessInfo{
				"eb48ada3a682e86363f06aa89e1041fa": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.18.254"),
						Port:      4501,
					},
					ProcessClass: ProcessClassStorage,
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.18.254:4501 --locality_instance_id=storage-1 --locality_machineid=test-cluster-storage-1 --locality_zoneid=test-cluster-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.18.254:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-1",
						"machineid":   "test-cluster-storage-1",
						"processid":   "eb48ada3a682e86363f06aa89e1041fa",
						"zoneid":      "test-cluster-storage-1",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0026,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{Role: string(ProcessRoleCoordinator)},
						{
							Role: string(ProcessRoleGrvProxy),
							ID:   "0de7f5c5e549cad1",
						},
						{
							Role: string(ProcessRoleStorage),
							ID:   "9941616400759d37",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"eab0db1aa7aae81a50ca97e9814a1b7d": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.18.253"),
						Port:      4501,
					},
					ProcessClass: ProcessClassStorage,
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.18.253:4501 --locality_instance_id=storage-3 --locality_machineid=test-cluster-storage-3 --locality_zoneid=test-cluster-storage-3 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.18.253:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-3",
						"machineid":   "test-cluster-storage-3",
						"processid":   "eab0db1aa7aae81a50ca97e9814a1b7d",
						"zoneid":      "test-cluster-storage-3",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0031,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{Role: string(ProcessRoleCoordinator)},
						{
							Role: string(ProcessClassStorage),
							ID:   "389c23d59a646e52",
						},
						{
							Role: string(ProcessRoleResolver),
							ID:   "dfd679875a386d06",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"f483247d4d5f279ef02c549680cbde64": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.19.0"),
						Port:      4501,
					},
					ProcessClass: ProcessClassStorage,
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.19.0:4501 --locality_instance_id=storage-2 --locality_machineid=test-cluster-storage-2 --locality_zoneid=test-cluster-storage-2 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.19.0:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-2",
						"machineid":   "test-cluster-storage-2",
						"processid":   "f483247d4d5f279ef02c549680cbde64",
						"zoneid":      "test-cluster-storage-2",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0029,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{Role: string(ProcessRoleCoordinator)},
						{
							Role: string(ProcessRoleCommitProxy),
							ID:   "0eb90e4a0ece85b3",
						},
						{
							Role: string(ProcessRoleStorage),
							ID:   "b5e42e100018bf11",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"f6e0f7fd80da429d20329ad95d793ca3": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.19.1"),
						Port:      4501,
					},
					ProcessClass: ProcessClassLog,
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.19.1:4501 --locality_instance_id=log-1 --locality_machineid=test-cluster-log-1 --locality_zoneid=test-cluster-log-1 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.19.1:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"machineid":   "test-cluster-log-1",
						"processid":   "f6e0f7fd80da429d20329ad95d793ca3",
						"zoneid":      "test-cluster-log-1",
						"instance_id": "log-1",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0027,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{
							Role: string(ProcessRoleMaster),
							ID:   "b05dfb13cf568dfd",
						},
						{
							Role: string(ProcessRoleDataDistributor),
							ID:   "cfdd8010b58eda01",
						},
						{
							Role: string(ProcessRoleRatekeeper),
							ID:   "cbeb915c6cceb4a9",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"f75644abdf1b06c803b5c3c124fdd0a0": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.18.255"),
						Port:      4501,
					},
					ProcessClass: ProcessClassClusterController,
					CommandLine:  "/usr/bin/fdbserver --class=cluster_controller --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.18.255:4501 --locality_instance_id=cluster_controller-1 --locality_machineid=test-cluster-cluster-controller-1 --locality_zoneid=test-cluster-cluster-controller-1 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.18.255:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"processid":   "f75644abdf1b06c803b5c3c124fdd0a0",
						"zoneid":      "test-cluster-cluster-controller-1",
						"instance_id": "cluster_controller-1",
						"machineid":   "test-cluster-cluster-controller-1",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0029,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{
							Role: string(ProcessClassClusterController),
							ID:   "1f953018ad2e746f",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"105bf6c041f8ec315d03e889c2746ecf": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.19.2"),
						Port:      4501,
					},
					ProcessClass: ProcessClassLog,
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.19.2:4501 --locality_instance_id=log-3 --locality_machineid=test-cluster-log-3 --locality_zoneid=test-cluster-log-3 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.19.2:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "log-3",
						"machineid":   "test-cluster-log-3",
						"processid":   "105bf6c041f8ec315d03e889c2746ecf",
						"zoneid":      "test-cluster-log-3",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0029,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{
							Role: string(ProcessRoleLog),
							ID:   "2c66a861b33b2697",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"78c1c84af4481f0df628d40358f0930a": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.19.4"),
						Port:      4501,
					},
					ProcessClass: ProcessClassLog,
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.19.4:4501 --locality_instance_id=log-2 --locality_machineid=test-cluster-log-2 --locality_zoneid=test-cluster-log-2 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.19.4:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"machineid":   "test-cluster-log-2",
						"processid":   "78c1c84af4481f0df628d40358f0930a",
						"zoneid":      "test-cluster-log-2",
						"instance_id": "log-2",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.003,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{
							Role: string(ProcessRoleLog),
							ID:   "56cf105980ec2b07",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
				"83084479b50c9c3a09b0286297be3796": {
					Address: ProcessAddress{
						IPAddress: net.ParseIP("10.1.19.3"),
						Port:      4501,
					},
					ProcessClass: ProcessClassLog,
					CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --listen_address=10.1.19.3:4501 --locality_instance_id=log-4 --locality_machineid=test-cluster-log-4 --locality_zoneid=test-cluster-log-4 --logdir=/var/log/fdb-trace-logs --loggroup=test-cluster --public_address=10.1.19.3:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "log-4",
						"machineid":   "test-cluster-log-4",
						"processid":   "83084479b50c9c3a09b0286297be3796",
						"zoneid":      "test-cluster-log-4",
					},
					Version:       "7.1.0-rc1",
					UptimeSeconds: 85.0027,
					Roles: []FoundationDBStatusProcessRoleInfo{
						{
							Role: string(ProcessRoleLog),
							ID:   "31754d1d7d8d6f05",
						},
					},
					Messages: []FoundationDBStatusProcessMessage{},
				},
			},
			Data: FoundationDBStatusDataStatistics{
				KVBytes:    0,
				MovingData: FoundationDBStatusMovingData{HighestPriority: 0, InFlightBytes: 0, InQueueBytes: 0},
				State:      FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy", MinReplicasRemaining: 2},
				TeamTrackers: []FoundationDBStatusTeamTracker{
					{
						Primary: true,
						State:   FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy", MinReplicasRemaining: 2},
					},
				},
			},
			FullReplication: true,
			Clients: FoundationDBStatusClusterClientInfo{
				Count: 8,
				SupportedVersions: []FoundationDBStatusSupportedVersion{
					{
						ClientVersion: "6.2.30",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.18.249:34874",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:49078",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:51834",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						MaxProtocolClients: nil,
						ProtocolVersion:    "fdb00b062010001",
						SourceVersion:      "c1acf5fc16a522b0f53b27874c88e21f5d34b251",
					},
					{
						ClientVersion: "6.3.10",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.18.249:34874",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:49078",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:51834",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						MaxProtocolClients: nil,
						ProtocolVersion:    "fdb00b063010001",
						SourceVersion:      "a461b9c93be19f846c2c41d9de455f968b53fd6d",
					},
					{
						ClientVersion: "7.1.0-rc1",
						ConnectedClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.18.249:34874",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:35022",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:35642",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:49078",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:49230",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:51834",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:51920",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:52610",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						MaxProtocolClients: []FoundationDBStatusConnectedClient{
							{
								Address:  "10.1.18.249:34874",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:35022",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:35642",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:49078",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:49230",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:51834",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:51920",
								LogGroup: "fdb-kubernetes-operator",
							},
							{
								Address:  "10.1.18.249:52610",
								LogGroup: "fdb-kubernetes-operator",
							},
						},
						ProtocolVersion: "fdb00b071010000",
						SourceVersion:   "079de5ba57f85e0abb9117e9378bb0b135a3da12",
					},
				},
			},
			Layers: FoundationDBStatusLayerInfo{
				Backup: FoundationDBStatusBackupInfo{Paused: false, Tags: nil},
				Error:  "",
			},
			Logs: []FoundationDBStatusLogInfo{
				{
					Current:                       true,
					LogFaultTolerance:             1,
					LogReplicationFactor:          2,
					RemoteLogFaultTolerance:       0,
					RemoteLogReplicationFactor:    0,
					SatelliteLogFaultTolerance:    0,
					SatelliteLogReplicationFactor: 0,
				},
			},
			Qos: FoundationDBStatusQosInfo{
				LimitingDurabilityLagStorageServer: FoundationDBStatusLagInfo{
					Seconds:  5.0145299999999997,
					Versions: 5014530,
				},
				WorstDataLagStorageServer: FoundationDBStatusLagInfo{
					Seconds:  0,
					Versions: 0,
				},
				WorstDurabilityLagStorageServer: FoundationDBStatusLagInfo{
					Seconds:  5.0150199999999998,
					Versions: 5015017,
				},
				WorstQueueBytesStorageServer: 1996,
				WorstQueueBytesLogServer:     12144,
			},
			FaultTolerance: FaultTolerance{
				MaxZoneFailuresWithoutLosingData:         1,
				MaxZoneFailuresWithoutLosingAvailability: 1,
			},
			RecoveryState: RecoveryState{
				ActiveGenerations:         1,
				Name:                      "fully_recovered",
				SecondsSinceLastRecovered: 76.8155,
			},
			Generation: 2,
			BounceImpact: FoundationDBBounceImpact{
				CanCleanBounce: pointer.Bool(true),
			},
		}

		It("should parse all values correctly", func() {
			statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_7_1_rc1.json"), os.O_RDONLY, os.ModePerm)
			Expect(err).NotTo(HaveOccurred())
			defer statusFile.Close()
			statusDecoder := json.NewDecoder(statusFile)
			statusParsed := FoundationDBStatus{}
			err = statusDecoder.Decode(&statusParsed)
			Expect(err).NotTo(HaveOccurred())
			Expect(statusParsed.Cluster).To(Equal(status))
		})
	})

	When("parsing a machine-readable status that contains the unreachable processes message", func() {
		It("should parse the cluster messages correct", func() {
			statusFile, err := os.OpenFile(filepath.Join("testdata", "unreachable_test_processes.json"), os.O_RDONLY, os.ModePerm)
			Expect(err).NotTo(HaveOccurred())
			defer statusFile.Close()
			statusDecoder := json.NewDecoder(statusFile)
			statusParsed := FoundationDBStatus{}
			Expect(statusDecoder.Decode(&statusParsed)).NotTo(HaveOccurred())
			Expect(statusParsed.Cluster.Messages).To(HaveLen(2))
			Expect(statusParsed.Cluster.Messages[0].UnreachableProcesses).To(HaveLen(1))
			Expect(statusParsed.Cluster.Messages[0].Name).To(Equal("unreachable_processes"))
			Expect(statusParsed.Cluster.Messages[0].UnreachableProcesses[0].Address).To(Equal("100.82.115.41:4500:tls"))
			Expect(statusParsed.Cluster.Messages[1].UnreachableProcesses).To(HaveLen(0))
			Expect(statusParsed.Cluster.Messages[1].Name).To(Equal("status_incomplete"))
		})
	})
})
