package operatorupgrades

import (
	"flag"
	"log"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"

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

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
})

func clusterSetup(beforeVersion string) {
	factory.SetBeforeVersion(beforeVersion)
	fdbCluster = factory.CreateFdbCluster(
		&fixtures.ClusterConfig{
			DebugSymbols: false,
		},
		factory.GetClusterOptions(fixtures.UseVersionBeforeUpgrade)...,
	)
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
			clusterSetup(beforeVersion)
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

			clusterSetup(beforeVersion)
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

			clusterSetup(beforeVersion)
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
			clusterSetup(beforeVersion)
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

			clusterSetup(beforeVersion)

			// Select one coordinator that will be restarted during the staging phase.
			coordinators := fdbCluster.GetCoordinators()
			Expect(coordinators).NotTo(BeEmpty())

			selectedCoordinator := coordinators[0]
			log.Println(
				"Selected coordinator:",
				selectedCoordinator.Name,
				" to be restarted during the staging phase",
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
			clusterSetup(beforeVersion)

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

			clusterSetup(beforeVersion)

			// 1. Select one coordinator and use the buggify option to skip it during the restart command.
			coordinators := fdbCluster.GetCoordinators()
			Expect(coordinators).NotTo(BeEmpty())

			selectedCoordinator := coordinators[0]
			log.Println(
				"Selected coordinator:",
				selectedCoordinator.Name,
				" to be skipped during the restart",
			)
			fdbCluster.SetIgnoreDuringRestart(
				[]fdbv1beta2.ProcessGroupID{
					fdbv1beta2.ProcessGroupID(selectedCoordinator.Labels[fdbCluster.GetCachedCluster().GetProcessGroupIDLabel()]),
				},
			)

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())
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

			clusterSetup(beforeVersion)

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
		EntryDescription("Upgrade from %[1]s to %[2]s with one coordinator not being restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with link that drops some packets",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("chaos mesh is disabled")
			}

			clusterSetup(beforeVersion)

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

			clusterSetup(beforeVersion)

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
		EntryDescription("Upgrade from %[1]s to %[2]s with one coordinator not being restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

})
