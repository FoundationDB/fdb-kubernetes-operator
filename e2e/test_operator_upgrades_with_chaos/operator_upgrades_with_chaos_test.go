/*
 * operator_upgrades_with_chaos_test.go
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

package operatorupgradeschaosmesh

/*
This test suite includes tests to validate the behaviour of the operator during upgrades on a FoundationDB cluster.
The executed tests will verify that the upgrades can proceed under different failure scenarios injected with chaos-mesh.
Each test will create a new FoundationDB cluster which will be upgraded.
Since FoundationDB is version incompatible for major and minor versions and the upgrade process for FoundationDB on Kubernetes requires multiple steps (see the documentation in the docs folder) we test different scenarios where only some processes are restarted.
*/

import (
	"fmt"
	"log"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Operator Upgrades with chaos-mesh", Label("e2e", "pr"), func() {
	BeforeEach(func() {
		factory = fixtures.CreateFactory(testOptions)
		if !factory.ChaosTestsEnabled() {
			Skip("chaos mesh is disabled")
		}
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
		"upgrading a cluster with a partitioned pod",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion)
			Expect(fdbCluster.SetAutoReplacements(false, 20*time.Minute)).ToNot(HaveOccurred())
			// Ensure the operator is not skipping the process because it's missing for to long
			fdbCluster.SetIgnoreMissingProcessesSeconds(5 * time.Minute)

			// 1. Introduce network partition b/w Pod and cluster.
			// Inject chaos only to storage Pods to reduce the risk of a long recovery because a transaction
			// system Pod was partitioned. The test still has the same checks that will be performed.
			// Once we have a better availability check for upgrades we can target all pods again.
			partitionedPod := factory.ChooseRandomPod(fdbCluster.GetStoragePods())
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
			fdbCluster.VerifyVersion(targetVersion)
		},

		EntryDescription("Upgrade from %[1]s to %[2]s with partitioned Pod"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with a partitioned pod which eventually gets replaced",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion)
			Expect(fdbCluster.SetAutoReplacements(false, 5*time.Minute)).ToNot(HaveOccurred())
			// Ensure the operator is not skipping the process because it's missing for too long
			fdbCluster.SetIgnoreMissingProcessesSeconds(30 * time.Minute)

			// 1. Introduce network partition b/w Pod and cluster.
			partitionedPod := factory.ChooseRandomPod(fdbCluster.GetStoragePods())
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
			}

			// Enable the auto replacement feature again, but don't wait for reconciliation, otherwise we wait
			// until the whole cluster is upgraded.
			Expect(
				fdbCluster.SetAutoReplacementsWithWait(true, 3*time.Minute, false),
			).ToNot(HaveOccurred())

			// Make sure to allow missing processes again to be ignored.
			fdbCluster.SetIgnoreMissingProcessesSeconds(10 * time.Second)

			// In the case of a version compatible upgrade the operator will proceed with recreating the storage Pods
			// to make sure they use the new version. At the same time all transaction processes are marked for removal
			// in order to bring up a new set of transaction processes with the new version, this prevents the operator
			// from doing the actual replacement of the partitioned Pod. If th partitioned Pod gets recreated by the operator
			// the injected partition from chaos-mesh is lost and therefore the process is reporting to the cluster again.
			if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				log.Println("waiting for pod removal:", partitionedPod.Name)
				fdbCluster.WaitForPodRemoval(partitionedPod)
				log.Println("pod removed:", partitionedPod.Name)
			}

			// 3. Upgrade should proceed without removing the partition, as Pod will be replaced.
			fdbCluster.VerifyVersion(targetVersion)
		},

		EntryDescription(
			"Upgrade from %[1]s to %[2]s",
		),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster with link that drops some packets",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion)

			// 1. Introduce packet loss b/w pods.
			log.Println("Injecting packet loss b/w pod")
			allPods := fdbCluster.GetAllPods()

			pickedPods := factory.RandomPickPod(allPods.Items, len(allPods.Items)/5)
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
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())

			// 3. Upgrade should finish.
			fdbCluster.VerifyVersion(targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s with network link that drops some packets"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"upgrading a cluster and one process has the fdbmonitor.conf file not ready",
		func(beforeVersion string, targetVersion string) {
			if fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
				Skip("this test only affects version incompatible upgrades")
			}

			// The unified image doesn't use the fdbmonitor.conf file as the arguments are generated in the process
			// itself.
			if factory.UseUnifiedImage() {
				Skip("this test only affects the split image setup")
			}

			clusterSetup(beforeVersion)

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Skip the reconciliation here to have time to stage everything.
			fdbCluster.SetSkipReconciliation(true)

			// Select one Pod, this Pod will mount the fdbmonitor config file as read-only.
			// This should block the upgrade.
			faultyPod := factory.RandomPickOnePod(fdbCluster.GetPods().Items)

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
			fdbCluster.SetSkipReconciliation(false)

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
			fdbCluster.VerifyVersion(targetVersion)
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

			clusterSetup(beforeVersion)

			// Update the cluster version.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Skip the reconciliation here to have time to stage everything.
			fdbCluster.SetSkipReconciliation(true)

			// Select one Pod, this Pod will miss the new fdbserver binary.
			// This should block the upgrade.
			faultyPod := factory.RandomPickOnePod(fdbCluster.GetPods().Items)

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
			var fdbserverBinaryBuilder strings.Builder
			if factory.UseUnifiedImage() {
				fdbserverBinaryBuilder.WriteString("/var/fdb/shared-binaries")
			} else {
				fdbserverBinaryBuilder.WriteString("/var/output-files")
			}

			fdbserverBinaryBuilder.WriteString("/bin/")
			fdbserverBinaryBuilder.WriteString(targetVersion)
			fdbserverBinaryBuilder.WriteString("/fdbserver")
			fdbserverBinary := fdbserverBinaryBuilder.String()

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
			fdbCluster.SetSkipReconciliation(false)

			// The cluster will be stuck in this state until the Pod is restarted and the new binary is present.
			expectedConditions := map[fdbv1beta2.ProcessGroupConditionType]bool{
				fdbv1beta2.IncorrectConfigMap:   true,
				fdbv1beta2.IncorrectCommandLine: true,
			}
			faultyProcessGroupID := fixtures.GetProcessGroupID(faultyPod)

			creationTimestamp := faultyPod.CreationTimestamp
			// The upgrade will be stuck until the new fdbserver binary is copied to the shared directory again.
			Eventually(func() bool {
				currentPod := fdbCluster.GetPod(faultyPod.Name)
				if creationTimestamp.Compare(currentPod.CreationTimestamp.Time) != 0 {
					Skip("Faulty Pod was deleted, skipping test as the test setup failed.")
				}

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
			fdbCluster.VerifyVersion(targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s and one process is missing the new binary"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)
})
