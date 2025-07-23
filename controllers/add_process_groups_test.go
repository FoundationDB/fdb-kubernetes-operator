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
	"fmt"

	"k8s.io/utils/ptr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("add_process_groups", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var initialProcessCounts fdbv1beta2.ProcessCounts
	var newProcessCounts fdbv1beta2.ProcessCounts

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialProcessCounts = fdbv1beta2.CreateProcessCountsFromProcessGroupStatus(
			cluster.Status.ProcessGroups,
			true,
		)
	})

	JustBeforeEach(func() {
		requeue = addProcessGroups{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
		if requeue != nil {
			Expect(requeue.curError).NotTo(HaveOccurred())
		}

		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		newProcessCounts = fdbv1beta2.CreateProcessCountsFromProcessGroupStatus(
			cluster.Status.ProcessGroups,
			true,
		)
	})

	When("the cluster is reconciled", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not change the process counts", func() {
			Expect(newProcessCounts).To(Equal(initialProcessCounts))
		})
	})

	When("a storage process group is marked for removal", func() {
		var removedProcessGroup fdbv1beta2.ProcessGroupID

		BeforeEach(func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
					processGroup.MarkForRemoval()
					removedProcessGroup = processGroup.ProcessGroupID
					break
				}
			}
		})

		It("should add a storage process", func() {
			storageProcesses := make([]fdbv1beta2.ProcessGroupID, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				if processGroup.IsMarkedForRemoval() {
					continue
				}

				storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
			}

			Expect(storageProcesses).NotTo(ContainElements(removedProcessGroup))
			Expect(storageProcesses).To(HaveLen(initialProcessCounts.Storage))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})
	})

	When("replacing a process with a different process group ID prefix", func() {
		var removedProcessGroup fdbv1beta2.ProcessGroupID

		BeforeEach(func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
					processGroup.MarkForRemoval()
					processGroup.ProcessGroupID = "old-" + processGroup.ProcessGroupID
					removedProcessGroup = processGroup.ProcessGroupID
					break
				}
			}
		})

		It("should add a storage process", func() {
			storageProcesses := make([]fdbv1beta2.ProcessGroupID, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				if processGroup.IsMarkedForRemoval() {
					continue
				}

				storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
			}

			Expect(storageProcesses).NotTo(ContainElements(removedProcessGroup))
			Expect(storageProcesses).To(HaveLen(initialProcessCounts.Storage))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})
	})

	Context("with an increase to the desired storage count", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage += 2
			Expect(k8sClient.Update(context.Background(), cluster)).To(Succeed())
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should add storage processes", func() {
			storageProcesses := make([]fdbv1beta2.ProcessGroupID, 0, newProcessCounts.Storage)
			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
					storageProcesses = append(storageProcesses, processGroup.ProcessGroupID)
				}
			}

			Expect(storageProcesses).To(HaveLen(cluster.Spec.ProcessCounts.Storage))
		})

		It("should not change the log or stateless processes", func() {
			Expect(newProcessCounts.Log).To(Equal(initialProcessCounts.Log))
			Expect(newProcessCounts.Stateless).To(Equal(initialProcessCounts.Stateless))
		})
	})

	When("a new processGroup is created", func() {
		var processGroupStatus *fdbv1beta2.ProcessGroupStatus

		BeforeEach(func() {
			processGroupStatus = fdbv1beta2.NewProcessGroupStatus(
				"1337",
				fdbv1beta2.ProcessClassStorage,
				[]string{"1.1.1.1"},
			)
		})

		It("should have the missing conditions", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(3))
			Expect(
				processGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType,
			).To(Equal(fdbv1beta2.MissingProcesses))
			Expect(
				processGroupStatus.ProcessGroupConditions[1].ProcessGroupConditionType,
			).To(Equal(fdbv1beta2.MissingPod))
			Expect(
				processGroupStatus.ProcessGroupConditions[2].ProcessGroupConditionType,
			).To(Equal(fdbv1beta2.MissingPVC))
		})
	})

	When("multiple process groups should be created", func() {
		var storageProcessCount int

		BeforeEach(func() {
			fdbv1beta2.MaxProcessGroupIDNum = 1000
			storageProcessCount = 500
			cluster.Spec.ProcessCounts.Storage = storageProcessCount
			cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsLocalityBasedExclusions71.String()
		})

		When("no exclusions are present", func() {
			It("should not create duplicate entries", func() {
				storageProcessGroups := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
						continue
					}

					storageProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
				}

				Expect(storageProcessGroups).To(HaveLen(storageProcessCount))
			})
		})

		When("exclusions are present", func() {
			var excludedProcessGroupIDs map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None

			BeforeEach(func() {
				cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(true)
				adminClient, err := mock.NewMockAdminClient(cluster, k8sClient)
				Expect(err).To(Succeed())

				currentProcessGroupIDs := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
				for _, processGroup := range cluster.Status.ProcessGroups {
					currentProcessGroupIDs[processGroup.ProcessGroupID] = fdbv1beta2.None{}
				}

				excludedCnt := 100
				exclusions := make([]fdbv1beta2.ProcessAddress, 0, excludedCnt)
				excludedProcessGroupIDs = map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
				for i := 0; i < excludedCnt; i++ {
					processGroupID := fdbv1beta2.ProcessGroupID(fmt.Sprintf("storage-%d", i))
					if _, ok := currentProcessGroupIDs[processGroupID]; ok {
						continue
					}
					excludedProcessGroupIDs[processGroupID] = fdbv1beta2.None{}
					exclusions = append(
						exclusions,
						fdbv1beta2.ProcessAddress{
							StringAddress: fmt.Sprintf(
								"%s:%s",
								fdbv1beta2.FDBLocalityExclusionPrefix,
								processGroupID,
							),
						},
					)
				}

				Expect(adminClient.ExcludeProcesses(exclusions)).To(Succeed())
			})

			It(
				"should not create duplicate entries and should not pick any entry from the exclusion list",
				func() {
					storageProcessGroups := make(
						[]fdbv1beta2.ProcessGroupID,
						0,
						len(cluster.Status.ProcessGroups),
					)
					for _, processGroup := range cluster.Status.ProcessGroups {
						if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
							continue
						}

						Expect(excludedProcessGroupIDs).NotTo(HaveKey(processGroup.ProcessGroupID))
						storageProcessGroups = append(
							storageProcessGroups,
							processGroup.ProcessGroupID,
						)
					}

					Expect(storageProcessGroups).To(HaveLen(storageProcessCount))
				},
			)
		})
	})
})
