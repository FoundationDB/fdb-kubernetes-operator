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
	"fmt"
	"net"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"k8s.io/utils/pointer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var _ = Describe("admin_client_test", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var mockAdminClient *mock.AdminClient

	var err error

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).NotTo(Equal(int64(0)))

		mockAdminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("JSON status", func() {
		var status *fdbv1beta2.FoundationDBStatus

		JustBeforeEach(func() {
			status, err = mockAdminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			When("the version supports grv and commit proxies", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())

					result, err := reconcileCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Requeue).To(BeFalse())

					generation, err := reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generation).NotTo(Equal(int64(0)))

					mockAdminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should generate the status", func() {
					Expect(status.Cluster.DatabaseConfiguration).To(Equal(fdbv1beta2.DatabaseConfiguration{
						RedundancyMode: fdbv1beta2.RedundancyModeDouble,
						StorageEngine:  fdbv1beta2.StorageEngineSSD2,
						UsableRegions:  1,
						RoleCounts: fdbv1beta2.RoleCounts{
							Logs:          3,
							Proxies:       3,
							CommitProxies: 0,
							GrvProxies:    0,
							Resolvers:     1,
							LogRouters:    -1,
							RemoteLogs:    -1,
						},
						VersionFlags: fdbv1beta2.VersionFlags{
							LogSpill: 2,
						},
					}))

					address := cluster.Status.ProcessGroups[13].Addresses[0]
					Expect(status.Cluster.Processes).To(HaveLen(len(cluster.Status.ProcessGroups)))
					Expect(status.Cluster.Processes["operator-test-1-storage-1-1"]).To(Equal(fdbv1beta2.FoundationDBStatusProcessInfo{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP(address),
							Port:      4501,
						},
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						CommandLine:  fmt.Sprintf("/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --listen_address=%s:4501 --locality_instance_id=storage-1 --locality_machineid=operator-test-1-storage-1 --locality_zoneid=operator-test-1-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=%s:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster", address, address),
						Excluded:     false,
						Locality: map[string]string{
							"instance_id": "storage-1",
							"zoneid":      "operator-test-1-storage-1",
							"dcid":        "",
						},
						Version:       fdbv1beta2.Versions.NextMajorVersion.String(),
						UptimeSeconds: 60000,
						Roles:         nil,
					}))
				})
			})

			When("the version does not supports grv and commit proxies", func() {
				It("should generate the status", func() {
					Expect(status.Cluster.DatabaseConfiguration).To(Equal(fdbv1beta2.DatabaseConfiguration{
						RedundancyMode: fdbv1beta2.RedundancyModeDouble,
						StorageEngine:  fdbv1beta2.StorageEngineSSD2,
						UsableRegions:  1,
						RoleCounts: fdbv1beta2.RoleCounts{
							Logs:          3,
							Proxies:       3,
							CommitProxies: 0,
							GrvProxies:    0,
							Resolvers:     1,
							LogRouters:    -1,
							RemoteLogs:    -1,
						},
						VersionFlags: fdbv1beta2.VersionFlags{
							LogSpill: 2,
						},
					}))

					address := cluster.Status.ProcessGroups[13].Addresses[0]
					Expect(status.Cluster.Processes).To(HaveLen(len(cluster.Status.ProcessGroups)))
					Expect(status.Cluster.Processes["operator-test-1-storage-1-1"]).To(Equal(fdbv1beta2.FoundationDBStatusProcessInfo{
						Address: fdbv1beta2.ProcessAddress{
							IPAddress: net.ParseIP(address),
							Port:      4501,
						},
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						CommandLine:  fmt.Sprintf("/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --listen_address=%s:4501 --locality_instance_id=storage-1 --locality_machineid=operator-test-1-storage-1 --locality_zoneid=operator-test-1-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=%s:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster", address, address),
						Excluded:     false,
						Locality: map[string]string{
							"instance_id": "storage-1",
							"zoneid":      "operator-test-1-storage-1",
							"dcid":        "",
						},
						Version:       cluster.Spec.Version,
						UptimeSeconds: 60000,
						Roles:         nil,
					}))
				})
			})
		})

		Context("with the DNS names enabled", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.DefineDNSLocalityFields = pointer.Bool(true)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			When("the cluster has not been reconciled", func() {
				It("should not have DNS names in the locality", func() {
					locality := status.Cluster.Processes["operator-test-1-storage-1-1"].Locality
					Expect(locality[fdbv1beta2.FDBLocalityDNSNameKey]).To(BeEmpty())
				})
			})

			When("the cluster has been reconciled", func() {
				BeforeEach(func() {
					result, err := reconcileCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Requeue).To(BeFalse())
				})

				It("should have DNS names in the locality", func() {
					locality := status.Cluster.Processes["operator-test-1-storage-1-1"].Locality
					Expect(locality[fdbv1beta2.FDBLocalityDNSNameKey]).To(Equal(internal.GetPodDNSName(cluster, "operator-test-1-storage-1")))
				})
			})
		})

		Context("with an additional process", func() {
			BeforeEach(func() {
				mockAdminClient.MockAdditionalProcesses([]fdbv1beta2.ProcessGroupStatus{{
					ProcessGroupID: "dc2-storage-1",
					ProcessClass:   "storage",
					Addresses:      []string{"1.2.3.4"},
				}})
			})

			It("puts the additional processes in the status", func() {
				Expect(status.Cluster.Processes).To(HaveLen(len(cluster.Status.ProcessGroups) + 1))
				Expect(status.Cluster.Processes["dc2-storage-1"]).To(Equal(fdbv1beta2.FoundationDBStatusProcessInfo{
					Address: fdbv1beta2.ProcessAddress{
						IPAddress: net.ParseIP("1.2.3.4"),
						Port:      4501,
					},
					ProcessClass: fdbv1beta2.ProcessClassStorage,
					Locality: map[string]string{
						"instance_id": "dc2-storage-1",
						"zoneid":      "dc2-storage-1",
					},
					Version:       cluster.Spec.Version,
					UptimeSeconds: 60000,
				}))
			})
		})

		Context("with a backup running", func() {
			BeforeEach(func() {
				err = mockAdminClient.StartBackup("blobstore://test@test-service/test-backup", 10)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the backup in the layer status", func() {
				Expect(status.Cluster.Layers.Backup.Paused).To(BeFalse())
				Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbv1beta2.FoundationDBStatusBackupTag{
					"default": {
						CurrentContainer: "blobstore://test@test-service/test-backup",
						RunningBackup:    true,
						Restorable:       true,
					},
				}))
			})

			Context("with a paused backup", func() {
				BeforeEach(func() {
					err = mockAdminClient.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as paused", func() {
					Expect(status.Cluster.Layers.Backup.Paused).To(BeTrue())
				})
			})

			Context("with an resume backup", func() {
				BeforeEach(func() {
					err = mockAdminClient.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
					err = mockAdminClient.ResumeBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as not paused", func() {
					Expect(status.Cluster.Layers.Backup.Paused).To(BeFalse())
				})
			})

			Context("with a stopped backup", func() {
				BeforeEach(func() {
					err = mockAdminClient.StopBackup("blobstore://test@test-service/test-backup")
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as stopped", func() {
					Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbv1beta2.FoundationDBStatusBackupTag{
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
		var status *fdbv1beta2.FoundationDBLiveBackupStatus
		JustBeforeEach(func() {
			status, err = mockAdminClient.GetBackupStatus()
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
				err = mockAdminClient.StartBackup("blobstore://test@test-service/test-backup", 10)
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
					err = mockAdminClient.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as paused", func() {
					Expect(status.BackupAgentsPaused).To(BeTrue())
				})
			})

			Context("with a resumed backup", func() {
				BeforeEach(func() {
					err = mockAdminClient.PauseBackups()
					Expect(err).NotTo(HaveOccurred())
					err = mockAdminClient.ResumeBackups()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as not paused", func() {
					Expect(status.BackupAgentsPaused).To(BeFalse())
				})
			})

			Context("with a stopped backup", func() {
				BeforeEach(func() {
					err = mockAdminClient.StopBackup("blobstore://test@test-service/test-backup")
					Expect(err).NotTo(HaveOccurred())
				})

				It("should mark the backup as stopped", func() {
					Expect(status.Status.Running).To(BeFalse())
				})
			})

			Context("with a modification to the snapshot time", func() {
				BeforeEach(func() {
					err = mockAdminClient.ModifyBackup(20)
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
				status, err = mockAdminClient.GetRestoreStatus()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should be empty", func() {
				Expect(status).To(Equal("\n"))
			})
		})

		Context("with a restore running", func() {
			BeforeEach(func() {
				err = mockAdminClient.StartRestore("blobstore://test@test-service/test-backup", nil)
				Expect(err).NotTo(HaveOccurred())

				status, err = mockAdminClient.GetRestoreStatus()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the backup URL", func() {
				Expect(status).To(Equal("blobstore://test@test-service/test-backup\n"))
			})
		})
	})
})
