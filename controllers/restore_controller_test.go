/*
 * restore_controller_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

func reloadRestore(client client.Client, backup *fdbtypes.FoundationDBRestore) error {
	return k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, backup)
}

var _ = Describe("restore_controller", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var restore *fdbtypes.FoundationDBRestore
	var adminClient *MockAdminClient
	var err error

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createDefaultCluster()
		restore = createDefaultRestore(cluster)
		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Reconciliation", func() {
		var timeout time.Duration

		BeforeEach(func() {
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			timeout = time.Second * 5
			Eventually(func() (int64, error) {
				return reloadCluster(k8sClient, cluster)
			}, timeout).ShouldNot(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), restore)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() (bool, error) {
				err := reloadRestore(k8sClient, restore)
				if err != nil {
					return false, err
				}
				return restore.Status.Running, nil
			}, timeout).Should(BeTrue())

			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanupCluster(cluster)
			cleanupRestore(restore)
		})

		Context("when reconciling a new restore", func() {
			It("should start a restore", func() {
				status, err := adminClient.GetRestoreStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal("blobstore://test@test-service/test-backup?bucket=fdb-backups\n"))
			})
		})
	})
})
