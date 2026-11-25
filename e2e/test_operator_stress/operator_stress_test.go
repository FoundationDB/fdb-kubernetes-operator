/*
 * operator_stress_test.go
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

package operatorstress

/*
This test suite includes tests that run the same operation multiple times to make sure those operations are robust and reliable.
Currently this test suite checks the cluster creation, the replacement of Pods and the upgrade of single clusters and of
multi-region (HA) clusters.
Every test records the duration (and for upgrades the number of recoveries and recreated Pods) of each run in a
gmeasure experiment, the experiments are added to the report of the test run. The report shows the minimum, maximum,
median and average of all runs, and the log prints the values of the single runs.
*/

import (
	"fmt"
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gmeasure"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// runs defines how often an operation is repeated in the tests of this suite.
	runs = 10

	// generationStableDuration defines how long the generation of the cluster must be unchanged until the cluster is
	// considered stable.
	generationStableDuration = 30 * time.Second

	// generationStableTimeout defines how long we wait for a stable generation at most.
	generationStableTimeout = 10 * time.Minute

	// haRuns defines how often an operation is repeated for a multi-region cluster. Creating and upgrading a multi-region
	// cluster takes a lot longer than a single cluster, so the number of runs is lower to stay within the timeout.
	haRuns = 5

	// Names of the measurements in the experiments.
	createDuration  = "create cluster duration"
	deleteDuration  = "delete cluster duration"
	replaceDuration = "replace Pod duration"
	upgradeDuration = "upgrade duration"
	recoveries      = "recoveries during the upgrade"
	recreatedPods   = "Pods recreated during the upgrade"
	totalPods       = "Pods before the upgrade"
	generationDelta = "cluster generation increase during the upgrade"
	lateGeneration  = "cluster generation increase after the upgrade finished"
)

var (
	factory     *fixtures.Factory
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
})

var _ = AfterSuite(func(ctx SpecContext) {
	factory.Shutdown(ctx)
})

// newExperiment creates a new experiment and makes sure it is added to the report when the spec is done, even if the
// spec failed. In this case the report contains the runs until the failure. The values of all runs of the provided
// measurements are printed in the log, so that the single runs can be compared with the summary.
func newExperiment(name string, measurements ...string) *gmeasure.Experiment {
	experiment := gmeasure.NewExperiment(name)

	DeferCleanup(func() {
		AddReportEntry(experiment.Name, experiment)

		for _, measurement := range measurements {
			data := experiment.Get(measurement)
			if len(data.Durations) > 0 {
				log.Printf(
					"%s - %s of all runs: %v\n",
					experiment.Name,
					measurement,
					data.Durations,
				)
			}

			if len(data.Values) > 0 {
				log.Printf("%s - %s of all runs: %v\n", experiment.Name, measurement, data.Values)
			}
		}
	})

	return experiment
}

// measureDuration measures the duration of the provided function with a precision of a second.
func measureDuration(experiment *gmeasure.Experiment, name string, f func()) {
	experiment.MeasureDuration(name, f, gmeasure.Precision(time.Second))
}

// getUpgradeDescription returns a description for the experiment of an upgrade. Every run of the tests creates a new
// cluster, so a protocol compatible upgrade with a pinned main container image is the first patch upgrade of the cluster.
// If the monitor doesn't support the shared binary config, the Pods must be replaced once to start the binary of the sidecar.
// This upgrade is a normal rolling update and must not be compared with in-place upgrades.
func getUpgradeDescription(prefix string, beforeVersion string, targetVersion string) string {
	description := fmt.Sprintf(
		"%s from %s to %s (pinned main container image: %t)",
		prefix,
		beforeVersion,
		targetVersion,
		factory.UsePinnedMonitorVersion(),
	)

	if !factory.UsePinnedMonitorVersion() ||
		!fixtures.VersionsAreProtocolCompatible(beforeVersion, targetVersion) {
		return description
	}

	monitorVersion := factory.GetMonitorVersion(
		&fixtures.ClusterConfig{Version: new(beforeVersion)},
	)
	if monitorVersion == nil {
		return description
	}

	parsedVersion, err := fdbv1beta2.ParseFdbVersion(*monitorVersion)
	if err != nil || parsedVersion.SupportsSharedBinaryConfig() {
		return description
	}

	return description + ", the first patch upgrade is a rolling update of the Pods"
}

// waitForStableGeneration waits until the generation of the cluster was not changed for generationStableDuration and
// returns this generation. If the generation doesn't become stable within generationStableTimeout the last generation is
// returned. Every recovery increases the generation, so this makes sure that recoveries that are not caused by the upgrade
// (e.g. late events of the cluster creation or Pods that are deleted after the upgrade) are not counted as recoveries of
// the upgrade.
func waitForStableGeneration(ctx SpecContext, getGeneration func() int) int {
	generation := getGeneration()
	lastChange := time.Now()
	deadline := time.Now().Add(generationStableTimeout)

	for time.Since(lastChange) < generationStableDuration && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return generation
		case <-time.After(5 * time.Second):
		}

		current := getGeneration()
		if current != generation {
			log.Printf("generation changed from %d to %d\n", generation, current)
			generation = current
			lastChange = time.Now()
		}
	}

	return generation
}

// getPodUIDs returns the UIDs of all Pods of the cluster.
func getPodUIDs(ctx SpecContext, cluster *fixtures.FdbCluster) map[types.UID]struct{} {
	pods := cluster.GetPods(ctx)
	result := make(map[types.UID]struct{}, len(pods.Items))
	for _, pod := range pods.Items {
		result[pod.UID] = struct{}{}
	}

	return result
}

// getHaPodUIDs returns the UIDs of all Pods of all clusters of the multi-region cluster.
func getHaPodUIDs(ctx SpecContext, haCluster *fixtures.HaFdbCluster) map[types.UID]struct{} {
	result := make(map[types.UID]struct{})
	for _, cluster := range haCluster.GetAllClusters() {
		for uid := range getPodUIDs(ctx, cluster) {
			result[uid] = struct{}{}
		}
	}

	return result
}

// countMissing returns the number of UIDs of the before set that are not in the after set.
func countMissing(before map[types.UID]struct{}, after map[types.UID]struct{}) int {
	var missing int
	for uid := range before {
		if _, ok := after[uid]; !ok {
			missing++
		}
	}

	return missing
}

var _ = Describe("Operator Stress", Label("e2e"), func() {
	When("creating and deleting a cluster multiple times", func() {
		It("should create a healthy and available cluster", func(ctx SpecContext) {
			experiment := newExperiment(
				fmt.Sprintf(
					"Create and delete a cluster (pinned main container image: %t)",
					factory.UsePinnedMonitorVersion(),
				),
				createDuration,
				deleteDuration,
			)

			// Since Ginkgo doesn't support what we want, we run this multiple times.
			// We create and delete a cluster multiple times to ensure we don't have any flaky behaviour in the operator.
			for range runs {
				var fdbCluster *fixtures.FdbCluster
				measureDuration(experiment, createDuration, func() {
					fdbCluster = factory.CreateFdbCluster(ctx,
						fixtures.DefaultClusterConfig(false),
					)
				})
				Expect(fdbCluster.IsAvailable(ctx)).To(BeTrue())

				measureDuration(experiment, deleteDuration, func() {
					Expect(fdbCluster.Delete(ctx)).NotTo(HaveOccurred())
				})
			}
		})
	})

	When("replacing processes in a continuously manner", func() {
		var fdbCluster *fixtures.FdbCluster

		BeforeEach(func(ctx SpecContext) {
			fdbCluster = factory.CreateFdbCluster(ctx,
				fixtures.DefaultClusterConfig(false),
			)
			Expect(fdbCluster.InvariantClusterStatusAvailable()).NotTo(HaveOccurred())
		})

		AfterEach(func(ctx SpecContext) {
			Expect(fdbCluster.Delete(ctx)).NotTo(HaveOccurred())
		})

		It("should replace the targeted Pod", func(ctx SpecContext) {
			experiment := newExperiment(
				fmt.Sprintf(
					"Replace a Pod (pinned main container image: %t)",
					factory.UsePinnedMonitorVersion(),
				),
				replaceDuration,
			)

			// Since Ginkgo doesn't support what we want, we run this multiple times.
			for range runs {
				Expect(fdbCluster.ClearProcessGroupsToRemove(ctx)).ShouldNot(HaveOccurred())
				pod := factory.ChooseRandomPod(fdbCluster.GetPods(ctx))
				measureDuration(experiment, replaceDuration, func() {
					fdbCluster.ReplacePod(ctx, *pod, true)
				})
			}
		})
	})

	When("creating and upgrading a cluster multiple times", func() {
		DescribeTable(
			"should create and upgrade the cluster",
			func(ctx SpecContext, beforeVersion string, targetVersion string) {
				experiment := newExperiment(
					getUpgradeDescription("Upgrade", beforeVersion, targetVersion),
					createDuration,
					upgradeDuration,
					recoveries,
					generationDelta,
					lateGeneration,
					recreatedPods,
					totalPods,
				)

				// Since Ginkgo doesn't support what we want, we run this multiple times.
				for range runs {
					config := fixtures.DefaultClusterConfig(false)
					config.Version = new(beforeVersion)

					var fdbCluster *fixtures.FdbCluster
					measureDuration(experiment, createDuration, func() {
						fdbCluster = factory.CreateFdbCluster(ctx, config)
					})

					getGeneration := func() int {
						return fdbCluster.GetStatus(ctx).Cluster.Generation
					}

					// Wait until the cluster is stable, otherwise recoveries that are not part of the upgrade could be counted.
					initialPods := getPodUIDs(ctx, fdbCluster)
					initialGeneration := waitForStableGeneration(ctx, getGeneration)
					experiment.RecordValue(
						totalPods,
						float64(len(initialPods)),
						gmeasure.Units("Pods"),
						gmeasure.Precision(0),
					)

					measureDuration(experiment, upgradeDuration, func() {
						fdbCluster.UpgradeAndVerify(ctx, targetVersion)
					})

					// Every recovery increases the generation by two. The raw increase is recorded as well, because the number of
					// recoveries is rounded down.
					finalGeneration := getGeneration()
					increase := finalGeneration - initialGeneration
					experiment.RecordValue(
						recoveries,
						float64(increase/2),
						gmeasure.Units("recoveries"),
						gmeasure.Precision(0),
					)
					experiment.RecordValue(
						generationDelta,
						float64(increase),
						gmeasure.Units("generations"),
						gmeasure.Precision(0),
					)

					// Count the Pods that existed before the upgrade and are gone now. This includes Pods that were
					// recreated and Pods that were replaced.
					experiment.RecordValue(
						recreatedPods,
						float64(countMissing(initialPods, getPodUIDs(ctx, fdbCluster))),
						gmeasure.Units("Pods"),
						gmeasure.Precision(0),
					)

					// Recoveries after the upgrade finished are not part of the upgrade, e.g. if a Pod of the transaction
					// system is deleted. They are recorded separately.
					experiment.RecordValue(
						lateGeneration,
						float64(waitForStableGeneration(ctx, getGeneration)-finalGeneration),
						gmeasure.Units("generations"),
						gmeasure.Precision(0),
					)

					Expect(fdbCluster.Delete(ctx)).NotTo(HaveOccurred())
				}
			},
			EntryDescription("Upgrade from %[1]s to %[2]s"),
			fixtures.GenerateUpgradeTableEntries(testOptions),
		)
	})

	When("creating and upgrading a multi-region cluster multiple times", func() {
		DescribeTable(
			"should create and upgrade the multi-region cluster",
			func(ctx SpecContext, beforeVersion string, targetVersion string) {
				experiment := newExperiment(
					getUpgradeDescription(
						"Upgrade of a multi-region cluster",
						beforeVersion,
						targetVersion,
					),
					createDuration,
					upgradeDuration,
					recoveries,
					generationDelta,
					lateGeneration,
					recreatedPods,
					totalPods,
				)

				// Since Ginkgo doesn't support what we want, we run this multiple times.
				for range haRuns {
					config := fixtures.DefaultClusterConfigWithHaMode(
						fixtures.HaFourZoneSingleSat,
						false,
					)
					config.Version = new(beforeVersion)

					var haCluster *fixtures.HaFdbCluster
					measureDuration(experiment, createDuration, func() {
						haCluster = factory.CreateFdbHaCluster(ctx, config)
					})

					getGeneration := func() int {
						return haCluster.GetPrimary().GetStatus(ctx).Cluster.Generation
					}

					// Wait until the cluster is stable, otherwise recoveries that are not part of the upgrade could be counted.
					initialPods := getHaPodUIDs(ctx, haCluster)
					initialGeneration := waitForStableGeneration(ctx, getGeneration)
					experiment.RecordValue(
						totalPods,
						float64(len(initialPods)),
						gmeasure.Units("Pods"),
						gmeasure.Precision(0),
					)

					measureDuration(experiment, upgradeDuration, func() {
						Expect(
							haCluster.UpgradeCluster(ctx, targetVersion, true),
						).NotTo(HaveOccurred())
						haCluster.VerifyVersion(ctx, targetVersion)
					})

					// Every recovery increases the generation by two. The raw increase is recorded as well, because the number of
					// recoveries is rounded down.
					finalGeneration := getGeneration()
					increase := finalGeneration - initialGeneration
					experiment.RecordValue(
						recoveries,
						float64(increase/2),
						gmeasure.Units("recoveries"),
						gmeasure.Precision(0),
					)
					experiment.RecordValue(
						generationDelta,
						float64(increase),
						gmeasure.Units("generations"),
						gmeasure.Precision(0),
					)

					// Count the Pods of all clusters that existed before the upgrade and are gone now.
					experiment.RecordValue(
						recreatedPods,
						float64(countMissing(initialPods, getHaPodUIDs(ctx, haCluster))),
						gmeasure.Units("Pods"),
						gmeasure.Precision(0),
					)

					// Recoveries after the upgrade finished are not part of the upgrade, e.g. if a Pod of the transaction
					// system is deleted. They are recorded separately.
					experiment.RecordValue(
						lateGeneration,
						float64(waitForStableGeneration(ctx, getGeneration)-finalGeneration),
						gmeasure.Units("generations"),
						gmeasure.Precision(0),
					)

					haCluster.Delete(ctx)
				}
			},
			EntryDescription("Upgrade from %[1]s to %[2]s"),
			fixtures.GenerateUpgradeTableEntries(testOptions),
		)
	})
})
