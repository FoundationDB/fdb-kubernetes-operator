/*
 * update_status_test.go
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
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("update_status", func() {
	Context("validate process group", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var configMap *corev1.ConfigMap
		var adminClient *mock.AdminClient
		var pods []*corev1.Pod
		var processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo
		var err error
		var allPods []*corev1.Pod
		var allPvcs *corev1.PersistentVolumeClaimList

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			err = setupClusterForTest(cluster)
			Expect(err).NotTo(HaveOccurred())

			pods, err = clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), clusterReconciler, cluster, internal.GetSinglePodListOptions(cluster, "storage-1")...)
			Expect(err).NotTo(HaveOccurred())

			for _, container := range pods[0].Spec.Containers {
				pods[0].Status.ContainerStatuses = append(pods[0].Status.ContainerStatuses, corev1.ContainerStatus{Ready: true, Name: container.Name})
			}

			adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			configMap = &corev1.ConfigMap{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-config"}, configMap)
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			databaseStatus, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
			processMap = make(map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo)
			for _, process := range databaseStatus.Cluster.Processes {
				processID, ok := process.Locality["process_id"]
				// if the processID is not set we fall back to the instanceID
				if !ok {
					processID = process.Locality["instance_id"]
				}
				processMap[fdbv1beta2.ProcessGroupID(processID)] = append(processMap[fdbv1beta2.ProcessGroupID(processID)], process)
			}

			allPods, err = clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), clusterReconciler, cluster, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())

			allPvcs = &corev1.PersistentVolumeClaimList{}
			err = clusterReconciler.List(context.TODO(), allPvcs, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())
		})

		When("process group has no Pod", func() {
			It("should be added to the failing Pods", func() {
				processGroupStatus := fdbv1beta2.NewProcessGroupStatus("storage-1337", fdbv1beta2.ProcessClassStorage, []string{"1.1.1.1"})
				// Reset the status to only tests for the missing Pod
				processGroupStatus.ProcessGroupConditions = []*fdbv1beta2.ProcessGroupCondition{}
				err := validateProcessGroup(context.TODO(), clusterReconciler, cluster, nil, nil, "", processGroupStatus)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.MissingPod))
			})
		})

		When("a process group is fine", func() {
			It("should not get any condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroupStatus[0].ProcessGroupConditions)).To(Equal(0))
				Expect(processGroup.LocalityDataHall).To(Equal(""))
			})
		})

		When("a process group is fine and data hall is replication enabled", func() {
			BeforeEach(func() {
				cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall
				cluster.Spec.Localities = []fdbv1beta2.Locality{
					{
						Key: "data_hall",
						NodeSelectors: [][]string{
							{"data_hall", "az-1"},
							{"data_hall", "az-2"},
							{"data_hall", "az-3"},
						},
					},
				}

				mock.ClearMockAdminClients()
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			JustBeforeEach(func() {
				databaseStatus, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				processMap = make(map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo)
				for _, process := range databaseStatus.Cluster.Processes {
					processID, ok := process.Locality["process_id"]
					// if the processID is not set we fall back to the instanceID
					if !ok {
						processID = process.Locality["instance_id"]
					}
					process.Locality[fdbv1beta2.FDBLocalityDataHallKey] = "az-1"
					processMap[fdbv1beta2.ProcessGroupID(processID)] = append(processMap[fdbv1beta2.ProcessGroupID(processID)], process)
				}
			})

			It("should get locality data hall assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.LocalityDataHall).To(BeElementOf(
					"az-1",
					"az-2",
					"az-3",
				))
			})
		})

		When("the pod for the process group is missing", func() {
			BeforeEach(func() {
				Expect(k8sClient.Delete(context.TODO(), pods[0])).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.MissingPod, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("a process group has the wrong command line", func() {
			BeforeEach(func() {
				adminClient.MockIncorrectCommandLine("storage-1", true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				incorrectProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("a process group is not reporting to the cluster", func() {
			BeforeEach(func() {
				adminClient.MockMissingProcessGroup("storage-1", true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.MissingProcesses, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod has the wrong spec", func() {
			BeforeEach(func() {
				pods[0].ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "bad"
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				incorrectPods := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.IncorrectPodSpec, false)
				Expect(incorrectPods).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the Pod is marked for deletion but still reporting to the cluster", func() {
			BeforeEach(func() {
				// We cannot use the k8sClient.MockStuckTermination() method because the deletionTimestamp must be
				// older than 5 minutes to detect the Pod as PodFailed.
				stuckPod := pods[0]
				stuckPod.SetDeletionTimestamp(&metav1.Time{Time: time.Now().Add(-10 * time.Minute)})
				stuckPod.SetFinalizers(append(stuckPod.GetFinalizers(), "foundationdb.org/testing"))
				Expect(k8sClient.Update(context.Background(), stuckPod)).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				// Ensure that we have the same number of Pods and processes
				Expect(allPods).To(HaveLen(len(processMap)))
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.PodFailing, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod is failing to launch", func() {
			BeforeEach(func() {
				pods[0].Status.ContainerStatuses[0].Ready = false
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.PodFailing, false)
				Expect(failingPods).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod is failed", func() {
			BeforeEach(func() {
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.PodFailing, false)
				Expect(failingPods).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("adding a process group to the ProcessGroupsToRemove list", func() {
			var removedProcessGroup fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				removedProcessGroup = "storage-1"
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{removedProcessGroup}
			})

			It("should mark the process group for removal", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == removedProcessGroup {
						Expect(processGroup.IsMarkedForRemoval()).To(BeTrue())
						Expect(processGroup.ExclusionSkipped).To(BeFalse())
						removalCount++
						continue
					}

					Expect(processGroup.IsMarkedForRemoval()).To(BeFalse())
				}

				Expect(removalCount).To(BeNumerically("==", 1))
			})
		})

		When("adding a process group to the ProcessGroupsToRemoveWithoutExclusion list", func() {
			var removedProcessGroup fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				removedProcessGroup = "storage-1"
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []fdbv1beta2.ProcessGroupID{removedProcessGroup}
			})

			It("should be mark the process group for removal without exclusion", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == removedProcessGroup {
						Expect(processGroup.IsMarkedForRemoval()).To(BeTrue())
						Expect(processGroup.ExclusionSkipped).To(BeTrue())
						removalCount++
						continue
					}

					Expect(processGroup.IsMarkedForRemoval()).To(BeFalse())
				}

				Expect(removalCount).To(BeNumerically("==", 1))
			})
		})

		When("one process group is not reachable", func() {
			var unreachableProcessGroup fdbv1beta2.ProcessGroupID

			// Must be a JustBeforeEach otherwise the cluster status will result in an error
			JustBeforeEach(func() {
				unreachablePod := pods[0]
				unreachableProcessGroup = podmanager.GetProcessGroupID(cluster, pods[0])
				unreachablePod.Annotations[internal.MockUnreachableAnnotation] = "banana"
				Expect(k8sClient.Update(context.TODO(), unreachablePod)).NotTo(HaveOccurred())

				// Update the Pod in out list, since we fetch the list before we set the annotation
				for idx, pod := range allPods {
					if pod.Name != unreachablePod.Name {
						continue
					}

					allPods[idx] = unreachablePod
				}
			})

			It("should mark the process group as unreachable", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				unreachableCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == unreachableProcessGroup {
						Expect(processGroup.GetConditionTime(fdbv1beta2.SidecarUnreachable)).NotTo(BeNil())
						unreachableCount++
						continue
					}
					Expect(processGroup.GetConditionTime(fdbv1beta2.SidecarUnreachable)).To(BeNil())
				}

				Expect(unreachableCount).To(BeNumerically("==", 1))
			})

			When("the process group is reachable again", func() {
				JustBeforeEach(func() {
					delete(pods[0].Annotations, internal.MockUnreachableAnnotation)
					err = k8sClient.Update(context.TODO(), pods[0])
					Expect(err).NotTo(HaveOccurred())
				})

				It("should remove the condition", func() {
					processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
					Expect(err).NotTo(HaveOccurred())

					unreachableCount := 0
					for _, processGroup := range processGroupStatus {
						if processGroup.GetConditionTime(fdbv1beta2.SidecarUnreachable) != nil {
							unreachableCount++
							continue
						}
						Expect(processGroup.GetConditionTime(fdbv1beta2.SidecarUnreachable)).To(BeNil())
					}

					Expect(unreachableCount).To(BeNumerically("==", 0))
				})
			})
		})

		When("a Pod is stuck in Pending", func() {
			var pendingProcessGroup fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				pendingProcessGroup = podmanager.GetProcessGroupID(cluster, pods[0])
				pods[0].Status.Phase = corev1.PodPending
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should mark the process group as Pod pending", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPods, allPvcs)
				Expect(err).NotTo(HaveOccurred())

				pendingCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == pendingProcessGroup {
						Expect(processGroup.GetConditionTime(fdbv1beta2.PodPending)).NotTo(BeNil())
						pendingCount++
						continue
					}

					Expect(processGroup.IsMarkedForRemoval()).To(BeFalse())
				}

				Expect(pendingCount).To(BeNumerically("==", 1))
			})
		})
	})

	When("removing duplicated entries in process group status", func() {
		var status fdbv1beta2.FoundationDBClusterStatus

		BeforeEach(func() {
			status = fdbv1beta2.FoundationDBClusterStatus{
				ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
					{
						ProcessGroupID: "storage-1",
						ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
							fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending),
							fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.ResourcesTerminating),
						},
					},
					{
						ProcessGroupID: "storage-2",
						ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
							fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending),
							fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending),
							fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.SidecarUnreachable),
						},
					},
				},
			}
		})

		When("running the remove duplicate method", func() {
			BeforeEach(func() {
				removeDuplicateConditions(status)
			})

			It("should remove duplicated entries", func() {
				Expect(len(status.ProcessGroups)).To(BeNumerically("==", 2))

				for _, pg := range status.ProcessGroups {
					if pg.ProcessGroupID == "storage-1" {
						Expect(len(pg.ProcessGroupConditions)).To(BeNumerically("==", 1))
						continue
					}

					if pg.ProcessGroupID == "storage-2" {
						Expect(len(pg.ProcessGroupConditions)).To(BeNumerically("==", 2))
						continue
					}

					Fail("unchecked process group")
				}
			})
		})
	})

	Describe("Reconcile", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var err error
		var requeue *requeue

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
		})

		JustBeforeEach(func() {
			err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
			requeue = updateStatus{}.reconcile(context.TODO(), clusterReconciler, cluster)
			if requeue != nil {
				Expect(requeue.curError).NotTo(HaveOccurred())
			}
			_, err = reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should mark the cluster as reconciled", func() {
			Expect(cluster.Status.Generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
		})

		When("disabling an explicit listen address", func() {
			BeforeEach(func() {
				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
				cluster.Spec.UseExplicitListenAddress = pointer.Bool(false)
			})

			Context("when the cluster has not been reconciled", func() {
				It("should report that pods do not have listen IPs", func() {
					Expect(cluster.Status.HasListenIPsForAllPods).To(BeFalse())
				})
			})
		})
	})

	DescribeTable("when getting the running version from the running processes", func(versionMap map[string]int, fallback string, expected string) {
		Expect(getRunningVersion(versionMap, fallback)).To(Equal(expected))
	},
		Entry("when nearly all processes running on the new version", map[string]int{
			"7.1.11": 1,
			"7.1.15": 99,
		}, "0", "7.1.15"),
		Entry("when all processes running on the same version", map[string]int{
			"7.1.15": 100,
		}, "0", "7.1.15"),
		Entry("when half of the processes running the old/new version", map[string]int{
			"7.1.11": 50,
			"7.1.15": 50,
		}, "0", "7.1.15"),
		Entry("when the versionMap is empty", map[string]int{}, "7.1.15", "7.1.15"))
})
