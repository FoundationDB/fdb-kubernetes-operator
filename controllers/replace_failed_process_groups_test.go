/*
 * replace_failed_process_groups_test.go
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
	ctx "context"
	"math/rand"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("replace_failed_process_groups", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var result *requeue

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = k8sClient.Create(ctx.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))
	})

	JustBeforeEach(func() {
		adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(adminClient).NotTo(BeNil())
		err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())
		result = replaceFailedProcessGroups{}.reconcile(ctx.Background(), clusterReconciler, cluster)
	})

	Context("replace pod on tainted node", func() {
		taintKeyStar := "*"
		taintKeyStarDuration := int64(20)
		taintKeyMaintenance := "foundationdb.org/maintenance"
		taintKeyMaintenanceDuration := int64(10)

		var allPvcs *corev1.PersistentVolumeClaimList
		var podOnTaintedNode *corev1.Pod
		var targetPodProcessGroupID fdbv1beta2.ProcessGroupID
		var node *corev1.Node
		var adminClient *mock.AdminClient
		var configMap *corev1.ConfigMap
		var processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo
		var logger logr.Logger

		BeforeEach(func() {
			logger = log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "test", "replace pod on tainted node")
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
				{
					Key:               &taintKeyStar,
					DurationInSeconds: &taintKeyStarDuration,
				},
				{
					Key:               &taintKeyMaintenance,
					DurationInSeconds: &taintKeyMaintenanceDuration,
				},
			}
			cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds = pointer.Int(5)
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementTimeSeconds = pointer.Int(1)
			// Update cluster config so that generic reconciliation will work
			err := k8sClient.Update(ctx.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)

			Expect(err).NotTo(HaveOccurred())
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

			allPvcs = &corev1.PersistentVolumeClaimList{}
			err = clusterReconciler.List(ctx.TODO(), allPvcs, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())

			configMap = &corev1.ConfigMap{}
			err = k8sClient.Get(ctx.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-config"}, configMap)
			Expect(err).NotTo(HaveOccurred())

			pods, err := clusterReconciler.PodLifecycleManager.GetPods(ctx.TODO(), clusterReconciler, cluster, internal.GetSinglePodListOptions(cluster, "storage-1")...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods)).To(Equal(1))

			podOnTaintedNode = pods[0] // Future: choose a random pod to test
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: podOnTaintedNode.Spec.NodeName},
			}
			targetPodProcessGroupID = internal.GetProcessGroupIDFromMeta(cluster, podOnTaintedNode.ObjectMeta)

			// Call validateProcessGroups to set processGroupStatus to tainted condition
			processGroupsStatus, err := validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			processGroup := processGroupsStatus[len(processGroupsStatus)-4]
			Expect(processGroup.ProcessGroupID).To(Equal(fdbv1beta2.ProcessGroupID("storage-1")))
			Expect(len(processGroupsStatus[0].ProcessGroupConditions)).To(Equal(0))
		})

		It("should not replace a pod whose condition is NodeTaintDetected but not NodeTaintReplacing ", func() {
			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now()},
				},
			}
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())
			log.Info("Taint node", "Node name", podOnTaintedNode.Name, "Node taints", node.Spec.Taints)

			processGroupsStatus, err := validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(1))
			Expect(targetProcessGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))

			Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster)).To(BeNil())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		It("should replace a pod whose condition is NodeTaintReplacing but not NodeTaintDetected", func() {
			// This test case covers the scenario when a node is tainted for a while and then no longer tainted
			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())
			log.Info("Taint node", "Node name", podOnTaintedNode.Name, "Node taints", node.Spec.Taints)

			processGroupsStatus, err := validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(2))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).NotTo(Equal(nil))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(Equal(nil))

			node.Spec.Taints = []corev1.Taint{}
			err = k8sClient.Update(ctx.TODO(), node)
			Expect(err).NotTo(HaveOccurred())
			processGroupsStatus, err = validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			targetProcessGroupStatus = fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(1))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing)).NotTo(BeNil())

			Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster)).To(BeNil())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		It("should replace a pod that is both NodeTaintDetected and NodeTaintReplacing ", func() {
			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", podOnTaintedNode.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			processGroupsStatus, err := validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(2))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))

			// cluster won't replace a failed process until GetTaintReplacementTimeSeconds() later
			Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster)).To(BeNil())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))

			Eventually(func() *requeue {
				result = replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster)
				return result
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*3) * time.Second).WithPolling(1 * time.Second).ShouldNot(BeNil())

			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{targetPodProcessGroupID}))
		})

		It("should replace a pod that has NodeTaintReplacing condition but no longer has NodeTaintDetected condition", func() {
			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", podOnTaintedNode.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			processGroupsStatus, err := validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(2))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintReplacing).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintReplacing))

			Eventually(func() *requeue {
				return replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster)
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*3) * time.Second).WithPolling(1 * time.Second).ShouldNot(BeNil())

			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{targetPodProcessGroupID}))
		})

		It("should not replace a pod that is on a flapping tainted node", func() {
			// Flapping tainted node
			tainted := int64(0)
			for taintTimeOffset := taintKeyMaintenanceDuration * 2; taintTimeOffset >= 0; taintTimeOffset-- {
				tainted = taintTimeOffset % 2
				if tainted == 0 {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-1 * time.Second * time.Duration(taintTimeOffset))},
						},
					}
				} else {
					node.Spec.Taints = []corev1.Taint{}
				}
				err = k8sClient.Update(ctx.TODO(), node)
				Expect(err).NotTo(HaveOccurred())
				log.Info("Taint node", "Tainted", tainted, "Node name", podOnTaintedNode.Name, "Node taints", node.Spec.Taints, "Now", time.Now())
			}
			Expect(tainted).To(Equal(int64(0)))

			processGroupsStatus, err := validateProcessGroups(ctx.TODO(), clusterReconciler, cluster, &cluster.Status, processMap, configMap, allPvcs, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(processGroupsStatus)).To(BeNumerically(">", 4))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(1))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))

			// cluster won't replace the process
			time.Sleep(time.Second * time.Duration(cluster.GetTaintReplacementTimeSeconds()+1))
			Expect(replaceFailedProcessGroups{}.reconcile(ctx.TODO(), clusterReconciler, cluster)).To(BeNil())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		It("should replace a pod that is both NodeTaintDetected and NodeTaintReplacing with cluster reconciliation", func() {
			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			result, err := reconcileClusterWithCustomRequeueLimit(cluster, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))

			Eventually(func() *corev1.Pod {
				_, err = reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				return (getPodByProcessGroupID(cluster, internal.GetProcessClassFromMeta(cluster, podOnTaintedNode.ObjectMeta), internal.GetProcessGroupIDFromMeta(cluster, podOnTaintedNode.ObjectMeta)))
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*3) * time.Second).WithPolling(1 * time.Second).Should(BeNil())

			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		It("exact matched taint key should be preferred over wildcard key", func() {
			err = k8sClient.Get(ctx.TODO(), client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
			taintKeyStarDurationShort := int64(5)
			taintKeyMaintenanceDurationLong := int64(50)
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
				{
					Key:               &taintKeyStar,
					DurationInSeconds: &taintKeyStarDurationShort,
				},
				{
					Key:               &taintKeyMaintenance,
					DurationInSeconds: &taintKeyMaintenanceDurationLong,
				},
			}
			Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyStarDurationShort))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			_, err = reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx.TODO(), client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)).NotTo(HaveOccurred())
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(1))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))

			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDurationLong))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			Eventually(func() *corev1.Pod {
				_, err = reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				return (getPodByProcessGroupID(cluster, internal.GetProcessClassFromMeta(cluster, podOnTaintedNode.ObjectMeta), internal.GetProcessGroupIDFromMeta(cluster, podOnTaintedNode.ObjectMeta)))
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*10) * time.Second).WithPolling(1 * time.Second).Should(BeNil())

			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		// This test is flaky because if code is executed slower, the tainted node will be there for too long and its Pod will be removed
		PIt("should not replace a pod that is on a flapping tainted node", FlakeAttempts(3), func() {
			// Flapping tainted node
			tainted := int64(0)
			for taintTimeOffset := taintKeyMaintenanceDuration * 3; taintTimeOffset >= 0; taintTimeOffset-- {
				tainted = taintTimeOffset % 2
				if tainted == 0 {
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-1 * time.Second * time.Duration(taintTimeOffset))},
						},
					}
				} else {
					node.Spec.Taints = []corev1.Taint{}
				}
				err = k8sClient.Update(ctx.TODO(), node)
				Expect(err).NotTo(HaveOccurred())
				log.Info("Taint node", "Not tainted", tainted, "Node name", podOnTaintedNode.Name, "Node taints", node.Spec.Taints, "Now", time.Now())
			}
			Expect(tainted).To(Equal(int64(0))) // ensure last Taint is empty
			startTime := time.Now()

			result, err := reconcileClusterWithCustomRequeueLimit(cluster, 1)
			Expect(err).NotTo(HaveOccurred())
			// pod with any condition is considered as unhealthy, but the pod won't be replaced w/o the NodeTaintReplacing condition
			Expect(result.Requeue).To(BeTrue())

			Expect(k8sClient.Get(ctx.TODO(), client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)).NotTo(HaveOccurred())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(1))
			Expect(targetProcessGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected).ProcessGroupConditionType).To(Equal(fdbv1beta2.NodeTaintDetected))

			// Wait long enough to satisfy cluster-wide threshold to replace tainted node
			time.Sleep(time.Second * time.Duration(cluster.GetTaintReplacementTimeSeconds()))

			// Sanity check test has not take taintKeyMaintenanceDuration seconds to execute
			Expect(time.Now().Unix() - startTime.Unix()).To(BeNumerically("<", taintKeyMaintenanceDuration))
			// still should not replace the target process group because it is not marked as failed
			_, err = reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			// target pod should not be removed by reconciliation
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
			Expect(getPodByProcessGroupID(cluster, internal.GetProcessClassFromMeta(cluster, podOnTaintedNode.ObjectMeta), internal.GetProcessGroupIDFromMeta(cluster, podOnTaintedNode.ObjectMeta))).NotTo(BeNil())
		})

		It("should not replace a pod on tainted node when cluster disables taint feature before node is tainted", func() {
			// Disable taint feature before a node is tainted
			// TODO: Disable taint feature AFTER a node is tainted
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{}
			Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			Eventually(func() int {
				_, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
				return len(targetProcessGroupStatus.ProcessGroupConditions)
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*3) * time.Second).WithPolling(1 * time.Second).Should(Equal(0))

			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		It("should not replace a pod on tainted node when cluster disables taint feature immediately after node is tainted", func() {
			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{}
			Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

			Eventually(func() []fdbv1beta2.ProcessGroupID {
				_, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				// target pod should not be removed by reconciliation;
				// targetProcessGroupStatus may or may not have its TaintDetected condition updated
				return (getRemovedProcessGroupIDs(cluster))

			}).WithTimeout(time.Second * time.Duration(cluster.GetTaintReplacementTimeSeconds()*3)).WithPolling(time.Second * time.Duration(1)).Should(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		It("should replace a pod on tainted node when the cluster disables and reenables taint feature", func() {
			// Disable taint feature before a node is tainted
			// TODO: Disable taint feature AFTER a node is tainted
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{}
			Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			// target pod should not be removed by reconciliation
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
			targetProcessGroupStatus := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, targetPodProcessGroupID)
			Expect(len(targetProcessGroupStatus.ProcessGroupConditions)).To(Equal(0))

			// Enable taint feature
			// Refresh cluster version before we update the cluster again
			Expect(k8sClient.Get(ctx.TODO(), client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)).NotTo(HaveOccurred())
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions = []fdbv1beta2.TaintReplacementOption{
				{
					Key:               &taintKeyStar,
					DurationInSeconds: &taintKeyStarDuration,
				},
				{
					Key:               &taintKeyMaintenance,
					DurationInSeconds: &taintKeyMaintenanceDuration,
				},
			}
			Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

			Eventually(func() *corev1.Pod {
				_, err = reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				// target pod should have been removed by reconciliation
				return getPodByProcessGroupID(cluster, internal.GetProcessClassFromMeta(cluster, podOnTaintedNode.ObjectMeta), internal.GetProcessGroupIDFromMeta(cluster, podOnTaintedNode.ObjectMeta))
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*3) * time.Second).WithPolling(1 * time.Second).Should(BeNil())
		})

		It("should replace a pod with NodeTaintReplacing condition when the conditions duration is longer than FailureDetectionTimeSeconds but shorter than TaintReplacementTimeSeconds", func() {
			// Test FailureDetectionTimeSeconds < TaintReplacementTimeSeconds scenario
			cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds = pointer.Int(1)
			cluster.Spec.AutomationOptions.Replacements.TaintReplacementTimeSeconds = pointer.Int(5)
			Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

			node.Spec.Taints = []corev1.Taint{
				{
					Key:       taintKeyMaintenance,
					Value:     "rack_maintenance",
					Effect:    corev1.TaintEffectNoExecute,
					TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
				},
			}
			log.Info("Taint node", "Node name", node.Name, "Node taints", node.Spec.Taints, "TaintTime", node.Spec.Taints[0].TimeAdded.Time, "Now", time.Now())
			Expect(k8sClient.Update(ctx.TODO(), node)).NotTo(HaveOccurred())

			result, err := reconcileClusterWithCustomRequeueLimit(cluster, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))

			Eventually(func() *corev1.Pod {
				result, err = reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				// target pod should have been removed by reconciliation
				return getPodByProcessGroupID(cluster, internal.GetProcessClassFromMeta(cluster, podOnTaintedNode.ObjectMeta), internal.GetProcessGroupIDFromMeta(cluster, podOnTaintedNode.ObjectMeta))
			}).WithTimeout(time.Duration(cluster.GetTaintReplacementTimeSeconds()*3) * time.Second).WithPolling(1 * time.Second).Should(BeNil())

			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})

		When("multiple nodes are tainted", func() {
			var taintedNodes []*corev1.Node
			var setValidTaint bool

			BeforeEach(func() {
				allPods, err := clusterReconciler.PodLifecycleManager.GetPods(ctx.TODO(), clusterReconciler, cluster, internal.GetPodListOptions(cluster, "", "")...)
				Expect(err).NotTo(HaveOccurred())

				concurrentTaints := 2
				Expect(len(allPods)).To(BeNumerically(">", concurrentTaints))
				taintedNodesIndex := map[int]struct{}{}
				taintedNodes = []*corev1.Node{}
				var taintKey string
				var taintTimeAdded *metav1.Time
				for len(taintedNodesIndex) < concurrentTaints {
					taintedNodesIndex[rand.Intn(len(allPods))] = struct{}{}
				}
				for key := range taintedNodesIndex {
					curPod := allPods[key]
					curNode := &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: curPod.Spec.NodeName},
					}
					taintedNodes = append(taintedNodes, curNode)
				}
				for i, taintedNode := range taintedNodes {
					if i%2 == 0 {
						taintKey = ""
						taintTimeAdded = &metav1.Time{Time: time.Now()}
					} else {
						taintKey = taintKeyMaintenance
						taintTimeAdded = nil
					}
					if setValidTaint {
						taintKey = taintKeyMaintenance
						taintTimeAdded = &metav1.Time{Time: time.Now()}
					}
					taintedNode.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKey,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectNoExecute,
							TimeAdded: taintTimeAdded,
						},
					}

					Expect(k8sClient.Update(ctx.TODO(), taintedNode)).NotTo(HaveOccurred())
					log.Info("Taint node", "Index", i, "Node name", taintedNode.Name, "Node taints", taintedNode.Spec.Taints)
				}
				// Replace all tainted nodes in one reconciliation loop
				cluster.Spec.AutomationOptions.MaxConcurrentReplacements = &concurrentTaints
				Expect(k8sClient.Update(ctx.TODO(), cluster)).NotTo(HaveOccurred())

				time.Sleep(time.Second * time.Duration(*cluster.Spec.AutomationOptions.Replacements.TaintReplacementTimeSeconds+1))
			})

			It("should remove all pods on tainted nodes", func() {
				retry := len(taintedNodes) * 2 // Hack: Ensure reconciler replaces all tainted nodes
				for {                          // re-run reconcileCluster up to retry times, assuming each reconciliation replaces at least one pod
					result, err := reconcileCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					if !result.Requeue || retry <= 0 {
						break
					}
					time.Sleep(time.Microsecond * time.Duration(500)) // Removing this will cause test failure because not all tainted pods are removed
					retry = retry - 1
				}

				Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
			})
		})
	})

	Context("with no missing processes", func() {
		It("should return nil",
			func() {
				Expect(result).To(BeNil())
			})

		It("should not mark anything for removal", func() {
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})
	})

	Context("with a process that has been missing for a long time", func() {
		BeforeEach(func() {
			processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-2")
			processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
				ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
				Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
			})
		})

		Context("with no other removals", func() {
			It("should requeue", func() {
				Expect(result).NotTo(BeNil())
				Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
			})

			It("should mark the process group for removal", func() {
				Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-2"}))
			})

			It("should not be marked to skip exclusion", func() {
				for _, pg := range cluster.Status.ProcessGroups {
					if pg.ProcessGroupID != "storage-2" {
						continue
					}

					Expect(pg.ExclusionSkipped).To(BeFalse())
				}
			})

			When("EmptyMonitorConf is set to true", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.EmptyMonitorConf = true
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("Crash loop is set for all process groups", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"*"}
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("Crash loop is set for the specific process group", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-2"}
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("Crash loop is set for the main container", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
						{
							ContainerName: fdbv1beta2.MainContainerName,
							Targets:       []fdbv1beta2.ProcessGroupID{"storage-2"},
						},
					}
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("Crash loop is set for the sidecar container", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
						{
							ContainerName: fdbv1beta2.SidecarContainerName,
							Targets:       []fdbv1beta2.ProcessGroupID{"storage-2"},
						},
					}
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})
		})

		Context("with multiple failed processes", func() {
			BeforeEach(func() {
				processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-3")
				processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
					ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
					Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
				})
			})

			It("should requeue", func() {
				Expect(result).NotTo(BeNil())
				Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
			})

			It("should mark the first process group for removal", func() {
				Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-2"}))
			})

			It("should not be marked to skip exclusion", func() {
				for _, pg := range cluster.Status.ProcessGroups {
					if pg.ProcessGroupID != "storage-2" {
						continue
					}

					Expect(pg.ExclusionSkipped).To(BeFalse())
				}
			})
		})

		Context("with another in-flight exclusion", func() {
			BeforeEach(func() {
				processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-3")
				processGroup.MarkForRemoval()
			})

			It("should return nil", func() {
				Expect(result).To(BeNil())
			})

			It("should not mark the process group for removal", func() {
				Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-3"}))
			})

			When("max concurrent replacements is set to two", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(2)
				})

				It("should requeue", func() {
					Expect(result).NotTo(BeNil())
					Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
				})

				It("should mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-2", "storage-3"}))
				})
			})

			When("max concurrent replacements is set to zero", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements = pointer.Int(0)
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-3"}))
				})
			})
		})

		Context("with another complete exclusion", func() {
			BeforeEach(func() {
				processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-3")
				processGroup.MarkForRemoval()
				processGroup.SetExclude()
			})

			It("should requeue", func() {
				Expect(result).NotTo(BeNil())
				Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
			})

			It("should mark the process group for removal", func() {
				Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-2", "storage-3"}))
			})
		})

		Context("with no addresses", func() {
			BeforeEach(func() {
				processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-2")
				processGroup.Addresses = nil
			})

			It("should requeue", func() {
				Expect(result).NotTo(BeNil())
				Expect(result.message).To(Equal("Removals have been updated in the cluster status"))
			})

			It("should mark the process group for removal", func() {
				Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-2"}))
			})

			It("should marked to skip exclusion", func() {
				for _, pg := range cluster.Status.ProcessGroups {
					if pg.ProcessGroupID != "storage-2" {
						continue
					}

					Expect(pg.ExclusionSkipped).To(BeTrue())
				}
			})

			When("the cluster is not available", func() {
				BeforeEach(func() {
					processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-2")
					processGroup.Addresses = nil

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.FrozenStatus = &fdbv1beta2.FoundationDBStatus{
						Client: fdbv1beta2.FoundationDBStatusLocalClientInfo{
							DatabaseStatus: fdbv1beta2.FoundationDBStatusClientDBStatus{
								Available: false,
							},
						},
					}
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})

			When("the cluster doesn't have full fault tolerance", func() {
				BeforeEach(func() {
					processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-2")
					processGroup.Addresses = nil

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					adminClient.MaxZoneFailuresWithoutLosingData = pointer.Int(0)
				})

				It("should return nil", func() {
					Expect(result).To(BeNil())
				})

				It("should not mark the process group for removal", func() {
					Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
				})
			})
		})
	})

	Context("with a process that has been missing for a brief time", func() {
		BeforeEach(func() {
			processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-2")
			processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
				ProcessGroupConditionType: fdbv1beta2.MissingProcesses,
				Timestamp:                 time.Now().Unix(),
			})
		})

		It("should return nil", func() {
			Expect(result).To(BeNil())
		})

		It("should not mark the process group for removal", func() {
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})
	})

	Context("with a process that has had an incorrect pod spec for a long time", func() {
		BeforeEach(func() {
			processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-2")
			processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, &fdbv1beta2.ProcessGroupCondition{
				ProcessGroupConditionType: fdbv1beta2.IncorrectPodSpec,
				Timestamp:                 time.Now().Add(-1 * time.Hour).Unix(),
			})
		})

		It("should return nil", func() {
			Expect(result).To(BeNil())
		})

		It("should not mark the process group for removal", func() {
			Expect(getRemovedProcessGroupIDs(cluster)).To(Equal([]fdbv1beta2.ProcessGroupID{}))
		})
	})
})

// getRemovedProcessGroupIDs returns a list of ids for the process groups that are marked for removal.
func getRemovedProcessGroupIDs(cluster *fdbv1beta2.FoundationDBCluster) []fdbv1beta2.ProcessGroupID {
	results := make([]fdbv1beta2.ProcessGroupID, 0)
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.IsMarkedForRemoval() {
			results = append(results, processGroupStatus.ProcessGroupID)
		}
	}
	return results
}

func getPodByProcessGroupID(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, processGroupID fdbv1beta2.ProcessGroupID) *corev1.Pod {
	pods, err := clusterReconciler.PodLifecycleManager.GetPods(ctx.TODO(), clusterReconciler, cluster, internal.GetPodListOptions(cluster, processClass, string(processGroupID))...)
	if err != nil {
		return nil
	}

	for _, pod := range pods {
		if internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta) == processGroupID {
			return pod
		}
	}

	return nil
}
