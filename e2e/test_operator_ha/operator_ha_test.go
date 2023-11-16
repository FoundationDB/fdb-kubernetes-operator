/*
 * operator_ha_test.go
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

package operatorha

/*
This test suite includes functional tests to ensure normal operational tasks are working fine.
Those tests include replacements of healthy or fault Pods and setting different configurations.

The assumption is that every test case reverts the changes that were done on the cluster.
In order to improve the test speed we only create one FoundationDB cluster initially.
This cluster will be used for all tests.
*/

import (
	corev1 "k8s.io/api/core/v1"
	"log"
	"strconv"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.HaFdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbHaCluster(
		fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false),
		factory.GetClusterOptions()...,
	)

	// Load some data into the cluster.
	factory.CreateDataLoaderIfAbsent(fdbCluster.GetPrimary())

	// In order to test the robustness of the operator we try to kill the operator Pods every minute.
	if factory.ChaosTestsEnabled() {
		for _, cluster := range fdbCluster.GetAllClusters() {
			factory.ScheduleInjectPodKill(
				fixtures.GetOperatorSelector(cluster.Namespace()),
				"*/2 * * * *",
				chaosmesh.OneMode,
			)
		}
	}
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator HA tests", Label("e2e", "pr"), func() {
	var availabilityCheck bool

	AfterEach(func() {
		// Reset availabilityCheck if a test case removes this check.
		availabilityCheck = true
		if CurrentSpecReport().Failed() {
			factory.DumpStateHaCluster(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
		// Make sure all data is present in the cluster
		fdbCluster.GetPrimary().EnsureTeamTrackersAreHealthy()
		fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
	})

	JustBeforeEach(func() {
		if availabilityCheck {
			Expect(fdbCluster.GetPrimary().InvariantClusterStatusAvailable()).NotTo(HaveOccurred())
		}
	})

	When("replacing satellite Pods and the new Pods are stuck in pending", func() {
		BeforeEach(func() {
			satellite := fdbCluster.GetPrimarySatellite()
			satelliteCluster := satellite.GetCluster()

			// We don't want to move our running Process Groups into the no schedule state, only new Process Groups
			processGroupMap := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}
			for _, processGroup := range satelliteCluster.Status.ProcessGroups {
				processGroupMap[processGroup.ProcessGroupID] = fdbv1beta2.None{}
			}

			// Generate a list of potential ProcessGroupIDs
			noScheduleList := make([]fdbv1beta2.ProcessGroupID, 0, 100)
			for i := 0; i < 100; i++ {
				processGroupID := fdbv1beta2.ProcessGroupID(satelliteCluster.Spec.ProcessGroupIDPrefix + "-log-" + strconv.Itoa(i))

				// Ignore running Process Groups
				if _, ok := processGroupMap[processGroupID]; ok {
					continue
				}

				noScheduleList = append(noScheduleList, processGroupID)
			}

			// Update the satellite, this update should be fast as no running Process Groups should be affected
			satellite.SetProcessGroupsAsUnschedulable(noScheduleList)
			// Replace all Pods for this cluster.
			satellite.ReplacePods(satellite.GetAllPods().Items, false)
		})

		AfterEach(func() {
			// Once the Pods can schedule the cluster should be able to reconcile.
			fdbCluster.GetPrimarySatellite().SetProcessGroupsAsUnschedulable(nil)
			Expect(fdbCluster.GetPrimarySatellite().GetCluster().Spec.Buggify.NoSchedule).To(BeEmpty())
			Expect(fdbCluster.GetPrimarySatellite().WaitForReconciliation()).NotTo(HaveOccurred())
		})

		It("should not replace too many Pods and bring down the satellite", func() {
			satellite := fdbCluster.GetPrimarySatellite()
			satelliteCluster := satellite.GetCluster()
			// Verify that the operator keeps the ProcessGroups up and running.
			processCounts, err := satelliteCluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())

			desiredRunningPods := processCounts.Log - satelliteCluster.DesiredFaultTolerance()
			Consistently(func() int {
				var runningPods int
				for _, pod := range satellite.GetAllPods().Items {
					if pod.Status.Phase != corev1.PodRunning {
						continue
					}

					runningPods++
				}

				// We should add here another check that the cluster stays in the primary.
				return runningPods
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(BeNumerically(">=", desiredRunningPods))
		})
	})
})
