/*
 * operator_plugin_test.go
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
This test suite includes functional tests for the kubectl-fdb plugin.
*/

import (
	"fmt"
	"log"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sync/errgroup"
)

var (
	factory       *fixtures.Factory
	testOptions   *fixtures.FactoryOptions
	clusterConfig *fixtures.ClusterConfig
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
})

var _ = AfterSuite(func(ctx SpecContext) {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown(ctx)
})

var _ = Describe("Operator Plugin", Label("e2e", "pr"), func() {
	When("getting the plugin version from the operator pod", func() {
		var fdbCluster *fixtures.FdbCluster

		BeforeEach(func(ctx SpecContext) {
			clusterConfig = fixtures.DefaultClusterConfig(false)
			fdbCluster = factory.CreateFdbCluster(ctx, clusterConfig)
		})

		AfterEach(func(ctx SpecContext) {
			Expect(fdbCluster.Delete(ctx)).NotTo(HaveOccurred())
		})

		It("should print the version", func(ctx SpecContext) {
			// Pick one operator pod and execute the kubectl version command to ensure that kubectl-fdb is present
			// and can be executed.
			operatorPod := factory.RandomPickOnePod(
				factory.GetOperatorPods(ctx, fdbCluster.Namespace()).Items,
			)
			log.Println("operatorPod:", operatorPod.Name)
			Eventually(func(g Gomega) string {
				stdout, stderr, err := factory.ExecuteCmdOnPod(
					ctx,
					&operatorPod,
					"manager",
					fmt.Sprintf(
						"kubectl-fdb -n %s --version-check=false version",
						fdbCluster.Namespace(),
					),
					false,
				)
				g.Expect(err).NotTo(HaveOccurred(), stderr)
				return stdout
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(And(ContainSubstring("kubectl-fdb build information:"), ContainSubstring("foundationdb-operator:")))
		})
	})

	When("all Pods in the primary and satellites are down", func() {
		var fdbCluster *fixtures.HaFdbCluster

		AfterEach(func(ctx SpecContext) {
			fdbCluster.Delete(ctx)
		})

		// Default case is to run with DNS enabled. The test case with IPs enabled can run into issues when
		// the underlying Kubernetes cluster deletes pods.
		// Because of the above issues the test case is currently disabled (marked as pending) and can be used
		// to run the test manually if needed.
		DescribeTableSubtree("should recover the coordinators", func(shouldUseDNS bool) {
			JustBeforeEach(func(ctx SpecContext) {
				clusterConfig = fixtures.DefaultClusterConfigWithHaMode(
					fixtures.HaFourZoneSingleSat,
					false,
				)
				fdbCluster = factory.CreateFdbHaCluster(ctx, clusterConfig)

				var errGroup errgroup.Group
				// Enable DNS names in the cluster file for the whole cluster.
				for _, cluster := range fdbCluster.GetAllClusters() {
					target := cluster
					errGroup.Go(func() error {
						return target.SetUseDNSInClusterFile(ctx, shouldUseDNS)
					})
				}
				Expect(errGroup.Wait()).NotTo(HaveOccurred())

				for _, cluster := range fdbCluster.GetAllClusters() {
					Expect(cluster.GetCluster(ctx).UseDNSInClusterFile()).To(Equal(shouldUseDNS))
				}

				// This tests is a destructive test where the cluster will stop working for some period.
				primary := fdbCluster.GetPrimary()
				primary.SetSkipReconciliation(ctx, true)

				primarySatellite := fdbCluster.GetPrimarySatellite()
				primarySatellite.SetSkipReconciliation(ctx, true)

				remoteSatellite := fdbCluster.GetRemoteSatellite()
				remoteSatellite.SetSkipReconciliation(ctx, true)

				remote := fdbCluster.GetRemote()
				remote.SetSkipReconciliation(ctx, true)

				var wg errgroup.Group
				log.Println("Delete Pods in primary")
				wg.Go(func() error {
					return factory.GetControllerRuntimeClient().
						DeleteAllOf(ctx, &corev1.Pod{}, ctrlClient.MatchingLabels(primary.GetResourceLabels()), ctrlClient.InNamespace(primary.Namespace()))
				})

				log.Println("Delete Pods in primary satellite")
				wg.Go(func() error {
					return factory.GetControllerRuntimeClient().
						DeleteAllOf(ctx, &corev1.Pod{}, ctrlClient.MatchingLabels(primarySatellite.GetResourceLabels()), ctrlClient.InNamespace(primarySatellite.Namespace()))
				})

				log.Println("Delete Pods in remote satellite")
				wg.Go(func() error {
					return factory.GetControllerRuntimeClient().
						DeleteAllOf(ctx, &corev1.Pod{}, ctrlClient.MatchingLabels(remoteSatellite.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))
				})

				Expect(wg.Wait()).NotTo(HaveOccurred())
				// Wait a short amount of time to let the cluster see that the primary and primary satellite is down.
				time.Sleep(5 * time.Second)

				// Ensure that all the pods are deleted.
				Eventually(func(g Gomega) []corev1.Pod {
					pods := &corev1.PodList{}
					g.Expect(factory.GetControllerRuntimeClient().List(ctx, pods, ctrlClient.MatchingLabels(primary.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))).
						To(Succeed())

					return pods.Items
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeEmpty())

				Eventually(func(g Gomega) []corev1.Pod {
					pods := &corev1.PodList{}
					g.Expect(factory.GetControllerRuntimeClient().List(ctx, pods, ctrlClient.MatchingLabels(primarySatellite.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))).
						To(Succeed())

					return pods.Items
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeEmpty())

				Eventually(func(g Gomega) []corev1.Pod {
					pods := &corev1.PodList{}
					g.Expect(factory.GetControllerRuntimeClient().List(ctx, pods, ctrlClient.MatchingLabels(remoteSatellite.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))).
						To(Succeed())

					return pods.Items
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeEmpty())
			})

			It("should recover the coordinators", func(ctx SpecContext) {
				remote := fdbCluster.GetRemote()
				// Pick one operator pod and execute the recovery command
				operatorPod := factory.RandomPickOnePod(
					factory.GetOperatorPods(ctx, remote.Namespace()).Items,
				)
				log.Println("operatorPod:", operatorPod.Name)
				stdout, stderr, err := factory.ExecuteCmdOnPod(
					ctx,
					&operatorPod,
					"manager",
					fmt.Sprintf(
						"kubectl-fdb -n %s recover multi-region --version-check=false --wait=false %s",
						remote.Namespace(),
						remote.Name(),
					),
					false,
				)
				log.Println("stdout:", stdout, "stderr:", stderr)
				if shouldUseDNS && strings.Contains(stderr, "Error determining public address") {
					Skip(
						"plugin was not able to determine public address, this means that all coordinators are probably gone",
					)
				}
				Expect(err).NotTo(HaveOccurred())

				// Ensure the cluster is available again.
				Eventually(func() bool {
					return remote.GetStatus(ctx).Client.DatabaseStatus.Available
				}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

				remote.SetSkipReconciliation(ctx, false)
				// Recreate the operator pods to ensure they get the new connection string.
				factory.RecreateOperatorPods(ctx, remote.Namespace())
				// Ensure that the cluster is able to reconcile
				Expect(remote.WaitForReconciliation(ctx)).To(Succeed())

				var currentConnectionString string
				if shouldUseDNS {
					currentConnectionString = remote.GetStatus(ctx).Cluster.ConnectionString
				} else {
					currentConnectionString = remote.GetCluster(ctx).Status.ConnectionString
				}
				log.Println("new connection string:", currentConnectionString)
				connectionString, err := fdbv1beta2.ParseConnectionString(currentConnectionString)
				Expect(err).NotTo(HaveOccurred())

				for _, coordinator := range connectionString.Coordinators {
					address, err := fdbv1beta2.ParseProcessAddress(coordinator)
					Expect(err).NotTo(HaveOccurred())
					if shouldUseDNS {
						log.Println("address", address)
						Expect(address.StringAddress).NotTo(BeEmpty())
					} else {
						Expect(address.StringAddress).To(BeEmpty())
					}
				}
			})
		},
			PEntry("DNS is disabled", false),
			Entry("DNS is enabled", true),
		)
	})

	When("a majority of coordinators are down in a single dc cluster", func() {
		var fdbCluster *fixtures.FdbCluster

		AfterEach(func(ctx SpecContext) {
			Expect(fdbCluster.Delete(ctx)).NotTo(HaveOccurred())
		})

		// Default case is to run with DNS enabled. The test case with IPs enabled can run into issues when
		// the underlying Kubernetes cluster deletes pods.
		// Because of the above issues the test case is currently disabled (marked as pending) and can be used
		// to run the test manually if needed.
		DescribeTableSubtree("should recover the coordinators", func(shouldUseDNS bool) {
			JustBeforeEach(func(ctx SpecContext) {
				clusterConfig = fixtures.DefaultClusterConfig(false)
				clusterConfig.UseDNS = ptr.To(shouldUseDNS)
				fdbCluster = factory.CreateFdbCluster(ctx, clusterConfig)
				coordinators := fdbCluster.GetCoordinators(ctx)
				minimumFaultDomains := fdbCluster.GetCluster(ctx).MinimumFaultDomains()
				downCoordinators := make([]corev1.Pod, 0, minimumFaultDomains)
				for _, coordinator := range coordinators {
					if len(downCoordinators) >= minimumFaultDomains {
						break
					}

					downCoordinators = append(downCoordinators, coordinator)
				}

				// Set those coordinators as unschedulable to simulate that those coordinators are down. Another option
				// would be to create a network partition.
				fdbCluster.SetPodsAsUnschedulable(ctx, downCoordinators)
			})

			It("should recover the coordinators", func(ctx SpecContext) {
				// Pick one operator pod and execute the recovery command
				operatorPod := factory.RandomPickOnePod(
					factory.GetOperatorPods(ctx, fdbCluster.Namespace()).Items,
				)
				log.Println("operatorPod:", operatorPod.Name)
				stdout, stderr, err := factory.ExecuteCmdOnPod(
					ctx,
					&operatorPod,
					"manager",
					fmt.Sprintf(
						"kubectl-fdb -n %s recover single-dc --version-check=false --wait=false %s",
						fdbCluster.Namespace(),
						fdbCluster.Name(),
					),
					false,
				)
				log.Println("stdout:", stdout, "stderr:", stderr)
				if shouldUseDNS && strings.Contains(stderr, "Error determining public address") {
					Skip(
						"plugin was not able to determine public address, this means that all coordinators are probably gone",
					)
				}
				Expect(err).NotTo(HaveOccurred())

				// Ensure the cluster is available again.
				Eventually(func() bool {
					return fdbCluster.GetStatus(ctx).Client.DatabaseStatus.Available
				}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

				fdbCluster.SetSkipReconciliation(ctx, false)
				// Recreate the operator pods to ensure they get the new connection string.
				factory.RecreateOperatorPods(ctx, fdbCluster.Namespace())
				// Ensure that the cluster is able to reconcile
				Expect(fdbCluster.WaitForReconciliation(ctx)).To(Succeed())

				log.Println(
					"new connection string:",
					fdbCluster.GetCluster(ctx).Status.ConnectionString,
				)
				connectionString, err := fdbv1beta2.ParseConnectionString(
					fdbCluster.GetCluster(ctx).Status.ConnectionString,
				)
				Expect(err).NotTo(HaveOccurred())

				for _, coordinator := range connectionString.Coordinators {
					address, err := fdbv1beta2.ParseProcessAddress(coordinator)
					Expect(err).NotTo(HaveOccurred())
					if shouldUseDNS {
						Expect(address.StringAddress).NotTo(BeEmpty())
					} else {
						Expect(address.StringAddress).To(BeEmpty())
					}
				}
			})
		},
			PEntry("DNS is disabled", false),
			Entry("DNS is enabled", true),
		)
	})
})
