/*
 * admin_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2020 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("admin_client_test", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var client *MockAdminClient

	var err error

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		timeout := time.Second * 5
		Eventually(func() (int64, error) {
			generations, err := reloadClusterGenerations(k8sClient, cluster)
			return generations.Reconciled, err
		}, timeout).ShouldNot(Equal(int64(0)))

		client, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	Describe("JSON status", func() {
		var status *fdbtypes.FoundationDBStatus
		JustBeforeEach(func() {
			status, err = client.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			It("should generate the status", func() {
				Expect(status).To(Equal(&fdbtypes.FoundationDBStatus{
					Client: fdbtypes.FoundationDBStatusLocalClientInfo{
						Coordinators: fdbtypes.FoundationDBStatusCoordinatorInfo{
							Coordinators: []fdbtypes.FoundationDBStatusCoordinator{
								{Address: "127.0.0.1:4501", Reachable: false},
							},
						},
						DatabaseStatus: fdbtypes.FoundationDBStatusClientDBStatus{Available: true, Healthy: true},
					},
					Cluster: fdbtypes.FoundationDBStatusClusterInfo{
						DatabaseConfiguration: fdbtypes.DatabaseConfiguration{
							RedundancyMode: "double",
							StorageEngine:  "ssd-2",
							UsableRegions:  1,
							RoleCounts: fdbtypes.RoleCounts{
								Logs:       3,
								Proxies:    3,
								Resolvers:  1,
								LogRouters: -1,
								RemoteLogs: -1,
							},
						},
						FullReplication: true,
						Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
							"operator-test-1-storage-1": {
								Address:      "1.1.0.1:4501",
								ProcessClass: "storage",
								CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=storage-1 --locality_machineid=operator-test-1-storage-1 --locality_zoneid=operator-test-1-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "storage-1",
								},
								Version: "6.2.15",
							},
							"operator-test-1-storage-2": {
								Address:      "1.1.0.2:4501",
								ProcessClass: "storage",
								CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=storage-2 --locality_machineid=operator-test-1-storage-2 --locality_zoneid=operator-test-1-storage-2 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "storage-2",
								},
								Version: "6.2.15",
							},
							"operator-test-1-storage-3": {
								Address:      "1.1.0.3:4501",
								ProcessClass: "storage",
								CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=storage-3 --locality_machineid=operator-test-1-storage-3 --locality_zoneid=operator-test-1-storage-3 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "storage-3",
								},
								Version: "6.2.15",
							},
							"operator-test-1-storage-4": {
								Address:      "1.1.0.4:4501",
								ProcessClass: "storage",
								CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=storage-4 --locality_machineid=operator-test-1-storage-4 --locality_zoneid=operator-test-1-storage-4 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "storage-4",
								},
								Version: "6.2.15",
							},
							"operator-test-1-log-1": {
								Address:      "1.1.5.1:4501",
								ProcessClass: "log",
								CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=log-1 --locality_machineid=operator-test-1-log-1 --locality_zoneid=operator-test-1-log-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "log-1",
								},
								Version: "6.2.15",
							},
							"operator-test-1-log-2": {
								Address:      "1.1.5.2:4501",
								ProcessClass: "log",
								CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=log-2 --locality_machineid=operator-test-1-log-2 --locality_zoneid=operator-test-1-log-2 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "log-2",
								},
								Version: "6.2.15",
							},
							"operator-test-1-log-3": {
								Address:      "1.1.5.3:4501",
								ProcessClass: "log",
								CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=log-3 --locality_machineid=operator-test-1-log-3 --locality_zoneid=operator-test-1-log-3 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "log-3",
								},
								Version: "6.2.15",
							},
							"operator-test-1-log-4": {
								Address:      "1.1.5.4:4501",
								ProcessClass: "log",
								CommandLine:  "/usr/bin/fdbserver --class=log --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=log-4 --locality_machineid=operator-test-1-log-4 --locality_zoneid=operator-test-1-log-4 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "log-4",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-1": {
								Address:      "1.1.2.1:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-1 --locality_machineid=operator-test-1-stateless-1 --locality_zoneid=operator-test-1-stateless-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-1",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-2": {
								Address:      "1.1.2.2:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-2 --locality_machineid=operator-test-1-stateless-2 --locality_zoneid=operator-test-1-stateless-2 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-2",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-3": {
								Address:      "1.1.2.3:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-3 --locality_machineid=operator-test-1-stateless-3 --locality_zoneid=operator-test-1-stateless-3 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-3",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-4": {
								Address:      "1.1.2.4:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-4 --locality_machineid=operator-test-1-stateless-4 --locality_zoneid=operator-test-1-stateless-4 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-4",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-5": {
								Address:      "1.1.2.5:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-5 --locality_machineid=operator-test-1-stateless-5 --locality_zoneid=operator-test-1-stateless-5 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-5",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-6": {
								Address:      "1.1.2.6:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-6 --locality_machineid=operator-test-1-stateless-6 --locality_zoneid=operator-test-1-stateless-6 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-6",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-7": {
								Address:      "1.1.2.7:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-7 --locality_machineid=operator-test-1-stateless-7 --locality_zoneid=operator-test-1-stateless-7 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-7",
								},
								Version: "6.2.15",
							},
							"operator-test-1-stateless-8": {
								Address:      "1.1.2.8:4501",
								ProcessClass: "stateless",
								CommandLine:  "/usr/bin/fdbserver --class=stateless --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=stateless-8 --locality_machineid=operator-test-1-stateless-8 --locality_zoneid=operator-test-1-stateless-8 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "stateless-8",
								},
								Version: "6.2.15",
							},
							"operator-test-1-cluster-controller-1": {
								Address:      "1.1.7.1:4501",
								ProcessClass: "cluster_controller",
								CommandLine:  "/usr/bin/fdbserver --class=cluster_controller --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=cluster_controller-1 --locality_machineid=operator-test-1-cluster-controller-1 --locality_zoneid=operator-test-1-cluster-controller-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
								Excluded:     false,
								Locality: map[string]string{
									"instance_id": "cluster_controller-1",
								},
								Version: "6.2.15",
							},
						},
					},
				}))
			})
		})

		Context("with a backup running", func() {
			BeforeEach(func() {
				err = client.StartBackup("blobstore://test@test-service/test-backup")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the backup in the layer status", func() {
				Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbtypes.FoundationDBStatusBackupTag{
					"default": {
						CurrentContainer: "blobstore://test@test-service/test-backup",
						RunningBackup:    true,
						Restorable:       true,
					},
				}))
			})
		})
	})
})
