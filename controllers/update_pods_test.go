/*
 * update_pods_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/utils/pointer"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("update_pods", func() {
	Context("When deleting Pods for an update", func() {
		var updates map[string][]*corev1.Pod
		var cluster *fdbv1beta2.FoundationDBCluster

		type testCase struct {
			deletionMode         fdbv1beta2.PodUpdateMode
			expectedDeletionsCnt int
			maintenanceZone      string
			expectedErr          error
		}

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(cluster), cluster)).NotTo(HaveOccurred())

			updates = map[string][]*corev1.Pod{
				"zone1": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod1",
							Labels: map[string]string{
								fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod2",
							Labels: map[string]string{
								fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
							},
						},
					},
				},
				"zone2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod3",
							Labels: map[string]string{
								fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod4",
							Labels: map[string]string{
								fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
							},
						},
					},
				},
			}
		})

		DescribeTable("should delete the Pods based on the deletion mode",
			func(input testCase) {
				_, deletion, err := getPodsToDelete(&fdbv1beta2.FoundationDBCluster{}, input.deletionMode, updates, input.maintenanceZone)
				if input.expectedErr != nil {
					Expect(err).To(Equal(input.expectedErr))
				}
				Expect(deletion).To(HaveLen(input.expectedDeletionsCnt))
			},
			Entry("With the deletion mode Zone",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeZone,
					expectedDeletionsCnt: 2,
					maintenanceZone:      "",
				}),
			Entry("With the deletion mode Zone and an active maintenance zone",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeZone,
					expectedDeletionsCnt: 2,
					maintenanceZone:      "zone1",
				}),
			Entry("With the deletion mode Zone and an active maintenance zone that doesn't match",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeZone,
					expectedDeletionsCnt: 0,
					maintenanceZone:      "zone3",
				}),
			Entry("With the deletion mode Process Group",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeProcessGroup,
					expectedDeletionsCnt: 1,
					maintenanceZone:      "",
				}),
			Entry("With the deletion mode All",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeAll,
					expectedDeletionsCnt: 4,
					maintenanceZone:      "",
				}),
			Entry("With the deletion mode None",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeNone,
					expectedDeletionsCnt: 0,
					maintenanceZone:      "",
				}),
			Entry("With the deletion mode All",
				testCase{
					deletionMode:         "banana",
					expectedDeletionsCnt: 0,
					maintenanceZone:      "",
					expectedErr:          fmt.Errorf("unknown deletion mode: \"banana\""),
				}),
		)
	})

	Context("Validating shouldRequeueDueToTerminatingPod", func() {
		var processGroup = fdbv1beta2.ProcessGroupID("")

		When("Pod is without deletionTimestamp", func() {
			var cluster *fdbv1beta2.FoundationDBCluster
			var pod *corev1.Pod

			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{}
				pod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "Pod1",
					},
				}
			})

			It("should not requeue due to terminating pods", func() {
				Expect(shouldRequeueDueToTerminatingPod(pod, cluster, processGroup)).To(BeFalse())
			})
		})

		When("Pod with deletionTimestamp less than ignore limit", func() {
			var cluster *fdbv1beta2.FoundationDBCluster
			var pod *corev1.Pod

			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{}
				pod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "Pod1",
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
				}
			})

			It("should requeue due to terminating pods", func() {
				Expect(shouldRequeueDueToTerminatingPod(pod, cluster, processGroup)).To(BeTrue())
			})
		})

		When("Pod with deletionTimestamp more than ignore limit", func() {
			var cluster *fdbv1beta2.FoundationDBCluster
			var pod *corev1.Pod

			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{}
				pod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "Pod1",
						DeletionTimestamp: &metav1.Time{Time: time.Now().Add(-15 * time.Minute)},
					},
				}

			})

			It("should not requeue", func() {
				Expect(shouldRequeueDueToTerminatingPod(pod, cluster, processGroup)).To(BeFalse())
			})
		})

		When("with configured IgnoreTerminatingPodsSeconds", func() {
			var cluster *fdbv1beta2.FoundationDBCluster
			var pod *corev1.Pod

			BeforeEach(func() {
				cluster = &fdbv1beta2.FoundationDBCluster{
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
							IgnoreTerminatingPodsSeconds: pointer.Int(int(5 * time.Minute.Seconds())),
						},
					},
				}
				pod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "Pod1",
						DeletionTimestamp: &metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
					},
				}

			})

			It("should not requeue", func() {
				Expect(shouldRequeueDueToTerminatingPod(pod, cluster, processGroup)).To(BeFalse())
			})
		})
	})

	When("getting the fault domains with unavailable Pods", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var processGroupsWithFaultDomains map[fdbv1beta2.FaultDomain]fdbv1beta2.None
		var processGroup *fdbv1beta2.ProcessGroupStatus

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(cluster), cluster)).NotTo(HaveOccurred())

			processGroup = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
		})

		JustBeforeEach(func() {
			processGroupsWithFaultDomains = getFaultDomainsWithUnavailablePods(context.Background(), globalControllerLogger, clusterReconciler, cluster)
		})

		When("a Process Group has a Pod with pending condition", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(fdbv1beta2.PodPending, true)
			})

			It("should be marked as unavailable", func() {
				Expect(processGroupsWithFaultDomains).To(HaveLen(1))
			})
		})

		When("a Process Group has missing processes", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
			})

			It("should be marked as unavailable", func() {
				Expect(processGroupsWithFaultDomains).To(HaveLen(1))
			})
		})

		When("a Process Group has a Pod running with failed containers", func() {
			BeforeEach(func() {
				processGroup.UpdateCondition(fdbv1beta2.PodFailing, true)
			})

			It("should be marked as unavailable", func() {
				Expect(processGroupsWithFaultDomains).To(HaveLen(1))
			})
		})

		When("a Process Group has a Pod marked for deletion", func() {
			BeforeEach(func() {
				pods, err := clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), clusterReconciler, cluster, internal.GetSinglePodListOptions(cluster, processGroup.ProcessGroupID)...)
				Expect(err).NotTo(HaveOccurred())
				pods[0].DeletionTimestamp = &metav1.Time{Time: time.Now()}
				err = clusterReconciler.PodLifecycleManager.UpdatePods(context.TODO(), clusterReconciler, cluster, []*corev1.Pod{pods[0]}, true)
				Expect(err).NotTo(HaveOccurred())
			})
			It("should be marked as unavailable", func() {
				Expect(processGroupsWithFaultDomains).To(HaveLen(1))
			})
		})

		When("a Process Group has no matching Pod", func() {
			BeforeEach(func() {
				pods, err := clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), clusterReconciler, cluster, internal.GetSinglePodListOptions(cluster, processGroup.ProcessGroupID)...)
				Expect(err).NotTo(HaveOccurred())
				err = clusterReconciler.PodLifecycleManager.DeletePod(context.TODO(), clusterReconciler, pods[0])
				Expect(err).NotTo(HaveOccurred())
			})
			It("should be marked as unavailable", func() {
				Expect(processGroupsWithFaultDomains).To(HaveLen(1))
			})
		})
	})

	When("fetching all Pods that needs an update", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var updates map[string][]*corev1.Pod
		var pvcMap map[fdbv1beta2.ProcessGroupID]corev1.PersistentVolumeClaim
		var expectedError bool
		var err error

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(cluster), cluster)).NotTo(HaveOccurred())

			allPvcs := &corev1.PersistentVolumeClaimList{}
			err = clusterReconciler.List(context.TODO(), allPvcs, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())

			pvcMap = internal.CreatePVCMap(cluster, allPvcs)
		})

		JustBeforeEach(func() {
			updates, err = getPodsToUpdate(context.Background(), globalControllerLogger, clusterReconciler, cluster, pvcMap)
			if !expectedError {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		})

		When("the cluster has no changes", func() {
			It("should return no errors and an empty map", func() {
				Expect(updates).To(HaveLen(0))
			})
		})

		When("there is a spec change for all processes", func() {
			BeforeEach(func() {
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.Tolerations = []corev1.Toleration{{Key: "test", Operator: "Exists", Effect: "NoSchedule"}}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should return no errors and a map with one zone", func() {
				// We only have one zone in this case, the simulation zone
				Expect(updates).To(HaveLen(1))
			})
		})
		When("there is a spec change requiring a removal", func() {
			BeforeEach(func() {
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				// Updates to NodeSelector requires a removal
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should return no updates", func() {
				Expect(updates).To(HaveLen(0))
			})
		})

		When("two process groups have with pods in pending state", func() {
			BeforeEach(func() {
				for _, processGroup := range internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2) {
					processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
				}
			})

			When("max zones with unavailable pods is set to 3", func() {
				BeforeEach(func() {
					expectedError = false
					cluster.Spec.MaxZonesWithUnavailablePods = pointer.Int(3)
					// Update all processes
					storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					storageSettings.PodTemplate.Spec.Tolerations = []corev1.Toleration{{Key: "test", Operator: "Exists", Effect: "NoSchedule"}}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should return no errors and a map with the zone and all pods to update", func() {
					Expect(updates).To(HaveLen(1))
					Expect(updates["simulation"]).To(HaveLen(4))
				})
			})

			When("max zones with unavailable pods is set to 2", func() {
				BeforeEach(func() {
					expectedError = false
					cluster.Spec.MaxZonesWithUnavailablePods = pointer.Int(2)
					// Update all processes
					storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					storageSettings.PodTemplate.Spec.Tolerations = []corev1.Toleration{{Key: "test", Operator: "Exists", Effect: "NoSchedule"}}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should return no errors and a map with the zone and two pods to update", func() {
					Expect(updates).To(HaveLen(1))
					Expect(updates["simulation"]).To(HaveLen(2))
				})
			})

			When("max zones with unavailable pods is set to 1", func() {
				BeforeEach(func() {
					expectedError = false
					cluster.Spec.MaxZonesWithUnavailablePods = pointer.Int(1)
					// Update all processes
					storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					storageSettings.PodTemplate.Spec.Tolerations = []corev1.Toleration{{Key: "test", Operator: "Exists", Effect: "NoSchedule"}}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should return no errors and a an empty update map", func() {
					Expect(updates).To(HaveLen(0))
				})
			})
		})
	})
})
