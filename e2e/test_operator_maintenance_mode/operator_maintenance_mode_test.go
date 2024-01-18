/*
 * operator_maintenance_mode_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2024 Apple Inc. and the FoundationDB project authors
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
This test suite includes tests around the interaction of the maintenance mode and the operator.
*/

import (
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"log"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbCluster(
		fixtures.DefaultClusterConfig(false),
		factory.GetClusterOptions()...,
	)

	// Make sure the unschedulable Pod is not removed
	Expect(fdbCluster.SetAutoReplacements(false, 12*time.Hour)).NotTo(HaveOccurred())

	// Load some data into the cluster.
	factory.CreateDataLoaderIfAbsent(fdbCluster)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator maintenance mode tests", Label("e2e"), func() {
	AfterEach(func() {
		Expect(fdbCluster.ClearBuggifyNoSchedule(false)).NotTo(HaveOccurred())
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
		// Make sure all data is present in the cluster
		fdbCluster.EnsureTeamTrackersAreHealthy()
		fdbCluster.EnsureTeamTrackersHaveMinReplicas()
	})

	When("the maintenance mode is set", func() {
		var failingStoragePod corev1.Pod
		var faultDomain fdbv1beta2.FaultDomain

		BeforeEach(func() {
			failingStoragePod = fixtures.RandomPickOnePod(fdbCluster.GetStoragePods().Items)

			// Set maintenance mode for this Pod
			for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
				if processGroup.ProcessClass != fdbv1beta2.ProcessClassStorage {
					continue
				}

				if processGroup.ProcessGroupID == fixtures.GetProcessGroupID(failingStoragePod) {
					faultDomain = processGroup.FaultDomain
				}
			}

			// Set the maintenance mode for 4 minutes.
			fdbCluster.RunFdbCliCommandInOperator(fmt.Sprintf("maintenance on %s 240", faultDomain), false, 60)

			// Set this Pod as unschedulable to keep it pending.
			Expect(fdbCluster.SetPodAsUnschedulable(failingStoragePod)).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			// Make sure that the quota is deleted and new PVCs can be created.
			Expect(fdbCluster.ClearBuggifyNoSchedule(true)).NotTo(HaveOccurred())
			// Reset the maintenance mode
			fdbCluster.RunFdbCliCommandInOperator("maintenance off", false, 60)
		})

		When("the Pod comes back before the maintenance mode times out", func() {
			It("should not set the team tracker status to unhealthy", func() {
				// Make sure the team tracker status shows healthy for the failed Pod and the maintenance zone is set.
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						log.Println(tracker.State.Name, ":", tracker.State.Healthy)
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					log.Println("Maintenance Zone:", status.Cluster.MaintenanceZone)
					return status.Cluster.MaintenanceZone
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))
			})
		})

		When("the maintenance mode times out", func() {
			It("should update the team tracker status to unhealthy", func() {
				// Make sure the team tracker status shows healthy for the failed Pod and the maintenance zone is set.
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))

				log.Println("Wait until maintenance mode times out")
				// Wait until the maintenance zone is reset
				Eventually(func(g Gomega) fdbv1beta2.FaultDomain {
					return fdbCluster.GetStatus().Cluster.MaintenanceZone
				}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(Equal(fdbv1beta2.FaultDomain("")))

				startTime := time.Now()
				log.Println("Wait until failure is detected")
				// We would expect that the team tracker gets unhealthy once the maintenance mode is timed out.
				Eventually(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()
					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeFalse())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(Equal(fdbv1beta2.FaultDomain("")))

				log.Println("It took:", time.Since(startTime).String(), "to detected the failure")
			})
		})

		When("there is a failure during maintenance mode", func() {
			BeforeEach(func() {
				// Set the maintenance mode for a long duration, e.g. 2h hours to make sure the mode is not timing out
				// but actually is being reset.
				fdbCluster.RunFdbCliCommandInOperator(fmt.Sprintf("maintenance on %s 7200", faultDomain), false, 60)
			})

			AfterEach(func() {
				fdbCluster.SetCrashLoopContainers(nil, false)
			})

			It("should remove the maintenance mode", func() {
				// Make sure the team tracker status shows healthy for the failed Pod and the maintenance zone is set.
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))

				log.Println("When another storage Pod is failing")
				var podToRecreate corev1.Pod

				for _, pod := range fdbCluster.GetStoragePods().Items {
					if pod.Name == failingStoragePod.Name {
						continue
					}

					podToRecreate = pod
					break
				}

				// We delete a Pod and make it crash-looping, to make sure the process is failed for more than 60 seconds.
				log.Println("Delete Pod", podToRecreate.Name)
				fdbCluster.SetCrashLoopContainers([]fdbv1beta2.CrashLoopContainerObject{
					{
						ContainerName: fdbv1beta2.MainContainerName,
						Targets:       []fdbv1beta2.ProcessGroupID{fixtures.GetProcessGroupID(podToRecreate)},
					},
				}, false)
				factory.DeletePod(&podToRecreate)

				log.Println("Wait until maintenance mode is reset")
				startTime := time.Now()
				// We would expect that the team tracker gets unhealthy once the maintenance mode is timed out.
				Eventually(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()
					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeFalse())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(Equal(fdbv1beta2.FaultDomain("")))

				log.Println("It took:", time.Since(startTime).String(), "to detected the failure")
			})
		})

		When("there is additional load on the cluster", func() {
			BeforeEach(func() {
				// Set the maintenance mode for a long duration, e.g. 2h hours to make sure the mode is not timing out
				// but actually is being reset.
				fdbCluster.RunFdbCliCommandInOperator(fmt.Sprintf("maintenance on %s 7200", faultDomain), false, 60)
			})

			AfterEach(func() {
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))
			})

			It("should not remove the maintenance mode", func() {
				// Make sure the team tracker status shows healthy for the failed Pod and the maintenance zone is set.
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))

				log.Println("When loading additional data into the cluster")
				factory.CreateDataLoaderIfAbsent(fdbCluster)
				factory.CreateDataLoaderIfAbsent(fdbCluster)
			})
		})

		When("the number of storage Pods is changed", func() {
			var initialStoragePods int

			BeforeEach(func() {
				counts, err := fdbCluster.GetCluster().GetProcessCountsWithDefaults()
				Expect(err).NotTo(HaveOccurred())

				initialStoragePods = counts.Storage
				// Set the maintenance mode for a long duration, e.g. 2h hours to make sure the mode is not timing out
				// but actually is being reset.
				fdbCluster.RunFdbCliCommandInOperator(fmt.Sprintf("maintenance on %s 7200", faultDomain), false, 60)
			})

			AfterEach(func() {
				Expect(fdbCluster.ClearBuggifyNoSchedule(false)).NotTo(HaveOccurred())
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				// Add 3 additional storage Pods.
				spec.ProcessCounts.Storage = initialStoragePods
				fdbCluster.UpdateClusterSpecWithSpec(spec)
			})

			It("should not remove the maintenance mode", func() {
				// Make sure the team tracker status shows healthy for the failed Pod and the maintenance zone is set.
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))

				log.Println("When adding additional storage processes to the cluster")
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				// Add 3 additional storage Pods.
				spec.ProcessCounts.Storage = initialStoragePods + 3
				fdbCluster.UpdateClusterSpecWithSpec(spec)

				// Make sure the maintenance mode is kept and the team tracker shows healthy.
				Consistently(func(g Gomega) fdbv1beta2.FaultDomain {
					status := fdbCluster.GetStatus()

					for _, tracker := range status.Cluster.Data.TeamTrackers {
						g.Expect(tracker.State.Healthy).To(BeTrue())
					}

					return status.Cluster.MaintenanceZone
				}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(Equal(faultDomain))
			})
		})
	})
})
