/*
 * operator_ha_flaky_upgrades_test.go
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

package operatorhaflakyupgrades

/*
This test suite includes tests to validate the behaviour of the operator during upgrades on a HA FoundationDB cluster.
The executed tests include a base test without any chaos/faults.
Each test will create a new HA FoundationDB cluster which will be upgraded.
*/

import (
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
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

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
})

func clusterSetupWithHealthCheckOption(beforeVersion string, enableOperatorPodChaos bool, enableHealthCheck bool) {
	// We set the before version here to overwrite the before version from the specific flag
	// the specific flag will be removed in the future.
	factory.SetBeforeVersion(beforeVersion)

	fdbCluster = factory.CreateFdbHaCluster(
		fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false),
		factory.GetClusterOptions(fixtures.UseVersionBeforeUpgrade)...,
	)
	if enableHealthCheck {
		Expect(
			fdbCluster.GetPrimary().InvariantClusterStatusAvailableWithThreshold(15 * time.Second),
		).ShouldNot(HaveOccurred())
	}

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

func clusterSetup(beforeVersion string, enableOperatorPodChaos bool) {
	clusterSetupWithHealthCheckOption(beforeVersion, enableOperatorPodChaos, true)
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

	// https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/172, debug why this test is flaky and how
	// to make it stable.
	DescribeTable(
		"when no remote processes are restarted",
		func(beforeVersion string, targetVersion string) {
			clusterSetup(beforeVersion, false)

			// Select remote processes and use the buggify option to skip those
			// processes during the restart command.
			remoteProcessGroups := fdbCluster.GetRemote().GetCluster().Status.ProcessGroups
			ignoreDuringRestart := make(
				[]fdbv1beta2.ProcessGroupID,
				0,
				len(remoteProcessGroups),
			)

			for _, processGroup := range remoteProcessGroups {
				ignoreDuringRestart = append(
					ignoreDuringRestart,
					processGroup.ProcessGroupID,
				)
			}

			log.Println(
				"Selected Process Groups:",
				ignoreDuringRestart,
				"to be skipped during the restart",
			)

			// We have to set this to all clusters as any operator could be doing the cluster wide restart.
			for _, cluster := range fdbCluster.GetAllClusters() {
				cluster.SetIgnoreDuringRestart(ignoreDuringRestart)
			}

			// The cluster should still be able to upgrade.
			Expect(fdbCluster.UpgradeCluster(targetVersion, false)).NotTo(HaveOccurred())
			// Verify that the upgrade proceeds
			fdbCluster.VerifyVersion(targetVersion)

			// TODO add validation here processes are updated new version
		},
		EntryDescription("Upgrade from %[1]s to %[2]s"),
		fixtures.GenerateUpgradeTableEntries(testOptions),
	)
})
