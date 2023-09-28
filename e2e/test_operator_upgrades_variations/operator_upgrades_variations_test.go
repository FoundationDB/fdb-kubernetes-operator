/*
 * operator_upgrades_variations_test.go
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

package operatorupgradesvariations

/*
This test suite includes tests to validate the behaviour of the operator during upgrades on a FoundationDB cluster.
Those tests run without additional chaos injection and will validate that the upgrades succeed with different configurations.
Each test will create a new FoundationDB cluster which will be upgraded.
*/

import (
	"k8s.io/utils/pointer"
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
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

			transactionSystemProcessGroups := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None)
			// Wait until the cluster is upgraded and fully reconciled.
			Expect(fdbCluster.WaitUntilWithForceReconcile(2, 1200, func(cluster *fdbv1beta2.FoundationDBCluster) bool {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
						continue
					}

					transactionSystemProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
				}

				// Allow soft reconciliation and make sure the running version was updated
				return cluster.Status.Generations.Reconciled == cluster.Generation && cluster.Status.RunningVersion == targetVersion
			})).NotTo(HaveOccurred())

			// Get the desired process counts based on the current cluster configuration
			processCounts, err := fdbCluster.GetProcessCounts()
			Expect(err).NotTo(HaveOccurred())

			// During an upgrade we expect that the transaction system processes are replaced, so we expect to have seen
			// 2 times the process counts for transaction system processes. Add a small buffer of 5 to allow automatic
			// replacements during an upgrade.
			expectedProcessCounts := (processCounts.Total()-processCounts.Storage)*2 + 5
			Expect(len(transactionSystemProcessGroups)).To(BeNumerically("<=", expectedProcessCounts))
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with 2 storage servers per Pod",
		func(beforeVersion string, targetVersion string) {
			clusterSetupWithConfig(beforeVersion, true, &fixtures.ClusterConfig{
				DebugSymbols:        false,
				StorageServerPerPod: 2,
			})

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Make sure the cluster is still running with 2 storage server per Pod.
			Expect(fdbCluster.GetCluster().Spec.StorageServersPerPod).To(Equal(2))

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

			transactionSystemProcessGroups := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None)
			// Wait until the cluster is upgraded and fully reconciled.
			Expect(fdbCluster.WaitUntilWithForceReconcile(2, 600, func(cluster *fdbv1beta2.FoundationDBCluster) bool {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
						continue
					}

					transactionSystemProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
				}

				// Allow soft reconciliation and make sure the running version was updated
				return cluster.Status.Generations.Reconciled == cluster.Generation && cluster.Status.RunningVersion == targetVersion
			})).NotTo(HaveOccurred())

			// Get the desired process counts based on the current cluster configuration
			processCounts, err := fdbCluster.GetProcessCounts()
			Expect(err).NotTo(HaveOccurred())

			// During an upgrade we expect that the transaction system processes are replaced, so we expect to have seen
			// 2 times the process counts for transaction system processes. Add a small buffer of 5 to allow automatic
			// replacements during an upgrade.
			expectedProcessCounts := (processCounts.Total()-processCounts.Storage)*2 + 5
			Expect(len(transactionSystemProcessGroups)).To(BeNumerically("<=", expectedProcessCounts))
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with 2 log servers per Pod",
		func(beforeVersion string, targetVersion string) {
			clusterSetupWithConfig(beforeVersion, true, &fixtures.ClusterConfig{
				DebugSymbols:     false,
				LogServersPerPod: 2,
			})

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Make sure the cluster is still running with 2 log server per Pod.
			Expect(fdbCluster.GetCluster().Spec.LogServersPerPod).To(Equal(2))

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

			transactionSystemProcessGroups := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None)
			// Wait until the cluster is upgraded and fully reconciled.
			Expect(fdbCluster.WaitUntilWithForceReconcile(2, 600, func(cluster *fdbv1beta2.FoundationDBCluster) bool {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
						continue
					}
					transactionSystemProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
				}

				// Allow soft reconciliation and make sure the running version was updated
				return cluster.Status.Generations.Reconciled == cluster.Generation && cluster.Status.RunningVersion == targetVersion
			})).NotTo(HaveOccurred())

			// Get the desired process counts based on the current cluster configuration
			processCounts, err := fdbCluster.GetProcessCounts()
			Expect(err).NotTo(HaveOccurred())
			expectedProcessCounts := (processCounts.Total()-processCounts.Storage)*2 + 5
			Expect(len(transactionSystemProcessGroups)).To(BeNumerically("<=", expectedProcessCounts))
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with maintenance mode enabled",
		func(beforeVersion string, targetVersion string) {
			clusterSetupWithConfig(beforeVersion, true, &fixtures.ClusterConfig{
				DebugSymbols:       false,
				UseMaintenanceMode: true,
			})

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

			// Make sure the maintenance zone is set at least once.
			Eventually(func() fdbv1beta2.FaultDomain {
				status := fdbCluster.GetStatus()
				if status == nil {
					return ""
				}

				return status.Cluster.MaintenanceZone
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).MustPassRepeatedly(5).Should(Not(BeEmpty()))

			// Make sure the FoundationDBCluster resource is updated.
			Eventually(func() fdbv1beta2.FaultDomain {
				return fdbCluster.GetCluster().Status.MaintenanceModeInfo.ZoneID
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).MustPassRepeatedly(5).Should(Not(BeEmpty()))

			fdbCluster.VerifyVersion(targetVersion)
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with locality based exclusions",
		func(beforeVersion string, targetVersion string) {
			clusterSetupWithConfig(beforeVersion, true, &fixtures.ClusterConfig{
				DebugSymbols:               false,
				UseLocalityBasedExclusions: true,
			})

			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			Expect(pointer.BoolDeref(fdbCluster.GetCluster().Spec.AutomationOptions.UseLocalitiesForExclusion, false)).To(BeTrue())

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

			transactionSystemProcessGroups := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None)
			// Wait until the cluster is upgraded and fully reconciled.
			Expect(fdbCluster.WaitUntilWithForceReconcile(2, 600, func(cluster *fdbv1beta2.FoundationDBCluster) bool {
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
						continue
					}
					transactionSystemProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
				}

				// Allow soft reconciliation and make sure the running version was updated
				return cluster.Status.Generations.Reconciled == cluster.Generation && cluster.Status.RunningVersion == targetVersion
			})).NotTo(HaveOccurred())

			// Get the desired process counts based on the current cluster configuration
			processCounts, err := fdbCluster.GetProcessCounts()
			Expect(err).NotTo(HaveOccurred())
			expectedProcessCounts := (processCounts.Total()-processCounts.Storage)*2 + 5
			Expect(len(transactionSystemProcessGroups)).To(BeNumerically("<=", expectedProcessCounts))
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)
})
