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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
)

func init() {
	testOptions = fixtures.InitFlags()
}

const (
	// The time will be the minimum uptime * no. of clusters e.g. 4 for an HA cluster
	// If we have a minimum uptime of 60 seconds we should target 240 seconds as the worst case.
	// In most cases the initial/first restart will be faster.
	// TODO (johscheuer): Change this back once https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1361 is fixed.
	normalKnobRolloutTimeoutSeconds = 600 // 240
)

var (
	factory                        *fixtures.Factory
	fdbCluster                     *fixtures.HaFdbCluster
	testOptions                    *fixtures.FactoryOptions
	initialGeneralCustomParameters fdbv1beta2.FoundationDBCustomParameters
	initialStorageCustomParameters fdbv1beta2.FoundationDBCustomParameters
	newGeneralCustomParameters     fdbv1beta2.FoundationDBCustomParameters
	newStorageCustomParameters     fdbv1beta2.FoundationDBCustomParameters
)

func CheckKnobRollout(
	fdbCluster *fixtures.HaFdbCluster,
	expectedGeneralCustomParameters fdbv1beta2.FoundationDBCustomParameters,
	expectedStorageCustomParameters fdbv1beta2.FoundationDBCustomParameters,
	knobRolloutTimeoutSeconds int,
) {
	startTime := time.Now()
	timeoutTime := startTime.Add(time.Duration(knobRolloutTimeoutSeconds) * time.Second)

	Eventually(func(g Gomega) bool {
		commandLines := fdbCluster.GetPrimary().GetCommandlineForProcessesPerClass()
		var generalProcessCounts, storageProcessCounts, totalGeneralProcessCount, totalStorageProcessCount int

		for pClass, cmdLines := range commandLines {
			if pClass == fdbv1beta2.ProcessClassStorage {
				storageProcessCounts += countMatchingCommandLines(
					cmdLines,
					expectedStorageCustomParameters,
				)
				totalStorageProcessCount += len(cmdLines)
				continue
			}

			generalProcessCounts += countMatchingCommandLines(
				cmdLines,
				expectedGeneralCustomParameters,
			)
			totalGeneralProcessCount += len(cmdLines)
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
			"time until timeout",
			time.Since(startTime).Seconds(),
		)

		g.Expect(generalProcessCounts).To(BeNumerically("==", totalGeneralProcessCount))
		g.Expect(storageProcessCounts).To(BeNumerically("==", totalStorageProcessCount))

		return true
	}).WithTimeout(time.Until(timeoutTime)).WithPolling(15 * time.Second).Should(BeTrue())

	log.Println("Knob rollout took", time.Since(startTime).String())
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
})

var _ = AfterSuite(func() {
	factory.Shutdown()
})

var _ = Describe("Test Operator Velocity", Label("e2e", "nightly"), func() {
	BeforeEach(func() {
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpStateHaCluster(fdbCluster)
		}
		Expect(factory.CleanupChaosMeshExperiments()).ToNot(HaveOccurred())
		Expect(
			fdbCluster.SetCustomParameters(
				fdbv1beta2.ProcessClassGeneral,
				initialGeneralCustomParameters,
				false,
			),
		).NotTo(HaveOccurred())
		Expect(
			fdbCluster.SetCustomParameters(
				fdbv1beta2.ProcessClassStorage,
				initialStorageCustomParameters,
				true,
			),
		).NotTo(HaveOccurred())
	})

	When("a knob is changed", func() {
		It("should roll out knob changes within expected time", func() {
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassGeneral,
					newGeneralCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassStorage,
					newStorageCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())

			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds)
		})
	})

	When("a knob is changed and the cluster is bounced", func() {
		It("should roll out knob changes within expected time", func() {
			Expect(fdbCluster.GetPrimary().BounceClusterWithoutWait()).ToNot(HaveOccurred())
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassGeneral,
					newGeneralCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassStorage,
					newStorageCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())

			cluster := fdbCluster.GetPrimary().GetCluster()
			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds+cluster.GetMinimumUptimeSecondsForBounce())
		})
	})

	When("a knob is changed and a single process in the primary region is partitioned", func() {
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
			pod := fixtures.ChooseRandomPod(fdbCluster.GetPrimary().GetStoragePods())
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
					fdbv1beta2.ProcessClassGeneral,
					newGeneralCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassStorage,
					newStorageCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())

			// We have to increase the timeout for this test as the maximum time this test might take, depends on the
			// ordering of the operators taking the kill action. If the operator in the primary cluster is the last one
			// the test will pass fairly fast. If the primary operator is not the last operator, this test take longer
			// as the partitioned Pod will block the release of the lock and the next operator has to wait until the
			// lock is expired.
			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds+int(fdbCluster.GetPrimary().GetCluster().GetLockDuration().Seconds()))
		})
	})

	When("a knob is changed and a single pod in the primary region is replaced", func() {
		BeforeEach(func() {
			// Start replacement of a random storage Pod.
			pod := fixtures.ChooseRandomPod(fdbCluster.GetPrimary().GetStoragePods())
			log.Println("Start replacing", pod.Name)
			fdbCluster.GetPrimary().ReplacePod(*pod, false)
		})

		It("should roll out knob changes within expected time", func() {
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassGeneral,
					newGeneralCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())
			Expect(
				fdbCluster.SetCustomParameters(
					fdbv1beta2.ProcessClassStorage,
					newStorageCustomParameters,
					false,
				),
			).NotTo(HaveOccurred())

			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds+int(fdbCluster.GetPrimary().GetCluster().GetLockDuration().Seconds()))
		})
	})
})
