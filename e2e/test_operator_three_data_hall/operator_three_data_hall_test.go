/*
 * operator_test_three_data_hall.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2024 Apple Inc. and the FoundationDB project authors
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

package operator

/*
This test suite includes functional tests for FDB clusters running in the three_data_hall configuration.
*/

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory               *fixtures.Factory
	fdbCluster            *fixtures.FdbCluster
	testOptions           *fixtures.FactoryOptions
	scheduleInjectPodKill *fixtures.ChaosMeshExperiment
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	if !factory.UseUnifiedImage() {
		Skip("three data hall tests are only supported with the unified image")
	}

	config := fixtures.DefaultClusterConfig(false)
	config.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall

	fdbCluster = factory.CreateFdbCluster(
		config,
		factory.GetClusterOptions()...,
	)

	// Make sure that the test suite is able to fetch logs from Pods.
	operatorPod := factory.RandomPickOnePod(factory.GetOperatorPods(fdbCluster.Namespace()).Items)
	Expect(factory.GetLogsForPod(&operatorPod, "manager", nil)).NotTo(BeEmpty())

	// Load some data async into the cluster. We will only block as long as the Job is created.
	factory.CreateDataLoaderIfAbsent(fdbCluster)

	// In order to test the robustness of the operator we try to kill the operator Pods every minute.
	if factory.ChaosTestsEnabled() {
		scheduleInjectPodKill = factory.ScheduleInjectPodKillWithName(
			fixtures.GetOperatorSelector(fdbCluster.Namespace()),
			"*/2 * * * *",
			chaosmesh.OneMode,
			fdbCluster.Namespace()+"-"+fdbCluster.Name(),
		)
	}
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator with three data hall", Label("e2e", "pr"), func() {
	var availabilityCheck bool

	AfterEach(func() {
		// Reset availabilityCheck if a test case removes this check.
		availabilityCheck = true
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
		// Make sure all data is present in the cluster
		fdbCluster.EnsureTeamTrackersAreHealthy()
		fdbCluster.EnsureTeamTrackersHaveMinReplicas()
	})

	JustBeforeEach(func() {
		if availabilityCheck {
			err := fdbCluster.InvariantClusterStatusAvailable()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("should bring up the cluster with three data hall redundancy", func() {
		status := fdbCluster.GetStatus()
		Expect(status.Cluster.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeThreeDataHall))
		for _, process := range status.Cluster.Processes {
			Expect(process.Locality).To(HaveKey(fdbv1beta2.FDBLocalityDataHallKey))
		}
	})

	When("replacing a coordinator Pod", func() {
		var replacedPod corev1.Pod
		var useLocalitiesForExclusion bool

		JustBeforeEach(func() {
			initialPods := fdbCluster.GetLogPods()
			coordinators := fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus())

			for _, pod := range initialPods.Items {
				_, isCoordinator := coordinators[string(fixtures.GetProcessGroupID(pod))]
				if isCoordinator {
					replacedPod = pod
					break
				}
			}

			// In case that none of the log processes are coordinators
			if replacedPod.Name == "" {
				replacedPod = factory.RandomPickOnePod(initialPods.Items)
			}

			log.Println("coordinators:", coordinators, "replacedPod", replacedPod.Name)
			fdbCluster.ReplacePod(replacedPod, true)
		})

		BeforeEach(func() {
			// Until the race condition is resolved in the FDB go bindings make sure the operator is not restarted.
			// See: https://github.com/apple/foundationdb/issues/11222
			// We can remove this once 7.1 is the default version.
			factory.DeleteChaosMeshExperimentSafe(scheduleInjectPodKill)
			useLocalitiesForExclusion = fdbCluster.GetCluster().UseLocalitiesForExclusion()
		})

		AfterEach(func() {
			Expect(fdbCluster.ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
			// Make sure we reset the previous behaviour.
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(useLocalitiesForExclusion)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.GetCluster().UseLocalitiesForExclusion()).To(Equal(useLocalitiesForExclusion))

			// Making sure we included back all the process groups after exclusion is complete.
			Expect(fdbCluster.GetStatus().Cluster.DatabaseConfiguration.ExcludedServers).To(BeEmpty())

			if factory.ChaosTestsEnabled() {
				scheduleInjectPodKill = factory.ScheduleInjectPodKillWithName(
					fixtures.GetOperatorSelector(fdbCluster.Namespace()),
					"*/2 * * * *",
					chaosmesh.OneMode,
					fdbCluster.Namespace()+"-"+fdbCluster.Name(),
				)
			}
		})

		When("IP addresses are used for exclusion", func() {
			BeforeEach(func() {
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(false)
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			It("should remove the targeted Pod", func() {
				fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
			})
		})

		When("localities are used for exclusion", func() {
			BeforeEach(func() {
				cluster := fdbCluster.GetCluster()

				fdbVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
				Expect(err).NotTo(HaveOccurred())

				if !fdbVersion.SupportsLocalityBasedExclusions() {
					Skip("provided FDB version: " + cluster.GetRunningVersion() + " doesn't support locality based exclusions")
				}

				spec := cluster.Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(true)
				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(fdbCluster.GetCluster().UseLocalitiesForExclusion()).To(BeTrue())
			})

			It("should remove the targeted Pod", func() {
				Expect(pointer.BoolDeref(fdbCluster.GetCluster().Spec.AutomationOptions.UseLocalitiesForExclusion, false)).To(BeTrue())
				fdbCluster.EnsurePodIsDeleted(replacedPod.Name)
			})
		})
	})
})
