/*
 * fdb_operator_fixtures.go
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

package fixtures

import (
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"log"
	"math"
	"math/rand"
	"time"
)

func validateProcessesCount(
	fdbCluster *FdbCluster,
	processClass fdbv1beta2.ProcessClass,
	countProcessGroups int,
	countServer int,
) {
	gomega.Eventually(func() int {
		var cnt int
		for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
			if processGroup.ProcessClass != processClass {
				continue
			}

			if processGroup.IsMarkedForRemoval() {
				continue
			}

			cnt++
		}

		return cnt
	}).Should(gomega.BeNumerically("==", countProcessGroups))

	gomega.Eventually(func() int {
		return fdbCluster.GetProcessCountByProcessClass(processClass)
	}).Should(gomega.BeNumerically("==", countServer))

	// Make sure that all process group have the fault domain set.
	for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
		gomega.Expect(processGroup.FaultDomain).NotTo(gomega.BeEmpty())
	}
}

// OperatorTestSuite is a wrapper method that can we used to test the same set of tests for different configurations on
// a FoundationDBCluster. For the usage see the "test_operator" test suite. The clusterSpec parameter allows to provide a
// customized FoundationDBCluster for the test cases.
func OperatorTestSuite(factory *Factory, config *ClusterConfig, clusterSpec *fdbv1beta2.FoundationDBCluster) {
	var fdbCluster *FdbCluster

	var _ = ginkgo.BeforeSuite(func() {
		fdbCluster = factory.CreateFdbClusterFromSpec(clusterSpec,
			config,
			factory.GetClusterOptions()...,
		)
		// In order to test the robustness of the operator we try to kill the operator Pods every minute.
		if factory.ChaosTestsEnabled() {
			factory.ScheduleInjectPodKill(
				GetOperatorSelector(fdbCluster.Namespace()),
				"*/2 * * * *",
				chaosmesh.OneMode,
			)
		}
	})

	var _ = ginkgo.AfterSuite(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			log.Printf("failed due to %s", ginkgo.CurrentSpecReport().FailureMessage())
		}
		factory.Shutdown()
	})

	ginkgo.When("testing the operator", ginkgo.Label("e2e", "pr"), func() {
		var availabilityCheck bool

		ginkgo.AfterEach(func() {
			// Reset availabilityCheck if a test case removes this check.
			availabilityCheck = true
			if ginkgo.CurrentSpecReport().Failed() {
				factory.DumpState(fdbCluster)
			}
			gomega.Expect(fdbCluster.WaitForReconciliation()).ToNot(gomega.HaveOccurred())
			factory.StopInvariantCheck()
		})

		ginkgo.JustBeforeEach(func() {
			if availabilityCheck {
				err := fdbCluster.InvariantClusterStatusAvailable()
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		})

		ginkgo.It("should set the fault domain for all process groups", func() {
			for _, processGroup := range fdbCluster.cluster.Status.ProcessGroups {
				gomega.Expect(processGroup.FaultDomain).NotTo(gomega.BeEmpty())
			}
		})

		ginkgo.When("replacing a Pod", func() {
			var replacedPod corev1.Pod

			ginkgo.BeforeEach(func() {
				initialPods := fdbCluster.GetStatelessPods()
				replacedPod = RandomPickOnePod(initialPods.Items)
				fdbCluster.ReplacePod(replacedPod, true)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should remove the targeted Pod", func() {
				fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
			})
		})

		ginkgo.When("changing the volume size", func() {
			var initialPods *corev1.PodList
			var newSize, initialStorageSize resource.Quantity

			ginkgo.BeforeEach(func() {
				var err error

				initialPods = fdbCluster.GetLogPods()
				// We use ProcessClassGeneral here because we are not setting any specific settings for the Log processes.
				initialStorageSize, err = fdbCluster.GetVolumeSize(fdbv1beta2.ProcessClassGeneral)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				// Add 10G to the current size
				newSize = initialStorageSize.DeepCopy()
				newSize.Add(resource.MustParse("10G"))
				gomega.Expect(
					fdbCluster.SetVolumeSize(fdbv1beta2.ProcessClassGeneral, newSize),
				).NotTo(gomega.HaveOccurred())
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(
					fdbCluster.SetVolumeSize(fdbv1beta2.ProcessClassGeneral, initialStorageSize),
				).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should replace all the log Pods and use the new volume size", func() {
				pods := fdbCluster.GetLogPods()
				gomega.Expect(pods.Items).NotTo(gomega.ContainElements(initialPods.Items))
				volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(
					fdbv1beta2.ProcessClassLog,
				)
				gomega.Expect(len(volumeClaims.Items)).To(gomega.Equal(len(initialPods.Items)))
				for _, volumeClaim := range volumeClaims.Items {
					req := volumeClaim.Spec.Resources.Requests["storage"]
					gomega.Expect((&req).Value()).To(gomega.Equal(newSize.Value()))
				}
			})
		})

		ginkgo.When("setting storageServersPerPod", func() {
			var initialStorageServerPerPod, expectedPodCnt, expectedStorageProcessesCnt int

			ginkgo.BeforeEach(func() {
				initialStorageServerPerPod = fdbCluster.GetStorageServerPerPod()
				initialPods := fdbCluster.GetStoragePods()
				expectedPodCnt = len(initialPods.Items)
				expectedStorageProcessesCnt = expectedPodCnt * initialStorageServerPerPod
				log.Printf(
					"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
					expectedPodCnt,
					expectedStorageProcessesCnt,
				)
				validateProcessesCount(fdbCluster, fdbv1beta2.ProcessClassStorage, expectedPodCnt, expectedStorageProcessesCnt)
			})

			ginkgo.AfterEach(func() {
				log.Printf("set storage server per pod to %d", initialStorageServerPerPod)
				gomega.Expect(fdbCluster.SetStorageServerPerPod(initialStorageServerPerPod)).ShouldNot(gomega.HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*initialStorageServerPerPod,
				)
				validateProcessesCount(
					fdbCluster,
					fdbv1beta2.ProcessClassStorage,
					expectedPodCnt,
					expectedPodCnt*initialStorageServerPerPod,
				)
			})

			ginkgo.It("should update the storage servers to the expected amount", func() {
				// Update to double the SS per Disk
				serverPerPod := initialStorageServerPerPod * 2
				log.Printf("set storage server per Pod to %d", serverPerPod)
				gomega.Expect(fdbCluster.SetStorageServerPerPod(serverPerPod)).ShouldNot(gomega.HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*serverPerPod,
				)
				validateProcessesCount(fdbCluster, fdbv1beta2.ProcessClassStorage, expectedPodCnt, expectedPodCnt*serverPerPod)
			})
		})

		ginkgo.When("Shrinking the number of log processes by one", func() {
			var initialLogPodCount int

			ginkgo.BeforeEach(func() {
				initialLogPodCount = len(fdbCluster.GetLogPods().Items)
			})

			ginkgo.AfterEach(func() {
				// Set the log process count back to the default value
				gomega.Expect(fdbCluster.UpdateLogProcessCount(initialLogPodCount)).NotTo(gomega.HaveOccurred())
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", initialLogPodCount))
			})

			ginkgo.It("should reduce the number of log processes by one", func() {
				gomega.Expect(fdbCluster.UpdateLogProcessCount(initialLogPodCount - 1)).NotTo(gomega.HaveOccurred())
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeNumerically("==", initialLogPodCount-1))
			})
		})

		ginkgo.When("a Pod is in a failed scheduling state", func() {
			var failedPod corev1.Pod

			ginkgo.BeforeEach(func() {
				initialPods := fdbCluster.GetStatelessPods()
				failedPod = RandomPickOnePod(initialPods.Items)
				log.Printf("Setting pod %s to unschedulable.", failedPod.Name)
				gomega.Expect(fdbCluster.SetPodAsUnschedulable(failedPod)).NotTo(gomega.HaveOccurred())
				fdbCluster.ReplacePod(failedPod, true)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearBuggifyNoSchedule(true)).NotTo(gomega.HaveOccurred())
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should remove the targeted Pod", func() {
				fdbCluster.EnsurePodIsDeleted(failedPod.Name)
			})
		})

		ginkgo.When("a Pod is unscheduled and another Pod is being replaced", func() {
			var failedPod *corev1.Pod
			var podToReplace *corev1.Pod

			ginkgo.BeforeEach(func() {
				failedPod = ChooseRandomPod(fdbCluster.GetStatelessPods())
				podToReplace = ChooseRandomPod(fdbCluster.GetStatelessPods())
				log.Println(
					"Failed (unscheduled) Pod:",
					failedPod.Name,
					", Pod to replace:",
					podToReplace.Name,
				)
				gomega.Expect(fdbCluster.SetPodAsUnschedulable(*failedPod)).NotTo(gomega.HaveOccurred())
				fdbCluster.ReplacePod(*podToReplace, false)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearBuggifyNoSchedule(false)).NotTo(gomega.HaveOccurred())
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should remove the targeted Pod", func() {
				fdbCluster.EnsurePodIsDeleted(podToReplace.Name)
			})
		})

		ginkgo.When("Skipping a cluster for reconciliation", func() {
			var initialGeneration int64

			ginkgo.BeforeEach(func() {
				gomega.Expect(fdbCluster.SetSkipReconciliation(true)).ShouldNot(gomega.HaveOccurred())
				initialGeneration = fdbCluster.GetCluster().Status.Generations.Reconciled
			})

			ginkgo.It("should not reconcile and keep the cluster in the same generation", func() {
				initialStoragePods := fdbCluster.GetStoragePods()
				podToDelete := ChooseRandomPod(initialStoragePods)
				log.Printf("deleting storage pod %s/%s", podToDelete.Namespace, podToDelete.Name)
				factory.DeletePod(podToDelete)
				gomega.Eventually(func() int {
					return len(fdbCluster.GetStoragePods().Items)
				}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeNumerically("==", len(initialStoragePods.Items)-1))

				gomega.Consistently(func() int64 {
					return fdbCluster.GetCluster().Status.Generations.Reconciled
				}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(gomega.BeNumerically("==", initialGeneration))
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.SetSkipReconciliation(false)).ShouldNot(gomega.HaveOccurred())
			})
		})

		ginkgo.PWhen("Changing the TLS setting", func() {
			// Currently disabled until a new release of the operator is out
			ginkgo.It("should disable or enable TLS and keep the cluster available", func() {
				// Only change the TLS setting for the cluster and not for the sidecar otherwise we have to recreate
				// all Pods which takes a long time since we recreate the Pods one by one.
				log.Println("disable TLS for main container")
				gomega.Expect(fdbCluster.SetTLS(false, true)).NotTo(gomega.HaveOccurred())
				gomega.Expect(fdbCluster.HasTLSEnabled()).To(gomega.BeFalse())
				log.Println("enable TLS for main container")
				gomega.Expect(fdbCluster.SetTLS(true, true)).NotTo(gomega.HaveOccurred())
				gomega.Expect(fdbCluster.HasTLSEnabled()).To(gomega.BeTrue())
			})
		})

		ginkgo.When("changing the public IP source", func() {
			ginkgo.It("should change the public IP source and create/delete services", func() {
				log.Printf("set public IP source to %s", fdbv1beta2.PublicIPSourceService)
				gomega.Expect(
					fdbCluster.SetPublicIPSource(fdbv1beta2.PublicIPSourceService),
				).ShouldNot(gomega.HaveOccurred())
				gomega.Eventually(func() bool {
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
				}).Should(gomega.BeTrue())
			})

			ginkgo.AfterEach(func() {
				log.Printf("set public IP source to %s", fdbv1beta2.PublicIPSourcePod)
				gomega.Expect(
					fdbCluster.SetPublicIPSource(fdbv1beta2.PublicIPSourcePod),
				).ShouldNot(gomega.HaveOccurred())
				svcList := fdbCluster.GetServices()

				var expectedSvcCnt int
				if fdbCluster.HasHeadlessService() {
					expectedSvcCnt = 1
				}
				gomega.Expect(len(svcList.Items)).To(gomega.BeNumerically("==", expectedSvcCnt))
			})
		})

		ginkgo.When("Deleting a FDB storage Pod", func() {
			var podToDelete corev1.Pod
			var deleteTime time.Time

			ginkgo.BeforeEach(func() {
				podToDelete = RandomPickOnePod(fdbCluster.GetStoragePods().Items)
				log.Printf("deleting storage pod %s/%s", podToDelete.Namespace, podToDelete.Name)
				deleteTime = time.Now()
				factory.DeletePod(&podToDelete)
			})

			ginkgo.It("Should recreate the storage Pod", func() {
				gomega.Eventually(func() bool {
					return fdbCluster.GetPod(podToDelete.Name).CreationTimestamp.After(deleteTime)
				}, 3*time.Minute, 1*time.Second).Should(gomega.BeTrue())
			})
		})

		ginkgo.When("enabling automatic replacements", func() {
			var exp *ChaosMeshExperiment
			var initialReplaceTime time.Duration

			ginkgo.BeforeEach(func() {
				if !factory.ChaosTestsEnabled() {
					ginkgo.Skip("Chaos tests are skipped for the operator")
				}
				availabilityCheck = false
				initialReplaceTime = time.Duration(pointer.IntDeref(
					fdbCluster.GetClusterSpec().AutomationOptions.Replacements.FailureDetectionTimeSeconds,
					90,
				)) * time.Second
				gomega.Expect(fdbCluster.SetAutoReplacements(true, 30*time.Second)).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.It("should replace the partitioned Pod", func() {
				pod := ChooseRandomPod(fdbCluster.GetStatelessPods())
				log.Printf("partition Pod: %s", pod.Name)
				exp = factory.InjectPartitionBetween(
					PodSelector(pod),
					chaosmesh.PodSelectorSpec{
						GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
							Namespaces:     []string{pod.Namespace},
							LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
						},
					},
				)

				log.Printf("waiting for pod removal: %s", pod.Name)
				gomega.Expect(fdbCluster.WaitForPodRemoval(pod)).ShouldNot(gomega.HaveOccurred())
				exists, err := factory.DoesPodExist(*pod)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(exists).To(gomega.BeFalse())
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.SetAutoReplacements(true, initialReplaceTime)).ShouldNot(gomega.HaveOccurred())
				factory.DeleteChaosMeshExperimentSafe(exp)
			})
		})

		ginkgo.When("a Pod has a bad disk", func() {
			var podWithIOError corev1.Pod
			var exp *ChaosMeshExperiment

			ginkgo.BeforeEach(func() {
				if !factory.ChaosTestsEnabled() {
					ginkgo.Skip("Chaos tests are skipped for the operator")
				}
				availabilityCheck = false
				initialPods := fdbCluster.GetLogPods()
				podWithIOError = RandomPickOnePod(initialPods.Items)
				log.Printf("Injecting I/O chaos to %s", podWithIOError.Name)
				exp = factory.InjectDiskFailure(PodSelector(&podWithIOError))

				log.Printf("iochaos injected to %s", podWithIOError.Name)
				// File creation should fail due to I/O error
				gomega.Eventually(func() error {
					_, _, err := factory.ExecuteCmdOnPod(
						&podWithIOError,
						fdbv1beta2.MainContainerName,
						"touch /var/fdb/data/test",
						false,
					)
					return err
				}, 5*time.Minute).Should(gomega.HaveOccurred())
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
				factory.DeleteChaosMeshExperimentSafe(exp)
			})

			ginkgo.It("should remove the targeted process", func() {
				fdbCluster.ReplacePod(podWithIOError, true)
				fdbCluster.EnsurePodIsDeleted(podWithIOError.Name)
			})
		})

		ginkgo.When("a Pod has high I/O latency", func() {
			var podWithIOError corev1.Pod
			var exp *ChaosMeshExperiment

			ginkgo.BeforeEach(func() {
				if !factory.ChaosTestsEnabled() {
					ginkgo.Skip("Chaos tests are skipped for the operator")
				}
				availabilityCheck = false
				initialPods := fdbCluster.GetLogPods()
				podWithIOError = RandomPickOnePod(initialPods.Items)
				log.Printf("injecting iochaos to %s", podWithIOError.Name)
				exp = factory.InjectIOLatency(PodSelector(&podWithIOError), "2s")
				log.Printf("iochaos injected to %s", podWithIOError.Name)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
				factory.DeleteChaosMeshExperimentSafe(exp)
			})

			ginkgo.It("should remove the targeted process", func() {
				fdbCluster.ReplacePod(podWithIOError, true)
				fdbCluster.EnsurePodIsDeleted(podWithIOError.Name)
			})
		})

		ginkgo.When("we change the process group prefix", func() {
			prefix := "banana"

			ginkgo.BeforeEach(func() {
				gomega.Expect(fdbCluster.SetProcessGroupPrefix(prefix)).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should add the prefix to all instances", func() {
				pods := fdbCluster.GetPods()
				for _, pod := range pods.Items {
					gomega.Expect(string(GetProcessGroupID(pod))).To(gomega.HavePrefix(prefix))
				}
			})
		})

		ginkgo.When("replacing multiple Pods", func() {
			var replacedPods []corev1.Pod

			ginkgo.BeforeEach(func() {
				fdbCluster.ReplacePods(RandomPickPod(fdbCluster.GetStatelessPods().Items, 4))
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should replace all the targeted Pods", func() {
				currentPodsNames := fdbCluster.GetPodsNames()
				for _, replacedPod := range replacedPods {
					gomega.Expect(currentPodsNames).ShouldNot(gomega.ContainElement(replacedPod.Name))
				}
			})
		})

		ginkgo.When("replacing a Pod stuck in Terminating state", func() {
			var podsBeforeReplacement []string
			var replacePod *corev1.Pod

			ginkgo.BeforeEach(func() {
				podsBeforeReplacement = fdbCluster.GetPodsNames()
				replacePod = ChooseRandomPod(fdbCluster.GetPods())
				gomega.Expect(factory.SetFinalizerForPod(replacePod, []string{"foundationdb.org/test"})).ShouldNot(gomega.HaveOccurred())
				fdbCluster.ReplacePod(*replacePod, true)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(factory.SetFinalizerForPod(replacePod, []string{})).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should replace the Pod stuck in Terminating state", func() {
				// The `replacePod` still exists as a
				// Terminating Pod because it has a finalizer
				// set to it and thus it's not deleted
				// yet. Moreover, `GetPods()` returns pods even
				// in terminating state since it's status.phase is
				// still Running (although the containers have exited).
				gomega.Expect(len(fdbCluster.GetPodsNames())).Should(gomega.Equal(len(podsBeforeReplacement) + 1))
			})
		})

		ginkgo.When("changing coordinator selection", func() {
			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.UpdateCoordinatorSelection(
					[]fdbv1beta2.CoordinatorSelectionSetting{},
				)).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.DescribeTable("changing coordinator selection",
				func(setting []fdbv1beta2.CoordinatorSelectionSetting, expectedProcessClassList []fdbv1beta2.ProcessClass) {
					gomega.Expect(fdbCluster.UpdateCoordinatorSelection(setting)).ShouldNot(gomega.HaveOccurred())
					pods := fdbCluster.GetCoordinators()
					for _, pod := range pods {
						log.Println(pod.Name)
						gomega.Expect(
							GetProcessClass(pod),
						).Should(gomega.BeElementOf(expectedProcessClassList))
					}
				},
				ginkgo.Entry("selecting only log processes as coordinators",
					[]fdbv1beta2.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbv1beta2.ProcessClassLog,
							Priority:     0,
						},
					},
					[]fdbv1beta2.ProcessClass{fdbv1beta2.ProcessClassLog},
				),
				ginkgo.Entry("selecting only storage processes as coordinators",
					[]fdbv1beta2.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbv1beta2.ProcessClassStorage,
							Priority:     0,
						},
					},
					[]fdbv1beta2.ProcessClass{fdbv1beta2.ProcessClassStorage},
				),
				ginkgo.Entry("selecting both storage and log processes as coordinators but preferring storage",
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

		ginkgo.When("increasing the number of log Pods by one", func() {
			var initialPodCount int

			ginkgo.BeforeEach(func() {
				initialPodCount = len(fdbCluster.GetLogPods().Items)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.UpdateLogProcessCount(initialPodCount)).ShouldNot(gomega.HaveOccurred())
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", initialPodCount))
			})

			ginkgo.It("should increase the count of log Pods by one", func() {
				gomega.Expect(fdbCluster.UpdateLogProcessCount(initialPodCount + 1)).ShouldNot(gomega.HaveOccurred())
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", initialPodCount+1))
			})
		})

		ginkgo.When("setting 2 logs per disk", func() {
			var initialLogServerPerPod, expectedPodCnt, expectedLogProcessesCnt int

			ginkgo.BeforeEach(func() {
				initialLogServerPerPod = fdbCluster.GetLogServersPerPod()
				initialPods := fdbCluster.GetLogPods()
				expectedPodCnt = len(initialPods.Items)
				expectedLogProcessesCnt = expectedPodCnt * initialLogServerPerPod
				log.Printf(
					"expectedPodCnt: %d, expectedProcessesCnt: %d",
					expectedPodCnt,
					expectedLogProcessesCnt,
				)
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", expectedPodCnt))
			})

			ginkgo.AfterEach(func() {
				log.Printf("set log servers per Pod to %d", initialLogServerPerPod)
				gomega.Expect(fdbCluster.SetLogServersPerPod(initialLogServerPerPod, true)).ShouldNot(gomega.HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*initialLogServerPerPod,
				)
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", expectedPodCnt))
			})

			ginkgo.It("should update the log servers to the expected amount", func() {
				serverPerPod := initialLogServerPerPod * 2
				log.Printf("set log servers per Pod to %d", initialLogServerPerPod)
				gomega.Expect(fdbCluster.SetLogServersPerPod(serverPerPod, true)).ShouldNot(gomega.HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*serverPerPod,
				)
				validateProcessesCount(fdbCluster, fdbv1beta2.ProcessClassLog, expectedPodCnt, expectedPodCnt*serverPerPod)
			})
		})

		ginkgo.When("setting 2 logs per disk to use transaction process", func() {
			var initialLogServerPerPod, expectedPodCnt, expectedLogProcessesCnt int

			ginkgo.BeforeEach(func() {
				initialLogServerPerPod = fdbCluster.GetLogServersPerPod()
				initialPods := fdbCluster.GetLogPods()
				expectedPodCnt = len(initialPods.Items)
				expectedLogProcessesCnt = expectedPodCnt * initialLogServerPerPod
				log.Printf(
					"expectedPodCnt: %d, expectedProcessesCnt: %d",
					expectedPodCnt,
					expectedLogProcessesCnt,
				)
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", expectedPodCnt))
			})

			ginkgo.AfterEach(func() {
				log.Printf("set log servers per Pod to %d", initialLogServerPerPod)
				gomega.Expect(fdbCluster.SetTransactionServerPerPod(initialLogServerPerPod, expectedLogProcessesCnt, true)).ShouldNot(gomega.HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*initialLogServerPerPod,
				)
				gomega.Eventually(func() int {
					return len(fdbCluster.GetLogPods().Items)
				}).Should(gomega.BeNumerically("==", expectedPodCnt))
			})

			ginkgo.It("should update the log servers to the expected amount and should create transaction Pods", func() {
				serverPerPod := initialLogServerPerPod * 2
				gomega.Expect(fdbCluster.SetTransactionServerPerPod(serverPerPod, expectedLogProcessesCnt, true)).ShouldNot(gomega.HaveOccurred())
				log.Printf(
					"expectedPodCnt: %d, expectedProcessesCnt: %d",
					expectedPodCnt,
					expectedPodCnt*serverPerPod,
				)
				validateProcessesCount(fdbCluster, fdbv1beta2.ProcessClassTransaction, expectedPodCnt, expectedPodCnt*serverPerPod)
			})
		})

		ginkgo.When("Migrating a cluster to a different storage class", func() {
			var defaultStorageClass, targetStorageClass string

			ginkgo.BeforeEach(func() {
				// This will only return StorageClasses that have a label foundationdb.org/operator-testing=true defined.
				storageClasses, err := factory.GetStorageClasses(map[string]string{
					"foundationdb.org/operator-testing": "true",
				})

				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				if len(storageClasses.Items) < 2 {
					ginkgo.Skip("This test requires at least two available StorageClasses")
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

				gomega.Expect(fdbCluster.UpdateStorageClass(
					targetStorageClass,
					fdbv1beta2.ProcessClassLog,
				)).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should migrate the cluster", func() {
				fdbCluster.ValidateStorageClass(fdbv1beta2.ProcessClassLog, targetStorageClass)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.UpdateStorageClass(
					defaultStorageClass,
					fdbv1beta2.ProcessClassLog,
				)).NotTo(gomega.HaveOccurred())
			})
		})

		ginkgo.When("Replacing a Pod with PVC stuck in Terminating state", func() {
			var replacePod *corev1.Pod
			var initialVolumeClaims *corev1.PersistentVolumeClaimList
			var pvc corev1.PersistentVolumeClaim

			ginkgo.BeforeEach(func() {
				replacePod = ChooseRandomPod(fdbCluster.GetLogPods())
				volClaimName := GetPvc(replacePod)
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
				gomega.Expect(
					fdbCluster.SetFinalizerForPvc([]string{"foundationdb.org/test"}, pvc),
				).ShouldNot(gomega.HaveOccurred())
				fdbCluster.ReplacePod(*replacePod, true)
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.SetFinalizerForPvc([]string{}, pvc)).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It("should replace the PVC stuck in Terminating state", func() {
				fdbCluster.EnsurePodIsDeleted(replacePod.Name)
				volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(
					fdbv1beta2.ProcessClassLog,
				)
				volumeClaimNames := make([]string, 0, len(volumeClaims.Items))
				for _, volumeClaim := range volumeClaims.Items {
					volumeClaimNames = append(volumeClaimNames, volumeClaim.Name)
				}
				gomega.Expect(volumeClaimNames).Should(gomega.ContainElement(pvc.Name))
				gomega.Expect(len(volumeClaims.Items)).Should(gomega.Equal(len(initialVolumeClaims.Items) + 1))
			})
		})

		// This test is currently flaky and we are working on making it stable.
		ginkgo.PWhen("setting the empty config to true", func() {
			var storageProcessCnt int

			ginkgo.BeforeEach(func() {
				storageProcessCnt = fdbCluster.GetProcessCount(
					fdbv1beta2.ProcessRoleStorage,
				)
				gomega.Expect(fdbCluster.SetEmptyMonitorConf(true)).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.It("should stop all running processes", func() {
				gomega.Consistently(func() int {
					return fdbCluster.GetProcessCount(fdbv1beta2.ProcessRoleStorage)
				}).Should(gomega.BeNumerically("==", -1))
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.SetEmptyMonitorConf(false)).ShouldNot(gomega.HaveOccurred())
				// Wait until all storage servers are ready and their "role" information gets
				// reported correctly in "status" output.
				time.Sleep(5 * time.Minute)
				gomega.Consistently(func() int {
					return fdbCluster.GetProcessCount(fdbv1beta2.ProcessRoleStorage)
				}).Should(gomega.BeNumerically("==", storageProcessCnt))
			})
		})

		ginkgo.When("using the buggify option to ignore a process during the restart", func() {
			var newGeneralCustomParameters, initialGeneralCustomParameters fdbv1beta2.FoundationDBCustomParameters
			var newKnob string
			var pickedPod *corev1.Pod

			ginkgo.BeforeEach(func() {
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

				pickedPod = ChooseRandomPod(fdbCluster.GetStatelessPods())
				log.Println("Selected Pod:", pickedPod.Name, " to be skipped during the restart")
				fdbCluster.SetIgnoreDuringRestart(
					[]fdbv1beta2.ProcessGroupID{GetProcessGroupID(*pickedPod)},
				)

				gomega.Expect(
					fdbCluster.SetCustomParameters(
						fdbv1beta2.ProcessClassGeneral,
						newGeneralCustomParameters,
						false,
					),
				).NotTo(gomega.HaveOccurred())
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassGeneral,
					initialGeneralCustomParameters,
					false,
				)).NotTo(gomega.HaveOccurred())
				fdbCluster.SetIgnoreDuringRestart(nil)
			})

			ginkgo.It("should not restart the process on the ignore list", func() {
				processGroupID := GetProcessGroupID(*pickedPod)

				// Ensure that the process group has the condition IncorrectCommandLine and is kept in that state for 1 minute.
				gomega.Eventually(func() bool {
					cluster := fdbCluster.GetCluster()
					for _, processGroup := range cluster.Status.ProcessGroups {
						if processGroup.ProcessGroupID != processGroupID {
							continue
						}

						// The IncorrectCommandLine condition represents that the process must be restarted.
						return processGroup.GetConditionTime(fdbv1beta2.IncorrectCommandLine) != nil
					}

					return false
				}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(12).Should(gomega.BeTrue())
			})
		})

		// TODO (johscheuer): enable this test once the CRD is updated in out CI cluster.
		ginkgo.PWhen("a process group is set to be blocked for removal", func() {
			var podMarkedForRemoval corev1.Pod
			var processGroupID fdbv1beta2.ProcessGroupID

			ginkgo.BeforeEach(func() {
				initialPods := fdbCluster.GetStatelessPods()
				podMarkedForRemoval = RandomPickOnePod(initialPods.Items)
				log.Println("Setting Pod", podMarkedForRemoval.Name, "to be blocked to be removed and mark it for removal.")
				processGroupID = GetProcessGroupID(podMarkedForRemoval)
				fdbCluster.SetBuggifyBlockRemoval([]fdbv1beta2.ProcessGroupID{processGroupID})
				fdbCluster.ReplacePod(podMarkedForRemoval, false)
			})

			ginkgo.AfterEach(func() {
				fdbCluster.SetBuggifyBlockRemoval(nil)
				fdbCluster.EnsurePodIsDeleted(podMarkedForRemoval.Name)
			})

			ginkgo.It("should exclude the Pod but not remove the resources", func() {
				gomega.Eventually(func() bool {
					cluster := fdbCluster.GetCluster()

					for _, processGroup := range cluster.Status.ProcessGroups {
						if processGroup.ProcessGroupID != processGroupID {
							continue
						}

						return !processGroup.ExclusionTimestamp.IsZero() && !processGroup.RemovalTimestamp.IsZero()
					}

					return false
				}).WithTimeout(5 * time.Minute).WithPolling(15 * time.Second).ShouldNot(gomega.BeTrue())
				gomega.Consistently(func() *corev1.Pod {
					return fdbCluster.GetPod(podMarkedForRemoval.Name)
				}).WithTimeout(1 * time.Minute).WithPolling(15 * time.Second).ShouldNot(gomega.BeNil())
			})
		})

		ginkgo.Context("testing maintenance mode functionality", func() {
			ginkgo.When("maintenance mode is on", func() {
				ginkgo.BeforeEach(func() {
					command := fmt.Sprintf("maintenance on %s %s", "operator-test-1-storage-4", "40000")
					_, _ = fdbCluster.RunFdbCliCommandInOperator(command, false, 40)
				})

				ginkgo.AfterEach(func() {
					_, _ = fdbCluster.RunFdbCliCommandInOperator("maintenance off", false, 40)
				})

				ginkgo.It("status maintenance zone should match", func() {
					status := fdbCluster.GetStatus()
					gomega.Expect(status.Cluster.MaintenanceZone).To(gomega.Equal(fdbv1beta2.FaultDomain("operator-test-1-storage-4")))
				})
			})
		})

		ginkgo.When("a process group has no address assigned and should be removed", func() {
			var processGroupID fdbv1beta2.ProcessGroupID
			var podName string

			ginkgo.BeforeEach(func() {
				initialPods := fdbCluster.GetStatelessPods()
				processGroupIDs := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}

				for _, pod := range initialPods.Items {
					processGroupIDs[GetProcessGroupID(pod)] = fdbv1beta2.None{}
				}

				idNum := 1
				for {
					_, processGroupID = internal.GetProcessGroupID(fdbCluster.GetCachedCluster(), fdbv1beta2.ProcessClassStateless, idNum)
					if fdbCluster.GetCachedCluster().ProcessGroupIsBeingRemoved(processGroupID) {
						idNum++
						continue
					}

					// If the process group is not present use this one.
					if _, ok := processGroupIDs[processGroupID]; !ok {
						break
					}

					idNum++
				}

				// Make sure the new Pod will be stuck in unschedulable.
				fdbCluster.SetProcessGroupsAsUnschedulable([]fdbv1beta2.ProcessGroupID{processGroupID})
				// Now replace a random Pod for replacement to force the cluster to create a new Pod.
				fdbCluster.ReplacePod(RandomPickOnePod(initialPods.Items), false)

				// Wait until the new Pod is actually created.
				gomega.Eventually(func() bool {
					for _, pod := range fdbCluster.GetStatelessPods().Items {
						if GetProcessGroupID(pod) == processGroupID {
							podName = pod.Name
							return true
						}
					}

					return false
				}).WithPolling(2 * time.Second).WithTimeout(5 * time.Minute).Should(gomega.BeTrue())

				fdbCluster.ReplacePod(RandomPickOnePod(initialPods.Items), false)

				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.ProcessGroupsToRemove = append(spec.ProcessGroupsToRemove, processGroupID)
				// Add the new pending Pod to the removal list.
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			ginkgo.It("should not remove the Pod as long as it is unschedulable", func() {
				// Make sure the Pod is stuck in pending for 2 minutes
				gomega.Consistently(func() corev1.PodPhase {
					return fdbCluster.GetPod(podName).Status.Phase
				}).WithTimeout(2 * time.Minute).WithPolling(15 * time.Second).Should(gomega.Equal(corev1.PodPending))
				// Clear the buggify list, this should allow the operator to move forward and delete the Pod
				gomega.Expect(fdbCluster.ClearBuggifyNoSchedule(true)).NotTo(gomega.HaveOccurred())
				// Make sure the Pod is deleted.
				gomega.Expect(fdbCluster.CheckPodIsDeleted(podName)).To(gomega.BeTrue())
			})
		})

		// TODO (johscheuer): Enable those tests.
		ginkgo.PWhen("nodes in the cluster are tainted", func() {
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

			ginkgo.BeforeEach(func() {
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

				gomega.Expect(len(fdbCluster.GetCluster().Spec.AutomationOptions.Replacements.TaintReplacementOptions)).To(gomega.BeNumerically(">=", 1))
				gomega.Expect(*fdbCluster.GetAutomationOptions().Replacements.Enabled).To(gomega.BeTrue())
			})

			ginkgo.AfterEach(func() {
				gomega.Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(gomega.HaveOccurred())
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

			ginkgo.When("cluster enables taint feature on focused maintenance taint key", func() {
				ginkgo.It("should remove the targeted Pod whose Node is tainted", func() {
					replacedPod := RandomPickOnePod(initialPods.Items)
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
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					fdbCluster.EnsurePodIsDeletedWithCustomTimeout(replacedPod.Name, ensurePodIsDeletedTimeoutMinutes)
				})

				ginkgo.It("should remove all Pods whose Nodes are tainted", func() {
					replacedPods := RandomPickPod(initialPods.Items, numNodesTainted)
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
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					for i, pod := range replacedPods {
						log.Printf("Ensure %dth pod:%s is deleted\n", i, pod.Name)
						fdbCluster.EnsurePodIsDeletedWithCustomTimeout(pod.Name, ensurePodIsDeletedTimeoutMinutes)
					}
				})
			})

			ginkgo.When("cluster enables taint feature on star taint key", func() {
				taintKeyMaintenanceDuration = int64(1)
				taintKeyStar = "*"
				taintKeyStarDuration = int64(5)

				ginkgo.BeforeEach(func() {
					// Custom TaintReplacementOptions to taintKeyStar
					fdbCluster.SetClusterTaintConfig([]fdbv1beta2.TaintReplacementOption{
						{
							Key:               &taintKeyStar,
							DurationInSeconds: &taintKeyStarDuration,
						},
					}, pointer.Int(1))

					automationOptions := fdbCluster.GetAutomationOptions()
					gomega.Expect(len(automationOptions.Replacements.TaintReplacementOptions)).To(gomega.BeNumerically(">=", 1))
					gomega.Expect(*automationOptions.Replacements.Enabled).To(gomega.BeTrue())
				})

				ginkgo.It("should eventually remove the targeted Pod whose Node is tainted", func() {
					replacedPod := RandomPickOnePod(initialPods.Items)
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
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					fdbCluster.EnsurePodIsDeletedWithCustomTimeout(replacedPod.Name, ensurePodIsDeletedTimeoutMinutes)
				})

				ginkgo.It("should eventually remove all Pods whose Nodes are tainted", func() {
					replacedPods := RandomPickPod(initialPods.Items, numNodesTainted)
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
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					for i, pod := range replacedPods {
						log.Printf("Ensure %dth pod:%s is deleted\n", i, pod.Name)
						fdbCluster.EnsurePodIsDeletedWithCustomTimeout(pod.Name, ensurePodIsDeletedTimeoutMinutes)
					}
				})
			})

			ginkgo.When("cluster disables taint feature with empty taint option", func() {
				taintKeyMaintenanceDuration = int64(1)
				taintKeyStar = "*"

				ginkgo.BeforeEach(func() {
					// Custom TaintReplacementOptions to taintKeyStar
					fdbCluster.SetClusterTaintConfig([]fdbv1beta2.TaintReplacementOption{}, pointer.Int(1))

					automationOptions := fdbCluster.GetAutomationOptions()
					gomega.Expect(len(automationOptions.Replacements.TaintReplacementOptions)).To(gomega.BeNumerically("==", 0))
					gomega.Expect(*automationOptions.Replacements.Enabled).To(gomega.BeTrue())
				})

				ginkgo.It("should not remove any Pod whose Nodes are tainted", func() {
					targetPods := RandomPickPod(initialPods.Items, numNodesTainted)
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

					gomega.Consistently(func() bool {
						for _, groupID := range targetPodProcessGroupIDs {
							processGroupStatus := fdbv1beta2.FindProcessGroupByID(fdbCluster.GetCluster().Status.ProcessGroups, groupID)
							gomega.Expect(processGroupStatus).NotTo(gomega.BeNil())
							gomega.Expect(processGroupStatus.GetCondition(fdbv1beta2.NodeTaintDetected)).To(gomega.BeNil())
						}
						return true
					}).WithTimeout(time.Duration(*fdbCluster.GetAutomationOptions().Replacements.TaintReplacementTimeSeconds-1) * time.Second).WithPolling(1 * time.Second).Should(gomega.BeTrue())

					// Wait for operator to replace the pod
					time.Sleep(time.Second * time.Duration(*fdbCluster.GetAutomationOptions().Replacements.TaintReplacementTimeSeconds+1))
					err := fdbCluster.WaitForReconciliation()
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					for i, pod := range targetPods {
						log.Printf("Ensure %dth pod:%s is not deleted\n", i, pod.Name)
						deleted := fdbCluster.CheckPodIsDeleted(pod.Name)
						gomega.Expect(deleted).To(gomega.BeFalse())
					}
				})
			})
		})
	})
}
