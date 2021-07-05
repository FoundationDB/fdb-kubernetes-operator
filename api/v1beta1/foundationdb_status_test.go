/*
Copyright 2021 FoundationDB project authors.

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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FoundationDBStatus", func() {

	When("parsing the status json with a 6.1 cluster", func() {
		It("should parse all values correctly", func() {
			statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_1.json"), os.O_RDONLY, os.ModePerm)
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
							ProcessClass: ProcessClassStorage,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-3 --locality_machineid=sample-cluster-storage-3 --locality_zoneid=sample-cluster-storage-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.91:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "storage-3",
								"machineid":   "sample-cluster-storage-3",
								"processid":   "c813e585043a7ab55a4905f465c4aa52",
								"zoneid":      "sample-cluster-storage-3",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.009,
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
							Address:      "10.1.38.82:4501",
							ProcessClass: ProcessClassStorage,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-1 --locality_machineid=sample-cluster-storage-1 --locality_zoneid=sample-cluster-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.82:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "storage-1",
								"machineid":   "sample-cluster-storage-1",
								"processid":   "f9efa90fc104f4e277b140baf89aab66",
								"zoneid":      "sample-cluster-storage-1",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.008,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "cluster_controller",
								},
								{
									Role: "ratekeeper",
								},
								{
									Role: "storage",
								},
							},
						},
						"5a633d7f4e98a6c938c84b97ec4aedbf": {
							Address:      "10.1.38.89:4501",
							ProcessClass: ProcessClassLog,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-2 --locality_machineid=sample-cluster-log-2 --locality_zoneid=sample-cluster-log-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.89:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "log-2",
								"machineid":   "sample-cluster-log-2",
								"processid":   "5a633d7f4e98a6c938c84b97ec4aedbf",
								"zoneid":      "sample-cluster-log-2",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.009,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "log",
								},
							},
						},
						"5c1b68147a0ef34ce005a38245851270": {
							Address:      "10.1.38.88:4501",
							ProcessClass: ProcessClassLog,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-4 --locality_machineid=sample-cluster-log-4 --locality_zoneid=sample-cluster-log-4 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.88:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"machineid":   "sample-cluster-log-4",
								"processid":   "5c1b68147a0ef34ce005a38245851270",
								"zoneid":      "sample-cluster-log-4",
								"instance_id": "log-4",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.008,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "proxy",
								},
							},
						},
						"653defde43cf1fdef131e2fb82bd192d": {
							Address:      "10.1.38.87:4501",
							ProcessClass: ProcessClassLog,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-1 --locality_machineid=sample-cluster-log-1 --locality_zoneid=sample-cluster-log-1 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.87:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"instance_id": "log-1",
								"machineid":   "sample-cluster-log-1",
								"processid":   "653defde43cf1fdef131e2fb82bd192d",
								"zoneid":      "sample-cluster-log-1",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.01,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "log",
								},
							},
						},
						"9c93d3b70118f16c72f7cb3f53e49f4c": {
							Address:      "10.1.38.86:4501",
							ProcessClass: ProcessClassStorage,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=storage-2 --locality_machineid=sample-cluster-storage-2 --locality_zoneid=sample-cluster-storage-2 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.86:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"processid":   "9c93d3b70118f16c72f7cb3f53e49f4c",
								"zoneid":      "sample-cluster-storage-2",
								"instance_id": "storage-2",
								"machineid":   "sample-cluster-storage-2",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.008,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "storage",
								},
								{
									Role: "resolver",
								},
							},
						},
						"b9c25278c0fa207bc2a73bda2300d0a9": {
							Address:      "10.1.38.90:4501",
							ProcessClass: ProcessClassLog,
							CommandLine:  "/var/dynamic-conf/bin/6.1.12/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --knob_disable_posix_kernel_aio=1 --locality_instance_id=log-3 --locality_machineid=sample-cluster-log-3 --locality_zoneid=sample-cluster-log-3 --logdir=/var/log/fdb-trace-logs --loggroup=sample-cluster --public_address=10.1.38.90:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
							Excluded:     false,
							Locality: map[string]string{
								"processid":   "b9c25278c0fa207bc2a73bda2300d0a9",
								"zoneid":      "sample-cluster-log-3",
								"instance_id": "log-3",
								"machineid":   "sample-cluster-log-3",
							},
							Version:       "6.1.12",
							UptimeSeconds: 160.01,
							Roles: []FoundationDBStatusProcessRoleInfo{
								{
									Role: "master",
								},
								{
									Role: "data_distributor",
								},
								{
									Role: "log",
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
							Address:      "10.1.38.95:4501",
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
							Address:      "10.1.38.92:4501",
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
							Address:      "10.1.38.105:4501",
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
							Address:      "10.1.38.102:4501",
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
							Address:      "10.1.38.104:4501",
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
							Address:      "10.1.38.94:4501",
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
})
