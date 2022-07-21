/*
 * remove_incompatible_processes_tests.go
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

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

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
		var initialCount int

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

			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			initialCount = len(pods.Items)
		})

		JustBeforeEach(func() {
			err := processIncompatibleProcesses(context.TODO(), clusterReconciler, logr.Discard(), cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		When("no incompatible processes are reported", func() {
			BeforeEach(func() {
				clusterReconciler.EnableRestartIncompatibleProcesses = true
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.frozenStatus = &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						IncompatibleConnections: []string{},
					},
				}
			})

			It("should have no deletions", func() {
				pods := &corev1.PodList{}
				err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(BeNumerically("==", initialCount))
			})
		})

		When("no matching incompatible processes are reported", func() {
			BeforeEach(func() {
				clusterReconciler.EnableRestartIncompatibleProcesses = true
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

			It("should have no deletions", func() {
				pods := &corev1.PodList{}
				err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(BeNumerically("==", initialCount))
			})
		})

		When("matching incompatible processes are reported and the subreconciler is enabled", func() {
			BeforeEach(func() {
				clusterReconciler.EnableRestartIncompatibleProcesses = true
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

			It("should have one deletion", func() {
				pods := &corev1.PodList{}
				err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(BeNumerically("==", initialCount-1))
			})
		})

		When("matching incompatible processes are reported and the subreconciler is disabled", func() {
			BeforeEach(func() {
				clusterReconciler.EnableRestartIncompatibleProcesses = false
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

			It("should have no deletions", func() {
				pods := &corev1.PodList{}
				err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(BeNumerically("==", initialCount))
			})
		})
	})
})
