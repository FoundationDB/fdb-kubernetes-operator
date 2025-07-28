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
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"k8s.io/utils/ptr"

	"github.com/apple/foundationdb/fdbkubernetesmonitor/api"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	factory               *fixtures.Factory
	fdbCluster            *fixtures.FdbCluster
	testOptions           *fixtures.FactoryOptions
	scheduleInjectPodKill *fixtures.ChaosMeshExperiment
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbCluster(
		fixtures.DefaultClusterConfig(false),
	)

	// Make sure that the test suite is able to fetch logs from Pods.
	operatorPod := factory.RandomPickOnePod(factory.GetOperatorPods(fdbCluster.Namespace()).Items)
	Expect(factory.GetLogsForPod(&operatorPod, "manager", nil)).NotTo(BeEmpty())

	// Load some data async into the cluster. We will only block as long as the Job is created.
	factory.CreateDataLoaderIfAbsent(fdbCluster)

	// In order to test the robustness of the operator we try to kill the operator Pods every minute.
	if factory.ChaosTestsEnabled() {
		scheduleInjectPodKill = factory.ScheduleInjectPodKillWithName(
			fixtures.GetOperatorSelector(fdbCluster.Namespace()),
			"*/2 * * * *",
			chaosmesh.OneMode,
			fdbCluster.Namespace()+"-"+fdbCluster.Name(),
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
		// Make sure all data is present in the cluster
		fdbCluster.EnsureTeamTrackersAreHealthy()
		fdbCluster.EnsureTeamTrackersHaveMinReplicas()
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
			}, ptr.To(1))

			Expect(
				len(
					fdbCluster.GetCluster().Spec.AutomationOptions.Replacements.TaintReplacementOptions,
				),
			).To(BeNumerically(">=", 1))

			Expect(*fdbCluster.GetAutomationOptions().Replacements.Enabled).To(BeTrue())
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			// Reset taint related options to default value in e2e test
			fdbCluster.SetClusterTaintConfig(
				[]fdbv1beta2.TaintReplacementOption{},
				ptr.To(150),
			)
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
				replacedPod := factory.RandomPickOnePod(initialPods.Items)
				// Taint replacePod's node
				taintedNode = fdbCluster.GetNode(replacedPod.Spec.NodeName)
				taintedNode.Spec.Taints = []corev1.Taint{
					{
						Key:    taintKeyMaintenance,
						Value:  "rack_maintenance",
						Effect: corev1.TaintEffectPreferNoSchedule,
						TimeAdded: &metav1.Time{
							Time: time.Now().
								Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1)),
						},
					},
				}
				log.Printf(
					"Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n",
					replacedPod.Name,
					taintedNode.Name,
					taintedNode.Spec.Taints,
					taintedNode.Spec.Taints[0].TimeAdded.Time,
					time.Now(),
				)
				fdbCluster.UpdateNode(taintedNode)

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				fdbCluster.EnsurePodIsDeletedWithCustomTimeout(
					replacedPod.Name,
					ensurePodIsDeletedTimeoutMinutes,
				)
			})

			It("should remove all Pods whose Nodes are tainted", func() {
				replacedPods := factory.RandomPickPod(initialPods.Items, numNodesTainted)
				for _, pod := range replacedPods {
					// Taint replacePod's node
					node := fdbCluster.GetNode(pod.Spec.NodeName)
					node.Spec.Taints = []corev1.Taint{
						{
							Key:    taintKeyMaintenance,
							Value:  "rack_maintenance",
							Effect: corev1.TaintEffectPreferNoSchedule,
							TimeAdded: &metav1.Time{
								Time: time.Now().
									Add(-time.Millisecond * time.Duration(taintKeyMaintenanceDuration*1000+int64(factory.Intn(1000)))),
							},
						},
					}
					log.Printf(
						"Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n",
						pod.Name,
						node.Name,
						node.Spec.Taints,
						node.Spec.Taints[0].TimeAdded.Time,
						time.Now(),
					)
					fdbCluster.UpdateNode(node)
					taintedNodes = append(taintedNodes, node)
				}

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				for i, pod := range replacedPods {
					log.Printf("Ensure %dth pod:%s is deleted\n", i, pod.Name)
					fdbCluster.EnsurePodIsDeletedWithCustomTimeout(
						pod.Name,
						ensurePodIsDeletedTimeoutMinutes,
					)
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
				}, ptr.To(1))

				automationOptions := fdbCluster.GetAutomationOptions()
				Expect(
					len(automationOptions.Replacements.TaintReplacementOptions),
				).To(BeNumerically(">=", 1))
				Expect(*automationOptions.Replacements.Enabled).To(BeTrue())

			})

			It("should eventually remove the targeted Pod whose Node is tainted", func() {
				replacedPod := factory.RandomPickOnePod(initialPods.Items)
				// Taint replacePod's node
				node := fdbCluster.GetNode(replacedPod.Spec.NodeName)
				node.Spec.Taints = []corev1.Taint{
					{
						Key:    taintKeyMaintenance,
						Value:  "rack_maintenance",
						Effect: corev1.TaintEffectPreferNoSchedule,
						TimeAdded: &metav1.Time{
							Time: time.Now().
								Add(-time.Second * time.Duration(taintKeyMaintenanceDuration+1)),
						},
					},
				}
				log.Printf(
					"Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n",
					replacedPod.Name,
					node.Name,
					node.Spec.Taints,
					node.Spec.Taints[0].TimeAdded.Time,
					time.Now(),
				)
				fdbCluster.UpdateNode(node)

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				fdbCluster.EnsurePodIsDeletedWithCustomTimeout(
					replacedPod.Name,
					ensurePodIsDeletedTimeoutMinutes,
				)
			})

			It("should eventually remove all Pods whose Nodes are tainted", func() {
				replacedPods := factory.RandomPickPod(initialPods.Items, numNodesTainted)
				for _, pod := range replacedPods {
					// Taint replacePod's node
					node := fdbCluster.GetNode(pod.Spec.NodeName)
					node.Spec.Taints = []corev1.Taint{
						{
							Key:    taintKeyMaintenance,
							Value:  "rack_maintenance",
							Effect: corev1.TaintEffectPreferNoSchedule,
							TimeAdded: &metav1.Time{
								Time: time.Now().
									Add(-time.Millisecond * time.Duration(taintKeyStarDuration*1000+int64(factory.Intn(1000)))),
							},
						},
					}
					log.Printf(
						"Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n",
						pod.Name,
						node.Name,
						node.Spec.Taints,
						node.Spec.Taints[0].TimeAdded.Time,
						time.Now(),
					)
					fdbCluster.UpdateNode(node)
				}

				err := fdbCluster.WaitForReconciliation()
				Expect(err).NotTo(HaveOccurred())

				for i, pod := range replacedPods {
					log.Printf("Ensure %dth pod:%s is deleted\n", i, pod.Name)
					fdbCluster.EnsurePodIsDeletedWithCustomTimeout(
						pod.Name,
						ensurePodIsDeletedTimeoutMinutes,
					)
				}
			})
		})

		When("cluster disables taint feature with empty taint option", func() {
			taintKeyMaintenanceDuration = int64(1)
			taintKeyStar = "*"

			BeforeEach(func() {
				// Custom TaintReplacementOptions to taintKeyStar
				fdbCluster.SetClusterTaintConfig(
					[]fdbv1beta2.TaintReplacementOption{},
					ptr.To(1),
				)

				automationOptions := fdbCluster.GetAutomationOptions()
				Expect(
					len(automationOptions.Replacements.TaintReplacementOptions),
				).To(BeNumerically("==", 0))
				Expect(*automationOptions.Replacements.Enabled).To(BeTrue())

			})

			It("should not remove any Pod whose Nodes are tainted", func() {
				targetPods := factory.RandomPickPod(initialPods.Items, numNodesTainted)
				var targetPodProcessGroupIDs []fdbv1beta2.ProcessGroupID
				for _, pod := range targetPods {
					targetPodProcessGroupIDs = append(
						targetPodProcessGroupIDs,
						internal.GetProcessGroupIDFromMeta(fdbCluster.GetCluster(), pod.ObjectMeta),
					)
					// Taint replacePod's node
					node := fdbCluster.GetNode(pod.Spec.NodeName)
					node.Spec.Taints = []corev1.Taint{
						{
							Key:    taintKeyMaintenance,
							Value:  "rack_maintenance",
							Effect: corev1.TaintEffectPreferNoSchedule,
							TimeAdded: &metav1.Time{
								Time: time.Now().
									Add(-time.Millisecond * time.Duration(taintKeyStarDuration*1000+int64(factory.Intn(1000)))),
							},
						},
					}
					log.Printf(
						"Taint node: Pod name:%s Node name:%s Node taints:%+v TaintTime:%+v Now:%+v\n",
						pod.Name,
						node.Name,
						node.Spec.Taints,
						node.Spec.Taints[0].TimeAdded.Time,
						time.Now(),
					)
					fdbCluster.UpdateNode(node)
				}

				Consistently(func() bool {
					for _, groupID := range targetPodProcessGroupIDs {
						processGroupStatus := fdbv1beta2.FindProcessGroupByID(
							fdbCluster.GetCluster().Status.ProcessGroups,
							groupID,
						)
						Expect(processGroupStatus).NotTo(BeNil())
						Expect(
							processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected),
						).To(BeNil())
					}
					return true
				}).WithTimeout(time.Duration(*fdbCluster.GetAutomationOptions().Replacements.TaintReplacementTimeSeconds-1) * time.Second).WithPolling(1 * time.Second).Should(BeTrue())

				// Wait for operator to replace the pod
				time.Sleep(
					time.Second * time.Duration(
						*fdbCluster.GetAutomationOptions().Replacements.TaintReplacementTimeSeconds+1,
					),
				)
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

	When("replacing a coordinator Pod", func() {
		var replacedPod corev1.Pod
		var useLocalitiesForExclusion bool

		JustBeforeEach(func() {
			initialPods := fdbCluster.GetPods()
			coordinators := fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus())

			for _, pod := range initialPods.Items {
				_, isCoordinator := coordinators[string(fixtures.GetProcessGroupID(pod))]
				if isCoordinator {
					replacedPod = pod
					break
				}
			}

			// In case that none of the log processes are coordinators
			if replacedPod.Name == "" {
				replacedPod = factory.RandomPickOnePod(initialPods.Items)
			}

			log.Println("coordinators:", coordinators, "replacedPod", replacedPod.Name)
			fdbCluster.ReplacePod(replacedPod, true)
		})

		BeforeEach(func() {
			// Until the race condition is resolved in the FDB go bindings make sure the operator is not restarted.
			// See: https://github.com/apple/foundationdb/issues/11222
			// We can remove this once 7.1 is the default version.
			factory.DeleteChaosMeshExperimentSafe(scheduleInjectPodKill)
			useLocalitiesForExclusion = fdbCluster.GetCluster().UseLocalitiesForExclusion()
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			// Make sure we reset the previous behaviour.
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
				useLocalitiesForExclusion,
			)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(
				fdbCluster.GetCluster().UseLocalitiesForExclusion(),
			).To(Equal(useLocalitiesForExclusion))

			// Making sure we included back all the process groups after exclusion is complete.
			Expect(
				fdbCluster.GetStatus().Cluster.DatabaseConfiguration.ExcludedServers,
			).To(BeEmpty())

			if factory.ChaosTestsEnabled() {
				scheduleInjectPodKill = factory.ScheduleInjectPodKillWithName(
					fixtures.GetOperatorSelector(fdbCluster.Namespace()),
					"*/2 * * * *",
					chaosmesh.OneMode,
					fdbCluster.Namespace()+"-"+fdbCluster.Name(),
				)
			}
		})

		When("IP addresses are used for exclusion", func() {
			BeforeEach(func() {
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(false)
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			It("should remove the targeted Pod", func() {
				fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
			})
		})

		When("localities are used for exclusion", func() {
			BeforeEach(func() {
				cluster := fdbCluster.GetCluster()

				fdbVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
				Expect(err).NotTo(HaveOccurred())

				if !fdbVersion.SupportsLocalityBasedExclusions() {
					Skip(
						"provided FDB version: " + cluster.GetRunningVersion() + " doesn't support locality based exclusions",
					)
				}

				spec := cluster.Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(true)
				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(fdbCluster.GetCluster().UseLocalitiesForExclusion()).To(BeTrue())
			})

			It("should remove the targeted Pod", func() {
				Expect(
					ptr.Deref(
						fdbCluster.GetCluster().Spec.AutomationOptions.UseLocalitiesForExclusion,
						false,
					),
				).To(BeTrue())
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
			fdbCluster.ValidateProcessesCount(
				fdbv1beta2.ProcessClassStorage,
				expectedPodCnt,
				expectedStorageProcessesCnt,
			)
		})

		AfterEach(func() {
			log.Printf("set storage server per pod to %d", initialStorageServerPerPod)
			Expect(
				fdbCluster.SetStorageServerPerPod(initialStorageServerPerPod),
			).ShouldNot(HaveOccurred())
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
			fdbCluster.ValidateProcessesCount(
				fdbv1beta2.ProcessClassStorage,
				expectedPodCnt,
				expectedPodCnt*serverPerPod,
			)
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
			failedPod = factory.RandomPickOnePod(initialPods.Items)
			log.Printf("Setting pod %s to unschedulable.", failedPod.Name)
			fdbCluster.SetPodAsUnschedulable(failedPod)
			fdbCluster.ReplacePod(failedPod, true)
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearBuggifyNoSchedule(true)).NotTo(HaveOccurred())
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should remove the targeted Pod", func() {
			fdbCluster.EnsurePodIsDeletedWithCustomTimeout(failedPod.Name, 15)
		})
	})

	When("a Pod is unscheduled and another Pod is being replaced", func() {
		var failedPod *corev1.Pod
		var podToReplace *corev1.Pod

		BeforeEach(func() {
			// We bring down two pods, which could cause some recoveries.
			availabilityCheck = false
			failedPod = factory.ChooseRandomPod(fdbCluster.GetStatelessPods())
			podToReplace = factory.ChooseRandomPod(fdbCluster.GetStatelessPods())
			log.Println(
				"Failed (unscheduled) Pod:",
				failedPod.Name,
				", Pod to replace:",
				podToReplace.Name,
			)
			fdbCluster.SetPodAsUnschedulable(*failedPod)
			fdbCluster.ReplacePod(*podToReplace, false)
		})
		// 2024/11/26 01:41:00 Failed (unscheduled) Pod: operator-test-fdnhokqf-stateless-42898 , Pod to replace: operator-test-fdnhokqf-stateless-93723
		AfterEach(func() {
			Expect(fdbCluster.ClearBuggifyNoSchedule(false)).NotTo(HaveOccurred())
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should remove the targeted Pod", func() {
			fdbCluster.EnsurePodIsDeletedWithCustomTimeout(podToReplace.Name, 15)
		})
	})

	When("Skipping a cluster for reconciliation", func() {
		var initialGeneration int64

		BeforeEach(func() {
			fdbCluster.SetSkipReconciliation(true)
			initialGeneration = fdbCluster.GetCluster().Status.Generations.Reconciled
		})

		It("should not reconcile and keep the cluster in the same generation", func() {
			initialStoragePods := fdbCluster.GetStoragePods()
			podToDelete := factory.ChooseRandomPod(initialStoragePods)
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
			fdbCluster.SetSkipReconciliation(false)
		})
	})

	When("Deleting a FDB storage Pod", func() {
		var podToDelete corev1.Pod
		var deleteTime time.Time

		BeforeEach(func() {
			podToDelete = factory.RandomPickOnePod(fdbCluster.GetStoragePods().Items)
			log.Printf("deleting storage pod %s/%s", podToDelete.Namespace, podToDelete.Name)
			// Add some buffer here to reduce the risk of a race condition.
			deleteTime = time.Now().Add(-15 * time.Second)
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
			initialReplaceTime = time.Duration(ptr.Deref(
				fdbCluster.GetClusterSpec().AutomationOptions.Replacements.FailureDetectionTimeSeconds,
				90,
			)) * time.Second
			Expect(fdbCluster.SetAutoReplacements(true, 30*time.Second)).ShouldNot(HaveOccurred())
		})

		When("a Pod gets partitioned", func() {
			var partitionedPod *corev1.Pod

			BeforeEach(func() {
				partitionedPod = factory.ChooseRandomPod(fdbCluster.GetStatelessPods())
				log.Printf("partition Pod: %s", partitionedPod.Name)
				exp = factory.InjectPartitionBetween(
					fixtures.PodSelector(partitionedPod),
					chaosmesh.PodSelectorSpec{
						GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
							Namespaces:     []string{partitionedPod.Namespace},
							LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
						},
					},
				)

				// Make sure the operator picks up the changes to the environment. This is not really needed but will
				// speed up the tests.
				fdbCluster.ForceReconcile()
			})

			It("should replace the partitioned Pod", func() {
				log.Printf("waiting for pod removal: %s", partitionedPod.Name)
				fdbCluster.WaitForPodRemoval(partitionedPod)
				exists, err := factory.DoesPodExist(*partitionedPod)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(exists).To(BeFalse())
			})
		})

		When(
			"more Pods are partitioned than the automatic replacement can replace concurrently",
			func() {
				var partitionedPods []corev1.Pod
				var concurrentReplacements int
				var experiments []*fixtures.ChaosMeshExperiment
				creationTimestamps := map[string]metav1.Time{}

				BeforeEach(func() {
					concurrentReplacements = fdbCluster.GetCluster().
						GetMaxConcurrentAutomaticReplacements()
					// Allow to replace 1 Pod concurrently
					spec := fdbCluster.GetCluster().Spec.DeepCopy()
					spec.AutomationOptions.Replacements.MaxConcurrentReplacements = ptr.To(1)
					fdbCluster.UpdateClusterSpecWithSpec(spec)

					// Make sure we disable automatic replacements to prevent the case that the operator replaces one Pod
					// before the partitioned is injected.
					Expect(
						fdbCluster.SetAutoReplacements(false, 30*time.Second),
					).ShouldNot(HaveOccurred())

					// Pick 2 Pods, so the operator has to replace them one after another
					partitionedPods = factory.RandomPickPod(fdbCluster.GetStatelessPods().Items, 3)
					for _, partitionedPod := range partitionedPods {
						pod := partitionedPod
						log.Printf("partition Pod: %s", pod.Name)
						experiments = append(experiments, factory.InjectPartitionBetween(
							fixtures.PodSelector(&pod),
							chaosmesh.PodSelectorSpec{
								GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
									Namespaces:     []string{pod.Namespace},
									LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
								},
							},
						))

						creationTimestamps[pod.Name] = pod.CreationTimestamp
					}

					// Make sure the status can settle
					time.Sleep(1 * time.Minute)

					// Now we can enable the replacements.
					Expect(
						fdbCluster.SetAutoReplacements(true, 30*time.Second),
					).ShouldNot(HaveOccurred())
				})

				AfterEach(func() {
					spec := fdbCluster.GetCluster().Spec.DeepCopy()
					spec.AutomationOptions.Replacements.MaxConcurrentReplacements = ptr.To(
						concurrentReplacements,
					)
					fdbCluster.UpdateClusterSpecWithSpec(spec)
					for _, experiment := range experiments {
						factory.DeleteChaosMeshExperimentSafe(experiment)
					}
				})

				It("should replace the all partitioned Pod", func() {
					Eventually(func() bool {
						allUpdatedOrRemoved := true
						for _, pod := range fdbCluster.GetStatelessPods().Items {
							creationTime, exists := creationTimestamps[pod.Name]
							// If the Pod doesn't exist we can assume it was updated.
							if !exists {
								continue
							}

							log.Println(
								pod.Name,
								"creation time map:",
								creationTime.String(),
								"creation time metadata:",
								pod.CreationTimestamp.String(),
							)
							// The current creation timestamp of the Pod is after the initial creation
							// timestamp, so the Pod was replaced.
							if pod.CreationTimestamp.Compare(creationTime.Time) < 1 {
								allUpdatedOrRemoved = false
							}
						}

						return allUpdatedOrRemoved
					})
				})
			},
		)

		When("a Pod has a bad disk", func() {
			var podWithIOError corev1.Pod

			BeforeEach(func() {
				if !factory.ChaosTestsEnabled() {
					Skip("Chaos tests are skipped for the operator")
				}
				availabilityCheck = false
				initialPods := fdbCluster.GetStoragePods()
				podWithIOError = factory.RandomPickOnePod(initialPods.Items)
				log.Printf("Injecting I/O chaos to %s for 1 minute", podWithIOError.Name)
				exp = factory.InjectDiskFailureWithDuration(
					fixtures.PodSelector(&podWithIOError),
					"1m",
				)

				log.Printf("iochaos injected to %s", podWithIOError.Name)
				// File creation should fail due to I/O error
				Eventually(func() error {
					_, _, err := factory.ExecuteCmdOnPod(
						context.Background(),
						&podWithIOError,
						fdbv1beta2.MainContainerName,
						"touch /var/fdb/data/test",
						false,
					)
					return err
				}).WithTimeout(5 * time.Minute).Should(HaveOccurred())

				processGroupID := string(fixtures.GetProcessGroupID(podWithIOError))
				Eventually(func(g Gomega) []string {
					status := fdbCluster.GetStatus()

					for _, process := range status.Cluster.Processes {
						processID, ok := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
						if !ok || processID != processGroupID {
							continue
						}

						log.Println(process.Messages)
						g.Expect(len(process.Messages)).To(BeNumerically(">=", 1))

						messageNames := make([]string, 0, len(process.Messages))
						for _, message := range process.Messages {
							messageNames = append(messageNames, message.Name)
						}

						log.Println(messageNames)
						return messageNames
					}

					return nil
				}).WithPolling(1 * time.Second).WithTimeout(5 * time.Minute).Should(Or(ContainElement("io_error"), ContainElement("io_timeout")))

				fdbCluster.ForceReconcile()
			})

			It("should remove the targeted process", func() {
				processGroupID := fixtures.GetProcessGroupID(podWithIOError)
				// Wait until the process group is marked to be removed.
				Expect(
					fdbCluster.WaitUntilWithForceReconcile(
						1,
						600,
						func(cluster *fdbv1beta2.FoundationDBCluster) bool {
							for _, processGroup := range cluster.Status.ProcessGroups {
								if processGroup.ProcessGroupID != processGroupID {
									continue
								}

								return processGroup.RemovalTimestamp != nil
							}

							return false
						},
					),
				).NotTo(HaveOccurred())

				// Wait until the process group is removed
				Expect(
					fdbCluster.WaitUntilWithForceReconcile(
						1,
						1200,
						func(cluster *fdbv1beta2.FoundationDBCluster) bool {
							for _, processGroup := range cluster.Status.ProcessGroups {
								if processGroup.ProcessGroupID != processGroupID {
									continue
								}

								// At this point the process group still exists.
								return false
							}

							return true
						},
					),
				).NotTo(HaveOccurred())
			})
		})

		AfterEach(func() {
			Expect(
				fdbCluster.SetAutoReplacements(true, initialReplaceTime),
			).ShouldNot(HaveOccurred())
			factory.DeleteChaosMeshExperimentSafe(exp)
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
			podWithIOError = factory.RandomPickOnePod(initialPods.Items)
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
		})

		It("should add the prefix to all instances", func() {
			lastForcedReconciliationTime := time.Now()
			forceReconcileDuration := 4 * time.Minute

			Eventually(func(g Gomega) bool {
				// Force a reconcile if needed to make sure we speed up the reconciliation if needed.
				if time.Since(lastForcedReconciliationTime) >= forceReconcileDuration {
					fdbCluster.ForceReconcile()
					lastForcedReconciliationTime = time.Now()
				}

				// Check if all process groups are migrated
				for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
					if processGroup.IsMarkedForRemoval() && processGroup.IsExcluded() {
						continue
					}
					g.Expect(string(processGroup.ProcessGroupID)).To(HavePrefix(prefix))
				}

				return true
			}).WithTimeout(20 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
		})
	})

	When("replacing multiple Pods", func() {
		var replacedPods []corev1.Pod

		BeforeEach(func() {
			fdbCluster.ReplacePods(
				factory.RandomPickPod(fdbCluster.GetStatelessPods().Items, 4),
				true,
			)
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
			replacePod = factory.ChooseRandomPod(fdbCluster.GetPods())
			factory.SetFinalizerForPod(replacePod, []string{"foundationdb.org/test"})
			fdbCluster.ReplacePod(*replacePod, true)
		})

		AfterEach(func() {
			factory.SetFinalizerForPod(replacePod, []string{})
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should replace the Pod stuck in Terminating state", func() {
			Expect(
				fdbCluster.GetPodsNames(),
			).Should(Or(HaveLen(len(podsBeforeReplacement)+1), HaveLen(len(podsBeforeReplacement))))
		})
	})

	When("changing coordinator selection", func() {
		AfterEach(func() {
			Expect(fdbCluster.UpdateCoordinatorSelection(
				[]fdbv1beta2.CoordinatorSelectionSetting{},
			)).ShouldNot(HaveOccurred())
		})

		DescribeTable(
			"changing coordinator selection",
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
			Expect(
				fdbCluster.SetLogServersPerPod(initialLogServerPerPod, true),
			).ShouldNot(HaveOccurred())
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
			fdbCluster.ValidateProcessesCount(
				fdbv1beta2.ProcessClassLog,
				expectedPodCnt,
				expectedPodCnt*serverPerPod,
			)
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
			Expect(
				fdbCluster.SetTransactionServerPerPod(
					initialLogServerPerPod,
					expectedLogProcessesCnt,
					true,
				),
			).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*initialLogServerPerPod,
			)
			Eventually(func() int {
				return len(fdbCluster.GetLogPods().Items)
			}).Should(BeNumerically("==", expectedPodCnt))
		})

		It(
			"should update the log servers to the expected amount and should create transaction Pods",
			func() {
				serverPerPod := initialLogServerPerPod * 2
				Expect(
					fdbCluster.SetTransactionServerPerPod(
						serverPerPod,
						expectedLogProcessesCnt,
						true,
					),
				).ShouldNot(HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*serverPerPod,
				)
				fdbCluster.ValidateProcessesCount(
					fdbv1beta2.ProcessClassTransaction,
					expectedPodCnt,
					expectedPodCnt*serverPerPod,
				)
			},
		)
	})

	When("Replacing a Pod with PVC stuck in Terminating state", func() {
		var replacePod *corev1.Pod
		var initialVolumeClaims *corev1.PersistentVolumeClaimList
		var pvc corev1.PersistentVolumeClaim

		BeforeEach(func() {
			replacePod = factory.ChooseRandomPod(fdbCluster.GetLogPods())
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

			pickedPod = factory.ChooseRandomPod(fdbCluster.GetStatelessPods())
			log.Println("Selected Pod:", pickedPod.Name, " to be skipped during the restart")
			fdbCluster.SetIgnoreDuringRestart(
				[]fdbv1beta2.ProcessGroupID{fixtures.GetProcessGroupID(*pickedPod)},
			)

			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassGeneral: newGeneralCustomParameters,
					},
					false,
				),
			).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassGeneral: initialGeneralCustomParameters,
				},
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

	// TODO (johscheuer): enable this test once the CRD is updated in our CI cluster.
	PWhen("a process group is set to be blocked for removal", func() {
		var podMarkedForRemoval corev1.Pod
		var processGroupID fdbv1beta2.ProcessGroupID

		BeforeEach(func() {
			initialPods := fdbCluster.GetStatelessPods()
			podMarkedForRemoval = factory.RandomPickOnePod(initialPods.Items)
			log.Println(
				"Setting Pod",
				podMarkedForRemoval.Name,
				"to be blocked to be removed and mark it for removal.",
			)
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

					return !processGroup.ExclusionTimestamp.IsZero() &&
						!processGroup.RemovalTimestamp.IsZero()
				}

				return false
			}).WithTimeout(5 * time.Minute).WithPolling(15 * time.Second).ShouldNot(BeTrue())
			Consistently(func() *corev1.Pod {
				return fdbCluster.GetPod(podMarkedForRemoval.Name)
			}).WithTimeout(1 * time.Minute).WithPolling(15 * time.Second).ShouldNot(BeNil())
		})
	})

	When("a process group has no address assigned and should be removed", func() {
		var processGroupID fdbv1beta2.ProcessGroupID
		var podName string
		var initialReplacementDuration time.Duration

		BeforeEach(func() {
			initialReplacementDuration = time.Duration(
				fdbCluster.GetCachedCluster().GetFailureDetectionTimeSeconds(),
			) * time.Second
			// Get the current Process Group ID numbers that are in use.
			_, processGroupIDs, err := fdbCluster.GetCluster().
				GetCurrentProcessGroupsAndProcessCounts()
			Expect(err).NotTo(HaveOccurred())

			// Make sure the operator doesn't modify the status.
			fdbCluster.SetSkipReconciliation(true)
			cluster := fdbCluster.GetCluster()
			status := cluster.Status.DeepCopy() // Fetch the current status.
			processGroupID = cluster.GetNextRandomProcessGroupID(
				fdbv1beta2.ProcessClassStateless,
				processGroupIDs[fdbv1beta2.ProcessClassStateless],
			)
			status.ProcessGroups = append(
				status.ProcessGroups,
				fdbv1beta2.NewProcessGroupStatus(
					processGroupID,
					fdbv1beta2.ProcessClassStateless,
					nil,
				),
			)
			fdbCluster.UpdateClusterStatusWithStatus(status)

			log.Println(
				"Next process group ID",
				processGroupID,
				"current processGroupIDs for stateless processes",
				processGroupIDs[fdbv1beta2.ProcessClassStateless],
			)

			// Make sure the new Pod will be stuck in unschedulable.
			fdbCluster.SetProcessGroupsAsUnschedulable([]fdbv1beta2.ProcessGroupID{processGroupID})
			// Now replace a random Pod for replacement to force the cluster to create a new Pod.
			fdbCluster.ReplacePod(
				factory.RandomPickOnePod(fdbCluster.GetStatelessPods().Items),
				false,
			)

			// Allow the operator again to reconcile.
			fdbCluster.SetSkipReconciliation(false)

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
			// Make sure we cleaned up the process groups to remove.
			Expect(fdbCluster.ClearProcessGroupsToRemove())
			Expect(
				fdbCluster.SetAutoReplacements(true, initialReplacementDuration),
			).NotTo(HaveOccurred())
		})

		When("automatic replacements are disabled", func() {
			BeforeEach(func() {
				Expect(
					fdbCluster.SetAutoReplacementsWithWait(false, 10*time.Hour, false),
				).NotTo(HaveOccurred())
			})

			It("should not remove the Pod as long as it is unschedulable", func() {
				log.Println(
					"Make sure process group",
					processGroupID,
					"is stuck in Pending state with Pod",
					podName,
				)
				// Make sure the Pod is stuck in pending for 2 minutes
				Consistently(func() corev1.PodPhase {
					return fdbCluster.GetPod(podName).Status.Phase
				}).WithTimeout(2 * time.Minute).WithPolling(15 * time.Second).Should(Equal(corev1.PodPending))
			})
		})

		When("automatic replacements are enabled", func() {
			BeforeEach(func() {
				Expect(
					fdbCluster.SetAutoReplacementsWithWait(true, 1*time.Minute, false),
				).NotTo(HaveOccurred())
			})

			It("should remove the Pod", func() {
				log.Println(
					"Make sure process group",
					processGroupID,
					"gets replaced with Pod",
					podName,
				)
				// Make sure the process group is removed after some time.
				Eventually(func() *fdbv1beta2.ProcessGroupStatus {
					return fdbv1beta2.FindProcessGroupByID(
						fdbCluster.GetCluster().Status.ProcessGroups,
						processGroupID,
					)
				}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(BeNil())

				// Make sure the Pod is actually deleted after some time.
				Eventually(func() bool {
					return fdbCluster.CheckPodIsDeleted(podName)
				}).WithTimeout(2 * time.Minute).WithPolling(15 * time.Second).Should(BeTrue())
			})
		})
	})

	// this test took ~8min to run for me, so I think it is best to just have the one test
	When(
		"replacing Pods due to securityContext changes (General spec change = log + stateless)",
		func() {
			var podsNotToReplace, podsToReplace []string
			var originalPodSpec, modifiedPodSpec, originalStoragePodSpec *corev1.PodSpec
			var initialPodUpdateStrategy fdbv1beta2.PodUpdateStrategy

			BeforeEach(func() {
				originalPodSpec = fdbCluster.GetPodTemplateSpec(fdbv1beta2.ProcessClassGeneral)
				originalStoragePodSpec = fdbCluster.GetPodTemplateSpec(
					fdbv1beta2.ProcessClassStorage,
				)
				Expect(originalPodSpec).NotTo(BeNil())
				modifiedPodSpec = originalPodSpec.DeepCopy()
				Expect(modifiedPodSpec.SecurityContext).NotTo(BeNil())
				if originalPodSpec.SecurityContext.FSGroupChangePolicy != nil {
					Expect(
						*originalPodSpec.SecurityContext.FSGroupChangePolicy,
					).NotTo(Equal(corev1.FSGroupChangeOnRootMismatch))
				}
				modifiedPodSpec.SecurityContext.FSGroupChangePolicy = &[]corev1.PodFSGroupChangePolicy{corev1.FSGroupChangeOnRootMismatch}[0]
				for _, logPod := range fdbCluster.GetLogPods().Items {
					podsToReplace = append(podsToReplace, logPod.Name)
				}
				for _, statelessPod := range fdbCluster.GetStatelessPods().Items {
					podsToReplace = append(podsToReplace, statelessPod.Name)
				}
				for _, storagePod := range fdbCluster.GetStoragePods().Items {
					podsToReplace = append(podsToReplace, storagePod.Name)
				}
				// if we do not set the PodUpdateStrategy to delete, then the pods will get replaced for the spec change,
				// meaning we cannot test the security context change on non-storage pods unless we use PodUpdateStrategyDelete
				// (and storage pod use would make the test take ~5min longer)
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				initialPodUpdateStrategy = spec.AutomationOptions.PodUpdateStrategy
				spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyDelete
				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(
					fdbCluster.SetPodTemplateSpec(
						fdbv1beta2.ProcessClassGeneral,
						modifiedPodSpec,
						false,
					),
				).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.AutomationOptions.PodUpdateStrategy = initialPodUpdateStrategy
				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(
					fdbCluster.SetPodTemplateSpec(
						fdbv1beta2.ProcessClassGeneral,
						originalPodSpec,
						true,
					),
				).NotTo(HaveOccurred())
			})

			It("should replace all the log pods (which had the securityContext change)", func() {
				lastForcedReconciliationTime := time.Now()
				forceReconcileDuration := 4 * time.Minute
				Eventually(func(g Gomega) {
					// Force a reconcile if needed to make sure we speed up the reconciliation if needed.
					if time.Since(lastForcedReconciliationTime) >= forceReconcileDuration {
						fdbCluster.ForceReconcile()
						lastForcedReconciliationTime = time.Now()
					}

					// check that we replaced the pods we should have and did not replace others
					currentPods := fdbCluster.GetPodsNames()
					g.Expect(currentPods).NotTo(ContainElements(podsToReplace))
					g.Expect(currentPods).To(ContainElements(podsNotToReplace))
					// check that General template pods (Log + Stateless) are replaced with the new security context
					logPods := fdbCluster.GetLogPods()
					for _, pod := range logPods.Items {
						g.Expect(pod.Spec.SecurityContext.FSGroupChangePolicy).ToNot(BeNil())
						g.Expect(*pod.Spec.SecurityContext.FSGroupChangePolicy).
							To(Equal(corev1.FSGroupChangeOnRootMismatch))
					}
					statelessPods := fdbCluster.GetStatelessPods()
					for _, pod := range statelessPods.Items {
						g.Expect(pod.Spec.SecurityContext.FSGroupChangePolicy).ToNot(BeNil())
						g.Expect(*pod.Spec.SecurityContext.FSGroupChangePolicy).
							To(Equal(corev1.FSGroupChangeOnRootMismatch))
					}
					// ensure that we only changed pods that are expected to be changed (i.e. don't touch storage)
					storagePods := fdbCluster.GetStoragePods()
					for _, pod := range storagePods.Items {
						g.Expect(pod.Spec.SecurityContext).
							To(Equal(originalStoragePodSpec.SecurityContext))
					}
				}).WithTimeout(15 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			})
		},
	)

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

	When("a process is excluded without being marked for removal", func() {
		var initialReplaceTime time.Duration
		var pod *corev1.Pod

		BeforeEach(func() {
			availabilityCheck = false
			initialReplaceTime = time.Duration(ptr.Deref(
				fdbCluster.GetClusterSpec().AutomationOptions.Replacements.FailureDetectionTimeSeconds,
				90,
			)) * time.Second
			Expect(fdbCluster.SetAutoReplacements(true, 30*time.Second)).ShouldNot(HaveOccurred())

			pod = factory.ChooseRandomPod(fdbCluster.GetStatelessPods())
			log.Printf("exclude Pod: %s", pod.Name)
			Expect(pod.Status.PodIP).NotTo(BeEmpty())
			_, _ = fdbCluster.RunFdbCliCommandInOperator(
				fmt.Sprintf("exclude %s", pod.Status.PodIP),
				false,
				30,
			)
			// Make sure we trigger a reconciliation to speed up the exclusion detection.
			fdbCluster.ForceReconcile()
		})

		It("should replace the excluded Pod", func() {
			log.Printf("waiting for pod removal: %s", pod.Name)
			fdbCluster.WaitForPodRemoval(pod)
			exists, err := factory.DoesPodExist(*pod)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		AfterEach(func() {
			Expect(
				fdbCluster.SetAutoReplacements(true, initialReplaceTime),
			).ShouldNot(HaveOccurred())
		})
	})

	When("a process is in the maintenance zone", func() {
		var initialReplaceTime time.Duration

		var exp *fixtures.ChaosMeshExperiment
		var targetProcessGroup *fdbv1beta2.ProcessGroupStatus

		BeforeEach(func() {
			availabilityCheck = false
			initialReplaceTime = time.Duration(ptr.Deref(
				fdbCluster.GetClusterSpec().AutomationOptions.Replacements.FailureDetectionTimeSeconds,
				90,
			)) * time.Second
			Expect(fdbCluster.SetAutoReplacements(true, 30*time.Second)).ShouldNot(HaveOccurred())
			for _, processGroup := range fdbCluster.GetCachedCluster().Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				targetProcessGroup = processGroup
				break
			}

			log.Println("targeted process group:", targetProcessGroup.ProcessGroupID)
			_, _ = fdbCluster.RunFdbCliCommandInOperator(
				fmt.Sprintf("maintenance on %s 600", targetProcessGroup.FaultDomain),
				false,
				30,
			)

			// Partition the Pod from the rest of the FoundationDB cluster.
			pod := fdbCluster.GetPod(targetProcessGroup.GetPodName(fdbCluster.GetCachedCluster()))
			exp = factory.InjectPartitionBetween(
				fixtures.PodSelector(pod),
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{pod.Namespace},
						LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
					},
				},
			)

			// Make sure the operator notices that the process is not reporting
			fdbCluster.ForceReconcile()
		})

		It("should not replace the process group", func() {
			Consistently(func() bool {
				for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
					if processGroup.ProcessGroupID != targetProcessGroup.ProcessGroupID {
						continue
					}

					return processGroup.IsMarkedForRemoval()
				}

				return true
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeFalse())
		})

		AfterEach(func() {
			factory.DeleteChaosMeshExperimentSafe(exp)
			// First reset the maintenance zone. Otherwise the reconciliation will be blocked until the maintenance mode
			// is timed out because the update status reconciler will skip process groups under the maintenance zone to
			// prevent misleading condition changes.
			fdbCluster.RunFdbCliCommandInOperator("maintenance off", false, 30)
			Expect(
				fdbCluster.SetAutoReplacements(true, initialReplaceTime),
			).ShouldNot(HaveOccurred())

		})
	})

	When("using proxies instead of grv and commit proxies", func() {
		var originalRoleCounts fdbv1beta2.RoleCounts

		BeforeEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			originalRoleCounts = spec.DatabaseConfiguration.RoleCounts
			spec.DatabaseConfiguration.RoleCounts.Proxies = spec.DatabaseConfiguration.RoleCounts.GrvProxies + spec.DatabaseConfiguration.RoleCounts.CommitProxies
			// Reset the more specific GRV and Commit proxy count.
			spec.DatabaseConfiguration.RoleCounts.GrvProxies = 0
			spec.DatabaseConfiguration.RoleCounts.CommitProxies = 0

			fmt.Println(
				"Original:",
				fixtures.ToJSON(originalRoleCounts),
				"new roleCounts:",
				fixtures.ToJSON(spec.DatabaseConfiguration.RoleCounts),
			)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.DatabaseConfiguration.RoleCounts = originalRoleCounts
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})

		It(
			"should configure the database to run with GRV and commit proxies but keep the proxies in the status field of the FoundationDB resource",
			func() {
				// Make sure GRV and commit proxies are configured.
				status := fdbCluster.GetStatus()
				Expect(
					status.Cluster.DatabaseConfiguration.RoleCounts.CommitProxies,
				).To(BeNumerically(">", 0))
				Expect(
					status.Cluster.DatabaseConfiguration.RoleCounts.GrvProxies,
				).To(BeNumerically(">", 0))
				Expect(status.Cluster.DatabaseConfiguration.RoleCounts.Proxies).NotTo(BeZero())

				fmt.Println(
					"Status configuration:",
					fixtures.ToJSON(status.Cluster.DatabaseConfiguration),
				)
				// Verify the FoundationDB Cluster resource
				cluster := fdbCluster.GetCluster()
				// Make sure that the spec of the FoundationDB resource only has proxies defined.
				Expect(cluster.Spec.DatabaseConfiguration.RoleCounts.GrvProxies).To(BeZero())
				Expect(cluster.Spec.DatabaseConfiguration.RoleCounts.CommitProxies).To(BeZero())
				Expect(cluster.Spec.DatabaseConfiguration.RoleCounts.Proxies).NotTo(BeZero())
				// Make sure that the status of the FoundationDB resource only has proxies defined.
				Expect(cluster.Status.DatabaseConfiguration.RoleCounts.GrvProxies).To(BeZero())
				Expect(cluster.Status.DatabaseConfiguration.RoleCounts.CommitProxies).To(BeZero())
				Expect(cluster.Status.DatabaseConfiguration.RoleCounts.Proxies).NotTo(BeZero())
			},
		)
	})

	When("adding and removing a test process", func() {
		BeforeEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.ProcessCounts.Test = 1
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.ProcessCounts.Test = 0
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})

		It("should create the test Pod", func() {
			Eventually(func(g Gomega) bool {
				for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
					if processGroup.ProcessClass != fdbv1beta2.ProcessClassTest {
						continue
					}

					g.Expect(processGroup.ProcessGroupConditions).To(BeZero())
				}

				return true
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())

			// Make sure the Pod is running
			var podName string
			for _, processGroup := range fdbCluster.GetCachedCluster().Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassTest {
					continue
				}

				podName = processGroup.GetPodName(fdbCluster.GetCachedCluster())
			}

			Expect(podName).NotTo(BeEmpty())
			Eventually(func() corev1.PodPhase {
				return fdbCluster.GetPod(podName).Status.Phase
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Equal(corev1.PodRunning))

			Eventually(func() int {
				var count int
				processes := fdbCluster.GetStatus().Cluster.Processes
				for _, process := range processes {
					if process.ProcessClass != fdbv1beta2.ProcessClassTest {
						continue
					}

					count++
				}

				return count
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(BeNumerically("==", 1))
		})
	})

	When("the operator is allowed to reset the maintenance zone", func() {
		var faultDomain fdbv1beta2.FaultDomain
		var pickedProcessGroup *fdbv1beta2.ProcessGroupStatus

		BeforeEach(func() {
			cluster := fdbCluster.GetCluster()
			spec := cluster.Spec.DeepCopy()
			// TODO (johscheuer): Once the new CRD is available change this to ResetMaintenanceMode.
			spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = ptr.To(
				true,
			)
			fdbCluster.UpdateClusterSpecWithSpec(spec)

			for _, processGroup := range cluster.Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				pickedProcessGroup = processGroup
				break
			}

			Expect(pickedProcessGroup).NotTo(BeNil())
			faultDomain = pickedProcessGroup.FaultDomain

			key := cluster.GetMaintenancePrefix() + "/" + string(pickedProcessGroup.ProcessGroupID)
			Eventually(func(g Gomega) error {
				timestampByteBuffer := new(bytes.Buffer)
				g.Expect(binary.Write(timestampByteBuffer, binary.LittleEndian, time.Now().Unix())).
					NotTo(HaveOccurred())
				g.Expect(timestampByteBuffer.Bytes()).NotTo(BeEmpty())
				cmd := fmt.Sprintf(
					"writemode on; option on ACCESS_SYSTEM_KEYS; set %s %s",
					fixtures.FdbPrintable([]byte(key)),
					fixtures.FdbPrintable(timestampByteBuffer.Bytes()),
				)
				_, _, err := fdbCluster.RunFdbCliCommandInOperatorWithoutRetry(cmd, true, 20)

				return err
			}).WithTimeout(5 * time.Minute).WithPolling(15 * time.Second).ShouldNot(HaveOccurred())

			command := fmt.Sprintf("maintenance on %s %s", pickedProcessGroup.FaultDomain, "3600")
			_, _ = fdbCluster.RunFdbCliCommandInOperator(command, false, 20)
		})

		It("should reset the maintenance mode once the Pod was restarted", func() {
			// Make sure the operator sees the maintenance mode.
			fdbCluster.ForceReconcile()
			// Make sure the maintenance mode is not reset until the Pod is recreated
			Consistently(func() fdbv1beta2.FaultDomain {
				return fdbCluster.GetStatus().Cluster.MaintenanceZone
			}).WithTimeout(1 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))

			podName := pickedProcessGroup.GetPodName(fdbCluster.GetCluster())
			log.Println("Delete Pod", podName)
			factory.DeletePod(fdbCluster.GetPod(podName))

			lastForceReconcile := time.Now()
			// Make sure the maintenance mode is reset
			Eventually(func() fdbv1beta2.FaultDomain {
				// Make sure we speed up the reconciliation and allow the operator to remove the maintenance mode.
				if time.Since(lastForceReconcile) > 1*time.Minute {
					fdbCluster.ForceReconcile()
					lastForceReconcile = time.Now()
				}
				return fdbCluster.GetStatus().Cluster.MaintenanceZone
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).MustPassRepeatedly(10).Should(Equal(fdbv1beta2.FaultDomain("")))
		})

		AfterEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			// TODO (johscheuer): Once the new CRD is available change this to ResetMaintenanceMode.
			spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = ptr.To(
				false,
			)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
		})
	})

	When("setting a locality that is using an environment variable", func() {
		var initialGeneralCustomParameters fdbv1beta2.FoundationDBCustomParameters

		BeforeEach(func() {
			// Disable the availability check to prevent flaky tests if the small cluster takes longer to be restarted
			availabilityCheck = false
			initialGeneralCustomParameters = fdbCluster.GetCustomParameters(
				fdbv1beta2.ProcessClassGeneral,
			)

			newGeneralCustomParameters := append(
				initialGeneralCustomParameters,
				"locality_testing=$FDB_INSTANCE_ID",
			)

			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassGeneral: newGeneralCustomParameters,
					},
					false,
				),
			).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassGeneral: initialGeneralCustomParameters,
				},
				true,
			)).NotTo(HaveOccurred())
		})

		It("should update the locality with the substituted environment variable", func() {
			localityKey := "testing"
			Eventually(func(g Gomega) bool {
				for _, process := range fdbCluster.GetStatus().Cluster.Processes {
					// We change the knob only for stateless processes.
					if process.ProcessClass != fdbv1beta2.ProcessClassStateless {
						continue
					}

					g.Expect(process.Locality).NotTo(BeEmpty())
					g.Expect(process.Locality).To(HaveKey(localityKey))
					g.Expect(process.Locality).To(HaveKey(fdbv1beta2.FDBLocalityInstanceIDKey))
					g.Expect(process.Locality[localityKey]).
						To(Equal(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]))
				}

				return true
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
		})
	})

	When("a FDB pod is partitioned from the Kubernetes API", func() {
		var selectedPod corev1.Pod
		var initialConfiguration string
		var exp *fixtures.ChaosMeshExperiment
		var initialCustomParameters fdbv1beta2.FoundationDBCustomParameters
		var initialRestarts int

		BeforeEach(func() {
			// If we are not using the unified image, we can skip this test.
			if !fdbCluster.GetCluster().UseUnifiedImage() {
				Skip("The sidecar image doesn't require connectivity to the Kubernetes API")
			}

			if !factory.ChaosTestsEnabled() {
				Skip("Chaos tests are skipped for the operator")
			}

			selectedPod = factory.RandomPickOnePod(fdbCluster.GetStoragePods().Items)
			for _, status := range selectedPod.Status.ContainerStatuses {
				initialRestarts += int(status.RestartCount)
			}

			initialConfiguration = selectedPod.Annotations[api.CurrentConfigurationAnnotation]

			var kubernetesServiceHost string
			Eventually(func(g Gomega) error {
				std, _, err := factory.ExecuteCmdOnPod(
					context.Background(),
					&selectedPod,
					fdbv1beta2.MainContainerName,
					"printenv KUBERNETES_SERVICE_HOST",
					false,
				)

				g.Expect(std).NotTo(BeEmpty())
				kubernetesServiceHost = strings.TrimSpace(std)

				return err
			}, 5*time.Minute).ShouldNot(HaveOccurred())

			exp = factory.InjectPartitionWithExternalTargets(
				fixtures.PodSelector(&selectedPod),
				[]string{kubernetesServiceHost},
			)
			// Make sure that the partition takes effect.
			Eventually(func() error {
				_, _, err := factory.ExecuteCmdOnPod(
					context.Background(),
					&selectedPod,
					fdbv1beta2.MainContainerName,
					fmt.Sprintf("nc -vz -w 2 %s 443", kubernetesServiceHost),
					false,
				)

				return err
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(HaveOccurred())

			// Rollout a new knob to check the behaviour in such a case.
			initialCustomParameters = fdbCluster.GetCustomParameters(
				fdbv1beta2.ProcessClassStorage,
			)
			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassStorage: append(
							initialCustomParameters,
							"knob_max_trace_lines=1000000",
						),
					},
					false,
				),
			).NotTo(HaveOccurred())
		})

		It("should keep the pod up and running", func() {
			selectedProcessGroupID := fixtures.GetProcessGroupID(selectedPod)
			// Make sure the partitioned Pod is not able to update its annotation.
			Eventually(func() *int64 {
				for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
					if processGroup.ProcessGroupID != selectedProcessGroupID {
						continue
					}

					return processGroup.GetConditionTime(fdbv1beta2.IncorrectConfigMap)
				}

				return nil
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).MustPassRepeatedly(10).ShouldNot(BeNil())

			expectedStorageServersPerPod := fdbCluster.GetCluster().GetStorageServersPerPod()
			// Make sure the processes are still reporting the whole time.
			Consistently(func() int {
				status := fdbCluster.GetStatus()

				var runningProcesses int
				for _, process := range status.Cluster.Processes {
					if process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey] != string(
						selectedProcessGroupID,
					) {
						continue
					}

					runningProcesses++
				}

				return runningProcesses
			}).WithTimeout(1 * time.Minute).WithPolling(5 * time.Second).Should(BeNumerically("==", expectedStorageServersPerPod))

			// Make sure the Pod was not restarted because of the partition.
			pod, err := factory.GetPod(fdbCluster.Namespace(), selectedPod.Name)
			Expect(err).NotTo(HaveOccurred())

			var restarts int
			for _, status := range pod.Status.ContainerStatuses {
				restarts += int(status.RestartCount)
			}

			Expect(restarts).To(BeNumerically("==", initialRestarts))

			// Restart all the processes, to ensure they are running with the expected parameters.
			stdout, stderr, err := fdbCluster.RunFdbCliCommandInOperatorWithoutRetry(
				"kill; kill all; sleep 10",
				true,
				60,
			)
			log.Println("stdout", stdout, "stderr", stderr, "err", err)

			// Delete the partition, this allows the partitioned Pod to update its annotations again.
			factory.DeleteChaosMeshExperimentSafe(exp)

			fdbCluster.ForceReconcile()

			// Ensure the partitioned Pod was able to update its annotations.
			Eventually(func() string {
				return fdbCluster.GetPod(selectedPod.Name).Annotations[api.CurrentConfigurationAnnotation]
			}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).ShouldNot(Equal(initialConfiguration))
		})

		AfterEach(func() {
			factory.DeleteChaosMeshExperimentSafe(exp)
			Expect(fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassStorage: initialCustomParameters,
				},
				true,
			)).NotTo(HaveOccurred())
		})
	})

	When("enabling the node watch feature", func() {
		var initialParameters fdbv1beta2.FoundationDBCustomParameters

		BeforeEach(func() {
			// If we are not using the unified image, we can skip this test.
			if !fdbCluster.GetCluster().UseUnifiedImage() {
				Skip("The sidecar image doesn't support reading node labels")
			}

			// Enable the node watch feature.
			spec := fdbCluster.GetPodTemplateSpec(fdbv1beta2.ProcessClassStorage)
			for idx, container := range spec.Containers {
				if container.Name != fdbv1beta2.MainContainerName {
					continue
				}

				var hasEnv bool
				for envIdx, env := range container.Env {
					if env.Name != "ENABLE_NODE_WATCH" {
						continue
					}

					if env.Value != "true" {
						spec.Containers[idx].Env[envIdx].Value = "true"
						spec.Containers[idx].Env[envIdx].ValueFrom = nil
					}

					hasEnv = true
				}

				if !hasEnv {
					spec.Containers[idx].Env = append(container.Env, corev1.EnvVar{
						Name:  "ENABLE_NODE_WATCH",
						Value: "true",
					})
				}
			}

			Expect(
				fdbCluster.SetPodTemplateSpec(fdbv1beta2.ProcessClassStorage, spec, true),
			).NotTo(HaveOccurred())
		})

		It("should have enabled the node watch feature on all Pods", func() {
			initialParameters = fdbCluster.GetCustomParameters(
				fdbv1beta2.ProcessClassStorage,
			)

			// Update the storage processes to have the new locality.
			Expect(fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassStorage: append(
						initialParameters,
						"locality_os=$NODE_LABEL_KUBERNETES_IO_OS",
					),
				},
				true,
			)).NotTo(HaveOccurred())

			Eventually(func(g Gomega) bool {
				status := fdbCluster.GetStatus()
				for _, process := range status.Cluster.Processes {
					if process.ProcessClass != fdbv1beta2.ProcessClassStorage {
						continue
					}
					log.Println(process.Locality)
					g.Expect(process.Locality).To(HaveKey("os"))
				}

				return true
			})
		})

		AfterEach(func() {
			Expect(fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassStorage: initialParameters,
				},
				true,
			)).NotTo(HaveOccurred())
		})
	})

	When("the Pod is set into isolate mode", func() {
		var isolatedPod corev1.Pod

		BeforeEach(func() {
			// If we are not using the unified image, we can skip this test.
			if !fdbCluster.GetCluster().UseUnifiedImage() {
				Skip("The sidecar image doesn't support reading node labels")
			}

			isolatedPod = factory.RandomPickOnePod(fdbCluster.GetStatelessPods().Items)
			isolatedPod.Annotations[fdbv1beta2.IsolateProcessGroupAnnotation] = "true"
			Expect(
				factory.GetControllerRuntimeClient().Update(context.Background(), &isolatedPod),
			).NotTo(HaveOccurred())
			fdbCluster.ReplacePod(isolatedPod, false)
		})

		It("should shutdown the fdbserver processes of this Pod", func() {
			processGroupID := string(fixtures.GetProcessGroupID(isolatedPod))
			// Make sure the process is not reporting to the cluster.
			Eventually(func(g Gomega) bool {
				status := fdbCluster.GetStatus()
				for _, process := range status.Cluster.Processes {
					if process.ProcessClass != fdbv1beta2.ProcessClassStateless {
						continue
					}

					g.Expect(process.Locality).
						NotTo(HaveKeyWithValue(fdbv1beta2.FDBLocalityInstanceIDKey, processGroupID))
				}

				return true
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

			Consistently(func(g Gomega) *metav1.Time {
				pod, err := factory.GetPod(isolatedPod.Namespace, isolatedPod.Name)
				g.Expect(err).NotTo(HaveOccurred())

				return pod.DeletionTimestamp
			}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).Should(BeNil())
		})

		AfterEach(func() {
			pod := &corev1.Pod{}
			Expect(
				factory.GetControllerRuntimeClient().
					Get(context.Background(), ctrlClient.ObjectKey{Name: isolatedPod.Name, Namespace: isolatedPod.Namespace}, pod),
			).NotTo(HaveOccurred())
			pod.Annotations[fdbv1beta2.IsolateProcessGroupAnnotation] = "false"
			Expect(
				factory.GetControllerRuntimeClient().Update(context.Background(), pod),
			).NotTo(HaveOccurred())
		})
	})

	When("a new knob for storage servers is rolled out to the cluster", func() {
		var initialCustomParameters fdbv1beta2.FoundationDBCustomParameters

		BeforeEach(func() {
			initialCustomParameters = fdbCluster.GetCustomParameters(
				fdbv1beta2.ProcessClassStorage,
			)

			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassStorage: append(
							initialCustomParameters,
							"knob_read_sampling_enabled=true",
						),
					},
					false,
				),
			).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassStorage: initialCustomParameters,
				},
				true,
			)).NotTo(HaveOccurred())
		})

		It("should update the locality with the substituted environment variable", func() {
			Eventually(func(g Gomega) bool {
				for _, process := range fdbCluster.GetStatus().Cluster.Processes {
					// We change the knob only for storage processes.
					if process.ProcessClass != fdbv1beta2.ProcessClassStorage {
						continue
					}

					g.Expect(process.CommandLine).To(ContainSubstring("knob_read_sampling_enabled"))
				}

				return true
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
		})
	})

	When("a process is marked for removal and was excluded", func() {
		var pickedProcessGroupID fdbv1beta2.ProcessGroupID
		var initialExclusionTimestamp *metav1.Time
		var pickedPod corev1.Pod

		BeforeEach(func() {
			pickedPod = factory.RandomPickOnePod(fdbCluster.GetStatelessPods().Items)
			pickedProcessGroupID = fixtures.GetProcessGroupID(pickedPod)
			log.Println(
				"pickedProcessGroupID",
				pickedProcessGroupID,
				"pickedPod",
				pickedPod.Status.PodIP,
			)

			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.RemovalMode = fdbv1beta2.PodUpdateModeNone
			fdbCluster.UpdateClusterSpecWithSpec(spec)

			fdbCluster.ReplacePod(pickedPod, false)
			var pickedProcessGroup *fdbv1beta2.ProcessGroupStatus
			Expect(
				fdbCluster.WaitUntilWithForceReconcile(
					1,
					900,
					func(cluster *fdbv1beta2.FoundationDBCluster) bool {
						for _, processGroup := range cluster.Status.ProcessGroups {
							if processGroup.ProcessGroupID != pickedProcessGroupID {
								continue
							}

							initialExclusionTimestamp = processGroup.ExclusionTimestamp
							pickedProcessGroup = processGroup
							break
						}

						log.Println("initialExclusionTimestamp", initialExclusionTimestamp)
						return initialExclusionTimestamp != nil
					},
				),
			).NotTo(HaveOccurred(), "process group is missing the exclusion timestamp")

			var excludedServer fdbv1beta2.ExcludedServers
			if fdbCluster.GetCluster().UseLocalitiesForExclusion() {
				Expect(pickedProcessGroup).NotTo(BeNil())
				excludedServer = fdbv1beta2.ExcludedServers{
					Locality: pickedProcessGroup.GetExclusionString(),
				}
			} else {
				excludedServer = fdbv1beta2.ExcludedServers{Address: pickedPod.Status.PodIP}
			}

			// Ensure that the IP is excluded
			Expect(
				fdbCluster.GetStatus().Cluster.DatabaseConfiguration.ExcludedServers,
			).To(ContainElements(excludedServer))
		})

		AfterEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.RemovalMode = fdbv1beta2.PodUpdateModeZone
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		When("the process gets included again", func() {
			BeforeEach(func() {
				fdbCluster.RunFdbCliCommandInOperator("include all", false, 60)
			})

			It("should be excluded a second time", func() {
				var newExclusionTimestamp *metav1.Time

				Expect(
					fdbCluster.WaitUntilWithForceReconcile(
						1,
						900,
						func(cluster *fdbv1beta2.FoundationDBCluster) bool {
							for _, processGroup := range cluster.Status.ProcessGroups {
								if processGroup.ProcessGroupID != pickedProcessGroupID {
									continue
								}

								newExclusionTimestamp = processGroup.ExclusionTimestamp
								log.Println("ProcessGroup:", processGroup.String())
								break
							}

							return newExclusionTimestamp != nil &&
								!newExclusionTimestamp.Equal(initialExclusionTimestamp)
						},
					),
				).NotTo(HaveOccurred(), "process group is missing the exclusion timestamp")
				Expect(initialExclusionTimestamp.Before(newExclusionTimestamp)).To(BeTrue())
				Expect(initialExclusionTimestamp).NotTo(Equal(newExclusionTimestamp))
			})
		})
	})
})
