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
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

				log.Println("deleting coordinator pod:", pod.Name, "with addresses", pod.Status.PodIPs)
				factory.DeletePod(&pod)
			}
		})

		It("should change the coordinators", func() {
			primary := fdbCluster.GetPrimary()
			Eventually(func(g Gomega) string {
				status := primary.GetStatus()

				// Make sure we have the same count of coordinators again and the deleted
				coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
				g.Expect(coordinators).To(HaveLen(len(initialCoordinators)))

				return status.Cluster.ConnectionString
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).ShouldNot(Equal(initialConnectionString))

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

			Consistently(func() int {
				var runningPods int
				for _, pod := range satellite.GetAllPods().Items {
					if pod.Status.Phase != corev1.PodRunning {
						continue
					}

					runningPods++
				}

				// We should add here another check that the cluster stays in the primary.
				return runningPods
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(BeNumerically(">=", desiredRunningPods))
		})
	})

	When("all Pods in the primary and primary satellite are down", func() {
		BeforeEach(func() {
			// This tests is a destructive test where the cluster will stop working for some period.
			availabilityCheck = false
			primary := fdbCluster.GetPrimary()
			primary.SetSkipReconciliation(true)

			primarySatellite := fdbCluster.GetPrimarySatellite()
			primarySatellite.SetSkipReconciliation(true)

			primaryPods := primary.GetPods()
			for _, pod := range primaryPods.Items {
				factory.DeletePod(&pod)
			}

			primarySatellitePods := primarySatellite.GetPods()
			for _, pod := range primarySatellitePods.Items {
				factory.DeletePod(&pod)
			}

			remoteSatellite := fdbCluster.GetRemoteSatellite()
			remoteSatellite.SetSkipReconciliation(true)

			remoteSatellitePods := remoteSatellite.GetPods()
			for _, pod := range remoteSatellitePods.Items {
				factory.DeletePod(&pod)
			}

			// Wait a short amount of time to let the cluster see that the primary and primary satellite is down.
			time.Sleep(5 * time.Second)
		})

		AfterEach(func() {
			// Delete the broken cluster.
			fdbCluster.Delete()
			// Recreate the cluster to make sure  the next tests can proceed
			fdbCluster = factory.CreateFdbHaCluster(clusterConfig, clusterOptions...)
			// Load some data into the cluster.
			factory.CreateDataLoaderIfAbsent(fdbCluster.GetPrimary())
		})

		It("should recover the coordinators", func() {
			// Set all the `FoundationDBCluster` resources for this FDB cluster to `spec.Skip = true` to make sure the operator is not changing the manual changed state.
			remote := fdbCluster.GetRemote()
			remote.SetSkipReconciliation(true)

			// Fetch the last connection string from the `FoundationDBCluster` status, e.g. `kubectl get fdb ${cluster} -o jsonpath='{ .status.connectionString }'`.
			lastConnectionString := remote.GetCluster().Status.ConnectionString
			lastConnectionStringParts := strings.Split(lastConnectionString, "@")
			addresses := strings.Split(lastConnectionStringParts[1], ",")
			// Since this is a multi-region cluster, we expect 9 coordinators.
			Expect(addresses).To(HaveLen(9))

			log.Println("lastConnectionString", lastConnectionString)

			var useTLS bool
			coordinators := map[string]fdbv1beta2.ProcessAddress{}
			for _, addr := range addresses {
				parsed, err := fdbv1beta2.ParseProcessAddress(addr)
				Expect(err).NotTo(HaveOccurred())
				log.Println("found coordinator", parsed.String())
				coordinators[parsed.MachineAddress()] = parsed
				// If the tls flag is present we assume that the coordinators should make use of TLS.
				_, useTLS = parsed.Flags["tls"]
			}

			log.Println("coordinators", coordinators, "useTLS", useTLS)
			runningCoordinators := map[string]fdbv1beta2.None{}
			var runningCoordinator *corev1.Pod
			newCoordinators := make([]fdbv1beta2.ProcessAddress, 0, 5)
			remotePods := remote.GetPods()
			candidates := make([]corev1.Pod, 0, len(remotePods.Items))
			for _, pod := range remotePods.Items {
				addr, err := fdbv1beta2.ParseProcessAddress(pod.Status.PodIP)
				Expect(err).NotTo(HaveOccurred())
				if coordinatorAddr, ok := coordinators[addr.MachineAddress()]; ok {
					log.Println("Found coordinator for remote", pod.Name, "address", coordinatorAddr.String())
					runningCoordinators[addr.MachineAddress()] = fdbv1beta2.None{}
					newCoordinators = append(newCoordinators, coordinatorAddr)
					if runningCoordinator == nil {
						loopPod := pod
						runningCoordinator = &loopPod
					}
					continue
				}

				if !fixtures.GetProcessClass(pod).IsTransaction() {
					continue
				}

				candidates = append(candidates, pod)
			}

			// Pick 5 new coordinators.
			needsUpload := make([]corev1.Pod, 0, 5)
			idx := 0
			for len(newCoordinators) < 5 {
				fmt.Println("Current coordinators:", len(newCoordinators))
				candidate := candidates[idx]
				addr, err := fdbv1beta2.ParseProcessAddress(candidate.Status.PodIP)
				Expect(err).NotTo(HaveOccurred())
				fmt.Println("Adding pod as new coordinators:", candidate.Name)
				if useTLS {
					addr.Port = 4500
					addr.Flags = map[string]bool{"tls": true}
				} else {
					addr.Port = 4501
				}
				newCoordinators = append(newCoordinators, addr)
				needsUpload = append(needsUpload, candidate)
				idx++
			}

			// Copy the coordinator state from one of the running coordinators to your local machine:
			coordinatorFiles := []string{"coordination-0.fdq", "coordination-1.fdq"}
			tmpCoordinatorFiles := make([]string, 2)
			tmpDir := GinkgoT().TempDir()
			for idx, coordinatorFile := range coordinatorFiles {
				tmpCoordinatorFiles[idx] = path.Join(tmpDir, coordinatorFile)
			}

			log.Println("tmpCoordinatorFiles", tmpCoordinatorFiles)
			stdout, stderr, err := factory.ExecuteCmdOnPod(context.Background(), runningCoordinator, fdbv1beta2.MainContainerName, "find /var/fdb/data/ -type f -name 'coordination-0.fdq'", true)
			Expect(err).NotTo(HaveOccurred())
			Expect(stderr).To(BeEmpty())

			dataDir := path.Dir(strings.TrimSpace(stdout))
			log.Println("find result:", stdout, ",dataDir", dataDir)
			for idx, coordinatorFile := range coordinatorFiles {
				tmpCoordinatorFile, err := os.OpenFile(tmpCoordinatorFiles[idx], os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
				Expect(err).NotTo(HaveOccurred())

				log.Println("Download files, target:", tmpCoordinatorFiles[idx], "source", path.Join(dataDir, coordinatorFile), "pod", runningCoordinator.Name, "namespace", runningCoordinator.Namespace)
				err = factory.DownloadFile(context.Background(), runningCoordinator, fdbv1beta2.MainContainerName, path.Join(dataDir, coordinatorFile), tmpCoordinatorFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(tmpCoordinatorFile.Close()).NotTo(HaveOccurred())

				fileInfo, err := os.Stat(tmpCoordinatorFiles[idx])
				Expect(err).NotTo(HaveOccurred())
				Expect(fileInfo.Size()).To(BeNumerically(">", 0))
			}

			for _, target := range needsUpload {
				for idx, coordinatorFile := range coordinatorFiles {
					tmpCoordinatorFile, err := os.OpenFile(tmpCoordinatorFiles[idx], os.O_RDONLY, 0600)
					Expect(err).NotTo(HaveOccurred())

					log.Println("Upload files, source:", tmpCoordinatorFile.Name(), "target", path.Join(dataDir, coordinatorFile), "pod", target.Name, "namespace", target.Namespace)
					err = factory.UploadFile(context.Background(), &target, fdbv1beta2.MainContainerName, tmpCoordinatorFile, path.Join(dataDir, coordinatorFile))
					Expect(err).NotTo(HaveOccurred())
					Expect(tmpCoordinatorFile.Close()).NotTo(HaveOccurred())
				}
			}

			// Update the `ConfigMap` to contain the new connection string, the new connection string must contain the still existing coordinators and the new coordinators. The old entries must be removed.
			var newConnectionString strings.Builder
			newConnectionString.WriteString(lastConnectionStringParts[0])
			newConnectionString.WriteString("@")
			for idx, coordinator := range newCoordinators {
				newConnectionString.WriteString(coordinator.String())
				if idx == len(newCoordinators)-1 {
					break
				}

				newConnectionString.WriteString(",")
			}

			newCS := newConnectionString.String()
			log.Println("new connection string:", newCS)
			for _, cluster := range fdbCluster.GetAllClusters() {
				cluster.UpdateConnectionString(newCS)
			}

			// Wait ~1 min until the `ConfigMap` is synced to all Pods, you can check the `/var/dynamic-conf/fdb.cluster` inside a Pod if you are unsure.
			time.Sleep(2 * time.Minute)

			log.Println("Kill fdbserver processes")

			debugOutput := true
			// Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).
			for _, pod := range remote.GetPods().Items {
				_, _, err := factory.ExecuteCmd(context.Background(), pod.Namespace, pod.Name, fdbv1beta2.MainContainerName, "pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver || true", debugOutput)
				Expect(err).NotTo(HaveOccurred())
			}

			log.Println("force recovery")
			dataCenterID := remote.GetCluster().Spec.DataCenter
			// Now you can exec into a container and use `fdbcli` to connect to the cluster.
			// If you use a multi-region cluster you have to issue `force_recovery_with_data_loss`
			_, _, err = remote.RunFdbCliCommandInOperatorWithoutRetry(fmt.Sprintf("force_recovery_with_data_loss %s", dataCenterID), true, 40)
			Expect(err).NotTo(HaveOccurred())

			// Now you can set `spec.Skip = false` to let the operator take over again.
			remote.SetSkipReconciliation(false)

			newDatabaseConfiguration := remote.GetCluster().Spec.DatabaseConfiguration.FailOver()
			// Drop the multi-region configuration.
			newDatabaseConfiguration.Regions = []fdbv1beta2.Region{
				{
					DataCenters: []fdbv1beta2.DataCenter{
						{
							ID: dataCenterID,
						},
					},
				},
			}

			Expect(remote.SetDatabaseConfiguration(newDatabaseConfiguration, false)).NotTo(HaveOccurred())

			// Ensure the cluster is available again.
			Eventually(func() bool {
				return remote.GetStatus().Client.DatabaseStatus.Available
			}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())
		})
	})
})
