/*
 * lock_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2026 Apple Inc. and the FoundationDB project authors
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

package fdbclient

import (
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// newTestCluster returns a minimal FoundationDBCluster with locking explicitly enabled via LockOptions.
func newTestCluster() *fdbv1beta2.FoundationDBCluster {
	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			ProcessGroupIDPrefix: "test-operator",
			LockOptions: fdbv1beta2.LockOptions{
				DisableLocks: ptr.To(false),
			},
		},
	}
}

var _ = Describe("LockClient", func() {
	var (
		cluster *fdbv1beta2.FoundationDBCluster
		mockLib *mockFdbLibClient
		lc      *realLockClient
	)

	BeforeEach(func() {
		cluster = newTestCluster()
		mockLib = &mockFdbLibClient{}
		lc = &realLockClient{
			cluster:      cluster,
			fdbLibClient: mockLib,
			log:          logr.Discard(),
		}
	})

	Describe("Disabled", func() {
		When("disableLocks is false", func() {
			It("returns false", func() {
				Expect(lc.Disabled()).To(BeFalse())
			})
		})

		When("disableLocks is true", func() {
			BeforeEach(func() {
				lc.disableLocks = true
			})

			It("returns true", func() {
				Expect(lc.Disabled()).To(BeTrue())
			})
		})
	})

	Describe("TakeLock", func() {
		When("locking is disabled", func() {
			BeforeEach(func() {
				lc.disableLocks = true
			})

			It("returns nil without calling executeTransaction", func() {
				Expect(lc.TakeLock()).To(Succeed())
				Expect(mockLib.executeTransactionCallCount).To(Equal(0))
			})
		})

		When("locking is enabled", func() {
			It("delegates to executeTransaction", func() {
				Expect(lc.TakeLock()).To(Succeed())
				Expect(mockLib.executeTransactionCallCount).To(Equal(1))
			})

			When("executeTransaction returns an error", func() {
				BeforeEach(func() {
					mockLib.mockedError = fmt.Errorf("fdb unavailable")
				})

				It("propagates the error", func() {
					Expect(lc.TakeLock()).To(MatchError("fdb unavailable"))
				})
			})
		})
	})

	Describe("ReleaseLock", func() {
		When("locking is disabled", func() {
			BeforeEach(func() {
				lc.disableLocks = true
			})

			It("returns nil without calling executeTransaction", func() {
				Expect(lc.ReleaseLock()).To(Succeed())
				Expect(mockLib.executeTransactionCallCount).To(Equal(0))
			})
		})

		When("locking is enabled", func() {
			It("delegates to executeTransaction", func() {
				Expect(lc.ReleaseLock()).To(Succeed())
				Expect(mockLib.executeTransactionCallCount).To(Equal(1))
			})

			When("executeTransaction returns an error", func() {
				BeforeEach(func() {
					mockLib.mockedError = fmt.Errorf("fdb unavailable")
				})

				It("propagates the error", func() {
					Expect(lc.ReleaseLock()).To(MatchError("fdb unavailable"))
				})
			})
		})
	})

	Describe("AddPendingUpgrades", func() {
		It("delegates to executeTransaction", func() {
			version, err := fdbv1beta2.ParseFdbVersion("7.1.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(
				lc.AddPendingUpgrades(version, []fdbv1beta2.ProcessGroupID{"pod-1", "pod-2"}),
			).To(Succeed())
			Expect(mockLib.executeTransactionCallCount).To(Equal(1))
		})

		When("executeTransaction returns an error", func() {
			BeforeEach(func() {
				mockLib.mockedError = fmt.Errorf("fdb unavailable")
			})

			It("propagates the error", func() {
				version, err := fdbv1beta2.ParseFdbVersion("7.1.0")
				Expect(err).NotTo(HaveOccurred())
				Expect(
					lc.AddPendingUpgrades(version, []fdbv1beta2.ProcessGroupID{"pod-1"}),
				).To(MatchError("fdb unavailable"))
			})
		})
	})

	Describe("GetPendingUpgrades", func() {
		It("delegates to executeTransaction", func() {
			version, err := fdbv1beta2.ParseFdbVersion("7.1.0")
			Expect(err).NotTo(HaveOccurred())
			_, err = lc.GetPendingUpgrades(version)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockLib.executeTransactionCallCount).To(Equal(1))
		})

		When("executeTransaction returns an error", func() {
			BeforeEach(func() {
				mockLib.mockedError = fmt.Errorf("fdb unavailable")
			})

			It("propagates the error", func() {
				version, err := fdbv1beta2.ParseFdbVersion("7.1.0")
				Expect(err).NotTo(HaveOccurred())
				_, err = lc.GetPendingUpgrades(version)
				Expect(err).To(MatchError("fdb unavailable"))
			})
		})
	})

	Describe("ClearPendingUpgrades", func() {
		It("delegates to executeTransaction", func() {
			Expect(lc.ClearPendingUpgrades()).To(Succeed())
			Expect(mockLib.executeTransactionCallCount).To(Equal(1))
		})

		When("executeTransaction returns an error", func() {
			BeforeEach(func() {
				mockLib.mockedError = fmt.Errorf("fdb unavailable")
			})

			It("propagates the error", func() {
				Expect(lc.ClearPendingUpgrades()).To(MatchError("fdb unavailable"))
			})
		})
	})

	Describe("GetDenyList", func() {
		It("delegates to executeTransaction", func() {
			_, err := lc.GetDenyList()
			Expect(err).NotTo(HaveOccurred())
			Expect(mockLib.executeTransactionCallCount).To(Equal(1))
		})

		When("executeTransaction returns an error", func() {
			BeforeEach(func() {
				mockLib.mockedError = fmt.Errorf("fdb unavailable")
			})

			It("propagates the error", func() {
				_, err := lc.GetDenyList()
				Expect(err).To(MatchError("fdb unavailable"))
			})
		})
	})

	Describe("UpdateDenyList", func() {
		It("delegates to executeTransaction", func() {
			Expect(lc.UpdateDenyList([]fdbv1beta2.LockDenyListEntry{
				{ID: "test-operator", Allow: false},
			})).To(Succeed())
			Expect(mockLib.executeTransactionCallCount).To(Equal(1))
		})

		When("executeTransaction returns an error", func() {
			BeforeEach(func() {
				mockLib.mockedError = fmt.Errorf("fdb unavailable")
			})

			It("propagates the error", func() {
				Expect(lc.UpdateDenyList(nil)).To(MatchError("fdb unavailable"))
			})
		})
	})
})
