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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

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
			return reloadCluster(cluster)
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
				Expect(status.Cluster.DatabaseConfiguration).To(Equal(fdbtypes.DatabaseConfiguration{
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
					VersionFlags: fdbtypes.VersionFlags{
						LogSpill: 2,
					},
				}))

				Expect(status.Cluster.Processes["operator-test-1-storage-1"]).To(Equal(fdbtypes.FoundationDBStatusProcessInfo{
					ProcessAddresses: []fdbtypes.ProcessAddress{
						{
							IPAddress: "1.1.0.1",
							Port:      4501,
						},
					},
					ProcessClass: "storage",
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=storage-1 --locality_machineid=operator-test-1-storage-1 --locality_zoneid=operator-test-1-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=1.1.0.1:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-1",
						"zoneid":      "operator-test-1-storage-1",
						"dcid":        "",
					},
					Version:       "6.2.20",
					UptimeSeconds: 60000,
				}))
			})
		})

		Context("with a backup running", func() {
			BeforeEach(func() {
				err = client.StartBackup("blobstore://test@test-service/test-backup", 10)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the backup in the layer status", func() {
				Expect(status.Cluster.Layers.Backup.Paused).To(BeFalse())
				Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbtypes.FoundationDBStatusBackupTag{
					"default": {
						CurrentContainer: "blobstore://test@test-service/test-backup",
						RunningBackup:    true,
						Restorable:       true,
					},
				}))
			})

			Context("with a paused backup", func() {
				BeforeEach(func() {
					err = client.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as paused", func() {
					Expect(status.Cluster.Layers.Backup.Paused).To(BeTrue())
				})
			})

			Context("with an resume backup", func() {
				BeforeEach(func() {
					err = client.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
					err = client.ResumeBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as not paused", func() {
					Expect(status.Cluster.Layers.Backup.Paused).To(BeFalse())
				})
			})

			Context("with a stopped backup", func() {
				BeforeEach(func() {
					err = client.StopBackup("blobstore://test@test-service/test-backup")
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as stopped", func() {
					Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbtypes.FoundationDBStatusBackupTag{
						"default": {
							CurrentContainer: "blobstore://test@test-service/test-backup",
							RunningBackup:    false,
							Restorable:       true,
						},
					}))
				})
			})
		})
	})

	Describe("backup status", func() {
		var status *fdbtypes.FoundationDBLiveBackupStatus
		JustBeforeEach(func() {
			status, err = client.GetBackupStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			It("should mark the backup as not running", func() {
				Expect(status.DestinationURL).To(Equal(""))
				Expect(status.Status.Running).To(BeFalse())
				Expect(status.BackupAgentsPaused).To(BeFalse())
			})
		})

		Context("with a backup running", func() {
			BeforeEach(func() {
				err = client.StartBackup("blobstore://test@test-service/test-backup", 10)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the backup in the status", func() {
				Expect(status.DestinationURL).To(Equal("blobstore://test@test-service/test-backup"))
				Expect(status.Status.Running).To(BeTrue())
				Expect(status.BackupAgentsPaused).To(BeFalse())
				Expect(status.SnapshotIntervalSeconds).To(Equal(10))
			})

			Context("with a paused backup", func() {
				BeforeEach(func() {
					err = client.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as paused", func() {
					Expect(status.BackupAgentsPaused).To(BeTrue())
				})
			})

			Context("with a resumed backup", func() {
				BeforeEach(func() {
					err = client.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
					err = client.ResumeBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as not paused", func() {
					Expect(status.BackupAgentsPaused).To(BeFalse())
				})
			})

			Context("with a stopped backup", func() {
				BeforeEach(func() {
					err = client.StopBackup("blobstore://test@test-service/test-backup")
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as stopped", func() {
					Expect(status.Status.Running).To(BeFalse())
				})
			})

			Context("with a modification to the snapshot time", func() {
				BeforeEach(func() {
					err = client.ModifyBackup(20)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as stopped", func() {
					Expect(status.SnapshotIntervalSeconds).To(Equal(20))
				})
			})
		})
	})

	Describe("restore status", func() {
		var status string

		Context("with no restore running", func() {
			BeforeEach(func() {
				status, err = client.GetRestoreStatus()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should be empty", func() {
				Expect(status).To(Equal("\n"))
			})
		})

		Context("with a restore running", func() {
			BeforeEach(func() {
				err = client.StartRestore("blobstore://test@test-service/test-backup")
				Expect(err).NotTo(HaveOccurred())

				status, err = client.GetRestoreStatus()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the backup URL", func() {
				Expect(status).To(Equal("blobstore://test@test-service/test-backup\n"))
			})
		})
	})

	Describe("helper methods", func() {
		Describe("parseExclusionOutput", func() {
			It("should map the output description to exclusion success", func() {
				output := "  10.1.56.36:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53:4501  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35:4501  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4501 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36:4501": "Success",
					"10.1.56.43:4501": "Success",
					"10.1.56.52:4501": "Success",
					"10.1.56.53:4501": "Missing",
					"10.1.56.35:4501": "In Progress",
					"10.1.56.56:4501": "Success",
				}))
			})
		})
	})

	Describe("helper methods", func() {
		Describe("parseExclusionOutputIPv6", func() {
			It("should map the output description to exclusion success", func() {
				output := "  [10.1.56.36]:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  [10.1.56.43]:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  [10.1.56.52]:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  [10.1.56.53]:4501  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  [10.1.56.35]:4501  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  [10.1.56.56]:4501  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: [10.1.56.56]:4501 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"[10.1.56.36]:4501": "Success",
					"[10.1.56.43]:4501": "Success",
					"[10.1.56.52]:4501": "Success",
					"[10.1.56.53]:4501": "Missing",
					"[10.1.56.35]:4501": "In Progress",
					"[10.1.56.56]:4501": "Success",
				}))
			})
		})
	})
})
