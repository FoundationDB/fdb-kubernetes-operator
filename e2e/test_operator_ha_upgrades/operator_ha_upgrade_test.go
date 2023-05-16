/*
 * operator_ha_upgrades_test.go
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

package operatorhaupgrades

/*
This test suite includes tests to validate the behaviour of the operator during upgrades on a HA FoundationDB cluster.
The executed tests include a base test without any chaos/faults.
Each test will create a new HA FoundationDB cluster which will be upgraded.
*/

import (
	"log"
	"math/rand"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
)

func init() {
	testOptions = fixtures.InitFlags()
}

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.HaFdbCluster
	testOptions *fixtures.FactoryOptions
)

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
})

func clusterSetup(beforeVersion string, enableOperatorPodChaos bool) {
	// We set the before version here to overwrite the before version from the specific flag
	// the specific flag will be removed in the future.
	factory.SetBeforeVersion(beforeVersion)
	startTime := time.Now()
	fdbCluster = factory.CreateFdbHaCluster(
		fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false),
		factory.GetClusterOptions(fixtures.UseVersionBeforeUpgrade)...,
	)
	Expect(
		fdbCluster.GetPrimary().InvariantClusterStatusAvailableWithThreshold(15 * time.Second),
	).ShouldNot(HaveOccurred())
	log.Println(
		"FoundationDB HA cluster created (at version",
		beforeVersion,
		") in minutes",
		time.Since(startTime).Minutes(),
	)

	if enableOperatorPodChaos && factory.ChaosTestsEnabled() {
		for _, curCluster := range fdbCluster.GetAllClusters() {
			factory.ScheduleInjectPodKill(
				fixtures.GetOperatorSelector(curCluster.Namespace()),
				"*/5 * * * *",
				chaosmesh.OneMode,
			)
		}
	}
}

// Checks if cluster is running at the expectedVersion. This is done by checking the status of the FoundationDBCluster status.
// Before that we checked the cluster status json by checking the reported version of all processes. This approach only worked for
// version compatible upgrades, since incompatible processes won't be part of the cluster anyway. To simplify the check
// we verify the reported running version from the operator.
func checkVersion(cluster *fixtures.HaFdbCluster, expectedVersion string) {
	Eventually(func() bool {
		for _, singleCluster := range cluster.GetAllClusters() {
			if singleCluster.GetCluster().Status.RunningVersion != expectedVersion {
				return false
			}
		}

		return true
	}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
}

func upgradeAndVerify(cluster *fixtures.HaFdbCluster, expectedVersion string) {
	// Upgrade the cluster and then verify that the processes have upgraded to "targetVersion".
	startTime := time.Now()
	Expect(cluster.UpgradeCluster(expectedVersion, true)).NotTo(HaveOccurred())
	checkVersion(cluster, expectedVersion)
	log.Println(
		"Multi-DC cluster upgraded to version",
		expectedVersion,
		"in minutes",
		time.Since(startTime).Minutes(),
	)
}

// Verify that bouncing is blocked in primary/remote/primary-satellite clusters.
// We do this by verifying that the processes in primary/remote/primary-satellite clusters
// are at "IncorrectCommandLine" state (which means that they have the new binaries and have
// the new configuration but are not restarted).
//
// Note: we tried doing this by polling for  "UpgradeRequeued" event on primary/remote/primary-satellite data centers,
// but we found that that approach is not very reliable.
func verifyBouncingIsBlocked() {
	Eventually(func() bool {
		for _, cluster := range fdbCluster.GetAllClusters() {
			if strings.HasSuffix(cluster.Name(), fixtures.RemoteSatelliteID) {
				continue
			}

			if !cluster.AllProcessGroupsHaveCondition(fdbv1beta2.IncorrectCommandLine) {
				return false
			}
		}

		return true
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(30).Should(BeTrue())
}

var _ = Describe("Operator HA Upgrades", Label("e2e"), func() {
	BeforeEach(func() {
		factory = fixtures.CreateFactory(testOptions)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			fdbCluster.DumpState()
		}
		factory.Shutdown()
	})

	// Ginkgo lacks the support for AfterEach and BeforeEach in tables, so we have to put everything inside the testing function
	// this setup allows to dynamically generate the table entries that will be executed e.g. to test different upgrades
	// for different versions without hard coding or having multiple flags.
	PDescribeTable(
		"Upgrading a multi-DC cluster without chaos",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, false)
			upgradeAndVerify(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade, without chaos, from %s to %s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with operator pod chaos and without foundationdb pod chaos",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, true /* = enableOperatorPodChaos */)
			initialGeneration := fdbCluster.GetPrimary().GetStatus().Cluster.Generation
			upgradeAndVerify(fdbCluster, targetVersion)
			// Verify that the cluster generation number didn't increase by more
			// than 40 (in an ideal case the number of recoveries that should happen
			// during an upgrade is 9, but in reality that number could be higher
			// because different server processes may get bounced at different times).
			Expect(fdbCluster.GetPrimary().GetStatus().Cluster.Generation).To(BeNumerically("<=", initialGeneration+40))

		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and one dc is upgraded before the other dcs started the upgrade",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("skipping test since those versions are protocol compatible")
			}

			clusterSetup(beforeVersion, true /* = enableOperatorPodChaos */)

			// Upgrade the primary cluster before upgrading the rest.
			primary := fdbCluster.GetPrimary()
			Expect(primary.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Should update all sidecars of the primary cluster.
			Eventually(func() bool {
				pods := primary.GetPods()
				if pods == nil {
					return false
				}

				for _, pod := range pods.Items {
					for _, con := range pod.Spec.Containers {
						if con.Name != fdbv1beta2.SidecarContainerName {
							continue
						}

						if !strings.Contains(con.Image, targetVersion) {
							return false
						}
					}
				}

				return true
			}).Should(BeTrue())

			// It should block the restart of the processes until the rest of the cluster is upgraded.
			Eventually(func() bool {
				return primary.AllProcessGroupsHaveCondition(fdbv1beta2.IncorrectCommandLine)
			}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(30).Should(BeTrue())

			// Verify that the upgrade proceeds
			upgradeAndVerify(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with one DC upgraded before the rest"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"Upgrading a multi-DC cluster, with a temporary partition",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, false /* = enableOperatorPodChaos */)

			// Find the remote satellite operator pods.
			operatorPods := factory.GetOperatorPods(
				fdbCluster.GetRemoteSatellite().Namespace(),
			)

			// Disable the auto replacement to prevent the operator te replace the partitioned processes, the default
			// replace time in the operator is 2h and in our test suite 5 minutes.
			Expect(
				fdbCluster.GetRemoteSatellite().SetAutoReplacements(false, 20*time.Minute),
			).NotTo(HaveOccurred())

			// Partition them from the rest of the database.
			log.Println("Partitioning the remote satellite operator pods")
			partitionExperiment := factory.InjectPartitionBetween(
				fdbCluster.GetNamespaceSelector(),
				fixtures.PodsSelector(operatorPods.Items),
			)

			// Start the upgrade, but do not wait for reconciliation to complete.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				// Verify that bouncing is blocked because the Pods in the remote satellite
				// data center are not ready for upgrade. We do this by verifying that the
				// process groups in all other data centers are at "IncorrectCommandLine"
				// state.
				log.Println("Ensure bouncing is blocked")
				verifyBouncingIsBlocked()

				// Verify that the processes have not upgraded to "targetVersion".
				checkVersion(fdbCluster, beforeVersion)
			} else {
				// If we do a version compatible upgrade, ensure the partition is present for 2 minutes.
				time.Sleep(2 * time.Minute)
			}

			// Restore connectivity.
			log.Println("Restoring connectivity")
			factory.DeleteChaosMeshExperimentSafe(partitionExperiment)

			// Delete the operator Pods to ensure they pickup the work directly otherwise it could take a long time
			// until the operator tries to reconcile the cluster again. If the operator is not able to reconcile a
			// cluster it will be put into a queue again, at some time the queue will delay the next reconcile attempt
			// for a long time and since the network partition is not emitting any events for the operator this won't trigger
			// a reconciliation either. So this step is only to speed up the reconcile process.
			factory.RecreateOperatorPods(fdbCluster.GetRemoteSatellite().Namespace())

			// Upgrade should make progress now - wait until all processes have upgraded
			// to "targetVersion".
			checkVersion(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade, with a temporary partition, from %s to %s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"Upgrading a multi-DC cluster, with a random pod deleted during the staging phase",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, false /* = enableOperatorPodChaos */)

			// Start the upgrade, but do not wait for reconciliation to complete.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Keep deleting pods until all clusters are running with the new version.
			clusters := fdbCluster.GetAllClusters()
			Eventually(func() bool {
				coordinatorMap := map[k8sTypes.UID]corev1.Pod{}

				// Are all clusters running at "targetVersion"?
				clustersAtTargetVersion := true
				for _, cluster := range clusters {
					coordinators := cluster.GetCoordinators()
					for _, pod := range coordinators {
						coordinatorMap[pod.UID] = pod
					}
					dbCluster := cluster.GetCluster()

					if dbCluster.Status.RunningVersion == targetVersion {
						log.Println(
							"Cluster ",
							cluster.Name(),
							"is running at version ",
							targetVersion,
						)
						continue
					}

					clustersAtTargetVersion = false
					break
				}

				if clustersAtTargetVersion == true {
					return true
				}

				randomCluster := clusters[rand.Intn(len(clusters))]
				// Make sure we are not deleting coordinator Pods
				var randomPod *corev1.Pod
				Eventually(func() bool {
					randomPod = fixtures.ChooseRandomPod(randomCluster.GetPods())
					_, ok := coordinatorMap[randomPod.UID]
					if ok {
						log.Println("Skipping pod: ", randomPod.Name, "as it is a coordinator")
					}

					return ok
				}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeFalse())

				log.Println("Deleting pod: ", randomPod.Name)
				factory.DeletePod(randomPod)
				return false
			}).WithTimeout(30 * time.Minute).WithPolling(2 * time.Minute).Should(BeTrue())
		},
		EntryDescription(
			"Upgrade, with a random pod deleted during the staging phase, from %s to %s",
		),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a HA cluster with link that drops some packets",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("chaos mesh is disabled")
			}

			clusterSetup(beforeVersion, false)

			// 1. Introduce packet loss b/w pods.
			log.Println("Injecting packet loss b/w pod")
			primaryPods := fdbCluster.GetPrimary().GetAllPods()
			primarySatellitePods := fdbCluster.GetPrimarySatellite().GetAllPods()
			remoteSatellitePods := fdbCluster.GetRemoteSatellite().GetAllPods()
			remotePods := fdbCluster.GetRemote().GetAllPods()
			operatorPrimaryPods := factory.GetOperatorPods(fdbCluster.GetPrimary().Namespace())
			operatorRemotePods := factory.GetOperatorPods(fdbCluster.GetRemote().Namespace())
			operatorPrimarySatellitePods := factory.GetOperatorPods(
				fdbCluster.GetPrimarySatellite().Namespace(),
			)

			factory.InjectNetworkLossBetweenPods([]chaosmesh.PodSelectorSpec{
				fixtures.PodsSelector(primaryPods.Items),
				fixtures.PodsSelector(primarySatellitePods.Items),
				fixtures.PodsSelector(remoteSatellitePods.Items),
				fixtures.PodsSelector(remotePods.Items),
				fixtures.PodsSelector(operatorPrimaryPods.Items),
				fixtures.PodsSelector(operatorRemotePods.Items),
				fixtures.PodsSelector(operatorPrimarySatellitePods.Items),
			}, "20")

			upgradeAndVerify(fdbCluster, targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with network link that drops some packets"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster when no remote storage processes are restarted",
		func(beforeVersion string, targetVersion string) {
			isAtLeast := factory.OperatorIsAtLeast(
				"v1.14.0",
			)

			if !isAtLeast {
				Skip("operator doesn't support feature for test case")
			}

			clusterSetup(beforeVersion, false)

			// Select remote storage processes and use the buggify option to skip those
			// processes during the restart command.
			storagePods := fdbCluster.GetRemote().GetStoragePods()
			Expect(storagePods.Items).NotTo(BeEmpty())

			ignoreDuringRestart := make(
				[]fdbv1beta2.ProcessGroupID,
				0,
				len(storagePods.Items),
			)

			for _, pod := range storagePods.Items {
				ignoreDuringRestart = append(
					ignoreDuringRestart,
					fdbv1beta2.ProcessGroupID(pod.Labels[fdbCluster.GetRemote().GetCachedCluster().GetProcessGroupIDLabel()]),
				)
			}

			log.Println(
				"Selected Pods:",
				ignoreDuringRestart,
				"to be skipped during the restart",
			)
			fdbCluster.GetRemote().SetIgnoreDuringRestart(ignoreDuringRestart)

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())
		},
		EntryDescription("Upgrade from %[1]s to %[2]s when no remote storage processes are restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster when no remote processes are restarted",
		func(beforeVersion string, targetVersion string) {
			isAtLeast := factory.OperatorIsAtLeast(
				"v1.14.0",
			)

			if !isAtLeast {
				Skip("operator doesn't support feature for test case")
			}

			clusterSetup(beforeVersion, false)

			// Select remote processes and use the buggify option to skip those
			// processes during the restart command.
			pods := fdbCluster.GetRemote().GetPods()
			Expect(pods.Items).NotTo(BeEmpty())

			ignoreDuringRestart := make(
				[]fdbv1beta2.ProcessGroupID,
				0,
				len(pods.Items),
			)

			for _, pod := range pods.Items {
				ignoreDuringRestart = append(
					ignoreDuringRestart,
					fdbv1beta2.ProcessGroupID(pod.Labels[fdbCluster.GetRemote().GetCachedCluster().GetProcessGroupIDLabel()]),
				)
			}

			log.Println(
				"Selected Pods:",
				ignoreDuringRestart,
				"to be skipped during the restart",
			)
			for _, cluster := range fdbCluster.GetAllClusters() {
				cluster.SetIgnoreDuringRestart(ignoreDuringRestart)
			}

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())
		},
		EntryDescription("Upgrade from %[1]s to %[2]s when no remote processes are restarted"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

})
