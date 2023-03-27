package operatorvelocity

import (
	"flag"
	"github.com/onsi/ginkgo/v2/types"
	"log"
	"strings"
	"testing"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
)

func init() {
	// TODO(johscheuer): move this into a common method to make it easier to be consumed
	testing.Init()
	_, err := types.NewAttachedGinkgoFlagSet(flag.CommandLine, types.GinkgoFlags{}, nil, types.GinkgoFlagSections{}, types.GinkgoFlagSection{})
	if err != nil {
		log.Fatal(err)
	}
	testOptions = &fixtures.FactoryOptions{}
	testOptions.BindFlags(flag.CommandLine)
	flag.Parse()
}

const (
	// The time will be the minimum uptime * no. of clusters e.g. 4 for an HA cluster
	// If we have a minimum uptime of 60 seconds we should target 240 seconds as the worst case.
	// In most cases the initial/first restart will be faster.
	// TODO (johscheuer): Change this back once https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1361 is fixed.
	normalKnobRolloutTimeoutSeconds = 420 // 240
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

	Eventually(func() bool {
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

		return generalProcessCounts == totalGeneralProcessCount &&
			storageProcessCounts == totalStorageProcessCount
	}).WithTimeout(time.Until(timeoutTime)).WithPolling(15 * time.Second).Should(BeTrue())

	log.Println("Knob rollout took", time.Since(startTime).Seconds(), "seconds")
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

var _ = Describe("Test Operator Velocity", Label("e2e"), func() {
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
			var err error
			exp = factory.InjectPartition(fixtures.PodSelector(pod))
			Expect(err).ShouldNot(HaveOccurred())
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

			CheckKnobRollout(
				fdbCluster,
				newGeneralCustomParameters,
				newStorageCustomParameters,
				normalKnobRolloutTimeoutSeconds)
		})
	})
})
