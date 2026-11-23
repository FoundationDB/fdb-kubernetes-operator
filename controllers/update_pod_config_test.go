/*
 * update_pod_config_test.go
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

package controllers

import (
	"context"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

var _ = Describe("updatePodConfig", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var req *requeue
	var err error
	var pod *corev1.Pod

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		Expect(setupClusterForTest(cluster)).NotTo(HaveOccurred())

		processGroup := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
		Expect(processGroup).NotTo(BeNil())
		pod, err = clusterReconciler.PodLifecycleManager.GetPod(
			context.TODO(),
			clusterReconciler,
			cluster,
			processGroup.GetPodName(cluster),
		)
		Expect(err).NotTo(HaveOccurred())

		for _, container := range pod.Spec.Containers {
			pod.Status.ContainerStatuses = append(
				pod.Status.ContainerStatuses,
				corev1.ContainerStatus{Ready: true, Name: container.Name},
			)
		}
	})

	JustBeforeEach(func() {
		req = updatePodConfig{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(req).To(BeNil())
		})
	})

	When("a Pod is stuck in Pending", func() {
		BeforeEach(func() {
			pod.Status.Phase = corev1.PodPending
		})

		AfterEach(func() {
			pod.Status.Phase = corev1.PodRunning
		})

		It("should not requeue", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(req).To(BeNil())
		})
	})

	When("a Pod is stuck in terminating", func() {
		BeforeEach(func() {
			Expect(k8sClient.MockStuckTermination(pod, true)).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(k8sClient.MockStuckTermination(pod, false)).NotTo(HaveOccurred())
		})

		It("should not requeue", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(req).To(BeNil())
		})
	})

	DescribeTable(
		"when deciding if the config of a Pod must be synced",
		func(
			version string,
			runningVersion string,
			monitorVersion *string,
			podAnnotations map[string]string,
			expected bool,
		) {
			testCluster := internal.CreateDefaultCluster()
			testCluster.Spec.ImageType = new(fdbv1beta2.ImageTypeUnified)
			testCluster.Spec.Version = version
			testCluster.Status.RunningVersion = runningVersion
			testCluster.Spec.MainContainer.MonitorVersion = monitorVersion
			testPod := &corev1.Pod{}
			testPod.Annotations = podAnnotations

			Expect(shouldSyncPodConfig(testCluster, testPod, "hash")).To(Equal(expected))
		},
		Entry("hash is outdated",
			"7.1.57", "7.1.57", nil,
			map[string]string{fdbv1beta2.LastConfigMapKey: "other"},
			true),
		Entry("hash matches and no upgrade is running",
			"7.1.57", "7.1.57", nil,
			map[string]string{fdbv1beta2.LastConfigMapKey: "hash"},
			false),
		Entry("hash matches during a version incompatible upgrade",
			"7.2.0", "7.1.57", nil,
			map[string]string{fdbv1beta2.LastConfigMapKey: "hash"},
			true),
		Entry("hash matches, main image is pinned and the monitor reported a configuration",
			"7.2.0", "7.2.0", new("7.1.57"),
			map[string]string{
				fdbv1beta2.LastConfigMapKey:        "hash",
				api.CurrentConfigurationAnnotation: "{}",
			},
			false),
		Entry("hash matches, main image is pinned and the monitor has not reported a configuration",
			"7.2.0", "7.2.0", new("7.1.57"),
			map[string]string{fdbv1beta2.LastConfigMapKey: "hash"},
			true),
		Entry(
			"hash matches, main image is pinned to the version and the monitor has not reported a configuration",
			"7.1.57",
			"7.1.57",
			new("7.1.57"),
			map[string]string{fdbv1beta2.LastConfigMapKey: "hash"},
			false,
		),
	)
})
