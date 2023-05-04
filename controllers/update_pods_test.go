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
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("update_pods", func() {
	Context("When deleting Pods for an update", func() {
		var updates map[string][]*corev1.Pod
		var cluster *fdbv1beta2.FoundationDBCluster

		type testCase struct {
			deletionMode         fdbv1beta2.PodUpdateMode
			expectedDeletionsCnt int
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
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod2",
						},
					},
				},
				"zone2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod3",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Pod4",
						},
					},
				},
			}
		})

		DescribeTable("should delete the Pods based on the deletion mode",
			func(input testCase) {
				_, deletion, err := getPodsToDelete(input.deletionMode, updates, 0, cluster)
				if input.expectedErr != nil {
					Expect(err).To(Equal(input.expectedErr))
				}
				Expect(len(deletion)).To(Equal(input.expectedDeletionsCnt))
			},
			Entry("With the deletion mode Zone",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeZone,
					expectedDeletionsCnt: 2,
				}),
			Entry("With the deletion mode Process Group",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeProcessGroup,
					expectedDeletionsCnt: 1,
				}),
			Entry("With the deletion mode All",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeAll,
					expectedDeletionsCnt: 4,
				}),
			Entry("With the deletion mode None",
				testCase{
					deletionMode:         fdbv1beta2.PodUpdateModeNone,
					expectedDeletionsCnt: 0,
				}),
			Entry("With the deletion mode All",
				testCase{
					deletionMode:         "banana",
					expectedDeletionsCnt: 0,
					expectedErr:          fmt.Errorf("unknown deletion mode: \"banana\""),
				}),
		)

		When("MaxUnavailablePods is greater than zero and max pods to delete is one", func() {
			BeforeEach(func() {
				cluster.Spec.MaxUnavailablePods = intstr.FromInt(1)
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			DescribeTable("should delete the Pods based on the deletion mode and max pods to delete is one",
				func(input testCase) {
					_, deletion, err := getPodsToDelete(input.deletionMode, updates, 1, cluster)
					if input.expectedErr != nil {
						Expect(err).To(Equal(input.expectedErr))
					}
					Expect(len(deletion)).To(Equal(input.expectedDeletionsCnt))
				},
				Entry("With the deletion mode Zone",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeZone,
						expectedDeletionsCnt: 1,
					}),
				Entry("With the deletion mode Process Group",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeProcessGroup,
						expectedDeletionsCnt: 1,
					}),
				Entry("With the deletion mode All",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeAll,
						expectedDeletionsCnt: 4,
					}),
				Entry("With the deletion mode None",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeNone,
						expectedDeletionsCnt: 0,
					}),
				Entry("With the deletion mode All",
					testCase{
						deletionMode:         "banana",
						expectedDeletionsCnt: 0,
						expectedErr:          fmt.Errorf("unknown deletion mode: \"banana\""),
					}),
			)
			DescribeTable("should delete the Pods based on the deletion mode and maxPods to delete is zero",
				func(input testCase) {
					_, deletion, err := getPodsToDelete(input.deletionMode, updates, 0, cluster)
					if input.expectedErr != nil {
						Expect(err).To(Equal(input.expectedErr))
					}
					Expect(len(deletion)).To(Equal(input.expectedDeletionsCnt))
				},
				Entry("With the deletion mode Zone",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeZone,
						expectedDeletionsCnt: 0,
					}),
				Entry("With the deletion mode Process Group",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeProcessGroup,
						expectedDeletionsCnt: 0,
					}),
				Entry("With the deletion mode All",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeAll,
						expectedDeletionsCnt: 4,
					}),
				Entry("With the deletion mode None",
					testCase{
						deletionMode:         fdbv1beta2.PodUpdateModeNone,
						expectedDeletionsCnt: 0,
					}),
				Entry("With the deletion mode All",
					testCase{
						deletionMode:         "banana",
						expectedDeletionsCnt: 0,
						expectedErr:          fmt.Errorf("unknown deletion mode: \"banana\""),
					}),
			)
		})
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

	When("fetching all Pods that needs an update", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var updates map[string][]*corev1.Pod
		var expectedError bool
		var err error
		var pods []*corev1.Pod
		var maxPodsToUpdate int

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(cluster), cluster)).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			pods, err = clusterReconciler.PodLifecycleManager.GetPods(context.TODO(), k8sClient, cluster, internal.GetPodListOptions(cluster, "", "")...)
			Expect(err).NotTo(HaveOccurred())

			updates, maxPodsToUpdate, err = getPodsToUpdate(log, clusterReconciler, cluster, internal.CreatePodMap(cluster, pods))
			if !expectedError {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		})

		When("the cluster has no changes", func() {
			It("should return no errors and an empty map", func() {
				Expect(updates).To(HaveLen(0))
				Expect(maxPodsToUpdate).To(Equal(0))
			})
		})

		When("there is a spec change for all processes", func() {
			BeforeEach(func() {
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should return no errors a map with one zone and the number of pods to update", func() {
				// We only have one zone in this case, the simulation zone
				Expect(updates).To(HaveLen(1))
				Expect(maxPodsToUpdate).To(Equal(4))
			})
		})

		When("max unavailable pods is set with a percent value and there are process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxUnavailablePods = intstr.FromString("10%")
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				var numPendingPods int
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass.IsStateful() {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
						numPendingPods++
						if numPendingPods == 8 {
							break
						}
					}
				}
			})

			It("should return no errors a map with the zone and the max pods to update limit", func() {
				Expect(updates).To(HaveLen(1))
				Expect(maxPodsToUpdate).To(Equal(-6))
			})
		})

		When("max unavailable pods is set with an int value and there are two process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxUnavailablePods = intstr.FromInt(1)
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				var numPendingPods int
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass.IsStateful() {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
						numPendingPods++
						if numPendingPods == 2 {
							break
						}
					}
				}
			})

			It("should return no errors a map with the zone and the max pods to update limit", func() {
				Expect(updates).To(HaveLen(1))
				Expect(maxPodsToUpdate).To(Equal(-1))
			})
		})

		When("max unavailable pods is greater than the number of pods requiring update and the number of pods for the zone is bigger than the max pods to update limit", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxUnavailablePods = intstr.FromInt(4)
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				var numPendingPods int
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass.IsStateful() {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
						numPendingPods++
						if numPendingPods == 2 {
							break
						}
					}
				}
			})

			It("should return no errors a map with the zone and the max pods to update limit", func() {
				Expect(updates).To(HaveLen(1))
				Expect(maxPodsToUpdate).To(Equal(2))
			})
		})

		When("max unavailable pods is greater than the number of pods requiring update and the number of pods for the zone is smaller than the max pods to update limit", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxUnavailablePods = intstr.FromInt(10)
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				var numPendingPods int
				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessClass.IsStateful() {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
						numPendingPods++
						if numPendingPods == 2 {
							break
						}
					}
				}
			})

			It("should return no errors a map with the zone and the max pods to update limit", func() {
				Expect(updates).To(HaveLen(1))
				Expect(maxPodsToUpdate).To(Equal(8))
			})
		})

		When("max unavailable pods has an invalid format", func() {
			BeforeEach(func() {
				expectedError = true
				cluster.Spec.MaxUnavailablePods = intstr.FromString("invalid")
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should return an error and nil updates", func() {
				Expect(updates).To(BeNil())
				Expect(expectedError).To(BeTrue())
				Expect(err.Error()).Should(ContainSubstring("invalid value for cluster.Spec.MaxUnavailablePods: invalid"))
			})
		})

	})
})
