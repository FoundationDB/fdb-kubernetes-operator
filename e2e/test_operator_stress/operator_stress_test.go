package operatorstress

import (
	"flag"
	"log"
	"testing"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

var (
	factory     *fixtures.Factory
	testOptions *fixtures.FactoryOptions
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

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
})

var _ = AfterSuite(func() {
	factory.Shutdown()
})

var _ = Describe("Operator Stress", Label("e2e"), func() {
	When("creating and deleting a cluster multiple times", func() {
		It("should create a healthy and available cluster", func() {
			// Since Ginkgo doesn't support what we want, we run this multiple times.
			// We create and delete a cluster 10 times to ensure we don't have any flaky behaviour in the operator.
			for i := 0; i < 10; i++ {
				fdbCluster := factory.CreateFdbCluster(
					fixtures.DefaultClusterConfig(false),
					factory.GetClusterOptions()...,
				)
				Expect(fdbCluster.IsAvailable()).To(BeTrue())
				Expect(fdbCluster.Destroy()).NotTo(HaveOccurred())
			}
		})
	})

	When("replacing processes in a continuously manner", func() {
		var fdbCluster *fixtures.FdbCluster

		BeforeEach(func() {
			fdbCluster = factory.CreateFdbCluster(
				fixtures.DefaultClusterConfig(false),
				factory.GetClusterOptions()...,
			)
			Expect(fdbCluster.InvariantClusterStatusAvailable()).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.Destroy()).NotTo(HaveOccurred())
		})

		It("should replace the targeted Pod", func() {
			// Since Ginkgo doesn't support what we want, we run this multiple times.
			var err error
			for i := 0; i < 10; i++ {
				err = fdbCluster.ClearProcessGroupsToRemove()
				Expect(err).ShouldNot(HaveOccurred())
				pod := fixtures.ChooseRandomPod(fdbCluster.GetPods())
				fdbCluster.ReplacePod(*pod, true)
			}
		})
	})
})
