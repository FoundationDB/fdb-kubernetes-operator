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
	"fmt"
	"k8s.io/utils/pointer"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("update_pods", func() {
	Context("When deleting Pods for an update", func() {
		var updates map[string][]*corev1.Pod

		type testCase struct {
			deletionMode         fdbtypes.PodUpdateMode
			expectedDeletionsCnt int
			expectedErr          error
		}

		BeforeEach(func() {
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
				_, deletion, err := getPodsToDelete(input.deletionMode, updates)
				if input.expectedErr != nil {
					Expect(err).To(Equal(input.expectedErr))
				}
				Expect(len(deletion)).To(Equal(input.expectedDeletionsCnt))
			},
			Entry("With the deletion mode Zone",
				testCase{
					deletionMode:         fdbtypes.PodUpdateModeZone,
					expectedDeletionsCnt: 2,
				}),
			Entry("With the deletion mode Process Group",
				testCase{
					deletionMode:         fdbtypes.PodUpdateModeProcessGroup,
					expectedDeletionsCnt: 1,
				}),
			Entry("With the deletion mode All",
				testCase{
					deletionMode:         fdbtypes.PodUpdateModeAll,
					expectedDeletionsCnt: 4,
				}),
			Entry("With the deletion mode None",
				testCase{
					deletionMode:         fdbtypes.PodUpdateModeNone,
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
		var processGroup = ""

		When("pod is without deletionTimestamp", func() {
			var cluster *fdbtypes.FoundationDBCluster
			var pod *corev1.Pod
			BeforeEach(func() {
				cluster = &fdbtypes.FoundationDBCluster{}
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

		When("pod with deletionTimestamp less than ignore limit", func() {
			var cluster *fdbtypes.FoundationDBCluster
			var pod *corev1.Pod
			BeforeEach(func() {
				cluster = &fdbtypes.FoundationDBCluster{}
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

		When("pod with deletionTimestamp more than ignore limit", func() {
			var cluster *fdbtypes.FoundationDBCluster
			var pod *corev1.Pod
			BeforeEach(func() {
				cluster = &fdbtypes.FoundationDBCluster{}
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
			var cluster *fdbtypes.FoundationDBCluster
			var pod *corev1.Pod
			BeforeEach(func() {
				cluster = &fdbtypes.FoundationDBCluster{
					Spec: fdbtypes.FoundationDBClusterSpec{
						AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
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
})
