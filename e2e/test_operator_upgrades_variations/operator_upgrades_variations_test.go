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
	"fmt"
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory *fixtures.Factory
	//fdbCluster  *fixtures.FdbCluster
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

func clusterSetupWithConfig(beforeVersion string, config *fixtures.ClusterConfig) *fixtures.FdbCluster {
	factory.SetBeforeVersion(beforeVersion)
	fdbCluster := factory.CreateFdbCluster(
		config,
		factory.GetClusterOptions(fixtures.UseVersionBeforeUpgrade)...,
	)

	Expect(
		fdbCluster.InvariantClusterStatusAvailableWithThreshold(15 * time.Second),
	).ShouldNot(HaveOccurred())

	return fdbCluster
}

func performUpgrade(beforeVersion string, targetVersion string, config *fixtures.ClusterConfig, validateFunc func(cluster *fixtures.FdbCluster)) {
	cluster := clusterSetupWithConfig(beforeVersion, config)
	startTime := time.Now()
	Expect(cluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
	validateFunc(cluster)

	if !fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
		// Ensure that the operator is setting the IncorrectConfigMap and IncorrectCommandLine conditions during the upgrade
		// process.
		expectedConditions := map[fdbv1beta2.ProcessGroupConditionType]bool{
			fdbv1beta2.IncorrectConfigMap:   true,
			fdbv1beta2.IncorrectCommandLine: true,
		}
		Eventually(func() bool {
			for _, processGroup := range cluster.GetCluster().Status.ProcessGroups {
				if !processGroup.MatchesConditions(expectedConditions) {
					return false
				}
			}

			return true
		}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
	}

	transactionSystemProcessGroups := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None)
	// Wait until the cluster is upgraded and fully reconciled.
	Expect(cluster.WaitUntilWithForceReconcile(2, 1200, func(cluster *fdbv1beta2.FoundationDBCluster) bool {
		for _, processGroup := range cluster.Status.ProcessGroups {
			if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
				continue
			}

			transactionSystemProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
		}

		// Allow soft reconciliation and make sure the running version was updated
		return cluster.Status.Generations.Reconciled == cluster.Generation && cluster.Status.RunningVersion == targetVersion
	})).NotTo(HaveOccurred())

	log.Println("Upgrade took:", time.Since(startTime).String())
	// Get the desired process counts based on the current cluster configuration
	processCounts, err := cluster.GetProcessCounts()
	Expect(err).NotTo(HaveOccurred())

	// During an upgrade we expect that the transaction system processes are replaced, so we expect to have seen
	// 2 times the process counts for transaction system processes. Add a small buffer of 5 to allow automatic
	// replacements during an upgrade.
	expectedProcessCounts := (processCounts.Total()-processCounts.Storage)*2 + 5
	Expect(len(transactionSystemProcessGroups)).To(BeNumerically("<=", expectedProcessCounts))
}

var _ = Describe("Operator Upgrades", Label("e2e", "pr"), func() {
	BeforeEach(func() {
		factory = fixtures.CreateFactory(testOptions)
	})

	AfterEach(func() {
		factory.Shutdown()
	})

	// Ginkgo lacks the support for AfterEach and BeforeEach in tables, so we have to put everything inside the testing function
	// this setup allows to dynamically generate the table entries that will be executed e.g. to test different upgrades
	// for different versions without hard coding or having multiple flags.
	DescribeTable(
		"upgrading a cluster without chaos",
		func(beforeVersion string, targetVersion string) {
			performUpgrade(beforeVersion, targetVersion, &fixtures.ClusterConfig{
				DebugSymbols: false,
			}, func(cluster *fixtures.FdbCluster) {})
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with 2 storage servers per Pod",
		func(beforeVersion string, targetVersion string) {
			performUpgrade(beforeVersion, targetVersion, &fixtures.ClusterConfig{
				DebugSymbols:        false,
				StorageServerPerPod: 2,
			}, func(cluster *fixtures.FdbCluster) {
				Expect(cluster.GetCluster().Spec.StorageServersPerPod).To(Equal(2))
			})
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with 2 log servers per Pod",
		func(beforeVersion string, targetVersion string) {
			performUpgrade(beforeVersion, targetVersion, &fixtures.ClusterConfig{
				DebugSymbols:     false,
				LogServersPerPod: 2,
			}, func(cluster *fixtures.FdbCluster) {
				Expect(cluster.GetCluster().Spec.LogServersPerPod).To(Equal(2))
			})
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with maintenance mode enabled",
		func(beforeVersion string, targetVersion string) {
			performUpgrade(beforeVersion, targetVersion, &fixtures.ClusterConfig{
				DebugSymbols:       false,
				UseMaintenanceMode: true,
			}, func(cluster *fixtures.FdbCluster) {
				Expect(cluster.GetCluster().UseMaintenaceMode()).To(BeTrue())
			})
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with locality based exclusions",
		func(beforeVersion string, targetVersion string) {
			performUpgrade(beforeVersion, targetVersion, &fixtures.ClusterConfig{
				DebugSymbols:       false,
				UseMaintenanceMode: true,
			}, func(cluster *fixtures.FdbCluster) {
				Expect(cluster.GetCluster().UseLocalitiesForExclusion()).To(BeTrue())
			})
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)

	DescribeTable(
		"with DNS in cluster file enabled",
		func(beforeVersion string, targetVersion string) {
			parsedVersion, err := fdbv1beta2.ParseFdbVersion(beforeVersion)
			Expect(err).NotTo(HaveOccurred())
			if !parsedVersion.SupportsDNSInClusterFile() {
				Skip(fmt.Sprintf("FoundationDB version: %s, does not support the usage of DNS", beforeVersion))
			}

			performUpgrade(beforeVersion, targetVersion, &fixtures.ClusterConfig{
				DebugSymbols: false,
				UseDNS:       true,
			}, func(cluster *fixtures.FdbCluster) {
				Expect(cluster.GetCluster().UseDNSInClusterFile()).To(BeTrue())
			})
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)
})
