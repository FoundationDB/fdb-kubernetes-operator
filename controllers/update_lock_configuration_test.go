/*
 * update_lock_configuration_test.go
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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("update_lock_configuration", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var lockClient *MockLockClient
	var err error
	var requeue *Requeue

	BeforeEach(func() {
		cluster = createDefaultCluster()
		cluster.Spec.InstanceIDPrefix = "dc1"
		var locksDisabled = false
		cluster.Spec.LockOptions.DisableLocks = &locksDisabled
		err = internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		lockClient = newMockLockClientUncast(cluster)
	})

	JustBeforeEach(func() {
		requeue = UpdateLockConfiguration{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		if requeue != nil {
			Expect(requeue.Error).NotTo(HaveOccurred())
		}
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should leave the lock status empty", func() {
			Expect(cluster.Status.Locks).To(Equal(fdbtypes.LockSystemStatus{}))
		})
	})

	Context("with an entry in the deny list", func() {
		BeforeEach(func() {
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, fdbtypes.LockDenyListEntry{ID: "dc2"})
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not update the deny list in the status", func() {
			Expect(cluster.Status.Locks.DenyList).To(BeNil())
		})

		It("should update the deny list in the lock client", func() {
			list, err := lockClient.GetDenyList()
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(Equal([]string{"dc2"}))
		})
	})

	Context("with an entry to remove from the deny list", func() {
		BeforeEach(func() {
			err = lockClient.UpdateDenyList([]fdbtypes.LockDenyListEntry{{ID: "dc2"}, {ID: "dc3"}})
			Expect(err).NotTo(HaveOccurred())
			cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, fdbtypes.LockDenyListEntry{ID: "dc2", Allow: true})
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should update the deny list in the lock client", func() {
			list, err := lockClient.GetDenyList()
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(Equal([]string{"dc3"}))
		})
	})
})
