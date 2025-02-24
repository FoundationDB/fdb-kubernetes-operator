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

package mock

import (
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

var _ = Describe("lock_client_test", func() {
	var lockClient *LockClient

	var err error

	BeforeEach(func() {
		lockClient = NewMockLockClientUncast(internal.CreateDefaultCluster())
	})

	Describe("TakeLock", func() {
		It("returns true", func() {
			Expect(lockClient.TakeLock()).To(Succeed())
		})
	})

	Describe("AddPendingUpgrades", func() {
		It("adds the upgrades to the map", func() {
			err = lockClient.AddPendingUpgrades(fdbv1beta2.Versions.Default, []fdbv1beta2.ProcessGroupID{"storage-1", "storage-2"})
			Expect(err).NotTo(HaveOccurred())

			err = lockClient.AddPendingUpgrades(fdbv1beta2.Versions.Default, []fdbv1beta2.ProcessGroupID{"storage-3"})
			Expect(err).NotTo(HaveOccurred())

			err = lockClient.AddPendingUpgrades(fdbv1beta2.Versions.NextMajorVersion, []fdbv1beta2.ProcessGroupID{"storage-3", "storage-4"})
			Expect(err).NotTo(HaveOccurred())

			Expect(lockClient.pendingUpgrades).To(Equal(map[fdbv1beta2.Version]map[fdbv1beta2.ProcessGroupID]bool{
				fdbv1beta2.Versions.Default: {
					"storage-1": true,
					"storage-2": true,
					"storage-3": true,
				},
				fdbv1beta2.Versions.NextMajorVersion: {
					"storage-3": true,
					"storage-4": true,
				},
			}))
		})
	})

	Describe("GetPendingUpgrades", func() {
		BeforeEach(func() {
			lockClient.pendingUpgrades = map[fdbv1beta2.Version]map[fdbv1beta2.ProcessGroupID]bool{
				fdbv1beta2.Versions.Default: {
					"storage-1": true,
					"storage-2": true,
					"storage-3": true,
				},
				fdbv1beta2.Versions.NextMajorVersion: {
					"storage-3": true,
					"storage-4": true,
				},
			}
		})

		Context("with upgrades in the map", func() {
			It("returns the upgrades in the map", func() {
				upgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.Default)
				Expect(err).NotTo(HaveOccurred())
				Expect(upgrades).To(Equal(map[fdbv1beta2.ProcessGroupID]bool{
					"storage-1": true,
					"storage-2": true,
					"storage-3": true,
				}))
			})
		})

		Context("with a version that is not in the map", func() {
			It("returns the upgrades in the map", func() {
				upgrades, err := lockClient.GetPendingUpgrades(fdbv1beta2.Versions.NextPatchVersion)
				Expect(err).NotTo(HaveOccurred())
				Expect(upgrades).NotTo(BeNil())
				Expect(upgrades).To(BeEmpty())
			})
		})
	})
})
