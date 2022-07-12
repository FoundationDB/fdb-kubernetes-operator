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
	"net"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FoundationDBStatus", func() {
	When("parsing the status json with a 6.2 cluster", func() {
		It("should parse all values correctly", func() {
			statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_2.json"), os.O_RDONLY, os.ModePerm)
			Expect(err).NotTo(HaveOccurred())
			defer statusFile.Close()
			statusDecoder := json.NewDecoder(statusFile)
			status := FoundationDBStatus{}
			err = statusDecoder.Decode(&status)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(Equal(FoundationDBStatus{
				Client: FoundationDBStatusLocalClientInfo{
					Coordinators: FoundationDBStatusCoordinatorInfo{
						Coordinators: []FoundationDBStatusCoordinator{
							{
								Address: ProcessAddress{
									IPAddress: net.ParseIP("10.1.38.94"),
									Port:      4501,
								},
								Reachable: true,
							},
							{
								Address: ProcessAddress{
									IPAddress: net.ParseIP("10.1.38.102"),
									Port:      4501,
								},
								Reachable: true,
							},
							{
								Address: ProcessAddress{
									IPAddress: net.ParseIP("10.1.38.104"),
									Port:      4501,
								},
								Reachable: true,
							},
						},
					},
					DatabaseStatus: FoundationDBStatusClientDBStatus{Available: true, Healthy: true},
				},
				Cluster: FoundationDBStatusClusterInfo{
					IncompatibleConnections: []string{},
					FaultTolerance: FaultTolerance{
						MaxZoneFailuresWithoutLosingAvailability: 1,
						MaxZoneFailuresWithoutLosingData:         1,
					},
					DatabaseConfiguration: DatabaseConfiguration{
						RedundancyMode:  RedundancyModeDouble,
						StorageEngine:   StorageEngineSSD2,
						UsableRegions:   1,
						Regions:         nil,
						ExcludedServers: make([]ExcludedServers, 0),
						RoleCounts:      RoleCounts{Storage: 0, Logs: 3, Proxies: 3, Resolvers: 1, LogRouters: 0, RemoteLogs: 0},
						VersionFlags:    VersionFlags{LogSpill: 2},
					},
					Processes: map[string]FoundationDBStatusProcessInfo{
						"b9c25278c0fa207bc2a73bda2300d0a9": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.93"),
								Port:      4501,
							},
							ProcessClass: ProcessClassLog,
							CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-3 --locality_machineid=sample-cluster-log-3 --locality_zoneid=sample-cluster-log-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.93:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"processid":   "b9c25278c0fa207bc2a73bda2300d0a9",
								"zoneid":      "sample-cluster-log-3",
								"instance_id": "log-3",
								"machineid":   "sample-cluster-log-3",
							},
							Version:       "6.2.15",
							UptimeSeconds: 2955.58,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "log",
								},
							},
						},
						"c813e585043a7ab55a4905f465c4aa52": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.95"),
								Port:      4501,
							},
							ProcessClass: ProcessClassStorage,
							CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-3 --locality_machineid=sample-cluster-storage-3 --locality_zoneid=sample-cluster-storage-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.95:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "storage-3",
								"machineid":   "sample-cluster-storage-3",
								"processid":   "c813e585043a7ab55a4905f465c4aa52",
								"zoneid":      "sample-cluster-storage-3",
							},
							Version:       "6.2.15",
							UptimeSeconds: 2475.33,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "proxy",
								},
								{
									Role: "storage",
								},
							},
						},
						"f9efa90fc104f4e277b140baf89aab66": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.92"),
								Port:      4501,
							},
							ProcessClass: ProcessClassStorage,
							CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-1 --locality_machineid=sample-cluster-storage-1 --locality_zoneid=sample-cluster-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.92:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "storage-1",
								"machineid":   "sample-cluster-storage-1",
								"processid":   "f9efa90fc104f4e277b140baf89aab66",
								"zoneid":      "sample-cluster-storage-1",
							},
							Version:       "6.2.15",
							UptimeSeconds: 2951.17,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "proxy",
								},
								{
									Role: "storage",
								},
							},
						},
						"5a633d7f4e98a6c938c84b97ec4aedbf": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.105"),
								Port:      4501,
							},
							ProcessClass: ProcessClassLog,
							CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-2 --locality_machineid=sample-cluster-log-2 --locality_zoneid=sample-cluster-log-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.105:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "log-2",
								"machineid":   "sample-cluster-log-2",
								"processid":   "5a633d7f4e98a6c938c84b97ec4aedbf",
								"zoneid":      "sample-cluster-log-2",
							},
							Version:       "6.2.15",
							UptimeSeconds: 710.119,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "cluster_controller",
								},
								{
									Role: "log",
								},
							},
						},
						"5c1b68147a0ef34ce005a38245851270": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.102"),
								Port:      4501,
							},
							ProcessClass: ProcessClassLog,
							CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-4 --locality_machineid=sample-cluster-log-4 --locality_zoneid=sample-cluster-log-4 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.102:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"zoneid":      "sample-cluster-log-4",
								"instance_id": "log-4",
								"machineid":   "sample-cluster-log-4",
								"processid":   "5c1b68147a0ef34ce005a38245851270",
							},
							Version:       "6.2.15",
							UptimeSeconds: 1095.18,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: string(ProcessRoleCoordinator),
								},
								{
									Role: "resolver",
								},
							},
						},
						"653defde43cf1fdef131e2fb82bd192d": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.104"),
								Port:      4501,
							},
							ProcessClass: ProcessClassLog,
							CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-1 --locality_machineid=sample-cluster-log-1 --locality_zoneid=sample-cluster-log-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.104:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "log-1",
								"machineid":   "sample-cluster-log-1",
								"processid":   "653defde43cf1fdef131e2fb82bd192d",
								"zoneid":      "sample-cluster-log-1",
							},
							Version:       "6.2.15",
							UptimeSeconds: 880.18,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "master",
								},
								{
									Role: "data_distributor",
								},
								{
									Role: "ratekeeper",
								},
								{
									Role: string(ProcessRoleCoordinator),
								},
								{
									Role: "log",
								},
							},
						},
						"9c93d3b70118f16c72f7cb3f53e49f4c": {
							Address: ProcessAddress{
								IPAddress: net.ParseIP("10.1.38.94"),
								Port:      4501,
							},
							ProcessClass: ProcessClassStorage,
							CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-2 --locality_machineid=sample-cluster-storage-2 --locality_zoneid=sample-cluster-storage-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.94:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "storage-2",
								"machineid":   "sample-cluster-storage-2",
								"processid":   "9c93d3b70118f16c72f7cb3f53e49f4c",
								"zoneid":      "sample-cluster-storage-2",
							},
							Version:       "6.2.15",
							UptimeSeconds: 2650.5,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: string(ProcessRoleCoordinator),
								},
								{
									Role: "proxy",
								},
								{
									Role: "storage",
								},
							},
						},
					},
					Data: FoundationDBStatusDataStatistics{
						KVBytes:    0,
						MovingData: FoundationDBStatusMovingData{HighestPriority: 0, InFlightBytes: 0, InQueueBytes: 0},
						State:      FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy"},
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
								"default": {
									CurrentContainer: "blobstore://minio@minio-service:9000/sample-cluster-test-backup?bucket=fdb-backups",
									RunningBackup:    true,
									Restorable:       false,
								},
							},
						},
					},
				},
			}))
		})
	})

	When("parsing the status json with a 7.1.0-rc1 cluster", func() {
		status := FoundationDBStatusClusterInfo{
			IncompatibleConnections: []string{},
			DatabaseConfiguration: DatabaseConfiguration{
				RedundancyMode:  "double",
				StorageEngine:   StorageEngineSSD2,
				UsableRegions:   1,
				Regions:         nil,
				ExcludedServers: make([]ExcludedServers, 0),
				RoleCounts:      RoleCounts{Storage: 0, Logs: 3, Proxies: 3, CommitProxies: 2, GrvProxies: 1, Resolvers: 1, LogRouters: -1, RemoteLogs: -1},
				VersionFlags:    VersionFlags{LogSpill: 2, LogVersion: 0},
			},
			Processes: map[string]FoundationDBStatusProcessInfo{
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
						{Role: "coordinator"},
						{Role: "grv_proxy"},
						{Role: "storage"},
					},
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
						{Role: "coordinator"},
						{Role: string(ProcessClassStorage)},
						{Role: "resolver"},
					},
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
						{Role: "coordinator"},
						{Role: "commit_proxy"},
						{Role: "storage"},
					},
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
						{Role: "master"},
						{Role: "data_distributor"},
						{Role: "ratekeeper"},
					},
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
						{Role: string(ProcessClassClusterController)},
					},
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
						{Role: "log"},
					},
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
						{Role: "log"},
					},
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
						{Role: "log"},
					},
				},
			},
			Data: FoundationDBStatusDataStatistics{
				KVBytes:    0,
				MovingData: FoundationDBStatusMovingData{HighestPriority: 0, InFlightBytes: 0, InQueueBytes: 0},
				State:      FoundationDBStatusDataState{Description: "", Healthy: true, Name: "healthy"},
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
			FaultTolerance: FaultTolerance{
				MaxZoneFailuresWithoutLosingData:         1,
				MaxZoneFailuresWithoutLosingAvailability: 1,
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
})
