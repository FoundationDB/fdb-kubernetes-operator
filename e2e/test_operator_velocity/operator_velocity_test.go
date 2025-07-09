/*
 * operator_velocity_test.go
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

package operatorvelocity

/*
This test suite includes tests to validate the rollout time of new knobs under different conditions.
*/

import (
	"log"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
)

func init() {
	testOptions = fixtures.InitFlags()
}

var (
	factory                         *fixtures.Factory
	fdbCluster                      *fixtures.HaFdbCluster
	testOptions                     *fixtures.FactoryOptions
	initialGeneralCustomParameters  fdbv1beta2.FoundationDBCustomParameters
	initialStorageCustomParameters  fdbv1beta2.FoundationDBCustomParameters
	newGeneralCustomParameters      fdbv1beta2.FoundationDBCustomParameters
	newStorageCustomParameters      fdbv1beta2.FoundationDBCustomParameters
	normalKnobRolloutTimeoutSeconds int
)

func CheckKnobRollout(
	fdbCluster *fixtures.HaFdbCluster,
	expectedGeneralCustomParameters fdbv1beta2.FoundationDBCustomParameters,
	expectedStorageCustomParameters fdbv1beta2.FoundationDBCustomParameters,
	knobRolloutTimeoutSeconds int,
	totalGeneralProcessCount int,
	totalStorageProcessCount int,
) {
	primary := fdbCluster.GetPrimary()
	initialGeneration := primary.GetStatus().Cluster.Generation

	startTime := time.Now()
	timeoutTime := startTime.Add(time.Duration(knobRolloutTimeoutSeconds) * time.Second)

	Eventually(func(g Gomega) {
		status := primary.GetStatus()
		commandLines := primary.GetCommandlineForProcessesPerClassWithStatus(status)
		var generalProcessCounts, storageProcessCounts int

		for pClass, cmdLines := range commandLines {
			if pClass == fdbv1beta2.ProcessClassStorage {
				storageProcessCounts += countMatchingCommandLines(
					cmdLines,
					expectedStorageCustomParameters,
				)
				continue
			}

			generalProcessCounts += countMatchingCommandLines(
				cmdLines,
				expectedGeneralCustomParameters,
			)
		}

		log.Println(
			"general processes with the new knob:",
			generalProcessCounts,
			"expected",
			totalGeneralProcessCount,
			"storage processes with the new knob:",
			storageProcessCounts,
			"expected",
			totalStorageProcessCount,
			"generation",
			status.Cluster.Generation,
			"time until timeout",
			time.Until(timeoutTime).Seconds(),
		)

		g.Expect(generalProcessCounts).To(BeNumerically("==", totalGeneralProcessCount))
		g.Expect(storageProcessCounts).To(BeNumerically("==", totalStorageProcessCount))
	}).WithTimeout(time.Until(timeoutTime)).WithPolling(15 * time.Second).Should(Succeed())

	rolloutDuration := time.Since(startTime)
	finalGeneration := primary.GetStatus().Cluster.Generation

	log.Println(
		"Knob rollout took",
		rolloutDuration.String(),
		"initialGeneration",
		initialGeneration,
		"finalGeneration",
		finalGeneration,
		"recoveryCount",
		(finalGeneration-initialGeneration)/2,
	)
	// If the synchronization mode is global, we expect to see only a single recovery. Since those tests are running on
	// a real cluster we add some additional buffer of one additional recovery. We check for an increase of 4 generation
	// because FDB increase the current generation by 2 if a recovery is triggered.
	if primary.GetCluster().GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		Expect(finalGeneration - initialGeneration).To(BeNumerically("<=", 4))
	}
}

func countMatchingCommandLines(
	cmdLines []string,
	expected fdbv1beta2.FoundationDBCustomParameters,
) int {
	var count int

	for _, cmdLine := range cmdLines {
		if parametersExists(cmdLine, expected) {
			count++
		}
	}

	return count
}

func parametersExists(cmdLine string, params fdbv1beta2.FoundationDBCustomParameters) bool {
	for _, param := range params {
		if !strings.Contains(cmdLine, string(param)) {
			return false
		}
	}
	return true
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbHaCluster(
		fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneDoubleSat, false),
		factory.GetClusterOptions(fixtures.WithOneMinuteMinimumUptimeSecondsForBounce)...,
	)

	// We only have to fetch the data once
	initialGeneralCustomParameters = fdbCluster.GetPrimary().GetCustomParameters(
		fdbv1beta2.ProcessClassGeneral,
	)
	initialStorageCustomParameters = fdbCluster.GetPrimary().GetCustomParameters(
		fdbv1beta2.ProcessClassStorage,
	)

	newGeneralCustomParameters = append(
		initialGeneralCustomParameters,
		"knob_max_trace_lines=1000000",
	)
	newStorageCustomParameters = append(
		initialStorageCustomParameters,
		"knob_max_trace_lines=1000000",
	)

	if factory.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		// The knobs will be rolled out with a single cluster-wide restart.
		normalKnobRolloutTimeoutSeconds = 240
	} else {
		// The knobs will be rolled out cluster by cluster with multiple restarts. Those restarts can have the side-effect
		// that coordinators are changing because some processes are restarting slower.
		normalKnobRolloutTimeoutSeconds = 600
	}
})

var _ = AfterSuite(func() {
	factory.Shutdown()
})

var _ = Describe("Test Operator Velocity", Label("e2e", "nightly"), func() {
	var totalGeneralProcessCount, totalStorageProcessCount int

	BeforeEach(func() {
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())

		totalGeneralProcessCount = 0
		totalStorageProcessCount = 0

		for _, cluster := range fdbCluster.GetAllClusters() {
			processCnt, err := cluster.GetCluster().GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			totalStorageProcessCount += processCnt.Storage * cluster.GetStorageServerPerPod()
			totalGeneralProcessCount += processCnt.Total() - processCnt.Storage
		}
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpStateHaCluster(fdbCluster)
		}
		Expect(factory.CleanupChaosMeshExperiments()).ToNot(HaveOccurred())
		log.Println("Reverting knobs back to initial settings")
		start := time.Now()
		Expect(
			fdbCluster.SetCustomParameters(
				map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
					fdbv1beta2.ProcessClassGeneral: initialGeneralCustomParameters,
					fdbv1beta2.ProcessClassStorage: initialStorageCustomParameters,
				},
				true,
			),
		).To(Succeed())

		log.Println("Knob rollout took", time.Since(start).String())
	})

	When("a knob is changed", func() {
		It("should roll out knob changes within expected time", func() {
			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassGeneral: newGeneralCustomParameters,
						fdbv1beta2.ProcessClassStorage: newStorageCustomParameters,
					},
					false,
				),
			).To(Succeed())

			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds,
				totalGeneralProcessCount,
				totalStorageProcessCount)
		})
	})

	When("a knob is changed and the cluster is bounced", func() {
		It("should roll out knob changes within expected time", func() {
			Expect(fdbCluster.GetPrimary().BounceClusterWithoutWait()).ToNot(HaveOccurred())
			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassGeneral: newGeneralCustomParameters,
						fdbv1beta2.ProcessClassStorage: newStorageCustomParameters,
					},
					false,
				),
			).To(Succeed())

			// Make sure to wait for the cluster to become available again.
			Eventually(func() bool {
				return fdbCluster.GetPrimary().IsAvailable()
			}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

			cluster := fdbCluster.GetPrimary().GetCluster()
			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds+cluster.GetMinimumUptimeSecondsForBounce(),
				totalGeneralProcessCount,
				totalStorageProcessCount)
		})
	})

	// See: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/2228
	PWhen("a knob is changed and a single process in the primary region is partitioned", func() {
		var initialReplaceTime time.Duration
		var exp *fixtures.ChaosMeshExperiment

		BeforeEach(func() {
			initialReplaceTime = time.Duration(pointer.IntDeref(
				fdbCluster.GetPrimary().
					GetClusterSpec().
					AutomationOptions.Replacements.FailureDetectionTimeSeconds,
				90,
			)) * time.Second
			Expect(
				fdbCluster.GetPrimary().SetAutoReplacements(false, 1*time.Hour),
			).NotTo(HaveOccurred())
			// Partition a storage Pod from the rest of the cluster
			pod := factory.ChooseRandomPod(fdbCluster.GetPrimary().GetStoragePods())
			log.Printf("partition Pod: %s", pod.Name)
			exp = factory.InjectPartition(fixtures.PodSelector(pod))
		})

		AfterEach(func() {
			factory.DeleteChaosMeshExperimentSafe(exp)
			Expect(
				fdbCluster.GetPrimary().SetAutoReplacements(true, initialReplaceTime),
			).NotTo(HaveOccurred())
		})

		It("should roll out knob changes within expected time", func() {
			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassGeneral: newGeneralCustomParameters,
						fdbv1beta2.ProcessClassStorage: newStorageCustomParameters,
					},
					false,
				),
			).To(Succeed())

			// We have to increase the timeout for this test as the maximum time this test might take, depends on the
			// ordering of the operators taking the kill action. If the operator in the primary cluster is the last one
			// the test will pass fairly fast. If the primary operator is not the last operator, this test take longer
			// as the partitioned Pod will block the release of the lock and the next operator has to wait until the
			// lock is expired.
			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds+int(
					fdbCluster.GetPrimary().GetCluster().GetLockDuration().Seconds(),
				),
				totalGeneralProcessCount,
				totalStorageProcessCount-(1*fdbCluster.GetPrimary().GetStorageServerPerPod()),
			)
		})
	})

	When("a knob is changed and a single pod in the primary region is replaced", func() {
		BeforeEach(func() {
			// Start replacement of a random storage Pod.
			pod := factory.ChooseRandomPod(fdbCluster.GetPrimary().GetStoragePods())
			log.Println("Start replacing", pod.Name)
			fdbCluster.GetPrimary().ReplacePod(*pod, false)
		})

		It("should roll out knob changes within expected time", func() {
			Expect(
				fdbCluster.SetCustomParameters(
					map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters{
						fdbv1beta2.ProcessClassGeneral: newGeneralCustomParameters,
						fdbv1beta2.ProcessClassStorage: newStorageCustomParameters,
					},
					false,
				),
			).To(Succeed())

			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds+int(
					fdbCluster.GetPrimary().GetCluster().GetLockDuration().Seconds(),
				),
				totalGeneralProcessCount,
				totalStorageProcessCount,
			)
		})
	})
})
