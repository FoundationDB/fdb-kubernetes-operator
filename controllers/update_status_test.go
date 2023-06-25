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

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

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
	var storageOneProcessGroupID fdbv1beta2.ProcessGroupID = "storage-1"
	logger := logf.Log.WithName("testController")

	Context("validate process group on taint node", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var err error
		var pod *corev1.Pod                                   // Pod to be tainted
		var processGroupStatus *fdbv1beta2.ProcessGroupStatus // Target pod's processGroup
		var node *corev1.Node                                 // Target pod's node
		var pvc *corev1.PersistentVolumeClaim                 // Target pod's pvc
		taintKeyWildcard := "*"
		taintKeyWildcardDuration := int64(20)
		taintKeyMaintenance := "foundationdb/maintenance"
		taintKeyMaintenanceDuration := int64(5)
		taintKeyUnhealthy := "foundationdb/unhealthy"
		taintKeyUnhealthyDuration := int64(11)
		taintValue := "unhealthy"

		BeforeEach(func() {
			var pods []*corev1.Pod
			cluster = internal.CreateDefaultCluster()
			err = setupClusterForTest(cluster)
			Expect(err).NotTo(HaveOccurred())
			// Define cluster's taint policy
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
				{
					Key:               &taintKeyWildcard,
					DurationInSeconds: &taintKeyWildcardDuration,
				},
				{
					Key:               &taintKeyMaintenance,
					DurationInSeconds: &taintKeyMaintenanceDuration,
				},
			}

			pods, err = clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), clusterReconciler, cluster, internal.GetSinglePodListOptions(cluster, storageOneProcessGroupID)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods)).To(Equal(1))

			pod = pods[0]
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: pod.Spec.NodeName},
			}

			allPvcs := &corev1.PersistentVolumeClaimList{}
			err = clusterReconciler.List(context.TODO(), allPvcs, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())

			processGroupStatus = fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta))
			pvcMap := internal.CreatePVCMap(cluster, allPvcs)
			pvcValue, pvcExists := pvcMap[processGroupStatus.ProcessGroupID]
			if pvcExists {
				pvc = &pvcValue
			}
			globalControllerLogger.Info("Target processGroupStatus Info", "ProcessGroupID", processGroupStatus.ProcessGroupID,
				"Conditions size", len(processGroupStatus.ProcessGroupConditions),
				"Conditions", processGroupStatus.ProcessGroupConditions)
		})

		When("process group's node is tainted", func() {
			JustBeforeEach(func() {
				globalControllerLogger.Info("Taint node", "Node name", pod.Name, "Node taints", node.Spec.Taints)
				Expect(k8sClient.Update(context.TODO(), node)).NotTo(HaveOccurred())

				err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pvc, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], processGroupStatus, cluster.IsTaintFeatureDisabled(), logger)
				Expect(err).NotTo(HaveOccurred())
			})

			When("the taint was just added", func() {
				BeforeEach(func() {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now()},
						},
					}
				})

				It("should get NodeTaintDetected condition", func() {
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(1))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
				})
			})

			When("the taint was just added long enough to to mark the Pod for replacement", func() {
				BeforeEach(func() {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
						},
					}
				})

				It("should get both NodeTaintDetected and NodeTaintReplacing condition", func() {
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(2))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(Equal(nil))
				})
			})

			When("disable and enable taint feature", func() {
				BeforeEach(func() {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
						},
					}
					// Disable cluster's taint mechanism
					cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{}
				})

				It("should enable cluster taint feature", func() {
					// Check taint feature is disabled
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(0))

					// Enable taint feature
					cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
						{
							Key:               &taintKeyWildcard,
							DurationInSeconds: &taintKeyWildcardDuration,
						},
						{
							Key:               &taintKeyMaintenance,
							DurationInSeconds: &taintKeyMaintenanceDuration,
						},
					}

					err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pvc, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], processGroupStatus, cluster.IsTaintFeatureDisabled(), logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(2))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(Equal(nil))
				})
			})

			When("cluster disables taint feature but node has wildcard taint", func() {
				BeforeEach(func() {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyWildcard,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyWildcardDuration+1))},
						},
					}
					// Disable cluster's taint mechanism
					cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{}
				})

				It("should disable taint feature", func() {
					Expect(err).NotTo(HaveOccurred())
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(0))
				})
			})

			When("node is tainted with a key different from cluster configured taint key", func() {
				BeforeEach(func() {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyUnhealthy,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyUnhealthyDuration+1))},
						},
					}
					// Only handle taintKeyMaintenance taint key
					cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
						{
							Key:               &taintKeyMaintenance,
							DurationInSeconds: &taintKeyMaintenanceDuration,
						},
					}
				})

				It("pods on the node should not be tainted", func() {
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(0))
					// Add taintKeyUnhealthy to handle the node's taint key
					cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
						{
							Key:               &taintKeyMaintenance,
							DurationInSeconds: &taintKeyMaintenanceDuration,
						},
						{
							Key:               &taintKeyUnhealthy,
							DurationInSeconds: &taintKeyUnhealthyDuration,
						},
					}

					err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pvc, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], processGroupStatus, cluster.IsTaintFeatureDisabled(), logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(2))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(BeNil())
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(BeNil())
				})
			})

			When("node taint is removed", func() {
				// Taint key is removed from tainted node. NodeTaintDetected condition should be removed, but NodeTaintReplacing condition still exist
				BeforeEach(func() {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     taintValue,
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
						},
					}
				})

				It("should keep NodeTaintReplacing condition", func() {
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(2))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(BeNil())
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(BeNil())

					// Remove the node taint
					node.Spec.Taints = []corev1.Taint{}
					err = k8sClient.Update(context.TODO(), node)
					Expect(err).NotTo(HaveOccurred())
					nodeMap = make(map[string]*corev1.Node)
					globalControllerLogger.Info("Remove node taint", "Node name", pod.Name, "Node taints", node.Spec.Taints, "Now", time.Now())

					err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pvc, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], processGroupStatus, cluster.IsTaintFeatureDisabled(), logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(1))
					Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(Equal(nil))
				})
			})
		})
	})

	Context("validate process group", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var configMap *corev1.ConfigMap
		var adminClient *mock.AdminClient
		var storagePod *corev1.Pod
		var processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo
		var err error
		var allPvcs *corev1.PersistentVolumeClaimList

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())

			pods, err := clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), clusterReconciler, cluster, internal.GetSinglePodListOptions(cluster, storageOneProcessGroupID)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(pods).To(HaveLen(1))

			storagePod = pods[0]
			for _, container := range storagePod.Spec.Containers {
				storagePod.Status.ContainerStatuses = append(storagePod.Status.ContainerStatuses, corev1.ContainerStatus{Ready: true, Name: container.Name})
			}

			adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			configMap = &corev1.ConfigMap{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-config"}, configMap)).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			databaseStatus, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
			processMap = make(map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo)
			for _, process := range databaseStatus.Cluster.Processes {
				processID, ok := process.Locality[fdbv1beta2.FDBLocalityProcessIDKey]
				// if the processID is not set we fall back to the instanceID
				if !ok {
					processID = process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
				}
				processMap[fdbv1beta2.ProcessGroupID(processID)] = append(processMap[fdbv1beta2.ProcessGroupID(processID)], process)
			}

			allPvcs = &corev1.PersistentVolumeClaimList{}
			err = clusterReconciler.List(context.TODO(), allPvcs, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())
		})

		When("process group has no Pod", func() {
			It("should be added to the failing Pods", func() {
				processGroupStatus := fdbv1beta2.NewProcessGroupStatus("storage-1337", fdbv1beta2.ProcessClassStorage, []string{"1.1.1.1"})
				// Reset the status to only tests for the missing Pod
				processGroupStatus.ProcessGroupConditions = []*fdbv1beta2.ProcessGroupCondition{}
				Expect(validateProcessGroup(context.TODO(), clusterReconciler, cluster, nil, nil, "", processGroupStatus, cluster.IsTaintFeatureDisabled(), logger)).NotTo(HaveOccurred())
				Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.MissingPod))
			})
		})

		When("a process group is fine", func() {
			It("should not get any condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroupStatus[0].ProcessGroupConditions)).To(Equal(0))
			})
		})

		When("the pod for the process group is missing", func() {
			BeforeEach(func() {
				Expect(k8sClient.Delete(context.TODO(), storagePod)).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				dummyPod := &corev1.Pod{}
				Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(storagePod), dummyPod)).To(HaveOccurred())
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.MissingPod, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("a process group has the wrong command line", func() {
			BeforeEach(func() {
				adminClient.MockIncorrectCommandLine(storageOneProcessGroupID, true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				incorrectProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})

			When("the process group is marked for removal", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}
				})

				It("should get a condition assigned", func() {
					processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
					Expect(err).NotTo(HaveOccurred())

					incorrectProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.IncorrectCommandLine, false)
					Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

					Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
					processGroup := processGroupStatus[len(processGroupStatus)-4]
					Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
					Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
					Expect(processGroup.IsMarkedForRemoval()).To(BeTrue())
				})
			})
		})

		When("a process group is not reporting to the cluster", func() {
			BeforeEach(func() {
				adminClient.MockMissingProcessGroup(storageOneProcessGroupID, true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.MissingProcesses, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod has the wrong spec", func() {
			BeforeEach(func() {
				storagePod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "bad"
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				incorrectPods := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.IncorrectPodSpec, false)
				Expect(incorrectPods).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the Pod is marked for deletion but still reporting to the cluster", func() {
			BeforeEach(func() {
				// We cannot use the k8sClient.MockStuckTermination() method because the deletionTimestamp must be
				// older than 5 minutes to detect the Pod as PodFailed.
				stuckPod := storagePod
				stuckPod.SetDeletionTimestamp(&metav1.Time{Time: time.Now().Add(-10 * time.Minute)})
				stuckPod.SetFinalizers(append(stuckPod.GetFinalizers(), "foundationdb.org/testing"))
				Expect(k8sClient.Update(context.Background(), stuckPod)).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.PodFailing, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod is failing to launch", func() {
			BeforeEach(func() {
				storagePod.Status.ContainerStatuses[0].Ready = false
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.PodFailing, false)
				Expect(failingPods).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("the pod is failed", func() {
			BeforeEach(func() {
				storagePod.Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbv1beta2.FilterByCondition(processGroupStatus, fdbv1beta2.PodFailing, false)
				Expect(failingPods).To(Equal([]fdbv1beta2.ProcessGroupID{storageOneProcessGroupID}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal(storageOneProcessGroupID))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		When("adding a process group to the ProcessGroupsToRemove list", func() {
			var removedProcessGroup fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				removedProcessGroup = storageOneProcessGroupID
				storagePod.Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{removedProcessGroup}
			})

			It("should mark the process group for removal", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
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
				removedProcessGroup = storageOneProcessGroupID
				storagePod.Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []fdbv1beta2.ProcessGroupID{removedProcessGroup}
			})

			It("should be mark the process group for removal without exclusion", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
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
				unreachablePod := storagePod
				unreachableProcessGroup = podmanager.GetProcessGroupID(cluster, storagePod)
				unreachablePod.Annotations[internal.MockUnreachableAnnotation] = "banana"
				Expect(k8sClient.Update(context.TODO(), unreachablePod)).NotTo(HaveOccurred())
			})

			It("should mark the process group as unreachable", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
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
					delete(storagePod.Annotations, internal.MockUnreachableAnnotation)
					err = k8sClient.Update(context.TODO(), storagePod)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should remove the condition", func() {
					processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
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
				pendingProcessGroup = podmanager.GetProcessGroupID(cluster, storagePod)
				storagePod.Status.Phase = corev1.PodPending
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should mark the process group as Pod pending", func() {
				processGroupStatus, err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
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

	Describe("Reconcile", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var err error
		var requeue *requeue
		var adminClient *mock.AdminClient

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

			adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
			requeue = updateStatus{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
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

		Context("testing maintenance mode functionality", func() {
			When("maintenance mode is on", func() {
				BeforeEach(func() {
					Expect(adminClient.SetMaintenanceZone("operator-test-1-storage-4", 0)).NotTo(HaveOccurred())
				})
				It("status maintenance zone should match", func() {
					Expect(cluster.Status.MaintenanceModeInfo).To(Equal(fdbv1beta2.MaintenanceModeInfo{ZoneID: "operator-test-1-storage-4"}))
				})
			})
		})
	})

	DescribeTable("when getting the running version from the running processes", func(versionMap map[string]int, fallback string, expected string) {
		Expect(getRunningVersion(globalControllerLogger, versionMap, fallback)).To(Equal(expected))
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

	When("updating the fault domains based on the cluster status", func() {
		var processes map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo
		var status fdbv1beta2.FoundationDBClusterStatus

		JustBeforeEach(func() {
			status = fdbv1beta2.FoundationDBClusterStatus{
				ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
					{
						ProcessGroupID: "storage-1",
					},
					{
						ProcessGroupID: "storage-2",
					},
					{
						ProcessGroupID: "storage-3",
					},
				},
			}

			updateFaultDomains(logr.Discard(), processes, &status)
		})

		When("storage-2 has two process information", func() {
			BeforeEach(func() {
				processes = map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo{
					"storage-1": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-1-zone",
							},
						},
					},
					"storage-2": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "second",
							},
							UptimeSeconds: 1000,
						},
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-2-zone",
							},
							UptimeSeconds: 1,
						},
					},
				}

			})

			It("should update the process group status", func() {
				Expect(status.ProcessGroups).To(HaveLen(3))

				for _, processGroup := range status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-3" {
						Expect(processGroup.FaultDomain).To(BeEmpty())
						continue
					}

					Expect(string(processGroup.FaultDomain)).To(And(HavePrefix(string(processGroup.ProcessGroupID)), HaveSuffix("zone")))
				}
			})
		})

		When("storage-2 has two process information and one has no localities", func() {
			BeforeEach(func() {
				processes = map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo{
					"storage-1": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-1-zone",
							},
						},
					},
					"storage-2": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							UptimeSeconds: 1,
						},
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-2-zone",
							},
							UptimeSeconds: 1000,
						},
					},
				}

			})

			It("should update the process group status", func() {
				Expect(status.ProcessGroups).To(HaveLen(3))

				for _, processGroup := range status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-3" {
						Expect(processGroup.FaultDomain).To(BeEmpty())
						continue
					}

					Expect(string(processGroup.FaultDomain)).To(And(HavePrefix(string(processGroup.ProcessGroupID)), HaveSuffix("zone")))
				}
			})
		})
	})
})
