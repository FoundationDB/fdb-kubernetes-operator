/*
 * operator_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package operator

/*
This test suite includes functional tests to ensure normal operational tasks are working fine.
Those tests include replacements of healthy or fault Pods and setting different configurations.

The assumption is that every test case reverts the changes that were done on the cluster.
In order to improve the test speed we only create one FoundationDB cluster initially.
This cluster will be used for all tests.
*/

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

func validateStorageClass(processClass fdbv1beta2.ProcessClass, targetStorageClass string) {
	Eventually(func() map[string]fdbv1beta2.None {
		storageClassNames := make(map[string]fdbv1beta2.None)
		volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(processClass)
		for _, volumeClaim := range volumeClaims.Items {
			storageClassNames[*volumeClaim.Spec.StorageClassName] = fdbv1beta2.None{}
		}
		return storageClassNames
	}, 5*time.Minute).Should(Equal(map[string]fdbv1beta2.None{targetStorageClass: {}}))
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbCluster(
		fixtures.DefaultClusterConfig(false),
		factory.GetClusterOptions()...,
	)

	// In order to test the robustness of the operator we try to kill the operator Pods every minute.
	if factory.ChaosTestsEnabled() {
		factory.ScheduleInjectPodKill(
			fixtures.GetOperatorSelector(fdbCluster.Namespace()),
			"*/2 * * * *",
			chaosmesh.OneMode,
		)
	}
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator", Label("e2e", "pr"), func() {
	var availabilityCheck bool

	AfterEach(func() {
		// Reset availabilityCheck if a test case removes this check.
		availabilityCheck = true
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
	})

	JustBeforeEach(func() {
		if availabilityCheck {
			err := fdbCluster.InvariantClusterStatusAvailable()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	PWhen("nodes in the cluster are tainted", func() {
		taintKeyMaintenance := "maintenance"
		taintKeyMaintenanceDuration := int64(2)
		taintKeyStar := "*"
		taintKeyStarDuration := int64(5)
		var initialPods *corev1.PodList
		var taintedNode *corev1.Node
		var taintedNodes []*corev1.Node
		historyTaintedNodes := make(map[string]fdbv1beta2.None)
		numNodesTainted := 2 // TODO: Change to higher number 5
		ensurePodIsDeletedTimeoutMinutes := 20

		BeforeEach(func() {
			// Cleanup tainted nodes in case previous test fails which will leave tainted nodes behind
			for nodeName := range historyTaintedNodes {
				log.Printf("BeforeEach Reset node as untainted:%s\n", nodeName)
				node := fdbCluster.GetNode(nodeName)
				node.Spec.Taints = []corev1.Taint{}
				fdbCluster.UpdateNode(node)
			}

			initialPods = fdbCluster.GetStatelessPods()

			// Setup cluster's taint config and wait for long enough
			fdbCluster.SetClusterTaintConfig([]fdbv1beta2.TaintReplacementOption{
				{
					Key:               &taintKeyMaintenance,
					DurationInSeconds: &taintKeyMaintenanceDuration,
				},
			}, pointer.Int(1))

			Expect(len(fdbCluster.GetCluster().Spec.AutomationOptions.Replacements.TaintReplacementOptions)).To(BeNumerically(">=", 1))

			Expect(*fdbCluster.GetAutomationOptions().Replacements.Enabled).To(BeTrue())
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			// Reset taint related options to default value in e2e test
			fdbCluster.SetClusterTaintConfig([]fdbv1beta2.TaintReplacementOption{}, pointer.Int(150))
			// untaint the nodes
			log.Printf("AfterEach Cleanup: Untaint the single node:%s\n", taintedNode.Name)
			historyTaintedNodes[taintedNode.Name] = fdbv1beta2.None{}
			taintedNode = fdbCluster.GetNode(taintedNode.Name)
			taintedNode.Spec.Taints = []corev1.Taint{}
			fdbCluster.UpdateNode(taintedNode)
			for i, node := range taintedNodes {
				historyTaintedNodes[node.Name] = fdbv1beta2.None{}
				log.Printf("Cleanup: Untaint the %dth node:%s\n", i, node.Name)
				node = fdbCluster.GetNode(node.Name)
				node.Spec.Taints = []corev1.Taint{}
				fdbCluster.UpdateNode(node)
			}
			taintedNodes = []*corev1.Node{}
		})

		When("cluster enables taint feature on focused maintenance taint key", func() {
			It("should remove the targeted Pod whose Node is tainted", func() {
				replacedPod := fixtures.RandomPickOnePod(initialPods.Items)
				// Taint replacePod's node
				taintedNode = fdbCluster.GetNode(replacedPod.Spec.NodeName)
				taintedNode.Spec.Taints = []corev1.Taint{
					{
						Key:       taintKeyMaintenance,
						Value:     "rack_maintenance",
						Effect:    corev1.TaintEffectPreferNoSchedule,
						TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
					},
				}
				log.Printf("Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n", replacedPod.Name,
					taintedNode.Name, taintedNode.Spec.Taints, taintedNode.Spec.Taints[0].TimeAdded.Time, time.Now())
				fdbCluster.UpdateNode(taintedNode)

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				fdbCluster.EnsurePodIsDeletedWithCustomTimeout(replacedPod.Name, ensurePodIsDeletedTimeoutMinutes)
			})

			It("should remove all Pods whose Nodes are tainted", func() {
				replacedPods := fixtures.RandomPickPod(initialPods.Items, numNodesTainted)
				for _, pod := range replacedPods {
					// Taint replacePod's node
					node := fdbCluster.GetNode(pod.Spec.NodeName)
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectPreferNoSchedule,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Millisecond * time.Duration(taintKeyMaintenanceDuration*1000+int64(rand.Intn(1000))))},
						},
					}
					log.Printf("Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n", pod.Name,
						node.Name, node.Spec.Taints, node.Spec.Taints[0].TimeAdded.Time, time.Now())
					fdbCluster.UpdateNode(node)
					taintedNodes = append(taintedNodes, node)
				}

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				for i, pod := range replacedPods {
					log.Printf("Ensure %dth pod:%s is deleted\n", i, pod.Name)
					fdbCluster.EnsurePodIsDeletedWithCustomTimeout(pod.Name, ensurePodIsDeletedTimeoutMinutes)
				}
			})
		})

		When("cluster enables taint feature on star taint key", func() {
			taintKeyMaintenanceDuration = int64(1)
			taintKeyStar = "*"
			taintKeyStarDuration = int64(5)

			BeforeEach(func() {
				// Custom TaintReplacementOptions to taintKeyStar
				fdbCluster.SetClusterTaintConfig([]fdbv1beta2.TaintReplacementOption{
					{
						Key:               &taintKeyStar,
						DurationInSeconds: &taintKeyStarDuration,
					},
				}, pointer.Int(1))

				automationOptions := fdbCluster.GetAutomationOptions()
				Expect(len(automationOptions.Replacements.TaintReplacementOptions)).To(BeNumerically(">=", 1))
				Expect(*automationOptions.Replacements.Enabled).To(BeTrue())

			})

			It("should eventually remove the targeted Pod whose Node is tainted", func() {
				replacedPod := fixtures.RandomPickOnePod(initialPods.Items)
				// Taint replacePod's node
				node := fdbCluster.GetNode(replacedPod.Spec.NodeName)
				node.Spec.Taints = []corev1.Taint{
					{
						Key:       taintKeyMaintenance,
						Value:     "rack_maintenance",
						Effect:    corev1.TaintEffectPreferNoSchedule,
						TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1))},
					},
				}
				log.Printf("Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n", replacedPod.Name,
					node.Name, node.Spec.Taints, node.Spec.Taints[0].TimeAdded.Time, time.Now())
				fdbCluster.UpdateNode(node)

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				fdbCluster.EnsurePodIsDeletedWithCustomTimeout(replacedPod.Name, ensurePodIsDeletedTimeoutMinutes)
			})

			It("should eventually remove all Pods whose Nodes are tainted", func() {
				replacedPods := fixtures.RandomPickPod(initialPods.Items, numNodesTainted)
				for _, pod := range replacedPods {
					// Taint replacePod's node
					node := fdbCluster.GetNode(pod.Spec.NodeName)
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectPreferNoSchedule,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Millisecond * time.Duration(taintKeyStarDuration*1000+int64(rand.Intn(1000))))},
						},
					}
					log.Printf("Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n", pod.Name,
						node.Name, node.Spec.Taints, node.Spec.Taints[0].TimeAdded.Time, time.Now())
					fdbCluster.UpdateNode(node)
				}

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				for i, pod := range replacedPods {
					log.Printf("Ensure %dth pod:%s is deleted\n", i, pod.Name)
					fdbCluster.EnsurePodIsDeletedWithCustomTimeout(pod.Name, ensurePodIsDeletedTimeoutMinutes)
				}
			})
		})

		When("cluster disables taint feature with empty taint option", func() {
			taintKeyMaintenanceDuration = int64(1)
			taintKeyStar = "*"

			BeforeEach(func() {
				// Custom TaintReplacementOptions to taintKeyStar
				fdbCluster.SetClusterTaintConfig([]fdbv1beta2.TaintReplacementOption{}, pointer.Int(1))

				automationOptions := fdbCluster.GetAutomationOptions()
				Expect(len(automationOptions.Replacements.TaintReplacementOptions)).To(BeNumerically("==", 0))
				Expect(*automationOptions.Replacements.Enabled).To(BeTrue())

			})

			It("should not remove any Pod whose Nodes are tainted", func() {
				targetPods := fixtures.RandomPickPod(initialPods.Items, numNodesTainted)
				var targetPodProcessGroupIDs []fdbv1beta2.ProcessGroupID
				for _, pod := range targetPods {
					targetPodProcessGroupIDs = append(targetPodProcessGroupIDs, internal.GetProcessGroupIDFromMeta(fdbCluster.GetCluster(), pod.ObjectMeta))
					// Taint replacePod's node
					node := fdbCluster.GetNode(pod.Spec.NodeName)
					node.Spec.Taints = []corev1.Taint{
						{
							Key:       taintKeyMaintenance,
							Value:     "rack_maintenance",
							Effect:    corev1.TaintEffectPreferNoSchedule,
							TimeAdded: &metav1.Time{Time: time.Now().Add(-time.Millisecond * time.Duration(taintKeyStarDuration*1000+int64(rand.Intn(1000))))},
						},
					}
					log.Printf("Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n", pod.Name,
						node.Name, node.Spec.Taints, node.Spec.Taints[0].TimeAdded.Time, time.Now())
					fdbCluster.UpdateNode(node)
				}

				Consistently(func() bool {
					for _, groupID := range targetPodProcessGroupIDs {
						processGroupStatus := fdbv1beta2.FindProcessGroupByID(fdbCluster.GetCluster().Status.ProcessGroups, groupID)
						Expect(processGroupStatus).NotTo(BeNil())
						Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).To(BeNil())
					}
					return true
				}).WithTimeout(time.Duration(*fdbCluster.GetAutomationOptions().Replacements.TaintReplacementTimeSeconds-1) * time.Second).WithPolling(1 * time.Second).Should(BeTrue())

				// Wait for operator to replace the pod
				time.Sleep(time.Second * time.Duration(*fdbCluster.GetAutomationOptions().Replacements.TaintReplacementTimeSeconds+1))
				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				for i, pod := range targetPods {
					log.Printf("Ensure %dth pod:%s is not deleted\n", i, pod.Name)
					deleted := fdbCluster.CheckPodIsDeleted(pod.Name)
					Expect(deleted).To(BeFalse())
				}
			})
		})
	})

	It("should set the fault domain for all process groups", func() {
		for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
			Expect(processGroup.FaultDomain).NotTo(BeEmpty())
		}

		// TODO (johscheuer): We should check here further fields in the FoundationDBCluster resource to make sure the
		// fields that we expect are actually set.
	})

	When("replacing a Pod", func() {
		var replacedPod corev1.Pod
		var useLocalitiesForExclusion bool

		JustBeforeEach(func() {
			initialPods := fdbCluster.GetStatelessPods()
			replacedPod = fixtures.RandomPickOnePod(initialPods.Items)
			fdbCluster.ReplacePod(replacedPod, true)
		})

		BeforeEach(func() {
			useLocalitiesForExclusion = pointer.BoolDeref(fdbCluster.GetCluster().Spec.AutomationOptions.UseLocalitiesForExclusion, false)
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			// Make sure we reset the previous behaviour.
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(useLocalitiesForExclusion)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
		})

		When("IP addresses are used for exclusion", func() {
			BeforeEach(func() {
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(false)
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			It("should remove the targeted Pod", func() {
				fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
			})
		})

		When("localities are used for exclusion", func() {
			BeforeEach(func() {
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(true)
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			It("should remove the targeted Pod", func() {
				Expect(pointer.BoolDeref(fdbCluster.GetCluster().Spec.AutomationOptions.UseLocalitiesForExclusion, false)).To(BeTrue())
				fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
			})
		})
	})

	When("setting storageServersPerPod", func() {
		var initialStorageServerPerPod, expectedPodCnt, expectedStorageProcessesCnt int

		BeforeEach(func() {
			initialStorageServerPerPod = fdbCluster.GetStorageServerPerPod()
			initialPods := fdbCluster.GetStoragePods()
			expectedPodCnt = len(initialPods.Items)
			expectedStorageProcessesCnt = expectedPodCnt * initialStorageServerPerPod
			log.Printf(
				"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
				expectedPodCnt,
				expectedStorageProcessesCnt,
			)
			fdbCluster.ValidateProcessesCount(fdbv1beta2.ProcessClassStorage, expectedPodCnt, expectedStorageProcessesCnt)
		})

		AfterEach(func() {
			log.Printf("set storage server per pod to %d", initialStorageServerPerPod)
			Expect(fdbCluster.SetStorageServerPerPod(initialStorageServerPerPod)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*initialStorageServerPerPod,
			)
			fdbCluster.ValidateProcessesCount(fdbv1beta2.ProcessClassStorage,
				expectedPodCnt,
				expectedPodCnt*initialStorageServerPerPod,
			)
		})

		It("should update the storage servers to the expected amount", func() {
			// Update to double the SS per Disk
			serverPerPod := initialStorageServerPerPod * 2
			log.Printf("set storage server per Pod to %d", serverPerPod)
			Expect(fdbCluster.SetStorageServerPerPod(serverPerPod)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*serverPerPod,
			)
			fdbCluster.ValidateProcessesCount(fdbv1beta2.ProcessClassStorage, expectedPodCnt, expectedPodCnt*serverPerPod)
		})
	})

	When("changing the volume size", func() {
		var initialPods *corev1.PodList
		var newSize, initialStorageSize resource.Quantity

		BeforeEach(func() {
			var err error

			initialPods = fdbCluster.GetLogPods()
			// We use ProcessClassGeneral here because we are not setting any specific settings for the Log processes.
			initialStorageSize, err = fdbCluster.GetVolumeSize(fdbv1beta2.ProcessClassGeneral)
			Expect(err).NotTo(HaveOccurred())
			// Add 10G to the current size
			newSize = initialStorageSize.DeepCopy()
			newSize.Add(resource.MustParse("10G"))
			Expect(
				fdbCluster.SetVolumeSize(fdbv1beta2.ProcessClassGeneral, newSize),
			).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(
				fdbCluster.SetVolumeSize(fdbv1beta2.ProcessClassGeneral, initialStorageSize),
			).NotTo(HaveOccurred())
		})

		It("should replace all the log Pods and use the new volume size", func() {
			pods := fdbCluster.GetLogPods()
			Expect(pods.Items).NotTo(ContainElements(initialPods.Items))
			volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(
				fdbv1beta2.ProcessClassLog,
			)
			Expect(len(volumeClaims.Items)).To(Equal(len(initialPods.Items)))
			for _, volumeClaim := range volumeClaims.Items {
				req := volumeClaim.Spec.Resources.Requests["storage"]
				Expect((&req).Value()).To(Equal(newSize.Value()))
			}
		})
	})

	When("Shrinking the number of log processes by one", func() {
		var initialLogPodCount int

		BeforeEach(func() {
			initialLogPodCount = len(fdbCluster.GetLogPods().Items)
		})

		AfterEach(func() {
			// Set the log process count back to the default value
			Expect(fdbCluster.UpdateLogProcessCount(initialLogPodCount)).NotTo(HaveOccurred())
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", initialLogPodCount))
		})

		It("should reduce the number of log processes by one", func() {
			Expect(fdbCluster.UpdateLogProcessCount(initialLogPodCount - 1)).NotTo(HaveOccurred())
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(BeNumerically("==", initialLogPodCount-1))
		})
	})

	When("a Pod is in a failed scheduling state", func() {
		var failedPod corev1.Pod

		BeforeEach(func() {
			initialPods := fdbCluster.GetStatelessPods()
			failedPod = fixtures.RandomPickOnePod(initialPods.Items)
			log.Printf("Setting pod %s to unschedulable.", failedPod.Name)
			Expect(fdbCluster.SetPodAsUnschedulable(failedPod)).NotTo(HaveOccurred())
			fdbCluster.ReplacePod(failedPod, true)
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearBuggifyNoSchedule(true)).NotTo(HaveOccurred())
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should remove the targeted Pod", func() {
			fdbCluster.EnsurePodIsDeleted(failedPod.Name)
		})
	})

	When("a Pod is unscheduled and another Pod is being replaced", func() {
		var failedPod *corev1.Pod
		var podToReplace *corev1.Pod

		BeforeEach(func() {
			failedPod = fixtures.ChooseRandomPod(fdbCluster.GetStatelessPods())
			podToReplace = fixtures.ChooseRandomPod(fdbCluster.GetStatelessPods())
			log.Println(
				"Failed (unscheduled) Pod:",
				failedPod.Name,
				", Pod to replace:",
				podToReplace.Name,
			)
			Expect(fdbCluster.SetPodAsUnschedulable(*failedPod)).NotTo(HaveOccurred())
			fdbCluster.ReplacePod(*podToReplace, false)
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearBuggifyNoSchedule(false)).NotTo(HaveOccurred())
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should remove the targeted Pod", func() {
			fdbCluster.EnsurePodIsDeleted(podToReplace.Name)
		})
	})

	When("Skipping a cluster for reconciliation", func() {
		var initialGeneration int64

		BeforeEach(func() {
			Expect(fdbCluster.SetSkipReconciliation(true)).ShouldNot(HaveOccurred())
			initialGeneration = fdbCluster.GetCluster().Status.Generations.Reconciled
		})

		It("should not reconcile and keep the cluster in the same generation", func() {
			initialStoragePods := fdbCluster.GetStoragePods()
			podToDelete := fixtures.ChooseRandomPod(initialStoragePods)
			log.Printf("deleting storage pod %s/%s", podToDelete.Namespace, podToDelete.Name)
			factory.DeletePod(podToDelete)
			Eventually(func() int {
				return len(fdbCluster.GetStoragePods().Items)
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(BeNumerically("==", len(initialStoragePods.Items)-1))

			Consistently(func() int64 {
				return fdbCluster.GetCluster().Status.Generations.Reconciled
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(BeNumerically("==", initialGeneration))
		})

		AfterEach(func() {
			Expect(fdbCluster.SetSkipReconciliation(false)).ShouldNot(HaveOccurred())
		})
	})

	PWhen("Changing the TLS setting", func() {
		// Currently disabled until a new release of the operator is out
		It("should disable or enable TLS and keep the cluster available", func() {
			// Only change the TLS setting for the cluster and not for the sidecar otherwise we have to recreate
			// all Pods which takes a long time since we recreate the Pods one by one.
			log.Println("disable TLS for main container")
			Expect(fdbCluster.SetTLS(false, true)).NotTo(HaveOccurred())
			Expect(fdbCluster.HasTLSEnabled()).To(BeFalse())
			log.Println("enable TLS for main container")
			Expect(fdbCluster.SetTLS(true, true)).NotTo(HaveOccurred())
			Expect(fdbCluster.HasTLSEnabled()).To(BeTrue())
		})
	})

	When("changing the public IP source", func() {
		It("should change the public IP source and create/delete services", func() {
			log.Printf("set public IP source to %s", fdbv1beta2.PublicIPSourceService)
			Expect(
				fdbCluster.SetPublicIPSource(fdbv1beta2.PublicIPSourceService),
			).ShouldNot(HaveOccurred())
			Eventually(func() bool {
				pods := fdbCluster.GetPods()
				svcList := fdbCluster.GetServices()

				svcMap := make(map[string]struct{}, len(svcList.Items))
				for _, svc := range svcList.Items {
					svcMap[svc.Name] = struct{}{}
				}

				for _, pod := range pods.Items {
					if fdbv1beta2.PublicIPSource(
						pod.Annotations[fdbv1beta2.PublicIPAnnotation],
					) == fdbv1beta2.PublicIPSourcePod {
						continue
					}

					if _, ok := svcMap[pod.Name]; !ok {
						return false
					}

					delete(svcMap, pod.Name)
				}

				// We only expect one service here at the end since we run the cluster with a headless service.
				if fdbCluster.HasHeadlessService() {
					return len(svcMap) == 1
				}
				return len(svcMap) == 0
			}).Should(BeTrue())
		})

		AfterEach(func() {
			log.Printf("set public IP source to %s", fdbv1beta2.PublicIPSourcePod)
			Expect(
				fdbCluster.SetPublicIPSource(fdbv1beta2.PublicIPSourcePod),
			).ShouldNot(HaveOccurred())
			svcList := fdbCluster.GetServices()

			var expectedSvcCnt int
			if fdbCluster.HasHeadlessService() {
				expectedSvcCnt = 1
			}
			Expect(len(svcList.Items)).To(BeNumerically("==", expectedSvcCnt))
		})
	})

	When("Deleting a FDB storage Pod", func() {
		var podToDelete corev1.Pod
		var deleteTime time.Time

		BeforeEach(func() {
			podToDelete = fixtures.RandomPickOnePod(fdbCluster.GetStoragePods().Items)
			log.Printf("deleting storage pod %s/%s", podToDelete.Namespace, podToDelete.Name)
			deleteTime = time.Now()
			factory.DeletePod(&podToDelete)
		})

		It("Should recreate the storage Pod", func() {
			Eventually(func() bool {
				return fdbCluster.GetPod(podToDelete.Name).CreationTimestamp.After(deleteTime)
			}, 3*time.Minute, 1*time.Second).Should(BeTrue())
		})
	})

	When("enabling automatic replacements", func() {
		var exp *fixtures.ChaosMeshExperiment
		var initialReplaceTime time.Duration

		BeforeEach(func() {
			if !factory.ChaosTestsEnabled() {
				Skip("Chaos tests are skipped for the operator")
			}
			availabilityCheck = false
			initialReplaceTime = time.Duration(pointer.IntDeref(
				fdbCluster.GetClusterSpec().AutomationOptions.Replacements.FailureDetectionTimeSeconds,
				90,
			)) * time.Second
			Expect(fdbCluster.SetAutoReplacements(true, 30*time.Second)).ShouldNot(HaveOccurred())
		})

		It("should replace the partitioned Pod", func() {
			pod := fixtures.ChooseRandomPod(fdbCluster.GetStatelessPods())
			log.Printf("partition Pod: %s", pod.Name)
			exp = factory.InjectPartitionBetween(
				fixtures.PodSelector(pod),
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{pod.Namespace},
						LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
					},
				},
			)

			log.Printf("waiting for pod removal: %s", pod.Name)
			Expect(fdbCluster.WaitForPodRemoval(pod)).ShouldNot(HaveOccurred())
			exists, err := factory.DoesPodExist(*pod)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		AfterEach(func() {
			Expect(fdbCluster.SetAutoReplacements(true, initialReplaceTime)).ShouldNot(HaveOccurred())
			factory.DeleteChaosMeshExperimentSafe(exp)
		})
	})

	When("a Pod has a bad disk", func() {
		var podWithIOError corev1.Pod
		var exp *fixtures.ChaosMeshExperiment

		BeforeEach(func() {
			if !factory.ChaosTestsEnabled() {
				Skip("Chaos tests are skipped for the operator")
			}
			availabilityCheck = false
			initialPods := fdbCluster.GetLogPods()
			podWithIOError = fixtures.RandomPickOnePod(initialPods.Items)
			log.Printf("Injecting I/O chaos to %s", podWithIOError.Name)
			exp = factory.InjectDiskFailure(fixtures.PodSelector(&podWithIOError))

			log.Printf("iochaos injected to %s", podWithIOError.Name)
			// File creation should fail due to I/O error
			Eventually(func() error {
				_, _, err := factory.ExecuteCmdOnPod(
					&podWithIOError,
					fdbv1beta2.MainContainerName,
					"touch /var/fdb/data/test",
					false,
				)
				return err
			}, 5*time.Minute).Should(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			factory.DeleteChaosMeshExperimentSafe(exp)
		})

		It("should remove the targeted process", func() {
			fdbCluster.ReplacePod(podWithIOError, true)
			fdbCluster.EnsurePodIsDeleted(podWithIOError.Name)
		})
	})

	When("a Pod has high I/O latency", func() {
		var podWithIOError corev1.Pod
		var exp *fixtures.ChaosMeshExperiment

		BeforeEach(func() {
			if !factory.ChaosTestsEnabled() {
				Skip("Chaos tests are skipped for the operator")
			}
			availabilityCheck = false
			initialPods := fdbCluster.GetLogPods()
			podWithIOError = fixtures.RandomPickOnePod(initialPods.Items)
			log.Printf("injecting iochaos to %s", podWithIOError.Name)
			exp = factory.InjectIOLatency(fixtures.PodSelector(&podWithIOError), "2s")
			log.Printf("iochaos injected to %s", podWithIOError.Name)
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			factory.DeleteChaosMeshExperimentSafe(exp)
		})

		It("should remove the targeted process", func() {
			fdbCluster.ReplacePod(podWithIOError, true)
			fdbCluster.EnsurePodIsDeleted(podWithIOError.Name)
		})
	})

	When("we change the process group prefix", func() {
		prefix := "banana"

		BeforeEach(func() {
			Expect(fdbCluster.SetProcessGroupPrefix(prefix)).NotTo(HaveOccurred())
			Expect(fdbCluster.WaitForReconciliation())
		})

		It("should add the prefix to all instances", func() {
			pods := fdbCluster.GetPods()
			for _, pod := range pods.Items {
				Expect(string(fixtures.GetProcessGroupID(pod))).To(HavePrefix(prefix))
			}
		})
	})

	When("replacing multiple Pods", func() {
		var replacedPods []corev1.Pod

		BeforeEach(func() {
			fdbCluster.ReplacePods(fixtures.RandomPickPod(fdbCluster.GetStatelessPods().Items, 4))
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should replace all the targeted Pods", func() {
			currentPodsNames := fdbCluster.GetPodsNames()
			for _, replacedPod := range replacedPods {
				Expect(currentPodsNames).ShouldNot(ContainElement(replacedPod.Name))
			}
		})
	})

	When("replacing a Pod stuck in Terminating state", func() {
		var podsBeforeReplacement []string
		var replacePod *corev1.Pod

		BeforeEach(func() {
			podsBeforeReplacement = fdbCluster.GetPodsNames()
			replacePod = fixtures.ChooseRandomPod(fdbCluster.GetPods())
			Expect(factory.SetFinalizerForPod(replacePod, []string{"foundationdb.org/test"})).ShouldNot(HaveOccurred())
			fdbCluster.ReplacePod(*replacePod, true)
		})

		AfterEach(func() {
			Expect(factory.SetFinalizerForPod(replacePod, []string{})).ShouldNot(HaveOccurred())
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should replace the Pod stuck in Terminating state", func() {
			// The `replacePod` still exists as a
			// Terminating Pod because it has a finalizer
			// set to it and thus it's not deleted
			// yet. Moreover, `GetPods()` returns pods even
			// in terminating state since it's status.phase is
			// still Running (although the containers have exited).
			Expect(len(fdbCluster.GetPodsNames())).Should(Equal(len(podsBeforeReplacement) + 1))
		})
	})

	When("changing coordinator selection", func() {
		AfterEach(func() {
			Expect(fdbCluster.UpdateCoordinatorSelection(
				[]fdbv1beta2.CoordinatorSelectionSetting{},
			)).ShouldNot(HaveOccurred())
		})

		DescribeTable("changing coordinator selection",
			func(setting []fdbv1beta2.CoordinatorSelectionSetting, expectedProcessClassList []fdbv1beta2.ProcessClass) {
				Expect(fdbCluster.UpdateCoordinatorSelection(setting)).ShouldNot(HaveOccurred())
				pods := fdbCluster.GetCoordinators()
				for _, pod := range pods {
					log.Println(pod.Name)
					Expect(
						fixtures.GetProcessClass(pod),
					).Should(BeElementOf(expectedProcessClassList))
				}
			},
			Entry("selecting only log processes as coordinators",
				[]fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassLog,
						Priority:     0,
					},
				},
				[]fdbv1beta2.ProcessClass{fdbv1beta2.ProcessClassLog},
			),
			Entry("selecting only storage processes as coordinators",
				[]fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     0,
					},
				},
				[]fdbv1beta2.ProcessClass{fdbv1beta2.ProcessClassStorage},
			),
			Entry("selecting both storage and log processes as coordinators but preferring storage",
				[]fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassLog,
						Priority:     0,
					},
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     math.MaxInt32,
					},
				},
				[]fdbv1beta2.ProcessClass{
					fdbv1beta2.ProcessClassLog,
					fdbv1beta2.ProcessClassStorage,
				},
			),
		)
	})

	When("increasing the number of log Pods by one", func() {
		var initialPodCount int

		BeforeEach(func() {
			initialPodCount = len(fdbCluster.GetLogPods().Items)
		})

		AfterEach(func() {
			Expect(fdbCluster.UpdateLogProcessCount(initialPodCount)).ShouldNot(HaveOccurred())
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", initialPodCount))
		})

		It("should increase the count of log Pods by one", func() {
			Expect(fdbCluster.UpdateLogProcessCount(initialPodCount + 1)).ShouldNot(HaveOccurred())
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", initialPodCount+1))
		})
	})

	When("setting 2 logs per disk", func() {
		var initialLogServerPerPod, expectedPodCnt, expectedLogProcessesCnt int

		BeforeEach(func() {
			initialLogServerPerPod = fdbCluster.GetLogServersPerPod()
			initialPods := fdbCluster.GetLogPods()
			expectedPodCnt = len(initialPods.Items)
			expectedLogProcessesCnt = expectedPodCnt * initialLogServerPerPod
			log.Printf(
				"expectedPodCnt: %d, expectedProcessesCnt: %d",
				expectedPodCnt,
				expectedLogProcessesCnt,
			)
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", expectedPodCnt))
		})

		AfterEach(func() {
			log.Printf("set log servers per Pod to %d", initialLogServerPerPod)
			Expect(fdbCluster.SetLogServersPerPod(initialLogServerPerPod, true)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*initialLogServerPerPod,
			)
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", expectedPodCnt))
		})

		It("should update the log servers to the expected amount", func() {
			serverPerPod := initialLogServerPerPod * 2
			log.Printf("set log servers per Pod to %d", initialLogServerPerPod)
			Expect(fdbCluster.SetLogServersPerPod(serverPerPod, true)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*serverPerPod,
			)
			fdbCluster.ValidateProcessesCount(fdbv1beta2.ProcessClassLog, expectedPodCnt, expectedPodCnt*serverPerPod)
		})
	})

	When("setting 2 logs per disk to use transaction process", func() {
		var initialLogServerPerPod, expectedPodCnt, expectedLogProcessesCnt int

		BeforeEach(func() {
			initialLogServerPerPod = fdbCluster.GetLogServersPerPod()
			initialPods := fdbCluster.GetLogPods()
			expectedPodCnt = len(initialPods.Items)
			expectedLogProcessesCnt = expectedPodCnt * initialLogServerPerPod
			log.Printf(
				"expectedPodCnt: %d, expectedProcessesCnt: %d",
				expectedPodCnt,
				expectedLogProcessesCnt,
			)
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", expectedPodCnt))
		})

		AfterEach(func() {
			log.Printf("set log servers per Pod to %d", initialLogServerPerPod)
			Expect(fdbCluster.SetTransactionServerPerPod(initialLogServerPerPod, expectedLogProcessesCnt, true)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*initialLogServerPerPod,
			)
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", expectedPodCnt))
		})

		It("should update the log servers to the expected amount and should create transaction Pods", func() {
			serverPerPod := initialLogServerPerPod * 2
			Expect(fdbCluster.SetTransactionServerPerPod(serverPerPod, expectedLogProcessesCnt, true)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*serverPerPod,
			)
			fdbCluster.ValidateProcessesCount(fdbv1beta2.ProcessClassTransaction, expectedPodCnt, expectedPodCnt*serverPerPod)
		})
	})

	When("Migrating a cluster to a different storage class", func() {
		var defaultStorageClass, targetStorageClass string

		BeforeEach(func() {
			// This will only return StorageClasses that have a label foundationdb.org/operator-testing=true defined.
			storageClasses := factory.GetStorageClasses(map[string]string{
				"foundationdb.org/operator-testing": "true",
			})
			if len(storageClasses.Items) < 2 {
				Skip("This test requires at least two available StorageClasses")
			}

			defaultStorageClass = factory.GetDefaultStorageClass()
			// Select all StorageClasses that are not the default one as candidate.
			candidates := make([]string, 0, len(storageClasses.Items))
			for _, storageClass := range storageClasses.Items {
				if storageClass.Name == defaultStorageClass {
					continue
				}

				candidates = append(candidates, storageClass.Name)
			}

			targetStorageClass = candidates[rand.Intn(len(candidates))]

			Expect(fdbCluster.UpdateStorageClass(
				targetStorageClass,
				fdbv1beta2.ProcessClassLog,
			)).NotTo(HaveOccurred())
		})

		It("should migrate the cluster", func() {
			validateStorageClass(fdbv1beta2.ProcessClassLog, targetStorageClass)
		})

		AfterEach(func() {
			Expect(fdbCluster.UpdateStorageClass(
				defaultStorageClass,
				fdbv1beta2.ProcessClassLog,
			)).NotTo(HaveOccurred())
		})
	})

	When("Replacing a Pod with PVC stuck in Terminating state", func() {
		var replacePod *corev1.Pod
		var initialVolumeClaims *corev1.PersistentVolumeClaimList
		var pvc corev1.PersistentVolumeClaim

		BeforeEach(func() {
			replacePod = fixtures.ChooseRandomPod(fdbCluster.GetLogPods())
			volClaimName := fixtures.GetPvc(replacePod)
			initialVolumeClaims = fdbCluster.GetVolumeClaimsForProcesses(
				fdbv1beta2.ProcessClassLog,
			)
			for _, volumeClaim := range initialVolumeClaims.Items {
				if volumeClaim.Name == volClaimName {
					pvc = volumeClaim
					break
				}
			}
			log.Printf("adding finalizer to pvc: %s", volClaimName)
			Expect(
				fdbCluster.SetFinalizerForPvc([]string{"foundationdb.org/test"}, pvc),
			).ShouldNot(HaveOccurred())
			fdbCluster.ReplacePod(*replacePod, true)
		})

		AfterEach(func() {
			Expect(fdbCluster.SetFinalizerForPvc([]string{}, pvc)).ShouldNot(HaveOccurred())
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should replace the PVC stuck in Terminating state", func() {
			fdbCluster.EnsurePodIsDeleted(replacePod.Name)
			volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(
				fdbv1beta2.ProcessClassLog,
			)
			volumeClaimNames := make([]string, 0, len(volumeClaims.Items))
			for _, volumeClaim := range volumeClaims.Items {
				volumeClaimNames = append(volumeClaimNames, volumeClaim.Name)
			}
			Expect(volumeClaimNames).Should(ContainElement(pvc.Name))
			Expect(len(volumeClaims.Items)).Should(Equal(len(initialVolumeClaims.Items) + 1))
		})
	})

	// This test is currently flaky and we are working on making it stable.
	PWhen("setting the empty config to true", func() {
		var storageProcessCnt int

		BeforeEach(func() {
			storageProcessCnt = fdbCluster.GetProcessCount(
				fdbv1beta2.ProcessRoleStorage,
			)
			Expect(fdbCluster.SetEmptyMonitorConf(true)).ShouldNot(HaveOccurred())
		})

		It("should stop all running processes", func() {
			Consistently(func() int {
				return fdbCluster.GetProcessCount(fdbv1beta2.ProcessRoleStorage)
			}).Should(BeNumerically("==", -1))
		})

		AfterEach(func() {
			Expect(fdbCluster.SetEmptyMonitorConf(false)).ShouldNot(HaveOccurred())
			// Wait until all storage servers are ready and their "role" information gets
			// reported correctly in "status" output.
			time.Sleep(5 * time.Minute)
			Consistently(func() int {
				return fdbCluster.GetProcessCount(fdbv1beta2.ProcessRoleStorage)
			}).Should(BeNumerically("==", storageProcessCnt))
		})
	})

	When("using the buggify option to ignore a process during the restart", func() {
		var newGeneralCustomParameters, initialGeneralCustomParameters fdbv1beta2.FoundationDBCustomParameters
		var newKnob string
		var pickedPod *corev1.Pod

		BeforeEach(func() {
			// Disable the availability check to prevent flaky tests if the small cluster takes longer to be restarted
			availabilityCheck = false
			initialGeneralCustomParameters = fdbCluster.GetCustomParameters(
				fdbv1beta2.ProcessClassGeneral,
			)

			newKnob = "knob_max_trace_lines=1000000"
			newGeneralCustomParameters = append(
				initialGeneralCustomParameters,
				fdbv1beta2.FoundationDBCustomParameter(newKnob),
			)

			pickedPod = fixtures.ChooseRandomPod(fdbCluster.GetStatelessPods())
			log.Println("Selected Pod:", pickedPod.Name, " to be skipped during the restart")
			fdbCluster.SetIgnoreDuringRestart(
				[]fdbv1beta2.ProcessGroupID{fixtures.GetProcessGroupID(*pickedPod)},
			)

			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassGeneral,
					newGeneralCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.SetCustomParameters(
				fdbv1beta2.ProcessClassGeneral,
				initialGeneralCustomParameters,
				false,
			)).NotTo(HaveOccurred())
			fdbCluster.SetIgnoreDuringRestart(nil)
		})

		It("should not restart the process on the ignore list", func() {
			processGroupID := fixtures.GetProcessGroupID(*pickedPod)

			// Ensure that the process group has the condition IncorrectCommandLine and is kept in that state for 1 minute.
			Eventually(func() bool {
				cluster := fdbCluster.GetCluster()
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID != processGroupID {
						continue
					}

					// The IncorrectCommandLine condition represents that the process must be restarted.
					return processGroup.GetConditionTime(fdbv1beta2.IncorrectCommandLine) != nil
				}

				return false
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(12).Should(BeTrue())
		})
	})

	// TODO (johscheuer): enable this test once the CRD is updated in out CI cluster.
	PWhen("a process group is set to be blocked for removal", func() {
		var podMarkedForRemoval corev1.Pod
		var processGroupID fdbv1beta2.ProcessGroupID

		BeforeEach(func() {
			initialPods := fdbCluster.GetStatelessPods()
			podMarkedForRemoval = fixtures.RandomPickOnePod(initialPods.Items)
			log.Println("Setting Pod", podMarkedForRemoval.Name, "to be blocked to be removed and mark it for removal.")
			processGroupID = fixtures.GetProcessGroupID(podMarkedForRemoval)
			fdbCluster.SetBuggifyBlockRemoval([]fdbv1beta2.ProcessGroupID{processGroupID})
			fdbCluster.ReplacePod(podMarkedForRemoval, false)
		})

		AfterEach(func() {
			fdbCluster.SetBuggifyBlockRemoval(nil)
			fdbCluster.EnsurePodIsDeleted(podMarkedForRemoval.Name)
		})

		It("should exclude the Pod but not remove the resources", func() {
			Eventually(func() bool {
				cluster := fdbCluster.GetCluster()

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID != processGroupID {
						continue
					}

					return !processGroup.ExclusionTimestamp.IsZero() && !processGroup.RemovalTimestamp.IsZero()
				}

				return false
			}).WithTimeout(5 * time.Minute).WithPolling(15 * time.Second).ShouldNot(BeTrue())
			Consistently(func() *corev1.Pod {
				return fdbCluster.GetPod(podMarkedForRemoval.Name)
			}).WithTimeout(1 * time.Minute).WithPolling(15 * time.Second).ShouldNot(BeNil())
		})
	})

	When("setting the maintenance mode", func() {
		When("maintenance mode is on", func() {
			BeforeEach(func() {
				command := fmt.Sprintf("maintenance on %s %s", "operator-test-1-storage-4", "40000")
				_, _ = fdbCluster.RunFdbCliCommandInOperator(command, false, 40)
				// Update the annotation of the FoundationDBCluster resource to make sure the operator starts a new
				// reconciliation loop. Since the maintenance mode is set outside of Kubernetes the operator will
				// not automatically be triggered to start a reconciliation loop.
				fdbCluster.ForceReconcile()
			})

			AfterEach(func() {
				_, _ = fdbCluster.RunFdbCliCommandInOperator("maintenance off", false, 40)
			})

			It("should update the machine-readable status and thr FoundationDBCluster Status to contain the maintenance zone", func() {
				// Make sure the machine-readable status reflects the maintenance mode.
				Eventually(func() fdbv1beta2.FaultDomain {
					return fdbCluster.GetStatus().Cluster.MaintenanceZone
				}).WithPolling(1 * time.Second).WithTimeout(1 * time.Minute).Should(Equal(fdbv1beta2.FaultDomain("operator-test-1-storage-4")))
				// Make sure the FoundationDBClusterStatus contains the ZoneID.
				Eventually(func() fdbv1beta2.FaultDomain {
					return fdbCluster.GetCluster().Status.MaintenanceModeInfo.ZoneID
				}).WithPolling(1 * time.Second).WithTimeout(1 * time.Minute).Should(Equal(fdbv1beta2.FaultDomain("operator-test-1-storage-4")))
			})
		})

		// TODO (johscheuer): https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1775
	})

	When("a process group has no address assigned and should be removed", func() {
		var processGroupID fdbv1beta2.ProcessGroupID
		var podName string
		var initialReplacementDuration time.Duration

		BeforeEach(func() {
			initialReplacementDuration = time.Duration(fdbCluster.GetCachedCluster().GetFailureDetectionTimeSeconds()) * time.Second
			// Get the current Process Group ID numbers that are in use.
			_, processGroupIDs, err := fdbCluster.GetCluster().GetCurrentProcessGroupsAndProcessCounts()
			Expect(err).NotTo(HaveOccurred())

			// The the next free ProcessGroupID and mark this one as unschedulable.
			processGroupID, _ = fdbCluster.GetCluster().GetNextProcessGroupID(fdbv1beta2.ProcessClassStateless, processGroupIDs[fdbv1beta2.ProcessClassStateless], 1)
			log.Println("Next process group ID", processGroupID, "current processGroupIDs for stateless processes", processGroupIDs[fdbv1beta2.ProcessClassStateless])

			// Make sure the new Pod will be stuck in unschedulable.
			fdbCluster.SetProcessGroupsAsUnschedulable([]fdbv1beta2.ProcessGroupID{processGroupID})
			// Now replace a random Pod for replacement to force the cluster to create a new Pod.
			fdbCluster.ReplacePod(fixtures.RandomPickOnePod(fdbCluster.GetStatelessPods().Items), false)

			// Wait until the new Pod is actually created.
			Eventually(func() bool {
				for _, pod := range fdbCluster.GetStatelessPods().Items {
					if fixtures.GetProcessGroupID(pod) == processGroupID {
						podName = pod.Name
						return pod.DeletionTimestamp.IsZero()
					}
				}

				return false
			}).WithPolling(2 * time.Second).WithTimeout(5 * time.Minute).MustPassRepeatedly(2).Should(BeTrue())
		})

		AfterEach(func() {
			// Clear the buggify list, this should allow the operator to move forward and delete the Pod
			Expect(fdbCluster.ClearBuggifyNoSchedule(true)).NotTo(HaveOccurred())
			// Make sure the Pod is deleted.
			Expect(fdbCluster.CheckPodIsDeleted(podName)).To(BeTrue())
			// Make sure we cleaned up the process groups to remove.
			Expect(fdbCluster.ClearProcessGroupsToRemove())
			Expect(fdbCluster.SetAutoReplacements(true, initialReplacementDuration)).NotTo(HaveOccurred())
		})

		When("automatic replacements are disabled", func() {
			BeforeEach(func() {
				// Disable automatic replacements
				Expect(fdbCluster.SetAutoReplacementsWithWait(false, 10*time.Hour, false)).NotTo(HaveOccurred())
				// Add the pending Process group to the removal list.
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.ProcessGroupsToRemove = append(spec.ProcessGroupsToRemove, processGroupID)
				// Add the new pending Pod to the removal list.
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			It("should not remove the Pod as long as it is unschedulable", func() {
				log.Println("Make sure process group", processGroupID, "is stuck in Pending state with Pod", podName)
				// Make sure the Pod is stuck in pending for 2 minutes
				Consistently(func() corev1.PodPhase {
					return fdbCluster.GetPod(podName).Status.Phase
				}).WithTimeout(2 * time.Minute).WithPolling(15 * time.Second).Should(Equal(corev1.PodPending))
			})
		})

		When("automatic replacements are enabled", func() {
			BeforeEach(func() {
				// Enable automatic replacements
				Expect(fdbCluster.SetAutoReplacementsWithWait(true, 1*time.Minute, false)).NotTo(HaveOccurred())
			})

			It("should remove the Pod", func() {
				log.Println("Make sure process group", processGroupID, "gets replaced with Pod", podName)
				stuckPodID, err := processGroupID.GetIDNumber()
				Expect(err).NotTo(HaveOccurred())

				// Make sure the process group is removed after some time.
				Eventually(func() map[int]bool {
					_, currentProcessGroups, err := fdbCluster.GetCluster().GetCurrentProcessGroupsAndProcessCounts()
					if err != nil {
						return map[int]bool{stuckPodID: true}
					}

					return currentProcessGroups[fdbv1beta2.ProcessClassStateless]
				}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveKey(stuckPodID))

				// Make sure the Pod is actually deleted after some time.
				Eventually(func() bool {
					return fdbCluster.CheckPodIsDeleted(podName)
				}).WithTimeout(2 * time.Minute).WithPolling(15 * time.Second).Should(BeTrue())
			})
		})
	})

	// This test is pending, as all Pods will be restarted at the same time, which will lead to unavailability without
	// using DNS.
	PWhen("crash looping the sidecar for all Pods", func() {
		BeforeEach(func() {
			availabilityCheck = false
			fdbCluster.SetCrashLoopContainers([]fdbv1beta2.CrashLoopContainerObject{
				{
					ContainerName: fdbv1beta2.SidecarContainerName,
					Targets:       []fdbv1beta2.ProcessGroupID{"*"},
				},
			}, false)
		})

		AfterEach(func() {
			fdbCluster.SetCrashLoopContainers(nil, true)

			Eventually(func() bool {
				allCrashLooping := true
				for _, pod := range fdbCluster.GetPods().Items {
					for _, container := range pod.Spec.Containers {
						if container.Name == fdbv1beta2.SidecarContainerName {
							if allCrashLooping {
								allCrashLooping = container.Args[0] != "crash-loop"
							}
						}
					}
				}

				return allCrashLooping
			}).WithPolling(4 * time.Second).WithTimeout(2 * time.Minute).MustPassRepeatedly(10).Should(BeTrue())
		})

		It("should set all sidecar containers into crash looping state", func() {
			Eventually(func() bool {
				allCrashLooping := true
				for _, pod := range fdbCluster.GetPods().Items {
					for _, container := range pod.Spec.Containers {
						if container.Name == fdbv1beta2.SidecarContainerName {
							if allCrashLooping {
								allCrashLooping = container.Args[0] == "crash-loop"
							}
						}
					}
				}

				return allCrashLooping
			}).WithPolling(4 * time.Second).WithTimeout(2 * time.Minute).MustPassRepeatedly(10).Should(BeTrue())
		})
	})
})
