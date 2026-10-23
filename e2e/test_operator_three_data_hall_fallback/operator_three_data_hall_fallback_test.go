/*
 * operator_three_data_hall_fallback_test.go
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
This test suite includes functional tests for FDB clusters moving between the three_data_hall and
three_data_hall_fallback configurations while one data hall is failing.
*/

import (
	"context"
	"log"
	"slices"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

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

	// In order to test the robustness of the operator we try to kill the operator Pods every 2 minutes.
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

// dataHallProcessCounts returns the number of non-excluded processes per data hall.
func dataHallProcessCounts(status *fdbv1beta2.FoundationDBStatus) map[string]int {
	counts := map[string]int{}
	for _, process := range status.Cluster.Processes {
		dataHall := process.Locality[fdbv1beta2.FDBLocalityDataHallKey]
		if process.Excluded || dataHall == "" {
			continue
		}
		counts[dataHall]++
	}

	return counts
}

// smallestDataHall mirrors the operator's check: fewest processes wins, ties are broken by the smallest name.
func smallestDataHall(counts map[string]int) string {
	smallest := ""
	for dataHall, count := range counts {
		if smallest == "" || count < counts[smallest] ||
			(count == counts[smallest] && dataHall < smallest) {
			smallest = dataHall
		}
	}

	return smallest
}

func coordinatorsPerDataHall(status *fdbv1beta2.FoundationDBStatus) map[string]int {
	coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
	counts := map[string]int{}
	for _, process := range status.Cluster.Processes {
		if _, ok := coordinators[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]]; ok {
			counts[process.Locality[fdbv1beta2.FDBLocalityDataHallKey]]++
		}
	}

	return counts
}

func podsOutsideDataHall(
	status *fdbv1beta2.FoundationDBStatus,
	pods []corev1.Pod,
	dataHall string,
) []corev1.Pod {
	hallByProcessGroup := map[string]string{}
	for _, process := range status.Cluster.Processes {
		hallByProcessGroup[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]] = process.Locality[fdbv1beta2.FDBLocalityDataHallKey]
	}

	var result []corev1.Pod
	for _, pod := range pods {
		hall, ok := hallByProcessGroup[string(fixtures.GetProcessGroupID(pod))]
		if ok && hall != dataHall {
			result = append(result, pod)
		}
	}

	return result
}

// failDataHall fails all FDB Pods running on nodes in the zone of the data hall. The data hall locality is derived
// from the zone label of the node.
func failDataHall(ctx context.Context, dataHall string) *fixtures.ChaosMeshExperiment {
	return factory.InjectPodFailure(
		ctx,
		chaosmesh.PodSelectorSpec{
			GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
				Namespaces:     []string{fdbCluster.Namespace()},
				LabelSelectors: fdbCluster.GetCachedCluster().GetMatchLabels(),
			},
			NodeSelectors: map[string]string{corev1.LabelTopologyZone: dataHall},
		},
	)
}

func setRedundancyMode(
	ctx SpecContext,
	mode fdbv1beta2.RedundancyMode,
	localDataHall string,
) {
	spec := fdbCluster.GetCluster(ctx).Spec.DeepCopy()
	spec.DatabaseConfiguration.RedundancyMode = mode
	spec.DataHall = localDataHall
	fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
}

func clearProcessGroupsToRemove(ctx SpecContext) {
	spec := fdbCluster.GetCluster(ctx).Spec.DeepCopy()
	spec.ProcessGroupsToRemove = nil
	fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
}

var _ = Describe("Operator with three data hall fallback", Label("e2e"), func() {
	var failure, podFailure *fixtures.ChaosMeshExperiment
	var originalReplacements fdbv1beta2.AutomaticReplacementOptions

	BeforeEach(func(ctx SpecContext) {
		if !factory.ChaosTestsEnabled() {
			Skip("chaos tests are required for this test suite")
		}

		originalReplacements = *fdbCluster.GetCluster(ctx).Spec.AutomationOptions.Replacements.DeepCopy()
	})

	AfterEach(func(ctx SpecContext) {
		if CurrentSpecReport().Failed() {
			factory.DumpState(ctx, fdbCluster)
		}

		if podFailure != nil {
			factory.DeleteChaosMeshExperiment(ctx, podFailure)
			podFailure = nil
		}

		if failure != nil {
			factory.DeleteChaosMeshExperiment(ctx, failure)
			failure = nil
		}

		// Always return the cluster to the initial state, independent of where the test case failed.
		clearProcessGroupsToRemove(ctx)
		Eventually(func() int {
			return len(dataHallProcessCounts(fdbCluster.GetStatus(ctx)))
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Equal(3))
		setRedundancyMode(
			ctx,
			fdbv1beta2.RedundancyModeThreeDataHall,
			smallestDataHall(dataHallProcessCounts(fdbCluster.GetStatus(ctx))),
		)
		spec := fdbCluster.GetCluster(ctx).Spec.DeepCopy()
		spec.AutomationOptions.Replacements = originalReplacements
		fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
		Expect(fdbCluster.WaitForReconciliation(ctx)).NotTo(HaveOccurred())

		fdbCluster.EnsureTeamTrackersAreHealthy(ctx)
		fdbCluster.EnsureTeamTrackersHaveMinReplicas(ctx)
	})

	It("should move to fallback with a failed data hall and back once the data hall recovered", func(ctx SpecContext) {
		By("verifying the three_data_hall cluster is up")
		status := fdbCluster.GetStatus(ctx)
		Expect(
			status.Cluster.DatabaseConfiguration.RedundancyMode,
		).To(Equal(fdbv1beta2.RedundancyModeThreeDataHall))
		Expect(status.Client.DatabaseStatus.Available).To(BeTrue())
		// Data distribution can still be settling right after the cluster was created or the data loader started.
		Eventually(func() bool {
			return fdbCluster.GetStatus(ctx).Client.DatabaseStatus.Healthy
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
		status = fdbCluster.GetStatus(ctx)
		Expect(fdbstatus.GetCoordinatorsFromStatus(status)).To(HaveLen(9))
		counts := dataHallProcessCounts(status)
		Expect(counts).To(HaveLen(3))

		dataHalls := make([]string, 0, len(counts))
		for dataHall := range counts {
			dataHalls = append(dataHalls, dataHall)
		}
		slices.Sort(dataHalls)
		failedDataHall := dataHalls[len(dataHalls)-1]
		survivingDataHall := dataHalls[0]
		log.Println("data halls:", dataHalls, "failing:", failedDataHall)

		// The failed processes must not be replaced by the operator, otherwise the failure is "fixed" automatically.
		Expect(fdbCluster.SetAutoReplacements(ctx, false, 24*time.Hour)).NotTo(HaveOccurred())
		// Until the race condition is resolved in the FDB go bindings make sure the operator is not restarted.
		factory.DeleteChaosMeshExperiment(ctx, scheduleInjectPodKill)

		By("failing one data hall")
		failure = failDataHall(ctx, failedDataHall)
		Eventually(func() bool {
			return fdbCluster.GetStatus(ctx).Client.DatabaseStatus.Healthy
		}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(BeFalse())

		By("changing to three_data_hall_fallback")
		setRedundancyMode(ctx, fdbv1beta2.RedundancyModeThreeDataHallFallback, survivingDataHall)
		Eventually(func() fdbv1beta2.RedundancyMode {
			return fdbCluster.GetStatus(ctx).Cluster.DatabaseConfiguration.RedundancyMode
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).
			Should(Equal(fdbv1beta2.RedundancyModeThreeDataHallFallback))

		By("verifying the coordinators were reduced to 5 and avoid the failed data hall")
		Eventually(func() int {
			return len(fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus(ctx)))
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Equal(5))
		coordinatorHalls := coordinatorsPerDataHall(fdbCluster.GetStatus(ctx))
		Expect(coordinatorHalls).NotTo(HaveKey(failedDataHall))
		for dataHall, count := range coordinatorHalls {
			Expect(count).To(BeNumerically("<=", 3), "data hall %s has too many coordinators", dataHall)
		}

		By("failing a coordinator Pod, forcing the coordinator to be replaced")
		coordinators := fdbCluster.GetCoordinators(ctx)
		Expect(coordinators).NotTo(BeEmpty())
		failedCoordinator := factory.RandomPickOnePod(coordinators)
		failedCoordinatorID := string(fixtures.GetProcessGroupID(failedCoordinator))
		podFailure = factory.InjectPodFailure(
			ctx,
			fixtures.PodsSelector([]corev1.Pod{failedCoordinator}),
		)
		Eventually(func() map[string]fdbv1beta2.None {
			return fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus(ctx))
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).
			ShouldNot(HaveKey(failedCoordinatorID))
		Eventually(func() int {
			return len(fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus(ctx)))
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Equal(5))
		Expect(coordinatorsPerDataHall(fdbCluster.GetStatus(ctx))).NotTo(HaveKey(failedDataHall))

		// Let the Pod recover so that it doesn't block the following exclusion.
		factory.DeleteChaosMeshExperiment(ctx, podFailure)
		podFailure = nil

		By("failing a non-coordinator Pod")
		status = fdbCluster.GetStatus(ctx)
		coordinatorIDs := fdbstatus.GetCoordinatorsFromStatus(status)
		var candidates []corev1.Pod
		for _, pod := range podsOutsideDataHall(status, fdbCluster.GetPods(ctx).Items, failedDataHall) {
			if _, isCoordinator := coordinatorIDs[string(fixtures.GetProcessGroupID(pod))]; !isCoordinator {
				candidates = append(candidates, pod)
			}
		}
		Expect(candidates).NotTo(BeEmpty())
		failedPod := factory.RandomPickOnePod(candidates)
		failedPodID := string(fixtures.GetProcessGroupID(failedPod))
		podFailure = factory.InjectPodFailure(ctx, fixtures.PodsSelector([]corev1.Pod{failedPod}))
		Eventually(func() bool {
			for _, process := range fdbCluster.GetStatus(ctx).Cluster.Processes {
				if process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey] == failedPodID {
					return false
				}
			}

			return true
		}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
		status = fdbCluster.GetStatus(ctx)
		Expect(status.Client.DatabaseStatus.Available).To(BeTrue())
		Expect(fdbstatus.GetCoordinatorsFromStatus(status)).To(HaveLen(5))

		factory.DeleteChaosMeshExperiment(ctx, podFailure)
		podFailure = nil

		By("waiting for the cluster to report healthy with the data hall still failing")
		Eventually(func() bool {
			return fdbCluster.GetStatus(ctx).Cluster.Data.State.Healthy
		}).WithTimeout(15 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		By("verifying that changing back to three_data_hall is refused while the data hall is failing")
		setRedundancyMode(ctx, fdbv1beta2.RedundancyModeThreeDataHall, survivingDataHall)
		Consistently(func() fdbv1beta2.RedundancyMode {
			return fdbCluster.GetStatus(ctx).Cluster.DatabaseConfiguration.RedundancyMode
		}).WithTimeout(3 * time.Minute).WithPolling(15 * time.Second).
			Should(Equal(fdbv1beta2.RedundancyModeThreeDataHallFallback))

		By("removing the failure")
		factory.DeleteChaosMeshExperiment(ctx, failure)
		failure = nil
		Eventually(func() int {
			return len(dataHallProcessCounts(fdbCluster.GetStatus(ctx)))
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Equal(3))
		Eventually(func() bool {
			return fdbCluster.GetStatus(ctx).Cluster.Data.State.Healthy
		}).WithTimeout(15 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		By("changing back to three_data_hall")
		// The operator only allows the change from the data hall with the fewest processes.
		setRedundancyMode(
			ctx,
			fdbv1beta2.RedundancyModeThreeDataHall,
			smallestDataHall(dataHallProcessCounts(fdbCluster.GetStatus(ctx))),
		)
		Eventually(func() fdbv1beta2.RedundancyMode {
			return fdbCluster.GetStatus(ctx).Cluster.DatabaseConfiguration.RedundancyMode
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).
			Should(Equal(fdbv1beta2.RedundancyModeThreeDataHall))
		Expect(fdbCluster.WaitForReconciliation(ctx)).NotTo(HaveOccurred())
		Expect(fdbstatus.GetCoordinatorsFromStatus(fdbCluster.GetStatus(ctx))).To(HaveLen(9))
	})
})
