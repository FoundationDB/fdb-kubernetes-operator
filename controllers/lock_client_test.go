/*
 * lock_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("lock_client_test", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var client *MockLockClient

	var err error

	BeforeEach(func() {
		cluster = createDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		client = newMockLockClientUncast(cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("TakeLock", func() {
		It("returns true", func() {
			success, err := client.TakeLock()
			Expect(err).NotTo(HaveOccurred())
			Expect(success).To(BeTrue())
		})
	})

	Describe("AddPendingUpgrades", func() {
		It("adds the upgrades to the map", func() {
			err = client.AddPendingUpgrades(Versions.Default, []string{"storage-1", "storage-2"})
			Expect(err).NotTo(HaveOccurred())

			err = client.AddPendingUpgrades(Versions.Default, []string{"storage-3"})
			Expect(err).NotTo(HaveOccurred())

			err = client.AddPendingUpgrades(Versions.NextMajorVersion, []string{"storage-3", "storage-4"})
			Expect(err).NotTo(HaveOccurred())

			Expect(client.pendingUpgrades).To(Equal(map[fdbtypes.FdbVersion]map[string]bool{
				Versions.Default: {
					"storage-1": true,
					"storage-2": true,
					"storage-3": true,
				},
				Versions.NextMajorVersion: {
					"storage-3": true,
					"storage-4": true,
				},
			}))
		})
	})

	Describe("GetPendingUpgrades", func() {
		BeforeEach(func() {
			client.pendingUpgrades = map[fdbtypes.FdbVersion]map[string]bool{
				Versions.Default: {
					"storage-1": true,
					"storage-2": true,
					"storage-3": true,
				},
				Versions.NextMajorVersion: {
					"storage-3": true,
					"storage-4": true,
				},
			}
		})

		Context("with upgrades in the map", func() {
			It("returns the upgrades in the map", func() {
				upgrades, err := client.GetPendingUpgrades(Versions.Default)
				Expect(err).NotTo(HaveOccurred())
				Expect(upgrades).To(Equal(map[string]bool{
					"storage-1": true,
					"storage-2": true,
					"storage-3": true,
				}))
			})
		})

		Context("with a version that is not in the map", func() {
			It("returns the upgrades in the map", func() {
				upgrades, err := client.GetPendingUpgrades(Versions.NextPatchVersion)
				Expect(err).NotTo(HaveOccurred())
				Expect(upgrades).NotTo(BeNil())
				Expect(upgrades).To(BeEmpty())
			})
		})
	})
})
