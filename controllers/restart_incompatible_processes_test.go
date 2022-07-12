/*
 * restart_incompatible_processes_tests.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("restart_incompatible_pods", func() {
	DescribeTable("", func(incompatibleConnections map[string]fdbv1beta2.None, processGroup *fdbv1beta2.ProcessGroupStatus, expected bool) {
		Expect(isIncompatible(incompatibleConnections, processGroup)).To(Equal(expected))
	},
		Entry("empty incompatible map",
			map[string]fdbv1beta2.None{},
			&fdbv1beta2.ProcessGroupStatus{
				Addresses: []string{"1.1.1.1"},
			},
			false),
		Entry("nil incompatible map",
			nil,
			&fdbv1beta2.ProcessGroupStatus{
				Addresses: []string{"1.1.1.1"},
			},
			false),
		Entry("incompatible map contains another address",
			map[string]fdbv1beta2.None{
				"1.1.1.2": {},
			},
			&fdbv1beta2.ProcessGroupStatus{
				Addresses: []string{"1.1.1.1"},
			},
			false),
		Entry("incompatible map contains matching address",
			map[string]fdbv1beta2.None{
				"1.1.1.1": {},
			},
			&fdbv1beta2.ProcessGroupStatus{
				Addresses: []string{"1.1.1.1"},
			},
			true),
	)

	When("running a reconcile for the restart incompatible process reconciler", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var requeue *requeue
		var err error

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			err := k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
		})

		JustBeforeEach(func() {
			requeue = restartIncompatibleProcesses{}.reconcile(context.TODO(), clusterReconciler, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		When("no incompatible processes are reported", func() {
			BeforeEach(func() {
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.frozenStatus = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						IncompatibleConnections: []string{},
					},
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})
		})

		When("no matching incompatible processes are reported", func() {
			BeforeEach(func() {
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.frozenStatus = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						IncompatibleConnections: []string{
							"192.192.192.192",
						},
					},
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})
		})

		When("matching incompatible processes are reported", func() {
			BeforeEach(func() {
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.frozenStatus = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						IncompatibleConnections: []string{
							cluster.Status.ProcessGroups[0].Addresses[0],
						},
					},
				}
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.delay).To(BeNumerically("==", 15*time.Second))
			})
		})
	})
})
