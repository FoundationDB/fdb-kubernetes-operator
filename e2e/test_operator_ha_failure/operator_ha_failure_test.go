/*
 * operator_ha_failure_test.go
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

package operatorhafailure

/*
This test suite includes functional tests to ensure normal operational tasks are working fine.
Those tests include replacements of healthy or fault Pods and setting different configurations.

The assumption is that every test case reverts the changes that were done on the cluster.
In order to improve the test speed we only create one FoundationDB HA cluster initially.
This cluster will be used for all tests.
*/

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"log"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.HaFdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbHaCluster(fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false), factory.GetClusterOptions()...)

	// In order to test the robustness of the operator we try to kill the operator Pods every minute.
	if factory.ChaosTestsEnabled() {
		for _, cluster := range fdbCluster.GetAllClusters() {
			factory.ScheduleInjectPodKill(
				fixtures.GetOperatorSelector(cluster.Namespace()),
				"*/2 * * * *",
				chaosmesh.OneMode,
			)
		}
	}
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

// This test suite contains destructive tests which will break the cluster after the test is done.
// Those tests are currently not run by our CI, as those tests might be flaky. The intention of those tests
// is to simplify the manual testing of different scenarios, that could lead to data loss.
var _ = Describe("Operator HA Failure tests", Label("e2e"), func() {
	FWhen("simulating data-loss during fail-over", func() {
		var experiments []*fixtures.ChaosMeshExperiment
		var keyValues []fixtures.KeyValue
		var prefix byte = 'a'

		BeforeEach(func() {
			if factory.ChaosTestsEnabled() {
				Skip("chaos tests are required for this test suite")
			}
			// The idea of this test case is to partition the primary and primary satellite from the remote and remote
			// satellite. Then the cluster will be loaded with data, which will work because the primary and primary
			// satellite are available. Before deleting the partition we destroy the primary and the primary satellite
			// and then we forcefully recover the remote side, which will cause data loss, as the mutation are not synced
			// from the primary side to the remove (partitioned).
			primary := fdbCluster.GetPrimary()
			primarySatellite := fdbCluster.GetPrimarySatellite()
			remote := fdbCluster.GetRemote()
			remoteSatellite := fdbCluster.GetRemoteSatellite()
			experiments = make([]*fixtures.ChaosMeshExperiment, 0, 2)
			// Inject a partition between primary and the remote + remote satellite
			experiments = append(experiments, factory.InjectPartitionBetween(
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{primary.Namespace()},
						LabelSelectors: primary.GetCachedCluster().GetMatchLabels(),
					},
				},
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces: []string{
							remote.Namespace(),
							remoteSatellite.Namespace(),
						},
					},
				}))

			// Inject a partition between primary satellite and the remote + remote satellite
			experiments = append(experiments, factory.InjectPartitionBetween(
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces:     []string{primarySatellite.Namespace()},
						LabelSelectors: primarySatellite.GetCachedCluster().GetMatchLabels(),
					},
				},
				chaosmesh.PodSelectorSpec{
					GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
						Namespaces: []string{
							remote.Namespace(),
							remoteSatellite.Namespace(),
						},
					},
				}))

			time.Sleep(10 * time.Second)

			keyValues = primary.GenerateRandomValues(10, prefix)
			primary.WriteKeyValuesWithTimeout(keyValues, 120)
			// Destroy primary and primary satellite (should have mutations that are not present in the remote side).
			primary.SetSkipReconciliation(true)
			primarySatellite.SetSkipReconciliation(true)
			// We also destroy the remote satellite, it shouldn't matter in this case as the remote satellite
			// has no data anyways. But the idea here is to reduce the possible interaction between the remote
			// and the remote satellite during the forced fail-over.
			remoteSatellite.SetSkipReconciliation(true)

			// We could probably simulate that with the suspend command, but destroying the pods is a more robust solution.
			var wg errgroup.Group
			log.Println("Delete Pods in primary")
			wg.Go(func() error {
				for _, pod := range primary.GetPods().Items {
					factory.DeletePod(&pod)
				}

				return nil
			})

			log.Println("Delete Pods in primary satellite")
			wg.Go(func() error {
				for _, pod := range primarySatellite.GetPods().Items {
					factory.DeletePod(&pod)
				}

				return nil
			})

			log.Println("Delete Pods in remote satellite")
			wg.Go(func() error {
				for _, pod := range remoteSatellite.GetPods().Items {
					factory.DeletePod(&pod)
				}

				return nil
			})

			Expect(wg.Wait()).NotTo(HaveOccurred())
			// Wait a short amount of time to let the cluster see that the primary and primary satellite is down.
			time.Sleep(30 * time.Second)

			// Ensure the cluster is unavailable.
			Eventually(func() bool {
				return remote.GetStatus().Client.DatabaseStatus.Available
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeFalse())
		})

		AfterEach(func() {
			for _, experiment := range experiments {
				factory.DeleteChaosMeshExperimentSafe(experiment)
			}
		})

		It("should fail-over and cause data loss", func() {
			remote := fdbCluster.GetRemote()
			// Pick one operator pod and execute the recovery command
			operatorPod := factory.RandomPickOnePod(factory.GetOperatorPods(remote.Namespace()).Items)
			log.Println("operatorPod:", operatorPod.Name)
			stdout, stderr, err := factory.ExecuteCmdOnPod(context.Background(), &operatorPod, "manager", fmt.Sprintf("kubectl-fdb -n %s recover-multi-region-cluster --version-check=false --wait=false %s", remote.Namespace(), remote.Name()), false)
			log.Println("stdout:", stdout, "stderr:", stderr)
			Expect(err).NotTo(HaveOccurred())

			// Ensure the cluster is available again.
			Eventually(func() bool {
				return remote.GetStatus().Client.DatabaseStatus.Available
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

			// Ensure we lost some data.
			Expect(remote.GetRange([]byte{prefix}, 25, 60)).Should(BeEmpty())
		})
	})
})
