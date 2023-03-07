package operatorcreationvelocity

import (
	"flag"
	"github.com/onsi/ginkgo/v2/types"
	"log"
	"testing"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var (
	factory     *fixtures.Factory
	namespace   string
	testOptions *fixtures.FactoryOptions
)

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	// Create a namespace and wait until the operator Pods are running. We don't want to measure that time.
	namespace = factory.SingleNamespace()
	factory.WaitUntilOperatorPodsRunning(namespace)
})

var _ = AfterSuite(func() {
	factory.Shutdown()
})

var _ = Describe("Test Operator Velocity", func() {
	When("creating a single FDB cluster", func() {
		It("should roll out knob changes within expected time", func() {
			startTime := time.Now()

			fdbCluster := factory.CreateFdbCluster(
				&fixtures.ClusterConfig{
					Namespace:       namespace,
					CreationTracker: fixtures.NewDefaultCreationTrackerLogger(),
				},
				factory.GetClusterOptions(
					fixtures.WithOneMinuteMinimumUptimeSecondsForBounce,
				)...,
			)

			runTime := time.Since(startTime)
			log.Println("Single-DC cluster creation took: ", runTime.String())
			Expect(fdbCluster.Destroy()).ToNot(HaveOccurred())
		})
	})

	When("creating a multi-DC FDB cluster", func() {
		It("benchmark multi-DC cluster creation", func() {
			startTime := time.Now()

			haCluster := factory.CreateFdbHaCluster(
				&fixtures.ClusterConfig{
					HaMode:          fixtures.HaFourZoneSingleSat,
					CreationTracker: fixtures.NewDefaultCreationTrackerLogger(),
				},
				factory.GetClusterOptions()...,
			)

			runTime := time.Since(startTime)
			log.Println("Multi-DC cluster creation took: ", runTime.String())
			haCluster.Delete()
		})
	})
})
