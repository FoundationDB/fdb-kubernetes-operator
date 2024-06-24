/*
 * operator_upgrades_test.go
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

package operatorupgrades

/*
This test suite includes tests to validate the behaviour of the operator during upgrades on a FoundationDB cluster.
The executed tests will verify that the upgrades can proceed under different failure scenarios.
Each test will create a new FoundationDB cluster which will be upgraded.
Since FoundationDB is version incompatible for major and minor versions and the upgrade process for FoundationDB on Kubernetes requires multiple steps (see the documentation in the docs folder) we test different scenarios where only some processes are restarted.
*/

import (
	"fmt"
	"log"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
})

func clusterSetupWithConfig(beforeVersion string, availabilityCheck bool, config *fixtures.ClusterConfig) {
	factory.SetBeforeVersion(beforeVersion)
	fdbCluster = factory.CreateFdbCluster(
		config,
		factory.GetClusterOptions(fixtures.UseVersionBeforeUpgrade)...,
	)

	// We have some tests where we expect some down time e.g. when no coordinator is restarted during an upgrade.
	// In order to make sure the test is not failing based on the availability check we can disable the availability check if required.
	if !availabilityCheck {
		return
	}

	Expect(
		fdbCluster.InvariantClusterStatusAvailableWithThreshold(15 * time.Second),
	).ShouldNot(HaveOccurred())
}

func clusterSetup(beforeVersion string, availabilityCheck bool) {
	clusterSetupWithConfig(beforeVersion, availabilityCheck, &fixtures.ClusterConfig{
		DebugSymbols: false,
	})
}

var _ = Describe("Operator Upgrades", Label("e2e", "pr"), func() {
	BeforeEach(func() {
		factory = fixtures.CreateFactory(testOptions)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		factory.Shutdown()
	})

	// Ginkgo lacks the support for AfterEach and BeforeEach in tables, so we have to put everything inside the testing function
	// this setup allows to dynamically generate the table entries that will be executed e.g. to test different upgrades
	// for different versions without hard coding or having multiple flags.
	DescribeTable(
		"upgrading a cluster with a random Pod deleted during rolling bounce phase",
		func(beforeVersion string, targetVersion string) {
			// We disable the availability check here as there could be some race conditions between the operator and
			// the test suite where two pods are taken down at the same time which would affect the availability of the
			// cluster.
			clusterSetup(beforeVersion, false)
			prevImage := fdbCluster.GetFDBImage()

			// 1. Start upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// If the versions are protocol compatible we only have a rolling bounce phase. The check below is only
			// required if the versions are not compatible and the operator is using the staging phase.
			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// 2. Wait until we get to rolling bounce phase.
				log.Println("wait for rolling bounce phase.")
				Eventually(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == targetVersion
				}).Should(BeTrue())
				log.Println("cluster in rolling bounce phase.")
			}

			// 3. Until all pods are bounced, try deleting some Pods.
			//
			// We can't use a scheduled PodKill (https://chaos-mesh.org/docs/define-scheduling-rules)
			// here since the PodSelector in Chaos-Mesh doesn't allow to select Pods based on
			// the running image.
			Eventually(func() bool {
				pods := make([]corev1.Pod, 0)
				for _, pod := range fdbCluster.GetPods().Items {
					for _, container := range pod.Spec.Containers {
						if container.Name != fdbv1beta2.MainContainerName {
							continue
						}

						if container.Image != prevImage {
							log.Println(
								"Pod",
								pod.Name,
								"is already upgraded and has image",
								container.Image,
								". Skipping.",
							)
							continue
						}

						pods = append(pods, pod)
					}
				}

				if len(pods) == 0 {
					log.Println("No more pods with older container image.")
					return true
				}

				selectedPod := factory.RandomPickOnePod(pods)
				log.Println("deleting pod: ", selectedPod.Name)
				factory.DeletePod(&selectedPod)
				Expect(fdbCluster.WaitForPodRemoval(&selectedPod)).ShouldNot(HaveOccurred())
				return false
			}).WithTimeout(20 * time.Minute).WithPolling(3 * time.Minute).Should(BeTrue())

			// 5. Verify a final time that the cluster is reconciled, this should be quick.
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},

		EntryDescription("Upgrade from %[1]s to %[2]s with pods deleted during rolling bounce"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster where one coordinator gets restarted during the staging phase",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test case only affects version incompatible upgrades")
			}

			clusterSetup(beforeVersion, false)

			// Select one coordinator that will be restarted during the staging phase.
			coordinators := fdbCluster.GetCoordinators()
			Expect(coordinators).NotTo(BeEmpty())

			selectedCoordinator := coordinators[0]
			log.Println(
				"Selected coordinator:",
				selectedCoordinator.Name,
				"(podIP:",
				selectedCoordinator.Status.PodIP,
				") to be restarted during the staging phase",
			)

			// Disable the feature that the operator restarts processes. This allows us to restart the coordinator
			// once tall new binaries are present.
			fdbCluster.SetKillProcesses(false)

			// Start the upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Wait until all process groups are in the staging phase and the new binaries are available.
			Eventually(func() bool {
				return fdbCluster.AllProcessGroupsHaveCondition(fdbv1beta2.IncorrectCommandLine)
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())

			// Restart the fdbserver process to pickup the new configuration and run with the newer version.
			_, _, err := fdbCluster.ExecuteCmdOnPod(
				selectedCoordinator,
				fdbv1beta2.MainContainerName,
				"pkill fdbserver",
				false,
			)
			Expect(err).NotTo(HaveOccurred())

			// Check if the restarted process is showing up in IncompatibleConnections list in status output.
			Eventually(func() bool {
				status := fdbCluster.GetStatus()
				if len(status.Cluster.IncompatibleConnections) == 0 {
					return false
				}

				log.Println("IncompatibleProcesses:", status.Cluster.IncompatibleConnections)
				Expect(len(status.Cluster.IncompatibleConnections)).To(Equal(1))
				// Extract the IP of the incompatible process.
				incompatibleProcess := strings.Split(status.Cluster.IncompatibleConnections[0], ":")[0]
				return incompatibleProcess == selectedCoordinator.Status.PodIP
			}).WithTimeout(180 * time.Second).WithPolling(4 * time.Second).Should(BeTrue())

			// Allow the operator to restart processes and the upgrade should continue and finish.
			fdbCluster.SetKillProcesses(true)

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},

		EntryDescription(
			"Upgrade from %[1]s to %[2]s with one coordinator restarted during the staging phase",
		),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with a crash looping sidecar process",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, true)

			Expect(fdbCluster.SetAutoReplacements(false, 20*time.Minute)).ToNot(HaveOccurred())

			// 1. Introduce crash-loop into sidecar container to artificially create a partition.
			pickedPod := factory.ChooseRandomPod(fdbCluster.GetStoragePods())
			log.Println("Injecting container fault to crash-loop sidecar process:", pickedPod.Name)

			fdbCluster.SetCrashLoopContainers([]fdbv1beta2.CrashLoopContainerObject{
				{
					ContainerName: fdbv1beta2.SidecarContainerName,
					Targets:       []fdbv1beta2.ProcessGroupID{fdbv1beta2.ProcessGroupID(pickedPod.Labels[fdbv1beta2.FDBProcessGroupIDLabel])},
				},
			}, false)
			log.Println("Crash injected in pod:", pickedPod.Name)

			// Wait until the Pod is running again and the sidecar is crash-looping.
			Eventually(func(g Gomega) corev1.PodPhase {
				pod := fdbCluster.GetPod(pickedPod.Name)
				for _, container := range pod.Spec.Containers {
					if container.Name == fdbv1beta2.SidecarContainerName {
						log.Println("Container:", container.Name, "Args:", container.Args, "Phase:", pod.Status.Phase)
						g.Expect(container.Args[0]).To(Equal("crash-loop"))
					}
				}

				return pod.Status.Phase
			}).WithPolling(2 * time.Second).WithTimeout(2 * time.Minute).MustPassRepeatedly(10).Should(Equal(corev1.PodRunning))

			// Make sure we trigger a new reconciliation to make sure the process is up and running and only the sidecar
			// is crash looping.
			fdbCluster.ForceReconcile()

			Eventually(func(g Gomega) bool {
				for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
					g.Expect(processGroup.GetConditionTime(fdbv1beta2.MissingProcesses)).To(BeNil())
				}

				return true
			}).WithPolling(2 * time.Second).WithTimeout(4 * time.Minute).MustPassRepeatedly(10).Should(BeTrue())

			// 2. Start cluster upgrade.
			log.Printf("Crash injected in sidecar container %s. Starting upgrade.", pickedPod.Name)
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// 3. Until we remove the crash loop setup, cluster should not be upgraded.
				//    Keep checking for 4m. The desired behavior is that binaries are staged
				//    but cluster is not restarted to new version.
				log.Println("upgrade should not finish while sidecar process is unavailable")
				Consistently(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == beforeVersion
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
			} else {
				// It should upgrade the cluster if the version is protocol compatible.
				fdbCluster.VerifyVersion(targetVersion)
			}

			// 4. Remove the crash-loop.
			log.Println("Removing crash-loop from", pickedPod.Name)
			fdbCluster.SetCrashLoopContainers(nil, false)

			// Wait for the Pod to come back again.
			Eventually(func() corev1.PodPhase {
				pod := fdbCluster.GetPod(pickedPod.Name)

				return pod.Status.Phase
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).MustPassRepeatedly(5).Should(Equal(corev1.PodRunning))

			fdbCluster.ForceReconcile()

			// 5. Upgrade should proceed after we stop killing the sidecar.
			fdbCluster.VerifyVersion(targetVersion)
			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with crash-looping sidecar"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and one coordinator is not restarted",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, true)

			// 1. Select one coordinator and use the buggify option to skip it during the restart command.
			coordinators := fdbCluster.GetCoordinators()
			Expect(coordinators).NotTo(BeEmpty())

			selectedCoordinator := coordinators[0]
			log.Println(
				"Selected coordinator:",
				selectedCoordinator.Name,
				"(podIP:",
				selectedCoordinator.Status.PodIP,
				") to be skipped during the restart",
			)
			fdbCluster.SetIgnoreDuringRestart(
				[]fdbv1beta2.ProcessGroupID{
					fdbv1beta2.ProcessGroupID(selectedCoordinator.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()]),
				},
			)

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())

			// Make sure that the incompatible connections are cleaned up after some time.
			Eventually(func() []string {
				return fdbCluster.GetStatus().Cluster.IncompatibleConnections
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(5).Should(BeEmpty())

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with one coordinator not being restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and multiple processes are not restarted",
		func(beforeVersion string, targetVersion string) {
			// We ignore the availability check here since this check is sometimes flaky if not all coordinators are running.
			clusterSetup(beforeVersion, false)

			// 1. Select half of the stateless and half of the log processes and use the buggify option to skip those
			// processes during the restart command.
			statelessPods := fdbCluster.GetStatelessPods()
			Expect(statelessPods.Items).NotTo(BeEmpty())
			selectedStatelessPods := factory.RandomPickPod(
				statelessPods.Items,
				len(statelessPods.Items)/2,
			)

			logPods := fdbCluster.GetLogPods()
			Expect(logPods.Items).NotTo(BeEmpty())
			selectedLogPods := factory.RandomPickPod(logPods.Items, len(logPods.Items)/2)

			ignoreDuringRestart := make(
				[]fdbv1beta2.ProcessGroupID,
				0,
				len(selectedLogPods)+len(selectedStatelessPods),
			)

			for _, pod := range selectedStatelessPods {
				ignoreDuringRestart = append(
					ignoreDuringRestart,
					fdbv1beta2.ProcessGroupID(pod.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()]),
				)
			}

			for _, pod := range selectedLogPods {
				ignoreDuringRestart = append(
					ignoreDuringRestart,
					fdbv1beta2.ProcessGroupID(pod.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()]),
				)
			}

			log.Println(
				"Selected Pods:",
				ignoreDuringRestart,
				" to be skipped during the restart",
			)
			fdbCluster.SetIgnoreDuringRestart(ignoreDuringRestart)

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s and multiple processes are not restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and no coordinator is restarted",
		func(beforeVersion string, targetVersion string) {
			// We ignore the availability check here since this check is sometimes flaky if not all coordinators are running.
			clusterSetup(beforeVersion, false)

			// 1. Select one coordinator and use the buggify option to skip it during the restart command.
			coordinators := fdbCluster.GetCoordinators()
			Expect(coordinators).NotTo(BeEmpty())

			ignoreDuringRestart := make([]fdbv1beta2.ProcessGroupID, 0, len(coordinators))
			for _, coordinator := range coordinators {
				ignoreDuringRestart = append(
					ignoreDuringRestart,
					fdbv1beta2.ProcessGroupID(coordinator.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()]),
				)
			}

			fdbCluster.SetIgnoreDuringRestart(ignoreDuringRestart)

			// The cluster will be stuck in this state until the coordinators are upgraded
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// The upgrade will be stuck until the coordinators are restarted
				Consistently(func() bool {
					cluster := fdbCluster.GetCluster()
					return cluster.Spec.Version != cluster.Status.RunningVersion
				}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())

				// Restart the fdbserver processes
				for _, coordinator := range coordinators {
					Eventually(func() error {
						_, _, err := fdbCluster.ExecuteCmdOnPod(
							coordinator,
							fdbv1beta2.MainContainerName,
							"pkill fdbserver",
							false,
						)
						return err
					}).WithTimeout(1 * time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveOccurred())
				}
			}

			fdbCluster.SetIgnoreDuringRestart(nil)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s and no coordinator is restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"one process is marked for removal",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			clusterSetup(beforeVersion, true)

			// Select one Pod, this Pod will be marked to be removed but the actual removal will be blocked. The intention
			// is to simulate a Pods that should be removed but the removal is not completed yet and an upgrade will be started.
			podMarkedForRemoval := factory.RandomPickOnePod(fdbCluster.GetPods().Items)
			processGroupMarkedForRemoval := fixtures.GetProcessGroupID(podMarkedForRemoval)
			log.Println("picked Pod", podMarkedForRemoval.Name, "to be marked for removal")
			// Use the buggify option to block the actual removal.
			fdbCluster.SetBuggifyBlockRemoval([]fdbv1beta2.ProcessGroupID{processGroupMarkedForRemoval})
			// Don't wait for reconciliation as the cluster will never reconcile.
			fdbCluster.ReplacePod(podMarkedForRemoval, false)
			// Make sure the process group is marked for removal
			Eventually(func() *metav1.Time {
				cluster := fdbCluster.GetCluster()

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID != processGroupMarkedForRemoval {
						continue
					}

					return processGroup.RemovalTimestamp
				}

				return nil
			}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).MustPassRepeatedly(5).ShouldNot(BeNil())

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Make sure the cluster is upgraded
			fdbCluster.VerifyVersion(targetVersion)

			// Make sure the other processes are updated to the new image and the operator is able to proceed with the upgrade.
			Eventually(func() int {
				var processesToUpdate int

				cluster := fdbCluster.GetCluster()

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == processGroupMarkedForRemoval {
						continue
					}

					if len(processGroup.ProcessGroupConditions) > 0 {
						processesToUpdate++
					}
				}

				log.Println("processes that needs to be updated", processesToUpdate)

				return processesToUpdate
			}).WithTimeout(30 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(5).Should(BeNumerically("==", 0))

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"one process is marked for removal and is stuck in terminating state",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			clusterSetup(beforeVersion, true)

			// Select one Pod, this Pod will be marked to be removed but the actual removal will be blocked. The intention
			// is to simulate a Pods that should be removed but the removal is not completed yet and an upgrade will be started.
			podMarkedForRemoval := factory.RandomPickOnePod(fdbCluster.GetPods().Items)
			processGroupMarkedForRemoval := fixtures.GetProcessGroupID(podMarkedForRemoval)
			log.Println("picked Pod", podMarkedForRemoval.Name, "to be marked for removal")
			// Set a finalizer for this Pod to make sure the Pod object cannot be garbage collected
			factory.SetFinalizerForPod(&podMarkedForRemoval, []string{"foundationdb.org/test"})
			// Don't wait for reconciliation as the cluster will never reconcile.
			fdbCluster.ReplacePod(podMarkedForRemoval, false)
			// Make sure the process group is marked for removal
			Eventually(func() *int64 {
				cluster := fdbCluster.GetCluster()

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID != processGroupMarkedForRemoval {
						continue
					}

					return processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating)
				}

				return nil
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).MustPassRepeatedly(5).ShouldNot(BeNil())

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Make sure the cluster is upgraded
			fdbCluster.VerifyVersion(targetVersion)

			// Make sure the other processes are updated to the new image and the operator is able to proceed with the upgrade.
			// We allow soft reconciliation here since the terminating Pod will block the "full" reconciliation
			Expect(fdbCluster.WaitForReconciliation(fixtures.SoftReconcileOption(true))).NotTo(HaveOccurred())

			// Make sure we remove the finalizer to not block the clean up.
			factory.SetFinalizerForPod(&podMarkedForRemoval, []string{})

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with a pending pod",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, true)
			pendingPod := factory.RandomPickOnePod(fdbCluster.GetPods().Items)
			// Set the pod in pending state.
			Expect(fdbCluster.SetPodAsUnschedulable(pendingPod)).NotTo(HaveOccurred())
			fdbCluster.UpgradeAndVerify(targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with a pending pod"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"one process is under maintenance",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			clusterSetup(beforeVersion, true)

			// Pick a storage process and set it under maintenance
			var storageProcessGroupUnderMaintenance *fdbv1beta2.ProcessGroupStatus
			for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				storageProcessGroupUnderMaintenance = processGroup
				break
			}

			Expect(storageProcessGroupUnderMaintenance).NotTo(BeNil())
			Expect(storageProcessGroupUnderMaintenance.FaultDomain).NotTo(BeEmpty())
			log.Println("picked process group", storageProcessGroupUnderMaintenance.ProcessGroupID, "to be under maintenance with fault domain:", storageProcessGroupUnderMaintenance.FaultDomain)
			_, _ = fdbCluster.RunFdbCliCommandInOperator(fmt.Sprintf("maintenance on %s 3600", storageProcessGroupUnderMaintenance.FaultDomain), false, 30)

			// Make sure the machine-readable status reflects the maintenance mode
			Eventually(func() fdbv1beta2.FaultDomain {
				return fdbCluster.GetStatus().Cluster.MaintenanceZone
			}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).MustPassRepeatedly(5).Should(Equal(storageProcessGroupUnderMaintenance.FaultDomain))

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Make sure the cluster is not upgraded until the maintenance is removed.
			Consistently(func() string {
				return fdbCluster.GetCluster().GetRunningVersion()
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Equal(beforeVersion))
			// Turn maintenance off.
			_, _ = fdbCluster.RunFdbCliCommandInOperator("maintenance off", false, 30)

			// Make sure the cluster is upgraded
			fdbCluster.VerifyVersion(targetVersion)

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"one process cannot connect to the Kubernetes API",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("Chaos tests are skipped for the operator")
			}

			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			// If we are not using the unified image, we can skip this test.
			if !factory.UseUnifiedImage() {
				Skip("The sidecar image doesn't require connectivity to the Kubernetes API")
			}

			clusterSetup(beforeVersion, true)

			selectedPod := factory.RandomPickOnePod(fdbCluster.GetStoragePods().Items)

			var kubernetesServiceHost string
			Eventually(func(g Gomega) error {
				std, _, err := factory.ExecuteCmdOnPod(
					&selectedPod,
					fdbv1beta2.MainContainerName,
					"printenv KUBERNETES_SERVICE_HOST",
					false,
				)

				g.Expect(std).NotTo(BeEmpty())
				kubernetesServiceHost = strings.TrimSpace(std)

				return err
			}, 5*time.Minute).ShouldNot(HaveOccurred())

			exp := factory.InjectPartitionWithExternalTargets(fixtures.PodSelector(&selectedPod), []string{kubernetesServiceHost})
			// Make sure that the partition takes effect.
			Eventually(func() error {
				_, _, err := factory.ExecuteCmdOnPod(
					&selectedPod,
					fdbv1beta2.MainContainerName,
					fmt.Sprintf("nc -vz -w 2 %s 443", kubernetesServiceHost),
					false,
				)

				return err
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(HaveOccurred())

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// If the upgrade is version incompatible it will block the upgrade process.
				// Otherwise, the operator will recreate the Pods.
				Consistently(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == beforeVersion
				}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
			}

			factory.DeleteChaosMeshExperimentSafe(exp)

			// Make sure the cluster is upgraded
			fdbCluster.VerifyVersion(targetVersion)

			// Make sure the cluster has no data loss.
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)
})
