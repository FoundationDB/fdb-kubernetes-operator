/*
 * choose_removals_test.go
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

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("choose_removals", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var adminClient *mock.AdminClient
	var err error
	var requeue *requeue
	var removals []fdbv1beta2.ProcessGroupID

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = chooseRemovals{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		removals = nil
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.IsMarkedForRemoval() {
				removals = append(removals, processGroup.ProcessGroupID)
			}
		}

	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not mark any removals", func() {
			Expect(removals).To(BeNil())
		})
	})

	Context("with a decreased process count", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 3
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should mark one of the process groups for removal", func() {
			Expect(removals).To(HaveLen(1))
			Expect(string(removals[0])).To(ContainSubstring("storage"))
		})

		Context("with a process group already marked", func() {
			var processGroupID fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				processGroup := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
				processGroup.MarkForRemoval()
				Expect(
					clusterReconciler.updateOrApply(context.TODO(), cluster),
				).NotTo(HaveOccurred())
				processGroupID = processGroup.ProcessGroupID
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should leave that process group for removal", func() {
				Expect(removals).To(Equal([]fdbv1beta2.ProcessGroupID{processGroupID}))
			})
		})

		Context("with multiple processes on one rack", func() {
			var processGroupIDs []fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				processGroups := internal.PickProcessGroups(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					2,
				)
				adminClient.MockLocalityInfo(
					processGroups[0].ProcessGroupID,
					map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "r1"},
				)
				adminClient.MockLocalityInfo(
					processGroups[1].ProcessGroupID,
					map[string]string{fdbv1beta2.FDBLocalityZoneIDKey: "r1"},
				)

				processGroupIDs = []fdbv1beta2.ProcessGroupID{
					processGroups[0].ProcessGroupID,
					processGroups[1].ProcessGroupID,
				}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should mark one of the process groups for removal", func() {
				Expect(removals).To(HaveLen(1))
				Expect(removals[0]).To(BeElementOf(processGroupIDs))
			})
		})
	})

	Context("with a decrease to multiple process counts", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 3
			cluster.Spec.ProcessCounts.ClusterController = 0
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should mark two of the process groups for removal", func() {
			Expect(removals).To(HaveLen(2))
			Expect(string(removals[0])).To(ContainSubstring("cluster_controller"))
			Expect(string(removals[1])).To(ContainSubstring("storage"))
		})
	})
})
