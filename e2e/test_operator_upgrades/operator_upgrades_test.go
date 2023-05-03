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
The executed tests include a base test without any chaos/faults.
Each test will create a new FoundationDB cluster which will be upgraded.
Since FoundationDB is version incompatible for major and minor versions and the upgrade process for FoundationDB on Kubernetes requires multiple steps (see the documentation in the docs folder) we test different scenarios where only some processes are restarted.
*/

import (
	"fmt"
	"log"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
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

func clusterSetup(beforeVersion string, availabilityCheck bool) {
	factory.SetBeforeVersion(beforeVersion)
	fdbCluster = factory.CreateFdbCluster(
		&fixtures.ClusterConfig{
			DebugSymbols: false,
		},
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

// Checks if cluster is running at the expectedVersion. This is done by checking the status of the FoundationDBCluster status.
// Before that we checked the cluster status json by checking the reported version of all processes. This approach only worked for
// version compatible upgrades, since incompatible processes won't be part of the cluster anyway. To simplify the check
// we verify the reported running version from the operator.
func verifyVersion(cluster *fixtures.FdbCluster, expectedVersion string) {
	Eventually(func() string {
		return cluster.GetCluster().Status.RunningVersion
	}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(Equal(expectedVersion))
}

func upgradeAndVerify(cluster *fixtures.FdbCluster, expectedVersion string) {
	startTime := time.Now()
	Expect(cluster.UpgradeCluster(expectedVersion, true)).NotTo(HaveOccurred())
	verifyVersion(cluster, expectedVersion)

	log.Println("Upgrade took:", time.Since(startTime).String())
}

var _ = Describe("Operator Upgrades", Label("e2e"), func() {
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
		"upgrading a cluster without chaos",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, true)
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// Ensure that the operator is setting the IncorrectConfigMap and IncorrectCommandLine conditions during the upgrade
				// process.
				expectedConditions := map[fdbv1beta2.ProcessGroupConditionType]bool{
					fdbv1beta2.IncorrectConfigMap:   true,
					fdbv1beta2.IncorrectCommandLine: true,
				}
				Eventually(func() bool {
					cluster := fdbCluster.GetCluster()

					for _, processGroup := range cluster.Status.ProcessGroups {
						if !processGroup.MatchesConditions(expectedConditions) {
							return false
						}
					}

					return true
				}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
			}

			// We can call this method again, this will make sure that the test waits until the cluster is upgraded and
			// reconciled.
			upgradeAndVerify(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with a partitioned pod",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("chaos mesh is disabled")
			}

			clusterSetup(beforeVersion, true)
			Expect(fdbCluster.SetAutoReplacements(false, 20*time.Minute)).ToNot(HaveOccurred())
			// Ensure the operator is not skipping the process because it's missing for to long
			Expect(
				fdbCluster.SetIgnoreMissingProcessesSeconds(5 * time.Minute),
			).NotTo(HaveOccurred())

			// 1. Introduce network partition b/w Pod and cluster.
			// Inject chaos only to storage Pods to reduce the risk of a long recovery because a transaction
			// system Pod was partitioned. The test still has the same checks that will be performed.
			// Once we have a better availability check for upgrades we can target all pods again.
			partitionedPod := fixtures.ChooseRandomPod(fdbCluster.GetStoragePods())
			log.Println("Injecting network partition to pod: ", partitionedPod.Name)
			exp := factory.InjectPartitionBetween(
				fixtures.PodSelector(partitionedPod),
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{partitionedPod.Namespace},
						LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
					},
				},
			)

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// 2. Until we remove the partition, cluster should not have
				//   upgraded. Keep checking for 2m.
				//
				// This is only true for version incompatible upgrades.
				Consistently(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == beforeVersion
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
			}

			// 3. Delete the partition, and the upgrade should proceed.
			log.Println("deleting chaos experiment and cluster should upgrade")
			factory.DeleteChaosMeshExperimentSafe(exp)
			verifyVersion(fdbCluster, targetVersion)
		},

		EntryDescription("Upgrade from %[1]s to %[2]s with partitioned Pod"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with a partitioned pod which eventually gets replaced",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("chaos mesh is disabled")
			}

			clusterSetup(beforeVersion, true)
			Expect(fdbCluster.SetAutoReplacements(false, 5*time.Minute)).ToNot(HaveOccurred())
			// Ensure the operator is not skipping the process because it's missing for to long
			Expect(
				fdbCluster.SetIgnoreMissingProcessesSeconds(5 * time.Minute),
			).NotTo(HaveOccurred())

			// 1. Introduce network partition b/w Pod and cluster.
			partitionedPod := fixtures.ChooseRandomPod(fdbCluster.GetStoragePods())
			log.Println("Injecting network partition to pod: ", partitionedPod.Name)
			_ = factory.InjectPartitionBetween(
				fixtures.PodSelector(partitionedPod),
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{partitionedPod.Namespace},
						LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
					},
				},
			)

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// 2. Until we remove the partition, cluster should not have upgraded. Keep checking for 2m.
				//
				// This is only true for version incompatible upgrades.
				Consistently(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == beforeVersion
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
			} else {
				// Simulate the 2 minute wait otherwise the auto replacement might not succeed in time.
				time.Sleep(2 * time.Minute)
			}

			// Enable the auto replacement feature again, but don't wait for reconciliation, otherwise we wait
			// until the whole cluster is upgraded.
			Expect(
				fdbCluster.SetAutoReplacementsWithWait(true, 3*time.Minute, false),
			).ToNot(HaveOccurred())

			log.Println("waiting for pod removal:", partitionedPod.Name)
			Expect(fdbCluster.WaitForPodRemoval(partitionedPod)).ShouldNot(HaveOccurred())
			log.Println("pod removed:", partitionedPod.Name)

			// 3. Upgrade should proceed without removing the partition, as
			//  Pod will be replaced.
			verifyVersion(fdbCluster, targetVersion)
		},

		EntryDescription(
			"Upgrade from %[1]s to %[2]s with partitioned Pod which eventually gets replaced",
		),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with a random Pod deleted during rolling bounce phase",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, true)
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

				selectedPod := fixtures.RandomPickOnePod(pods)
				log.Println("deleting pod: ", selectedPod.Name)
				factory.DeletePod(&selectedPod)
				Expect(fdbCluster.WaitForPodRemoval(&selectedPod)).ShouldNot(HaveOccurred())
				return false
			}).WithTimeout(20 * time.Minute).WithPolling(3 * time.Minute).Should(BeTrue())

			// 5. Verify a final time that the cluster is reconciled, this should be quick.
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		},

		EntryDescription("Upgrade from %[1]s to %[2]s with pods deleted during rolling bounce"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster where one coordinator gets restarted during the staging phase",
		func(beforeVersion string, targetVersion string) {
			// We set the before version here to overwrite the before version from the specific flag
			// the specific flag will be removed in the future.
			isAtLeast := factory.OperatorIsAtLeast(
				"v1.14.0",
			)

			if !isAtLeast {
				Skip("operator doesn't support feature for test case")
			}

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

			// 1. Introduce crashloop into sidecar container to artificially create a partition.
			pickedPod := fixtures.ChooseRandomPod(fdbCluster.GetStoragePods())
			log.Println("Injecting container fault to crash-loop sidecar process: ", pickedPod.Name)

			fdbCluster.SetCrashLoopContainers([]fdbv1beta2.CrashLoopContainerObject{
				{
					ContainerName: fdbv1beta2.SidecarContainerName,
					Targets:       []fdbv1beta2.ProcessGroupID{fdbv1beta2.ProcessGroupID(pickedPod.Labels[fdbv1beta2.FDBProcessGroupIDLabel])},
				},
			}, false)
			log.Println("Crash injected in pod: ", pickedPod.Name)

			// Wait until sidecar is crash-looping.
			Eventually(func() bool {
				pod := fdbCluster.GetPod(pickedPod.Name)
				for _, container := range pod.Spec.Containers {
					log.Printf("Container: %s, Args: %s", container.Name, container.Args)
					if container.Name == fdbv1beta2.SidecarContainerName {
						return container.Args[0] == "crash-loop"
					}
				}

				return false
			}).WithPolling(2 * time.Second).WithTimeout(2 * time.Minute).Should(BeTrue())

			// 2. Start cluster upgrade.
			log.Printf("Crash injected in sidecar container %s. Starting upgrade.", pickedPod.Name)
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// 3. Until we remove the partition, cluster should not have
				//   upgraded. Keep checking for 4m. The desired behavior is that binaries
				//   are staged but cluster is not bounced to new version.
				log.Println("upgrade should not finish while sidecar process is unavailable")
				Consistently(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == beforeVersion
				}).WithTimeout(4 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
			} else {
				// It should upgrade the cluster if the version is protocol compatible
				Eventually(func() bool {
					return fdbCluster.GetCluster().Status.RunningVersion == targetVersion
				}).WithTimeout(15 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
			}

			// 4. Remove the crash-loop.
			log.Println("Removing crash-loop from ", pickedPod.Name)
			fdbCluster.SetCrashLoopContainers(nil, false)

			// 5. upgrade should proceed after we stop killing sidecar.
			verifyVersion(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with crash-looping sidecar"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and one coordinator is not restarted",
		func(beforeVersion string, targetVersion string) {
			// We set the before version here to overwrite the before version from the specific flag
			// the specific flag will be removed in the future.
			isAtLeast := factory.OperatorIsAtLeast(
				"v1.14.0",
			)

			if !isAtLeast {
				Skip("operator doesn't support feature for test case")
			}

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

			status := fdbCluster.GetStatus()
			Expect(len(status.Cluster.IncompatibleConnections)).To(Equal(0))
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with one coordinator not being restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and multiple processes are not restarted",
		func(beforeVersion string, targetVersion string) {
			// We set the before version here to overwrite the before version from the specific flag
			// the specific flag will be removed in the future.
			isAtLeast := factory.OperatorIsAtLeast(
				"v1.14.0",
			)

			if !isAtLeast {
				Skip("operator doesn't support feature for test case")
			}

			// We ignore the availability check here since this check is sometimes flaky if not all coordinators are running.
			clusterSetup(beforeVersion, false)

			// 1. Select half of the stateless and half of the log processes and use the buggify option to skip those
			// processes during the restart command.
			statelessPods := fdbCluster.GetStatelessPods()
			Expect(statelessPods.Items).NotTo(BeEmpty())
			selectedStatelessPods := fixtures.RandomPickPod(
				statelessPods.Items,
				len(statelessPods.Items)/2,
			)

			logPods := fdbCluster.GetLogPods()
			Expect(logPods.Items).NotTo(BeEmpty())
			selectedLogPods := fixtures.RandomPickPod(logPods.Items, len(logPods.Items)/2)

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
		},
		EntryDescription("Upgrade from %[1]s to %[2]s and multiple processes are not restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with link that drops some packets",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("chaos mesh is disabled")
			}

			clusterSetup(beforeVersion, true)

			// 1. Introduce packet loss b/w pods.
			log.Println("Injecting packet loss b/w pod")
			allPods := fdbCluster.GetAllPods()

			pickedPods := fixtures.RandomPickPod(allPods.Items, len(allPods.Items)/5)
			Expect(pickedPods).ToNot(BeEmpty())
			for _, pod := range pickedPods {
				log.Println("Picked Pod", pod.Name)
			}

			factory.InjectNetworkLoss("20", fixtures.PodsSelector(pickedPods),
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{fdbCluster.Namespace()},
						LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
					},
				}, chaosmesh.Both)

			// Also inject network loss b/w operator pods and cluster pods.
			operatorPods := factory.GetOperatorPods(fdbCluster.Namespace())
			factory.InjectNetworkLoss(
				"20",
				fixtures.PodsSelector(allPods.Items),
				fixtures.PodsSelector(operatorPods.Items),
				chaosmesh.Both)

			// 2. Start cluster upgrade.
			log.Println("Starting cluster upgrade.")
			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())

			// 3. Upgrade should finish.
			verifyVersion(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with network link that drops some packets"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and no coordinator is restarted",
		func(beforeVersion string, targetVersion string) {
			// We set the before version here to overwrite the before version from the specific flag
			// the specific flag will be removed in the future.
			isAtLeast := factory.OperatorIsAtLeast(
				"v1.14.0",
			)

			if !isAtLeast {
				Skip("operator doesn't support feature for test case")
			}

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
		"upgrading a cluster and one process has the fdbmonitor.conf file not ready",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			clusterSetup(beforeVersion, true)

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Skip the reonciliation here to have time to stage everything.
			Expect(fdbCluster.SetSkipReconciliation(true)).NotTo(HaveOccurred())

			// Select one Pod, this Pod will mount the fdbmonitor config file as read-only.
			// This should block the upgrade.
			faultyPod := fixtures.RandomPickOnePod(fdbCluster.GetPods().Items)

			// We have to update the sidecar before the operator is doing it. If we don't do this here the operator
			// will update the sidecar and then the I/O chaos will be gone. So we prepare the faulty Pod to already
			// be using the new sidecar image and then we inject IO chaos.
			sidecarImage := fdbCluster.GetSidecarImageForVersion(targetVersion)
			fdbCluster.UpdateContainerImage(&faultyPod, fdbv1beta2.SidecarContainerName, sidecarImage)

			Eventually(func() bool {
				pod := fdbCluster.GetPod(faultyPod.Name)

				for _, status := range pod.Status.ContainerStatuses {
					if status.Name != fdbv1beta2.SidecarContainerName {
						continue
					}

					log.Println("expected", sidecarImage, "got", status.Image)
					return status.Image == sidecarImage
				}

				return false
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(5).Should(BeTrue())

			log.Println("Inject IO chaos to", faultyPod.Name)
			// Ensure that the fdbmonitor config file is not writeable for the sidecar.
			exp := factory.InjectDiskFailureWithPath(
				fixtures.PodSelector(&faultyPod),
				"/var/output-files",
				"/var/output-files/fdbmonitor.conf",
				[]chaosmesh.IoMethod{
					chaosmesh.Write,
					chaosmesh.Read,
					chaosmesh.Open,
					chaosmesh.Flush,
					chaosmesh.Fsync,
				},
				[]string{
					fdbv1beta2.SidecarContainerName,
				})

			// Make sure the sidecar is not able to write the fdbmonitor config.
			Eventually(func() error {
				stdout, stderr, err := fdbCluster.ExecuteCmdOnPod(
					faultyPod,
					fdbv1beta2.SidecarContainerName,
					"cat /var/output-files/fdbmonitor.conf && echo '\n' >> /var/output-files/fdbmonitor.conf",
					false)

				log.Println(stdout, stderr)

				return err
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(HaveOccurred())

			// Now we have the faulty Pod prepared with I/O chaos injected, so we can continue with the upgrade.
			Expect(fdbCluster.SetSkipReconciliation(false)).NotTo(HaveOccurred())

			// The cluster will be stuck in this state until the I/O chaos is resolved.
			expectedConditions := map[fdbv1beta2.ProcessGroupConditionType]bool{
				fdbv1beta2.IncorrectConfigMap:   true,
				fdbv1beta2.IncorrectCommandLine: true,
			}
			faultyProcessGroupID := fixtures.GetProcessGroupID(faultyPod)

			// The upgrade will be stuck until the I/O chaos is removed and the sidecar is able to provide the latest
			// fdbmonitor conf file.
			Eventually(func() bool {
				cluster := fdbCluster.GetCluster()
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID != faultyProcessGroupID {
						continue
					}

					for _, condition := range processGroup.ProcessGroupConditions {
						log.Println(processGroup.ProcessGroupID, string(condition.ProcessGroupConditionType))
					}

					if !processGroup.MatchesConditions(expectedConditions) {
						return false
					}
				}

				return true
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).MustPassRepeatedly(30).Should(BeTrue())

			// Make sure the cluster was not upgraded yet.
			cluster := fdbCluster.GetCluster()
			Expect(cluster.Spec.Version).NotTo(Equal(cluster.Status.RunningVersion))

			// Remove the IO chaos, the cluster should proceed.
			factory.DeleteChaosMeshExperimentSafe(exp)

			// Ensure the upgrade proceeds and is able to finish.
			verifyVersion(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s and one process has the fdbmonitor.conf file not ready"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and one process is missing the new binary",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			clusterSetup(beforeVersion, true)

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Skip the reonciliation here to have time to stage everything.
			Expect(fdbCluster.SetSkipReconciliation(true)).NotTo(HaveOccurred())

			// Select one Pod, this Pod will miss the new fdbserver binary.
			// This should block the upgrade.
			faultyPod := fixtures.RandomPickOnePod(fdbCluster.GetPods().Items)

			// We have to update the sidecar before the operator is doing it. If we don't do this here the operator
			// will update the sidecar and then the sidecar will copy the binaries at start-up. So we prepare the faulty Pod to already
			// be using the new sidecar image and then we delete he new fdbserver binary.
			sidecarImage := fdbCluster.GetSidecarImageForVersion(targetVersion)
			fdbCluster.UpdateContainerImage(&faultyPod, fdbv1beta2.SidecarContainerName, sidecarImage)

			Eventually(func() bool {
				pod := fdbCluster.GetPod(faultyPod.Name)

				for _, status := range pod.Status.ContainerStatuses {
					if status.Name != fdbv1beta2.SidecarContainerName {
						continue
					}

					log.Println("expected", sidecarImage, "got", status.Image)
					return status.Image == sidecarImage
				}

				return false
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(5).Should(BeTrue())

			// Ensure that the new fdbserver binary is deleted by the sidecar.
			fdbserverBinary := fmt.Sprintf("/var/output-files/bin/%s/fdbserver", targetVersion)
			log.Println("Delete", fdbserverBinary, "from", faultyPod.Name)

			// Make sure the sidecar is missing the fdbserver binary.
			Eventually(func() error {
				_, _, err := fdbCluster.ExecuteCmdOnPod(
					faultyPod,
					fdbv1beta2.SidecarContainerName,
					fmt.Sprintf("rm -f %s", fdbserverBinary),
					false)

				return err
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveOccurred())

			// Now we have the faulty Pod prepared, so we can continue with the upgrade.
			Expect(fdbCluster.SetSkipReconciliation(false)).NotTo(HaveOccurred())

			// The cluster will be stuck in this state until the Pod is restarted and the new binary is present.
			expectedConditions := map[fdbv1beta2.ProcessGroupConditionType]bool{
				fdbv1beta2.IncorrectConfigMap:   true,
				fdbv1beta2.IncorrectCommandLine: true,
			}
			faultyProcessGroupID := fixtures.GetProcessGroupID(faultyPod)

			// The upgrade will be stuck until the new fdbserver binary is copied to the shared directory again.
			Eventually(func() bool {
				cluster := fdbCluster.GetCluster()
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID != faultyProcessGroupID {
						continue
					}

					for _, condition := range processGroup.ProcessGroupConditions {
						log.Println(processGroup.ProcessGroupID, string(condition.ProcessGroupConditionType))
					}

					if !processGroup.MatchesConditions(expectedConditions) {
						return false
					}
				}

				return true
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).MustPassRepeatedly(30).Should(BeTrue())

			// Make sure the cluster was not upgraded yet.
			cluster := fdbCluster.GetCluster()
			Expect(cluster.Spec.Version).NotTo(Equal(cluster.Status.RunningVersion))

			// Ensure the binary is present in the shared folder.
			Eventually(func() error {
				_, _, err := fdbCluster.ExecuteCmdOnPod(
					faultyPod,
					fdbv1beta2.SidecarContainerName,
					fmt.Sprintf("cp -f /usr/bin/fdbserver %s", fdbserverBinary),
					false)

				return err
			}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveOccurred())

			// Ensure the upgrade proceeds and is able to finish.
			verifyVersion(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s and one process is missing the new binary"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

})
