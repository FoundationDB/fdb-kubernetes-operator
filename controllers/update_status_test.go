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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("update_status", func() {
	Context("validate process group", func() {
		var cluster *fdbtypes.FoundationDBCluster
		var configMap *corev1.ConfigMap
		var adminClient *mockAdminClient
		var pods []*corev1.Pod
		var processMap map[string][]fdbtypes.FoundationDBStatusProcessInfo
		var err error

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			err = setupClusterForTest(cluster)
			Expect(err).NotTo(HaveOccurred())

			pods, err = clusterReconciler.PodLifecycleManager.GetPods(clusterReconciler, cluster, context.TODO(), internal.GetSinglePodListOptions(cluster, "storage-1")...)
			Expect(err).NotTo(HaveOccurred())

			for _, container := range pods[0].Spec.Containers {
				pods[0].Status.ContainerStatuses = append(pods[0].Status.ContainerStatuses, corev1.ContainerStatus{Ready: true, Name: container.Name})
			}

			adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			configMap = &corev1.ConfigMap{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-config"}, configMap)
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			databaseStatus, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
			processMap = make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)
			for _, process := range databaseStatus.Cluster.Processes {
				processID, ok := process.Locality["process_id"]
				// if the processID is not set we fall back to the instanceID
				if !ok {
					processID = process.Locality["instance_id"]
				}
				processMap[processID] = append(processMap[processID], process)
			}
		})

		When("process group has no Pod", func() {
			It("should be added to the failing Pods", func() {
				processGroupStatus := fdbtypes.NewProcessGroupStatus("storage-1337", fdbtypes.ProcessClassStorage, []string{"1.1.1.1"})
				// Reset the status to only tests for the missing Pod
				processGroupStatus.ProcessGroupConditions = []*fdbtypes.ProcessGroupCondition{}
				_, err := validateProcessGroup(clusterReconciler, context.TODO(), cluster, nil, "", processGroupStatus)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbtypes.MissingPod))
			})
		})

		When("a process group is fine", func() {
			It("should not get any condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroupStatus[0].ProcessGroupConditions)).To(Equal(0))
			})
		})

		When("the pod for the process group is missing", func() {
			It("should get a condition assigned", func() {
				err = k8sClient.Delete(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())

				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.MissingPod, false)
				Expect(missingProcesses).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("a process group has the wrong command line", func() {
			BeforeEach(func() {
				adminClient.MockIncorrectCommandLine("storage-1", true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				incorrectProcesses := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("a process group is not reporting to the cluster", func() {
			BeforeEach(func() {
				adminClient.MockMissingProcessGroup("storage-1", true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.MissingProcesses, false)
				Expect(missingProcesses).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod has the wrong spec", func() {
			BeforeEach(func() {
				pods[0].ObjectMeta.Annotations[fdbtypes.LastSpecKey] = "bad"
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				incorrectPods := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.IncorrectPodSpec, false)
				Expect(incorrectPods).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
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
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.PodFailing, false)
				Expect(failingPods).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
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
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.PodFailing, false)
				Expect(failingPods).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("adding a process group to the InstancesToRemove list", func() {
			var removedProcessGroup string

			BeforeEach(func() {
				removedProcessGroup = "storage-1"
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.InstancesToRemove = []string{removedProcessGroup}
			})

			It("should mark the process group for removal", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == removedProcessGroup {
						Expect(processGroup.Remove).To(BeTrue())
						Expect(processGroup.ExclusionSkipped).To(BeFalse())
						removalCount++
						continue
					}

					Expect(processGroup.Remove).To(BeFalse())
				}

				Expect(removalCount).To(BeNumerically("==", 1))
			})
		})

		When("adding a process group to the ProcessGroupsToRemove list", func() {
			var removedProcessGroup string

			BeforeEach(func() {
				removedProcessGroup = "storage-1"
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemove = []string{removedProcessGroup}
			})

			It("should mark the process group for removal", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == removedProcessGroup {
						Expect(processGroup.Remove).To(BeTrue())
						Expect(processGroup.ExclusionSkipped).To(BeFalse())
						removalCount++
						continue
					}

					Expect(processGroup.Remove).To(BeFalse())
				}

				Expect(removalCount).To(BeNumerically("==", 1))
			})
		})

		When("adding a process group to the InstancesToRemoveWithoutExclusion list", func() {
			var removedProcessGroup string

			BeforeEach(func() {
				removedProcessGroup = "storage-1"
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.InstancesToRemoveWithoutExclusion = []string{removedProcessGroup}
			})

			It("should be mark the process group for removal without exclusion", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == removedProcessGroup {
						Expect(processGroup.Remove).To(BeTrue())
						Expect(processGroup.ExclusionSkipped).To(BeTrue())
						removalCount++
						continue
					}

					Expect(processGroup.Remove).To(BeFalse())
				}

				Expect(removalCount).To(BeNumerically("==", 1))
			})
		})

		When("adding a process group to the ProcessGroupsToRemoveWithoutExclusion list", func() {
			var removedProcessGroup string

			BeforeEach(func() {
				removedProcessGroup = "storage-1"
				pods[0].Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []string{removedProcessGroup}
			})

			It("should be mark the process group for removal without exclusion", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == removedProcessGroup {
						Expect(processGroup.Remove).To(BeTrue())
						Expect(processGroup.ExclusionSkipped).To(BeTrue())
						removalCount++
						continue
					}

					Expect(processGroup.Remove).To(BeFalse())
				}

				Expect(removalCount).To(BeNumerically("==", 1))
			})
		})

		When("one process group is not reachable", func() {
			var unreachableProcessGroup string

			// Must be a JustBeforeEach otherwise the cluster status will result in an error
			JustBeforeEach(func() {
				unreachableProcessGroup = podmanager.GetProcessGroupID(cluster, pods[0])
				pods[0].Annotations[internal.MockUnreachableAnnotation] = "banana"
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should mark the process group as unreachable", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				unreachableCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == unreachableProcessGroup {
						Expect(processGroup.GetConditionTime(fdbtypes.SidecarUnreachable)).NotTo(BeNil())
						unreachableCount++
						continue
					}
					Expect(processGroup.GetConditionTime(fdbtypes.SidecarUnreachable)).To(BeNil())
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
					processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
					Expect(err).NotTo(HaveOccurred())

					unreachableCount := 0
					for _, processGroup := range processGroupStatus {
						if processGroup.GetConditionTime(fdbtypes.SidecarUnreachable) != nil {
							unreachableCount++
							continue
						}
						Expect(processGroup.GetConditionTime(fdbtypes.SidecarUnreachable)).To(BeNil())
					}

					Expect(unreachableCount).To(BeNumerically("==", 0))
				})
			})
		})

		When("a Pod is stuck in Pending", func() {
			var pendingProcessGroup string

			BeforeEach(func() {
				pendingProcessGroup = podmanager.GetProcessGroupID(cluster, pods[0])
				pods[0].Status.Phase = corev1.PodPending
				err = k8sClient.Update(context.TODO(), pods[0])
				Expect(err).NotTo(HaveOccurred())
			})

			It("should mark the process group as Pod pending", func() {
				processGroupStatus, err := validateProcessGroups(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, configMap)
				Expect(err).NotTo(HaveOccurred())

				pendingCount := 0
				for _, processGroup := range processGroupStatus {
					if processGroup.ProcessGroupID == pendingProcessGroup {
						Expect(processGroup.GetConditionTime(fdbtypes.PodPending)).NotTo(BeNil())
						pendingCount++
						continue
					}

					Expect(processGroup.Remove).To(BeFalse())
				}

				Expect(pendingCount).To(BeNumerically("==", 1))
			})
		})
	})

	When("removing duplicated entries in process group status", func() {
		var status fdbtypes.FoundationDBClusterStatus

		BeforeEach(func() {
			status = fdbtypes.FoundationDBClusterStatus{
				ProcessGroups: []*fdbtypes.ProcessGroupStatus{
					{
						ProcessGroupID: "storage-1",
						ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
							fdbtypes.NewProcessGroupCondition(fdbtypes.PodPending),
							fdbtypes.NewProcessGroupCondition(fdbtypes.ResourcesTerminating),
						},
					},
					{
						ProcessGroupID: "storage-2",
						ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
							fdbtypes.NewProcessGroupCondition(fdbtypes.PodPending),
							fdbtypes.NewProcessGroupCondition(fdbtypes.PodPending),
							fdbtypes.NewProcessGroupCondition(fdbtypes.SidecarUnreachable),
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
		var cluster *fdbtypes.FoundationDBCluster
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

		When("enabling an explicit listen address", func() {
			BeforeEach(func() {
				enabled := false
				cluster.Spec.UseExplicitListenAddress = &enabled
				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				enabled = true
				cluster.Spec.UseExplicitListenAddress = &enabled
			})

			Context("when the cluster has not been reconciled", func() {
				It("should report that pods do not have listen IPs", func() {
					Expect(cluster.Status.HasListenIPsForAllPods).To(BeFalse())
				})
			})
		})
	})
})
