/*
 * operator_plugin_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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
This test suite includes functional tests for the kubectl-fdb plugin.
*/

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory        *fixtures.Factory
	fdbCluster     *fixtures.HaFdbCluster
	testOptions    *fixtures.FactoryOptions
	clusterConfig  *fixtures.ClusterConfig
	clusterOptions []fixtures.ClusterOption
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	clusterOptions = factory.GetClusterOptions()
	clusterConfig = fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false)
	fdbCluster = factory.CreateFdbHaCluster(clusterConfig, clusterOptions...)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator Plugin", Label("e2e", "pr"), func() {
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpStateHaCluster(fdbCluster)
		}
	})

	When("getting the plugin version from the operator pod", func() {
		It("should print the version", func() {
			// Pick one operator pod and execute the kubectl version command to ensure that kubectl-fdb is present
			// and can be executed.
			operatorPod := factory.RandomPickOnePod(factory.GetOperatorPods(fdbCluster.GetPrimary().Namespace()).Items)
			log.Println("operatorPod", operatorPod.Name)
			Eventually(func(g Gomega) string {
				stdout, stderr, err := factory.ExecuteCmdOnPod(context.Background(), &operatorPod, "manager", fmt.Sprintf("kubectl-fdb -n %s --version-check=false version", fdbCluster.GetPrimary().Namespace()), false)
				g.Expect(err).NotTo(HaveOccurred(), stderr)
				return stdout
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(And(ContainSubstring("kubectl-fdb:"), ContainSubstring("foundationdb-operator:")))
		})
	})

	When("all Pods in the primary and satellites are down", func() {
		BeforeEach(func() {
			// This tests is a destructive test where the cluster will stop working for some period.
			primary := fdbCluster.GetPrimary()
			primary.SetSkipReconciliation(true)

			primarySatellite := fdbCluster.GetPrimarySatellite()
			primarySatellite.SetSkipReconciliation(true)

			remoteSatellite := fdbCluster.GetRemoteSatellite()
			remoteSatellite.SetSkipReconciliation(true)

			log.Println("Delete Pods in primary")
			primaryPods := primary.GetPods()
			for _, pod := range primaryPods.Items {
				factory.DeletePod(&pod)
			}

			log.Println("Delete Pods in primary satellite")
			primarySatellitePods := primarySatellite.GetPods()
			for _, pod := range primarySatellitePods.Items {
				factory.DeletePod(&pod)
			}

			log.Println("Delete Pods in remote satellite")
			remoteSatellitePods := remoteSatellite.GetPods()
			for _, pod := range remoteSatellitePods.Items {
				factory.DeletePod(&pod)
			}

			// Wait a short amount of time to let the cluster see that the primary and primary satellite is down.
			time.Sleep(5 * time.Second)

			remote := fdbCluster.GetRemote()
			// Ensure the cluster is unavailable.
			Eventually(func() bool {
				return remote.GetStatus().Client.DatabaseStatus.Available
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeFalse())
		})

		AfterEach(func() {
			log.Println("Recreate cluster")
			// Delete the broken cluster.
			fdbCluster.Delete()
			// Recreate the cluster to make sure  the next tests can proceed
			fdbCluster = factory.CreateFdbHaCluster(clusterConfig, clusterOptions...)
		})

		It("should recover the coordinators", func() {
			remote := fdbCluster.GetRemote()
			// Pick one operator pod and execute the recovery command
			operatorPod := factory.RandomPickOnePod(factory.GetOperatorPods(remote.Namespace()).Items)
			log.Println("operatorPod:", operatorPod.Name)
			Eventually(func() error {
				stdout, stderr, err := factory.ExecuteCmdOnPod(context.Background(), &operatorPod, "manager", fmt.Sprintf("kubectl-fdb -n %s recover-multi-region-cluster --version-check=false --wait=false %s", remote.Namespace(), remote.Name()), false)
				log.Println("stdout:", stdout, "stderr:", stderr)
				return err
			}).WithTimeout(30 * time.Minute).WithPolling(5 * time.Minute).ShouldNot(HaveOccurred())

			// Ensure the cluster is available again.
			Eventually(func() bool {
				return remote.GetStatus().Client.DatabaseStatus.Available
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())
		})
	})
})
