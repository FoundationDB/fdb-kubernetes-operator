/*
 * add_process_groups_test.go
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
	"sort"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("add_process_groups", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error
	var shouldContinue bool
	var initialProcessCounts fdbtypes.ProcessCounts
	var newProcessCounts fdbtypes.ProcessCounts

	BeforeEach(func() {
		cluster = createDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialProcessCounts = fdbtypes.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true)
	})

	JustBeforeEach(func() {
		shouldContinue, err = AddProcessGroups{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		newProcessCounts = fdbtypes.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true)

	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(shouldContinue).To(BeTrue())
		})

		It("should not change the process counts", func() {
			Expect(newProcessCounts).To(Equal(initialProcessCounts))
		})
	})

	Context("with a storage process group marked for removal", func() {
		BeforeEach(func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessGroupID == "storage-4" {
					processGroup.Remove = true
				}
			}
		})

		It("should add a storage process", func() {
			storageProcesses := make([]string, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == "storage" {
					storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
				}
			}
			sort.Strings(storageProcesses)
			Expect(storageProcesses).To(Equal([]string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
			}))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})
	})

	Context("when replacing a process with a different instance ID prefix", func() {
		BeforeEach(func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessGroupID == "storage-4" {
					processGroup.ProcessGroupID = "old-prefix-storage-4"
					processGroup.Remove = true
				}
			}
		})

		It("should add a storage process", func() {
			storageProcesses := make([]string, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == "storage" {
					storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
				}
			}
			sort.Strings(storageProcesses)
			Expect(storageProcesses).To(Equal([]string{
				"old-prefix-storage-4",
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-5",
			}))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})
	})

	Context("with an increase to the desired storage count", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage += 2
		})

		It("should not requeue", func() {
			Expect(shouldContinue).To(BeTrue())
		})

		It("should add storage processes", func() {
			storageProcesses := make([]string, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == "storage" {
					storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
				}
			}
			sort.Strings(storageProcesses)
			Expect(storageProcesses).To(Equal([]string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
				"storage-6",
			}))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})

		Context("with a gap in the process numbers", func() {
			BeforeEach(func() {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-4" {
						processGroup.ProcessGroupID = "storage-7"
					}
				}
			})

			It("should fill in the gap", func() {
				storageProcesses := make([]string, 0, newProcessCounts.Storage)
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass == "storage" {
						storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
					}
				}
				sort.Strings(storageProcesses)
				Expect(storageProcesses).To(Equal([]string{
					"storage-1",
					"storage-2",
					"storage-3",
					"storage-4",
					"storage-5",
					"storage-7",
				}))
			})
		})
	})
})
