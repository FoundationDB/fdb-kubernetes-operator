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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("add_process_groups", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error
	var requeue *requeue
	var initialProcessCounts fdbtypes.ProcessCounts
	var newProcessCounts fdbtypes.ProcessCounts

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
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
		requeue = addProcessGroups{}.reconcile(context.TODO(), clusterReconciler, cluster)
		if requeue != nil {
			Expect(requeue.curError).NotTo(HaveOccurred())
		}

		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		newProcessCounts = fdbtypes.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true)

	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not change the process counts", func() {
			Expect(newProcessCounts).To(Equal(initialProcessCounts))
		})
	})

	Context("with a storage process group marked for removal", func() {
		BeforeEach(func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessGroupID == "storage-4" {
					processGroup.MarkForRemoval()
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
			expectedStorageProcesses := []string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
			}
			Expect(len(storageProcesses)).To(BeNumerically("==", len(expectedStorageProcesses)))
			Expect(storageProcesses).To(ContainElements(expectedStorageProcesses))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})
	})

	Context("when replacing a process with a different process group ID prefix", func() {
		BeforeEach(func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessGroupID == "storage-4" {
					processGroup.ProcessGroupID = "old-prefix-storage-4"
					processGroup.MarkForRemoval()
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
			expectedStorageProcesses := []string{
				"old-prefix-storage-4",
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-5",
			}
			Expect(len(storageProcesses)).To(BeNumerically("==", len(expectedStorageProcesses)))
			Expect(storageProcesses).To(ContainElements(expectedStorageProcesses))
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
			Expect(requeue).To(BeNil())
		})

		It("should add storage processes", func() {
			storageProcesses := make([]string, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == "storage" {
					storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
				}
			}
			expectedStorageProcesses := []string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
				"storage-6",
			}
			Expect(len(storageProcesses)).To(BeNumerically("==", len(expectedStorageProcesses)))
			Expect(storageProcesses).To(ContainElements(expectedStorageProcesses))
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
				expectedStorageProcesses := []string{
					"storage-1",
					"storage-2",
					"storage-3",
					"storage-4",
					"storage-5",
					"storage-7",
				}
				Expect(len(storageProcesses)).To(BeNumerically("==", len(expectedStorageProcesses)))
				Expect(storageProcesses).To(ContainElements(expectedStorageProcesses))
			})
		})
	})

	When("a new processGroup is created", func() {
		var processGroupStatus *fdbtypes.ProcessGroupStatus

		BeforeEach(func() {
			processGroupStatus = fdbtypes.NewProcessGroupStatus("1337", fdbtypes.ProcessClassStorage, []string{"1.1.1.1"})
		})

		It("should have the missing conditions", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(3))
			Expect(processGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbtypes.MissingProcesses))
			Expect(processGroupStatus.ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbtypes.MissingPod))
			Expect(processGroupStatus.ProcessGroupConditions[2].ProcessGroupConditionType).To(Equal(fdbtypes.MissingPVC))
		})
	})
})
