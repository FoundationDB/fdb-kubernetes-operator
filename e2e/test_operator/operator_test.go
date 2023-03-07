package operator

import (
	"flag"
	"log"
	"math"
	"math/rand"
	"testing"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/pointer"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	// TODO(johscheuer): move this into a common method to make it easier to be consumed
	testing.Init()
	_, err := types.NewAttachedGinkgoFlagSet(flag.CommandLine, types.GinkgoFlags{}, nil, types.GinkgoFlagSections{}, types.GinkgoFlagSection{})
	if err != nil {
		log.Fatal(err)
	}
	testOptions = &fixtures.FactoryOptions{}
	testOptions.BindFlags(flag.CommandLine)
	flag.Parse()
}

func validateStorageProcesses(
	fdbCluster *fixtures.FdbCluster,
	countStoragePods int,
	countStorageServer int,
) {
	// Using Eventually here to prevent some weird timing from the test runner
	Eventually(func() int {
		return len(fdbCluster.GetStoragePods().Items)
	}).Should(BeNumerically("==", countStoragePods))
	Eventually(func() int {
		return fdbCluster.GetProcessCount(fdbv1beta2.ProcessRoleStorage)
	}).Should(BeNumerically("==", countStorageServer))
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
			"*/1 * * * *",
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

var _ = Describe("Operator", func() {
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

	When("replacing a Pod", func() {
		var replacedPod corev1.Pod

		BeforeEach(func() {
			initialPods := fdbCluster.GetStatelessPods()
			replacedPod = fixtures.RandomPickOnePod(initialPods.Items)
			fdbCluster.ReplacePod(replacedPod, true)
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
		})

		It("should remove the targeted Pod", func() {
			fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
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
			validateStorageProcesses(fdbCluster, expectedPodCnt, expectedStorageProcessesCnt)
		})

		AfterEach(func() {
			log.Printf("set storage server per pod to %d", initialStorageServerPerPod)
			Expect(fdbCluster.SetStorageServerPerPod(initialStorageServerPerPod)).ShouldNot(HaveOccurred())
			log.Printf(
				"expectedPodCnt: %d, expectedStorageProcessesCnt: %d",
				expectedPodCnt,
				expectedPodCnt*initialStorageServerPerPod,
			)
			validateStorageProcesses(
				fdbCluster,
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
			validateStorageProcesses(fdbCluster, expectedPodCnt, expectedPodCnt*serverPerPod)
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
				[]fdbv1beta2.ProcessGroupID{fdbv1beta2.ProcessGroupID(pickedPod.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()])},
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
			processGroupID := fdbv1beta2.ProcessGroupID(pickedPod.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()])

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
})
