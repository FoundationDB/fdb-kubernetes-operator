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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("admin_client_test", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var client *MockAdminClient

	var err error

	BeforeEach(func() {
		cluster = createDefaultCluster()
		ClearMockAdminClients()
		client, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
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
							RedundancyMode: "",
							StorageEngine:  "",
							UsableRegions:  0,
							Regions:        nil,
							RoleCounts: fdbtypes.RoleCounts{
								Logs:       3,
								Proxies:    3,
								Resolvers:  1,
								LogRouters: -1,
								RemoteLogs: -1,
							},
						},
						FullReplication: true,
						Processes:       map[string]fdbtypes.FoundationDBStatusProcessInfo{},
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
