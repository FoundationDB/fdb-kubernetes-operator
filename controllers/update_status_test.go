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

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	"k8s.io/apimachinery/pkg/types"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("update_status", func() {
	logger := logf.Log.WithName("testController")
	var pickedProcessGroup *fdbv1beta2.ProcessGroupStatus

	Context("validate process group on taint node", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var err error
		var pod *corev1.Pod   // Pod to be tainted
		var node *corev1.Node // Target pod's node
		taintKeyWildcard := "*"
		taintKeyWildcardDuration := int64(20)
		taintKeyMaintenance := "foundationdb/maintenance"
		taintKeyMaintenanceDuration := int64(5)
		taintKeyUnhealthy := "foundationdb/unhealthy"
		taintKeyUnhealthyDuration := int64(11)
		taintValue := "unhealthy"

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())
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

			pickedProcessGroup = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
			pod, err = clusterReconciler.PodLifecycleManager.GetPod(context.TODO(), clusterReconciler, cluster, pickedProcessGroup.GetPodName(cluster))
			Expect(err).NotTo(HaveOccurred())
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: pod.Spec.NodeName},
			}

			globalControllerLogger.Info("Target processGroupStatus Info", "ProcessGroupID", pickedProcessGroup.ProcessGroupID,
				"Conditions size", len(pickedProcessGroup.ProcessGroupConditions),
				"Conditions", pickedProcessGroup.ProcessGroupConditions)
		})

		When("process group's node is tainted", func() {
			JustBeforeEach(func() {
				globalControllerLogger.Info("Taint node", "Node name", pod.Name, "Node taints", node.Spec.Taints)
				Expect(k8sClient.Update(context.TODO(), node)).NotTo(HaveOccurred())

				err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], pickedProcessGroup, cluster.IsTaintFeatureDisabled(), logger)
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
					Expect(len(pickedProcessGroup.ProcessGroupConditions)).To(Equal(1))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
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
					Expect(pickedProcessGroup.ProcessGroupConditions).To(HaveLen(2))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(Equal(nil))
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
					Expect(pickedProcessGroup.ProcessGroupConditions).To(HaveLen(0))

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

					err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], pickedProcessGroup, cluster.IsTaintFeatureDisabled(), logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(pickedProcessGroup.ProcessGroupConditions).To(HaveLen(2))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(Equal(nil))
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
					Expect(pickedProcessGroup.ProcessGroupConditions).To(HaveLen(0))
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
					Expect(len(pickedProcessGroup.ProcessGroupConditions)).To(Equal(0))
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

					err = validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], pickedProcessGroup, cluster.IsTaintFeatureDisabled(), logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(pickedProcessGroup.ProcessGroupConditions).To(HaveLen(2))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(BeNil())
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(BeNil())
				})
			})

			When("node taint is removed", func() {
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

				It("shouldn't keep NodeTaintReplacing condition", func() {
					Expect(len(pickedProcessGroup.ProcessGroupConditions)).To(Equal(2))
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(BeNil())
					Expect(pickedProcessGroup.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(BeNil())

					// Remove the node taint
					node.Spec.Taints = nil
					Expect(k8sClient.Update(context.TODO(), node)).NotTo(HaveOccurred())
					globalControllerLogger.Info("Remove node taint", "Node name", pod.Name, "Node taints", node.Spec.Taints, "Now", time.Now())

					Expect(validateProcessGroup(context.TODO(), clusterReconciler, cluster, pod, pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey], pickedProcessGroup, cluster.IsTaintFeatureDisabled(), logger)).NotTo(HaveOccurred())
					Expect(pickedProcessGroup.ProcessGroupConditions).To(BeEmpty())
				})
			})
		})
	})

	When("validating process groups", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var configMap *corev1.ConfigMap
		var adminClient *mock.AdminClient
		var storagePod *corev1.Pod
		var processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo
		var err error

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())

			pickedProcessGroup = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
			storagePod, err = clusterReconciler.PodLifecycleManager.GetPod(context.TODO(), clusterReconciler, cluster, pickedProcessGroup.GetPodName(cluster))
			Expect(err).NotTo(HaveOccurred())
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
		})

		When("process group has no Pod", func() {
			BeforeEach(func() {
				Expect(k8sClient.Delete(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pickedProcessGroup.GetPodName(cluster), Namespace: cluster.Namespace}})).NotTo(HaveOccurred())
			})

			It("should be added to the failing Pods", func() {
				Expect(validateProcessGroup(context.TODO(), clusterReconciler, cluster, nil, "", pickedProcessGroup, cluster.IsTaintFeatureDisabled(), logger)).NotTo(HaveOccurred())
				Expect(pickedProcessGroup.ProcessGroupConditions).To(HaveLen(1))
				Expect(pickedProcessGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.MissingPod))
			})
		})

		When("a process group is fine", func() {
			It("should not get any condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
				for _, processGroup := range cluster.Status.ProcessGroups {
					Expect(processGroup.ProcessGroupConditions).To(HaveLen(0))
				}
			})
		})

		When("the command line is too long", func() {
			BeforeEach(func() {
				// Generate a random long string
				cluster.Spec.MainContainer.PeerVerificationRules = internal.GenerateRandomString(10000)
			})

			It("should not get any condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).To(HaveOccurred())
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
				// We expect that no conditions are added in this case.
				for _, processGroup := range cluster.Status.ProcessGroups {
					Expect(processGroup.ProcessGroupConditions).To(HaveLen(0))
				}
			})
		})

		When("the pod for the process group is missing", func() {
			BeforeEach(func() {
				Expect(k8sClient.Delete(context.TODO(), storagePod)).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				dummyPod := &corev1.Pod{}
				Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(storagePod), dummyPod)).To(HaveOccurred())
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.MissingPod, false)
				Expect(missingProcesses).To(ConsistOf([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})
		})

		When("a process group has a process that is excluded", func() {
			BeforeEach(func() {
				adminClient.ExcludedAddresses[storagePod.Status.PodIP] = fdbv1beta2.None{}
			})

			It("should get the ProcessIsMarkedAsExcluded condition", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.ProcessIsMarkedAsExcluded, false)
				Expect(incorrectProcesses).To(ConsistOf([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})
		})

		When("a process group has the wrong command line", func() {
			BeforeEach(func() {
				adminClient.MockIncorrectCommandLine(pickedProcessGroup.ProcessGroupID, true)
			})

			It("should get a condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(ConsistOf([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})

			When("the process group is marked for removal", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}
				})

				It("should get a condition assigned", func() {
					err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
					Expect(err).NotTo(HaveOccurred())

					incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)
					Expect(incorrectProcesses).To(ConsistOf([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
					Expect(pickedProcessGroup.IsMarkedForRemoval()).To(BeTrue())
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
				})
			})
		})

		When("a process group is not reporting to the cluster", func() {
			BeforeEach(func() {
				adminClient.MockMissingProcessGroup(pickedProcessGroup.ProcessGroupID, true)
			})

			It("should get a condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.MissingProcesses, false)
				Expect(missingProcesses).To(ConsistOf(pickedProcessGroup.ProcessGroupID))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})

			When("no processes are provided in the process map", func() {
				It("should not get a condition assigned", func() {
					err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo{}, configMap, logger, "")
					Expect(err).NotTo(HaveOccurred())

					missingProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.MissingProcesses, false)
					Expect(missingProcesses).To(BeEmpty())
				})
			})
		})

		When("the pod has the wrong spec", func() {
			BeforeEach(func() {
				storagePod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "bad"
				Expect(k8sClient.Update(context.TODO(), storagePod)).NotTo(HaveOccurred())
			})

			It("should get a IncorrectPodSpec condition assigned", func() {
				Expect(validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")).To(Succeed())
				incorrectPods := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectPodSpec, false)
				Expect(incorrectPods).To(Equal([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
				Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectPodMetadata, false)).To(BeEmpty())
				Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectConfigMap, false)).To(BeEmpty())
			})
		})

		When("the pod has the wrong metadata", func() {
			When("an annotation is missing", func() {
				BeforeEach(func() {
					delete(storagePod.ObjectMeta.Annotations, fdbv1beta2.NodeAnnotation)
					Expect(k8sClient.Update(context.TODO(), storagePod)).NotTo(HaveOccurred())
				})

				It("should get a IncorrectPodMetadata condition assigned", func() {
					Expect(validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")).NotTo(HaveOccurred())
					incorrectPods := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectPodMetadata, false)
					Expect(incorrectPods).To(Equal([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
					Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectPodSpec, false)).To(BeEmpty())
					Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectConfigMap, false)).To(BeEmpty())
				})
			})
		})

		When("the pod has the wrong config map hash", func() {
			When("an annotation is missing", func() {
				BeforeEach(func() {
					storagePod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey] = "bad"
					Expect(k8sClient.Update(context.TODO(), storagePod)).NotTo(HaveOccurred())
				})

				It("should get a IncorrectConfigMap condition assigned", func() {
					Expect(validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")).NotTo(HaveOccurred())
					incorrectPods := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectConfigMap, false)
					Expect(incorrectPods).To(Equal([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
					Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectPodSpec, false)).To(BeEmpty())
					Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectPodMetadata, false)).To(BeEmpty())
				})
			})
		})

		// Currently disabled as the previous setup for setting a pod into stuck terminating doesn't work anymore.
		PWhen("the Pod is marked for deletion but still reporting to the cluster", func() {
			BeforeEach(func() {
				// We cannot use the k8sClient.MockStuckTermination() method because the deletionTimestamp must be
				// older than 5 minutes to detect the Pod as PodFailed.
				storagePod.SetDeletionTimestamp(&metav1.Time{Time: time.Now().Add(-10 * time.Minute)})
				Expect(k8sClient.Update(context.Background(), storagePod)).NotTo(HaveOccurred())
			})

			It("should get a condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.PodFailing, false)
				Expect(missingProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})
		})

		When("the pod is failing to launch", func() {
			BeforeEach(func() {
				storagePod.Status.ContainerStatuses[0].Ready = false
				Expect(k8sClient.Status().Update(context.TODO(), storagePod)).To(Succeed())
			})

			It("should get a condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.PodFailing, false)
				Expect(failingPods).To(Equal([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})
		})

		When("the pod is failed", func() {
			BeforeEach(func() {
				storagePod.Status.Phase = corev1.PodFailed
				Expect(k8sClient.Status().Update(context.TODO(), storagePod)).To(Succeed())
			})

			It("should get a condition assigned", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.PodFailing, false)
				Expect(failingPods).To(Equal([]fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}))
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
			})

			When("the process group is under maintenance", func() {
				It("should not set the conditions", func() {
					processGroup := cluster.Status.ProcessGroups[len(cluster.Status.ProcessGroups)-4]
					Expect(validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, processGroup.FaultDomain)).NotTo(HaveOccurred())

					failingPods := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.PodFailing, false)
					Expect(failingPods).To(BeEmpty())
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
				})
			})
		})

		When("adding a process group to the ProcessGroupsToRemove list", func() {
			BeforeEach(func() {
				storagePod.Status.Phase = corev1.PodFailed
				Expect(k8sClient.Update(context.TODO(), storagePod)).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}
			})

			It("should mark the process group for removal", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == pickedProcessGroup.ProcessGroupID {
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
			BeforeEach(func() {
				storagePod.Status.Phase = corev1.PodFailed
				err = k8sClient.Update(context.TODO(), storagePod)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []fdbv1beta2.ProcessGroupID{pickedProcessGroup.ProcessGroupID}
			})

			It("should be mark the process group for removal without exclusion", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				removalCount := 0
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == pickedProcessGroup.ProcessGroupID {
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
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				unreachableCount := 0
				for _, processGroup := range cluster.Status.ProcessGroups {
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
					err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
					Expect(err).NotTo(HaveOccurred())

					unreachableCount := 0
					for _, processGroup := range cluster.Status.ProcessGroups {
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
				Expect(k8sClient.Status().Update(context.TODO(), storagePod)).To(Succeed())
			})

			It("should mark the process group as Pod pending", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())

				pendingCount := 0
				for _, processGroup := range cluster.Status.ProcessGroups {
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

		When("a process group is set as excluded but the processes are not excluded", func() {
			BeforeEach(func() {
				processGroup := cluster.Status.ProcessGroups[0]
				processGroup.SetExclude()
				Expect(processGroup.IsExcluded()).To(BeTrue())
			})

			It("should remove the exclusion", func() {
				err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
				for _, processGroup := range cluster.Status.ProcessGroups {
					Expect(processGroup.ProcessGroupConditions).To(HaveLen(0))
					Expect(processGroup.IsExcluded()).To(BeFalse())
				}
			})
		})

		When("the sidecar image doesn't match", func() {
			When("the cluster is upgraded", func() {
				BeforeEach(func() {
					version, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.Version = version.NextMinorVersion().String()
				})

				It("should update the incorrect sidecar image condition", func() {
					err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
					for _, processGroup := range cluster.Status.ProcessGroups {
						Expect(processGroup.ProcessGroupConditions).To(HaveLen(2))
						Expect(processGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectCommandLine))
						Expect(processGroup.ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectSidecarImage))
					}
				})
			})

			When("the image type is changed", func() {
				BeforeEach(func() {
					imageType := fdbv1beta2.ImageTypeUnified
					Expect(imageType).NotTo(Equal(cluster.DesiredImageType()))
					cluster.Spec.ImageType = &imageType
				})

				It("should update the incorrect sidecar image condition", func() {
					err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
					for _, processGroup := range cluster.Status.ProcessGroups {
						Expect(processGroup.ProcessGroupConditions).To(HaveLen(3))
						Expect(processGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectCommandLine))
						Expect(processGroup.ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectPodSpec))
						Expect(processGroup.ProcessGroupConditions[2].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectSidecarImage))
					}
				})
			})

			When("the image config is changed", func() {
				BeforeEach(func() {
					cluster.Spec.SidecarContainer.ImageConfigs = []fdbv1beta2.ImageConfig{
						{
							BaseImage: fdbv1beta2.FoundationDBSidecarBaseImage,
							Tag:       "testing",
						},
					}
				})

				It("should update the incorrect sidecar image condition", func() {
					err := validateProcessGroups(context.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, logger, "")
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.ProcessGroups).To(HaveLen(17))
					for _, processGroup := range cluster.Status.ProcessGroups {
						Expect(processGroup.ProcessGroupConditions).To(HaveLen(2))
						Expect(processGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectPodSpec))
						Expect(processGroup.ProcessGroupConditions[1].ProcessGroupConditionType).To(Equal(fdbv1beta2.IncorrectSidecarImage))
					}
				})
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

		It("should set the fault domain for all process groups", func() {
			for _, processGroup := range cluster.Status.ProcessGroups {
				Expect(processGroup.FaultDomain).NotTo(BeEmpty())
			}
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

		When("multiple storage server per Pod are used", func() {
			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(int64(2)))
			})

			It("should set the fault domain for all process groups", func() {
				for _, processGroup := range cluster.Status.ProcessGroups {
					Expect(processGroup.FaultDomain).NotTo(BeEmpty())
				}
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

		When("multiple storage servers per Pod are used", func() {
			BeforeEach(func() {
				processes = map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo{
					"storage-1-1": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-1-zone",
							},
						},
					},
					"storage-1-2": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-1-zone",
							},
						},
					},
					"storage-2-1": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-2-zone",
							},
						},
					},
					"storage-2-2": {
						fdbv1beta2.FoundationDBStatusProcessInfo{
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityZoneIDKey: "storage-2-zone",
							},
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

		When("the process map is empty", func() {
			BeforeEach(func() {
				processes = map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo{}
			})

			It("should skip the process group fault domains", func() {
				Expect(status.ProcessGroups).To(HaveLen(3))

				for _, processGroup := range status.ProcessGroups {
					Expect(processGroup.FaultDomain).To(BeEmpty())
				}
			})
		})
	})
})
