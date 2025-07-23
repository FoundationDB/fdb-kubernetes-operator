/*
 * operator_ha_test.go
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

package operatorha

/*
This test suite includes functional tests to ensure normal operational tasks are working fine.
Those tests include replacements of healthy or fault Pods and setting different configurations.

The assumption is that every test case reverts the changes that were done on the cluster.
In order to improve the test speed we only create one FoundationDB HA cluster initially.
This cluster will be used for all tests.
*/

import (
	"context"
	"log"
	"strconv"
	"time"

	"k8s.io/utils/ptr"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	fdbCluster = factory.CreateFdbHaCluster(
		fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false),
		factory.GetClusterOptions()...)

	// Load some data into the cluster.
	factory.CreateDataLoaderIfAbsent(fdbCluster.GetPrimary())

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

var _ = Describe("Operator HA tests", Label("e2e", "pr"), func() {
	var availabilityCheck bool

	AfterEach(func() {
		// Reset availabilityCheck if a test case removes this check.
		availabilityCheck = true
		if CurrentSpecReport().Failed() {
			factory.DumpStateHaCluster(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
		// Make sure all data is present in the cluster
		fdbCluster.GetPrimary().EnsureTeamTrackersAreHealthy()
		fdbCluster.GetPrimary().EnsureTeamTrackersHaveMinReplicas()
	})

	JustBeforeEach(func() {
		if availabilityCheck {
			Expect(fdbCluster.GetPrimary().InvariantClusterStatusAvailable()).NotTo(HaveOccurred())
		}
	})

	When("deleting all Pods in the primary", func() {
		var initialConnectionString string
		var initialCoordinators map[string]fdbv1beta2.None

		BeforeEach(func() {
			primary := fdbCluster.GetPrimary()
			status := primary.GetStatus()
			initialConnectionString = status.Cluster.ConnectionString

			initialCoordinators = fdbstatus.GetCoordinatorsFromStatus(status)
			primaryPods := primary.GetPods()

			for _, pod := range primaryPods.Items {
				processGroupID := fixtures.GetProcessGroupID(pod)
				if _, ok := initialCoordinators[string(processGroupID)]; !ok {
					continue
				}

				log.Println(
					"deleting coordinator pod:",
					pod.Name,
					"with addresses",
					pod.Status.PodIPs,
				)
				// Set Pod as unschedulable to ensure that they are not recreated. Otherwise the Pods might be recreated
				// fast enough to not result in a new connection string.
				fdbCluster.GetPrimary().SetPodAsUnschedulable(pod)
				factory.DeletePod(&pod)
			}
		})

		AfterEach(func() {
			Expect(fdbCluster.GetPrimary().ClearBuggifyNoSchedule(true)).To(Succeed())
		})

		It("should change the coordinators", func() {
			primary := fdbCluster.GetPrimary()

			lastForceReconcile := time.Now()
			Eventually(func(g Gomega) string {
				// Ensure that the coordinators are changed in a timely manner for the test case.
				if time.Since(lastForceReconcile) > 1*time.Minute {
					for _, cluster := range fdbCluster.GetAllClusters() {
						cluster.ForceReconcile()
					}

					lastForceReconcile = time.Now()
				}

				status := primary.GetStatus()

				// Make sure we have the same count of coordinators again and the deleted
				coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
				g.Expect(coordinators).To(HaveLen(len(initialCoordinators)))

				return status.Cluster.ConnectionString
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).ShouldNot(Equal(initialConnectionString))

			// Make sure the new connection string is propagated in time to all FoundationDBCLuster resources.
			for _, cluster := range fdbCluster.GetAllClusters() {
				tmpCluster := cluster
				Eventually(func() string {
					// The unified image has a mechanism to propagate changes in the cluster file, this allows multi-region
					// clusters to reconcile faster. In the case of the split image we need "external" events in Kubernetes
					// to trigger a reconciliation.
					if !tmpCluster.GetCluster().UseUnifiedImage() {
						tmpCluster.ForceReconcile()
					}

					return tmpCluster.GetCluster().Status.ConnectionString
				}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).ShouldNot(Equal(initialConnectionString))
			}
		})
	})

	When("replacing satellite Pods and the new Pods are stuck in pending", func() {
		var desiredRunningPods int
		var quota *corev1.ResourceQuota

		BeforeEach(func() {
			satellite := fdbCluster.GetPrimarySatellite()
			satelliteCluster := satellite.GetCluster()

			processCounts, err := satelliteCluster.GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())

			// Create Quota to limit the PVCs that can be created to 0. This will mean no new PVCs can be created.
			quota = &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testing-quota",
					Namespace: satellite.Namespace(),
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourcePersistentVolumeClaims: resource.MustParse(strconv.Itoa(0)),
					},
				},
			}
			Expect(factory.CreateIfAbsent(quota)).NotTo(HaveOccurred())

			desiredRunningPods = processCounts.Log - satelliteCluster.DesiredFaultTolerance()

			// Replace all Pods for this cluster.
			satellite.ReplacePods(satellite.GetAllPods().Items, false)
		})

		AfterEach(func() {
			// Make sure that the quota is deleted and new PVCs can be created.
			factory.Delete(quota)
			Expect(fdbCluster.GetPrimarySatellite().WaitForReconciliation()).NotTo(HaveOccurred())
		})

		It("should not replace too many Pods and bring down the satellite", func() {
			satellite := fdbCluster.GetPrimarySatellite()
			primary := fdbCluster.GetPrimary()
			primaryDCID := primary.GetCluster().Spec.DataCenter

			Consistently(func(g Gomega) int {
				var runningPods int
				for _, pod := range satellite.GetAllPods().Items {
					if pod.Status.Phase != corev1.PodRunning {
						continue
					}

					runningPods++
				}

				status := fdbCluster.GetPrimary().GetStatus()
				if status.Client.DatabaseStatus.Available {
					g.Expect(status.Cluster.DatabaseConfiguration.GetPrimaryDCID()).
						To(Equal(primaryDCID))
				} else {
					log.Println("Database is unavailable will skip the primary DC check", status)
				}

				return runningPods
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(BeNumerically(">=", desiredRunningPods))

			// Add enough quota, so that the log processes can be updated after some time.
			processCounts, err := fdbCluster.GetPrimarySatellite().
				GetCluster().
				GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			Expect(
				factory.GetControllerRuntimeClient().
					Update(context.Background(), &corev1.ResourceQuota{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "testing-quota",
							Namespace: satellite.Namespace(),
						},
						Spec: corev1.ResourceQuotaSpec{
							Hard: corev1.ResourceList{
								corev1.ResourcePersistentVolumeClaims: resource.MustParse(
									strconv.Itoa(processCounts.Log),
								),
							},
						},
					}),
			).To(Succeed())

			// Wait until the cluster has replaced all the log processes.
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})
	})

	When("locality based exclusions are enabled", func() {
		var initialUseLocalitiesForExclusion bool

		BeforeEach(func() {
			spec := fdbCluster.GetRemote().GetCluster().Spec.DeepCopy()
			initialUseLocalitiesForExclusion = fdbCluster.GetRemote().
				GetCluster().
				UseLocalitiesForExclusion()
			spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(true)
			fdbCluster.GetRemote().UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.GetRemote().GetCluster().UseLocalitiesForExclusion()).To(BeTrue())
		})

		AfterEach(func() {
			spec := fdbCluster.GetRemote().GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.UseLocalitiesForExclusion = ptr.To(
				initialUseLocalitiesForExclusion,
			)
			fdbCluster.GetRemote().UpdateClusterSpecWithSpec(spec)
		})

		When("when a remote log has network latency issues and gets replaced", func() {
			var experiment *fixtures.ChaosMeshExperiment
			var processGroupID fdbv1beta2.ProcessGroupID
			var replacedPod corev1.Pod

			BeforeEach(func() {
				// Ensure the other clusters are not interacting.
				for _, cluster := range fdbCluster.GetAllClusters() {
					cluster.SetSkipReconciliation(true)
				}

				remote := fdbCluster.GetRemote()
				remote.SetSkipReconciliation(false)
				dcID := remote.GetCluster().Spec.DataCenter
				status := remote.GetStatus()
				for _, process := range status.Cluster.Processes {
					dc, ok := process.Locality[fdbv1beta2.FDBLocalityDCIDKey]
					if !ok || dc != dcID {
						continue
					}

					var isLog bool
					for _, role := range process.Roles {
						if role.Role == "log" {
							isLog = true
							break
						}
					}

					if !isLog {
						continue
					}

					processGroupID = fdbv1beta2.ProcessGroupID(
						process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
					)
					break
				}

				log.Println("Will inject chaos into", processGroupID, "and replace it")
				for _, pod := range remote.GetLogPods().Items {
					if fixtures.GetProcessGroupID(pod) != processGroupID {
						continue
					}

					replacedPod = pod
					break
				}

				log.Println("Inject latency chaos")
				experiment = factory.InjectNetworkLatency(
					fixtures.PodSelector(&replacedPod),
					chaosmesh.PodSelectorSpec{
						GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
							Namespaces:     []string{remote.Namespace()},
							LabelSelectors: remote.GetCachedCluster().GetMatchLabels(),
						},
					}, chaosmesh.Both,
					&chaosmesh.DelaySpec{
						Latency:     "250ms",
						Correlation: "100",
						Jitter:      "0",
					})

				// TODO (johscheuer): Allow to have this as a long running task until the test is done.
				factory.CreateDataLoaderIfAbsentWithWait(fdbCluster.GetPrimary(), false)

				time.Sleep(1 * time.Minute)
				log.Println(
					"replacedPod",
					replacedPod.Name,
					"useLocalitiesForExclusion",
					fdbCluster.GetPrimary().GetCluster().UseLocalitiesForExclusion(),
				)
				remote.ReplacePod(replacedPod, true)
			})

			It("should exclude and remove the pod", func() {
				excludedServer := fdbv1beta2.ExcludedServers{
					Locality: processGroupID.GetExclusionString(),
				}

				Eventually(func(g Gomega) []fdbv1beta2.ExcludedServers {
					status := fdbCluster.GetPrimary().GetStatus()
					excludedServers := status.Cluster.DatabaseConfiguration.ExcludedServers
					log.Println("excludedServers", excludedServers)

					pod := &corev1.Pod{}
					err := factory.GetControllerRuntimeClient().
						Get(context.Background(), ctrlClient.ObjectKeyFromObject(&replacedPod), pod)
					g.Expect(err).To(HaveOccurred())
					g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())

					return excludedServers
				}).WithTimeout(15 * time.Minute).WithPolling(1 * time.Second).ShouldNot(ContainElement(excludedServer))
			})

			AfterEach(func() {
				for _, cluster := range fdbCluster.GetAllClusters() {
					cluster.SetSkipReconciliation(false)
				}
				Expect(fdbCluster.GetRemote().ClearProcessGroupsToRemove()).To(Succeed())
				factory.DeleteChaosMeshExperimentSafe(experiment)
				factory.DeleteDataLoader(fdbCluster.GetPrimary())
			})
		})

		PWhen("when a remote side has network latency issues and a pod gets replaced", func() {
			/*

				*Note* This test should be running with a bigger multi-region cluster e.g.:

					config := fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false)
					config.StorageServerPerPod = 8
					config.MachineCount = 10
					config.DisksPerMachine = 8

			*/
			var experiment *fixtures.ChaosMeshExperiment

			BeforeEach(func() {
				dcID := fdbCluster.GetRemote().GetCluster().Spec.DataCenter

				status := fdbCluster.GetPrimary().GetStatus()

				var processGroupID fdbv1beta2.ProcessGroupID
				for _, process := range status.Cluster.Processes {
					dc, ok := process.Locality[fdbv1beta2.FDBLocalityDCIDKey]
					if !ok || dc != dcID {
						continue
					}

					var isLog bool
					for _, role := range process.Roles {
						if role.Role == "log" {
							isLog = true
							break
						}
					}

					if !isLog {
						continue
					}

					processGroupID = fdbv1beta2.ProcessGroupID(
						process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
					)
					break
				}

				log.Println("Will inject chaos into", processGroupID, "and replace it")
				var replacedPod corev1.Pod
				for _, pod := range fdbCluster.GetRemote().GetLogPods().Items {
					if fixtures.GetProcessGroupID(pod) != processGroupID {
						continue
					}

					replacedPod = pod
					break
				}

				log.Println("Inject latency chaos")
				experiment = factory.InjectNetworkLatency(
					chaosmesh.PodSelectorSpec{
						GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
							Namespaces: []string{fdbCluster.GetRemote().Namespace()},
							LabelSelectors: fdbCluster.GetRemote().
								GetCachedCluster().
								GetMatchLabels(),
						},
					},
					chaosmesh.PodSelectorSpec{
						GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
							Namespaces: []string{
								fdbCluster.GetPrimary().Namespace(),
								fdbCluster.GetPrimarySatellite().Namespace(),
								fdbCluster.GetRemote().Namespace(),
								fdbCluster.GetRemoteSatellite().Namespace(),
							},
							ExpressionSelectors: []metav1.LabelSelectorRequirement{
								{
									Key:      fdbv1beta2.FDBClusterLabel,
									Operator: metav1.LabelSelectorOpExists,
								},
							},
						},
					}, chaosmesh.Both,
					&chaosmesh.DelaySpec{
						Latency:     "250ms",
						Correlation: "100",
						Jitter:      "0",
					})

				// TODO (johscheuer): Allow to have this as a long running task until the test is done.
				factory.CreateDataLoaderIfAbsentWithWait(fdbCluster.GetPrimary(), false)

				time.Sleep(1 * time.Minute)
				log.Println(
					"replacedPod",
					replacedPod.Name,
					"useLocalitiesForExclusion",
					fdbCluster.GetPrimary().GetCluster().UseLocalitiesForExclusion(),
				)
				fdbCluster.GetRemote().ReplacePod(replacedPod, true)
			})

			It("should exclude and remove the pod", func() {
				Eventually(func() []fdbv1beta2.ExcludedServers {
					status := fdbCluster.GetPrimary().GetStatus()
					excludedServers := status.Cluster.DatabaseConfiguration.ExcludedServers
					log.Println("excludedServers", excludedServers)
					return excludedServers
				}).WithTimeout(15 * time.Minute).WithPolling(1 * time.Second).Should(BeEmpty())
			})

			AfterEach(func() {
				Expect(fdbCluster.GetRemote().ClearProcessGroupsToRemove()).NotTo(HaveOccurred())
				factory.DeleteChaosMeshExperimentSafe(experiment)
				// Making sure we included back all the process groups after exclusion is complete.
				Expect(
					fdbCluster.GetPrimary().
						GetStatus().
						Cluster.DatabaseConfiguration.ExcludedServers,
				).To(BeEmpty())
				factory.DeleteDataLoader(fdbCluster.GetPrimary())
			})
		})
	})
})
