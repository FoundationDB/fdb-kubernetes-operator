/*
 * operator_three_data_hall_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"log"

	"k8s.io/utils/ptr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	corev1 "k8s.io/api/core/v1"

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

var _ = BeforeSuite(func(ctx SpecContext) {
	factory = fixtures.CreateFactory(testOptions)
	config := fixtures.DefaultClusterConfig(false)
	config.RedundancyMode = fdbv1beta2.RedundancyModeThreeDataHall
	config.UseUnifiedImage = ptr.To(true)

	fdbCluster = factory.CreateFdbCluster(ctx,
		config,
	)

	// Make sure that the test suite is able to fetch logs from Pods.
	operatorPod := factory.RandomPickOnePod(
		factory.GetOperatorPods(ctx, fdbCluster.Namespace()).Items,
	)
	Expect(
		factory.GetLogsForPod(ctx, &operatorPod, "manager", nil),
	).NotTo(BeEmpty())

	// Load some data async into the cluster. We will only block as long as the Job is created.
	factory.CreateDataLoaderIfAbsent(ctx, fdbCluster)

	// In order to test the robustness of the operator we try to kill the operator Pods every minute.
	if factory.ChaosTestsEnabled() {
		scheduleInjectPodKill = factory.ScheduleInjectPodKillWithName(ctx,
			fixtures.GetOperatorSelector(fdbCluster.Namespace()),
			"*/2 * * * *",
			chaosmesh.OneMode,
			fdbCluster.Namespace()+"-"+fdbCluster.Name(),
		)
	}
})

var _ = AfterSuite(func(ctx SpecContext) {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown(ctx)
})

// TODO (johscheuer): Add those tests later again to the e2e pipeline.
var _ = Describe("Operator with three data hall", Label("e2e"), func() {
	var availabilityCheck bool

	AfterEach(func(ctx SpecContext) {
		// Reset availabilityCheck if a test case removes this check.
		availabilityCheck = true
		if CurrentSpecReport().Failed() {
			factory.DumpState(ctx, fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation(ctx)).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
		// Make sure all data is present in the cluster
		fdbCluster.EnsureTeamTrackersAreHealthy(ctx)
		fdbCluster.EnsureTeamTrackersHaveMinReplicas(ctx)
	})

	JustBeforeEach(func() {
		if availabilityCheck {
			err := fdbCluster.InvariantClusterStatusAvailable()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("should bring up the cluster with three data hall redundancy", func(ctx SpecContext) {
		status := fdbCluster.GetStatus(ctx)
		Expect(
			status.Cluster.DatabaseConfiguration.RedundancyMode,
		).To(Equal(fdbv1beta2.RedundancyModeThreeDataHall))
		for _, process := range status.Cluster.Processes {
			Expect(process.Locality).To(HaveKey(fdbv1beta2.FDBLocalityDataHallKey))
		}
	})

	When("replacing a coordinator Pod", func() {
		var replacedPod corev1.Pod
		var useLocalitiesForExclusion bool

		JustBeforeEach(func(ctx SpecContext) {
			initialPods := fdbCluster.GetLogPods(ctx)
			coordinators := fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus(ctx))

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
			fdbCluster.ReplacePod(ctx, replacedPod, true)
		})

		BeforeEach(func(ctx SpecContext) {
			// Until the race condition is resolved in the FDB go bindings make sure the operator is not restarted.
			// See: https://github.com/apple/foundationdb/issues/11222
			// We can remove this once 7.1 is the default version.
			factory.DeleteChaosMeshExperiment(ctx, scheduleInjectPodKill)
			useLocalitiesForExclusion = fdbCluster.GetCluster(ctx).UseLocalitiesForExclusion()
		})

		AfterEach(func(ctx SpecContext) {
			Expect(fdbCluster.ClearProcessGroupsToRemove(ctx)).NotTo(HaveOccurred())
			// Make sure we reset the previous behaviour.
			spec := fdbCluster.GetCluster(ctx).Spec.DeepCopy()
			spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
				useLocalitiesForExclusion,
			)
			fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
			Expect(
				fdbCluster.GetCluster(ctx).UseLocalitiesForExclusion(),
			).To(Equal(useLocalitiesForExclusion))

			// Making sure we included back all the process groups after exclusion is complete.
			Expect(
				fdbCluster.GetStatus(ctx).Cluster.DatabaseConfiguration.ExcludedServers,
			).To(BeEmpty())

			if factory.ChaosTestsEnabled() {
				scheduleInjectPodKill = factory.ScheduleInjectPodKillWithName(ctx,
					fixtures.GetOperatorSelector(fdbCluster.Namespace()),
					"*/2 * * * *",
					chaosmesh.OneMode,
					fdbCluster.Namespace()+"-"+fdbCluster.Name(),
				)
			}
		})

		When("IP addresses are used for exclusion", func() {
			BeforeEach(func(ctx SpecContext) {
				spec := fdbCluster.GetCluster(ctx).Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(false)
				fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
				Expect(fdbCluster.GetCluster(ctx).UseLocalitiesForExclusion()).To(BeFalse())
			})

			It("should remove the targeted Pod", func(ctx SpecContext) {
				fdbCluster.EnsurePodIsDeleted(ctx, replacedPod.Name)
			})
		})

		When("localities are used for exclusion", func() {
			BeforeEach(func(ctx SpecContext) {
				cluster := fdbCluster.GetCluster(ctx)

				fdbVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
				Expect(err).NotTo(HaveOccurred())

				if !fdbVersion.SupportsLocalityBasedExclusions() {
					Skip(
						"provided FDB version: " + cluster.GetRunningVersion() + " doesn't support locality based exclusions",
					)
				}

				spec := cluster.Spec.DeepCopy()
				spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(true)
				fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
				Expect(fdbCluster.GetCluster(ctx).UseLocalitiesForExclusion()).To(BeTrue())
			})

			It("should remove the targeted Pod", func(ctx SpecContext) {
				Expect(
					ptr.Deref(
						fdbCluster.GetCluster(ctx).Spec.AutomationOptions.UseLocalitiesForExclusion,
						false,
					),
				).To(BeTrue())
				fdbCluster.EnsurePodIsDeleted(ctx, replacedPod.Name)
			})
		})
	})
})
