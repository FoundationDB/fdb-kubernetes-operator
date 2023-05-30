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
				_, deletion, err := getPodsToDelete(input.deletionMode, updates, cluster)
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

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(cluster), cluster)).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			updates, err = getPodsToUpdate(context.Background(), log, clusterReconciler, cluster)
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
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should return no errors and a map with one zone", func() {
				// We only have one zone in this case, the simulation zone
				Expect(updates).To(HaveLen(1))
			})
		})

		When("max zones with unavailable pods is set to 1% value and there are 3 process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxZonesWithUnavailablePods = intstr.FromString("1%")
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-1" || processGroup.ProcessGroupID == "storage-2" || processGroup.ProcessGroupID == "storage-3" {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
					}
				}
			})

			It("should return no errors and a map with the zone and one pod to update", func() {
				Expect(updates).To(HaveLen(1))
				Expect(updates["simulation"]).To(HaveLen(1))
			})
		})

		When("max zones with unavailable pods is set to 99% value and there are three process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxZonesWithUnavailablePods = intstr.FromString("99%")
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-1" || processGroup.ProcessGroupID == "storage-2" || processGroup.ProcessGroupID == "storage-3" {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
					}
				}
			})

			It("should return no errors and a map with the zone and all pods to update", func() {
				Expect(updates).To(HaveLen(1))
				Expect(updates["simulation"]).To(HaveLen(4))
			})
		})

		When("max zones with unavailable pods is set to 3 and there are two process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxZonesWithUnavailablePods = intstr.FromInt(3)
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-1" || processGroup.ProcessGroupID == "storage-2" {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
					}
				}
			})

			It("should return no errors and a map with the zone and all pods to update", func() {
				Expect(updates).To(HaveLen(1))
				Expect(updates["simulation"]).To(HaveLen(4))
			})
		})

		When("max zones with unavailable pods is set to 2 and there are two process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxZonesWithUnavailablePods = intstr.FromInt(2)
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-1" || processGroup.ProcessGroupID == "storage-2" {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
					}
				}
			})

			It("should return no errors and a map with the zone and two pods to update", func() {
				Expect(updates).To(HaveLen(1))
				Expect(updates["simulation"]).To(HaveLen(2))
			})
		})

		When("max zones with unavailable pods is set to 1 and there are two process groups with pods in pending status", func() {
			BeforeEach(func() {
				expectedError = false
				cluster.Spec.MaxZonesWithUnavailablePods = intstr.FromInt(1)
				// Update all processes
				storageSettings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
				storageSettings.PodTemplate.Spec.NodeSelector = map[string]string{"test": "test"}
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = storageSettings
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if processGroup.ProcessGroupID == "storage-1" || processGroup.ProcessGroupID == "storage-2" {
						processGroup.ProcessGroupConditions = append(processGroup.ProcessGroupConditions, fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodPending))
					}
				}
			})

			It("should return no errors and a map with the zone and one pod to update", func() {
				Expect(updates).To(HaveLen(1))
				Expect(updates["simulation"]).To(HaveLen(1))
			})
		})

		When("max zones with unavailable pods has an invalid format", func() {
			BeforeEach(func() {
				expectedError = true
				cluster.Spec.MaxZonesWithUnavailablePods = intstr.FromString("invalid")
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should return an error and nil updates", func() {
				Expect(updates).To(BeNil())
				Expect(expectedError).To(BeTrue())
				Expect(err.Error()).Should(ContainSubstring("invalid value for cluster.Spec.MaxZonesWithUnavailablePods: invalid"))
			})
		})

	})
})
