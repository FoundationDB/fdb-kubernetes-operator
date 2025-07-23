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
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func init() {
	testOptions = fixtures.InitFlags()
}

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.HaFdbCluster
	testOptions *fixtures.FactoryOptions
)

type testConfig struct {
	beforeVersion          string
	enableOperatorPodChaos bool
	enableHealthCheck      bool
	loadData               bool
	clusterConfig          *fixtures.ClusterConfig
}

func clusterSetupWithTestConfig(config testConfig) {
	// We set the before version here to overwrite the before version from the specific flag
	// the specific flag will be removed in the future.
	factory.SetBeforeVersion(config.beforeVersion)

	if config.clusterConfig == nil {
		config.clusterConfig = fixtures.DefaultClusterConfigWithHaMode(
			fixtures.HaFourZoneSingleSat,
			false,
		)
	}

	fdbCluster = factory.CreateFdbHaCluster(
		config.clusterConfig,
		factory.GetClusterOptions(fixtures.UseVersionBeforeUpgrade)...,
	)

	if config.enableHealthCheck {
		Expect(
			fdbCluster.GetPrimary().InvariantClusterStatusAvailable(),
		).ShouldNot(HaveOccurred())
	}

	if config.loadData {
		// Load some data async into the cluster. We will only block as long as the Job is created.
		factory.CreateDataLoaderIfAbsent(fdbCluster.GetPrimary())
	}

	if config.enableOperatorPodChaos && factory.ChaosTestsEnabled() {
		for _, curCluster := range fdbCluster.GetAllClusters() {
			factory.ScheduleInjectPodKill(
				fixtures.GetOperatorSelector(curCluster.Namespace()),
				"*/5 * * * *",
				chaosmesh.OneMode,
			)
		}
	}
}

func clusterSetup(beforeVersion string, enableOperatorPodChaos bool) {
	clusterSetupWithTestConfig(
		testConfig{
			beforeVersion:          beforeVersion,
			enableOperatorPodChaos: enableOperatorPodChaos,
			enableHealthCheck:      true,
			loadData:               false,
		},
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
	Eventually(func(g Gomega) bool {
		for _, cluster := range fdbCluster.GetAllClusters() {
			if strings.HasSuffix(cluster.Name(), fixtures.RemoteSatelliteID) {
				continue
			}

			g.Expect(cluster.AllProcessGroupsHaveCondition(fdbv1beta2.IncorrectCommandLine)).
				To(BeTrue())
		}

		return true
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).MustPassRepeatedly(30).Should(BeTrue())
}

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
})

var _ = Describe("Operator HA Upgrades", Label("e2e", "pr"), func() {
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
		"without chaos",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, false)

			Expect(fdbCluster.UpgradeCluster(targetVersion, true)).NotTo(HaveOccurred())
			fdbCluster.VerifyVersion(targetVersion)
		},
		EntryDescription("Upgrade from %s to %s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with operator pod chaos and without foundationdb pod chaos",
		func(beforeVersion string, targetVersion string) {
			clusterSetupWithTestConfig(testConfig{
				beforeVersion:          beforeVersion,
				enableOperatorPodChaos: true,
				enableHealthCheck:      true,
				loadData:               true,
			})
			initialGeneration := fdbCluster.GetPrimary().GetStatus().Cluster.Generation
			// Make use of a sync.Map here as we have to modify it concurrently.
			var transactionSystemProcessGroups sync.Map
			// Fetch all initial process groups before starting the upgrade.
			for _, cluster := range fdbCluster.GetAllClusters() {
				processGroups := cluster.GetCluster().Status.ProcessGroups
				for _, processGroup := range processGroups {
					if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
						continue
					}

					transactionSystemProcessGroups.Store(
						processGroup.ProcessGroupID,
						fdbv1beta2.None{},
					)
				}
			}

			startTime := time.Now()
			// Start the upgrade for the whole cluster
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Wait until all clusters are reconciled and collect the process groups during that time.
			g := new(errgroup.Group)
			for _, fdbCluster := range fdbCluster.GetAllClusters() {
				targetCluster := fdbCluster // https://golang.org/doc/faq#closures_and_goroutines

				g.Go(func() error {
					err := targetCluster.WaitUntilWithForceReconcile(
						2,
						1200,
						func(cluster *fdbv1beta2.FoundationDBCluster) bool {
							for _, processGroup := range cluster.Status.ProcessGroups {
								if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
									continue
								}

								transactionSystemProcessGroups.Store(
									processGroup.ProcessGroupID,
									fdbv1beta2.None{},
								)
							}

							// Allow soft reconciliation and make sure the running version was updated
							return cluster.Status.Generations.Reconciled == cluster.Generation &&
								cluster.Status.RunningVersion == targetVersion
						},
					)

					if err != nil {
						return fmt.Errorf(
							"error during WaitForReconciliation for %s, original error: %w",
							targetCluster.Name(),
							err,
						)
					}

					return err
				})
			}

			Expect(g.Wait()).NotTo(HaveOccurred())

			// Check how many transaction process groups we have seen.
			var expectedProcessCounts int
			for _, cluster := range fdbCluster.GetAllClusters() {
				// Get the desired process counts based on the current cluster configuration.
				processCounts, err := cluster.GetProcessCounts()
				Expect(err).NotTo(HaveOccurred())

				// During an upgrade we expect that the transaction system processes are replaced, so we expect to have seen
				// 2 times the process counts for transaction system processes.
				expectedProcessCounts += (processCounts.Total() - processCounts.Storage) * 2
			}

			// The sync.Map has not length method, so we have to calculate it.
			var processCounts int
			transactionSystemProcessGroups.Range(func(_, _ any) bool {
				processCounts++
				return true
			})

			// Add a small buffer of 5 to allow automatic replacements during an upgrade.
			expectedProcessCounts += 5
			log.Println(
				"expectedProcessCounts",
				expectedProcessCounts,
				"processCounts",
				processCounts,
			)

			// Make sure we haven't replaced to many transaction processes.
			Expect(processCounts).To(BeNumerically("<=", expectedProcessCounts))

			finalGeneration := fdbCluster.GetPrimary().GetStatus().Cluster.Generation
			log.Println(
				"upgrade took:",
				time.Since(startTime).String(),
				"initialGeneration:",
				initialGeneration,
				"finalGeneration",
				finalGeneration,
				"gap",
				finalGeneration-initialGeneration,
				"recoveryCount",
				(finalGeneration-initialGeneration)/2,
			)

			// Verify that the cluster generation number didn't increase by more
			// than 80 (in an ideal case the number of recoveries that should happen
			// during an upgrade, because of bouncing of server processes, is 9, but
			// in reality that number could be higher because different server processes
			// may get bounced at different times and also because of cluster controller
			// change, which happens a number of times, during an upgrade).
			Expect(finalGeneration).To(BeNumerically("<=", initialGeneration+80))
			// Make sure the cluster has no data loss
			fdbCluster.GetPrimary().EnsureTeamTrackersAreHealthy()
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"one dc is upgraded before the other dcs started the upgrade",
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

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Verify that the upgrade proceeds
			fdbCluster.VerifyVersion(targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with a temporary partition",
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
				log.Println("Ensure cluster(s) are not upgraded")
				fdbCluster.VerifyVersion(beforeVersion)
			} else {
				// If we do a version compatible upgrade, ensure the partition is present for 30 seconds.
				time.Sleep(30 * time.Second)
			}

			log.Println("Restoring connectivity")
			factory.DeleteChaosMeshExperimentSafe(partitionExperiment)

			// When using protocol compatible versions, the other operator instances are able to move forward. In some
			// cases it can happen that new coordinators are selected and all the old coordinators are deleted. In this
			// case the remote satellite operator will not be able to connect to the cluster anymore and needs an
			// update to the connection string.
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Eventually(func(g Gomega) {
					currentConnectionString := fdbCluster.GetPrimary().
						GetStatus().
						Cluster.ConnectionString
					remoteSat := fdbCluster.GetRemoteSatellite()
					remoteConnectionString := remoteSat.GetCluster().Status.ConnectionString

					// If the connection string is different we have to update it on the remote satellite side
					// as the operator instances were partitioned.
					if currentConnectionString != remoteConnectionString {
						if !remoteSat.GetCluster().Spec.Skip {
							remoteSat.SetSkipReconciliation(true)
							// Wait one minute, that should be enough time for the operator to end the reconciliation loop
							// if started.
							time.Sleep(1 * time.Minute)
						}

						remoteSatStatus := remoteSat.GetCluster().Status.DeepCopy()
						remoteSatStatus.ConnectionString = currentConnectionString
						fdbCluster.GetRemoteSatellite().
							UpdateClusterStatusWithStatus(remoteSatStatus)
					}

					g.Expect(remoteConnectionString).To(Equal(currentConnectionString))
				}).WithTimeout(5 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())
			}

			// Delete the operator Pods to ensure they pick up the work directly otherwise it could take a long time
			// until the operator tries to reconcile the cluster again. If the operator is not able to reconcile a
			// cluster it will be put into a queue again, at some time the queue will delay the next reconcile attempt
			// for a long time and since the network partition is not emitting any events for the operator this won't trigger
			// a reconciliation either. So this step is only to speed up the reconcile process.
			factory.RecreateOperatorPods(fdbCluster.GetRemoteSatellite().Namespace())

			// Ensure that the remote satellite is not set to skip.
			fdbCluster.GetRemoteSatellite().SetSkipReconciliation(false)

			// Upgrade should make progress now - wait until all processes have upgraded
			// to "targetVersion".
			fdbCluster.VerifyVersion(targetVersion)
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %s to %s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with a random pod deleted during the staging phase",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, false /* = enableOperatorPodChaos */)

			// Start the upgrade, but do not wait for reconciliation to complete.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// Keep deleting pods until all clusters are running with the new version.
			clusters := fdbCluster.GetAllClusters()
			Eventually(func(g Gomega) bool {
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
							"Cluster",
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

				randomCluster := factory.RandomPickOneCluster(clusters)
				// Make sure we are not deleting coordinator Pods
				var randomPod *corev1.Pod
				g.Eventually(func() bool {
					randomPod = factory.ChooseRandomPod(randomCluster.GetPods())
					_, ok := coordinatorMap[randomPod.UID]
					if ok {
						log.Println("Skipping pod:", randomPod.Name, "as it is a coordinator")
					}

					return ok
				}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeFalse())

				log.Println("Deleting pod:", randomPod.Name)
				factory.DeletePod(randomPod)
				return false
			}).WithTimeout(30 * time.Minute).WithPolling(2 * time.Minute).Should(BeTrue())

			// Make sure the cluster has no data loss
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription(
			"Upgrade from %s to %s",
		),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	// TODO(johscheuer): Enable tests again, once they are stable.
	PDescribeTable(
		"with network link that drops some packets",
		func(beforeVersion string, targetVersion string) {
			if !factory.ChaosTestsEnabled() {
				Skip("chaos mesh is disabled")
			}

			clusterSetupWithTestConfig(testConfig{
				beforeVersion:          beforeVersion,
				enableOperatorPodChaos: false,
				enableHealthCheck:      false,
				loadData:               false,
			})

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

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Verify that the upgrade proceeds
			fdbCluster.VerifyVersion(targetVersion)
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	// TODO (johscheuer): Ensure this test passes reliably. Right now the cluster sometimes gets stuck and needs
	// manual intervention.
	// See: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/2196
	PDescribeTable(
		"when no remote storage processes are restarted",
		func(beforeVersion string, targetVersion string) {
			// We disable the health check here, as the remote storage processes are not restarted and this can be
			// disruptive to the cluster.
			clusterSetupWithTestConfig(testConfig{
				beforeVersion:          beforeVersion,
				enableOperatorPodChaos: false,
				enableHealthCheck:      false,
				loadData:               false,
			})

			// Select remote storage processes and use the buggify option to skip those
			// processes during the restart command.
			remoteProcessGroups := fdbCluster.GetRemote().GetCluster().Status.ProcessGroups

			ignoreDuringRestart := make(
				[]fdbv1beta2.ProcessGroupID,
				0,
				len(remoteProcessGroups),
			)

			for _, processGroup := range remoteProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				ignoreDuringRestart = append(
					ignoreDuringRestart,
					processGroup.ProcessGroupID,
				)
			}

			log.Println(
				"Selected Process groups:",
				ignoreDuringRestart,
				"to be skipped during the restart",
			)
			fdbCluster.GetRemote().SetIgnoreDuringRestart(ignoreDuringRestart)

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Verify that the upgrade proceeds
			fdbCluster.VerifyVersion(targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"when locality based exclusions are used and the resources are limited in a satellite namespace",
		func(beforeVersion string, targetVersion string) {
			fdbVersion, err := fdbv1beta2.ParseFdbVersion(beforeVersion)
			Expect(err).NotTo(HaveOccurred())
			if !fdbVersion.SupportsLocalityBasedExclusions() {
				Skip(
					"provided FDB version: " + beforeVersion + " doesn't support locality based exclusions",
				)
			}

			clusterConfig := fixtures.DefaultClusterConfigWithHaMode(
				fixtures.HaFourZoneSingleSat,
				false,
			)
			clusterConfig.UseLocalityBasedExclusions = pointer.Bool(true)

			clusterSetupWithTestConfig(
				testConfig{
					beforeVersion:          beforeVersion,
					enableOperatorPodChaos: false,
					enableHealthCheck:      true,
					loadData:               false,
					clusterConfig:          clusterConfig,
				},
			)

			Expect(
				fdbCluster.GetPrimarySatellite().GetCluster().UseLocalitiesForExclusion(),
			).To(BeTrue())
			processCounts, err := fdbCluster.GetPrimarySatellite().
				GetCluster().
				GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())

			currentPods := fdbCluster.GetPrimarySatellite().GetPods()
			podLimit := processCounts.Total() + 3
			Expect(currentPods.Items).To(HaveLen(podLimit - 3))

			// Create Quota to limit the additional Pods that can be created to 1, the actual value here is 3, because we run
			// 2 Operator Pods.
			Expect(factory.CreateIfAbsent(&corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testing-quota",
					Namespace: fdbCluster.GetPrimarySatellite().Namespace(),
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						"count/pods": resource.MustParse(strconv.Itoa(processCounts.Total() + 3)),
					},
				},
			})).NotTo(HaveOccurred())

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Wait for clusters to be updated.
			time.Sleep(15 * time.Second)
			// Wait here for the clusters to reconcile.
			Expect(fdbCluster.WaitForReconciliation(
				fixtures.SoftReconcileOption(true),
			)).NotTo(HaveOccurred())
			// Verify that the upgrade took place.
			fdbCluster.VerifyVersion(targetVersion)
			// Make sure the cluster has no data loss
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable("when maintenance feature is enabled",
		func(beforeVersion string, targetVersion string) {
			clusterConfig := fixtures.DefaultClusterConfigWithHaMode(
				fixtures.HaFourZoneSingleSat,
				false,
			)
			clusterConfig.UseMaintenanceMode = true

			clusterSetupWithTestConfig(
				testConfig{
					beforeVersion:          beforeVersion,
					enableOperatorPodChaos: false,
					enableHealthCheck:      true,
					loadData:               false,
					clusterConfig:          clusterConfig,
				},
			)

			Expect(fdbCluster.GetPrimary().GetCluster().UseMaintenaceMode()).To(BeTrue())
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Verify that the upgrade proceeds
			fdbCluster.VerifyVersion(targetVersion)
			// Wait here for the primary satellite to reconcile, this means all Pods have been replaced
			Expect(fdbCluster.GetPrimarySatellite().WaitForReconciliation()).NotTo(HaveOccurred())
			// Make sure the cluster has no data loss
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable("when tester processes are running in the primary and remote dc",
		func(beforeVersion string, targetVersion string) {
			clusterConfig := fixtures.DefaultClusterConfigWithHaMode(
				fixtures.HaFourZoneSingleSat,
				false,
			)

			clusterSetupWithTestConfig(
				testConfig{
					beforeVersion:          beforeVersion,
					enableOperatorPodChaos: false,
					enableHealthCheck:      false,
					loadData:               false,
					clusterConfig:          clusterConfig,
				},
			)

			// Start tester processes in the primary side
			primaryTester := fdbCluster.GetPrimary().CreateTesterDeployment(4)
			// Start tester processes in the remote side
			remoteTester := fdbCluster.GetRemote().CreateTesterDeployment(4)

			// Start the upgrade with the tester processes present.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Verify that the upgrade proceeds
			fdbCluster.VerifyVersion(targetVersion)
			// Wait here for the primary satellite to reconcile, this means all Pods have been replaced
			Expect(fdbCluster.GetPrimarySatellite().WaitForReconciliation()).NotTo(HaveOccurred())
			// Make sure the cluster has no data loss
			fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()

			factory.Delete(primaryTester)
			factory.Delete(remoteTester)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)
})
