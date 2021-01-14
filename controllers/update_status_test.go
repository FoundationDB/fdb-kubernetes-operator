/*
 * update_status_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("update_status", func() {
	Context("when env var is set with 1", func() {
		It("should return 1", func() {
			instance := FdbInstance{
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Env: []corev1.EnvVar{
								{
									Name:  "STORAGE_SERVERS_PER_POD",
									Value: "1",
								},
							},
						}},
					},
				},
			}

			storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(storageServersPerPod).To(Equal(1))
		})
	})

	Context("when env var is set with 2", func() {
		It("should return 2", func() {
			instance := FdbInstance{
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Env: []corev1.EnvVar{
								{
									Name:  "STORAGE_SERVERS_PER_POD",
									Value: "2",
								},
							},
						}},
					},
				},
			}

			storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(storageServersPerPod).To(Equal(2))
		})
	})

	Context("when env var is unset", func() {
		It("should return 1", func() {
			instance := FdbInstance{
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Env: []corev1.EnvVar{},
						}},
					},
				},
			}

			storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(storageServersPerPod).To(Equal(1))
		})
	})

	Context("when instance is missing Pod", func() {
		It("should return 1", func() {
			instance := FdbInstance{
				Pod: &corev1.Pod{},
			}

			storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(storageServersPerPod).To(Equal(1))
		})
	})


	Context("when Pod doesn't contain a Spec", func() {
		It("should return 1", func() {
			instance := FdbInstance{}
			storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(storageServersPerPod).To(Equal(1))
		})
	})

	Context("when Pod doesn't contain a containers", func() {
		It("should return 1", func() {
			instance := FdbInstance{
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{},
				},
			}

			storageServersPerPod, err := getStorageServersPerPodForInstance(&instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(storageServersPerPod).To(Equal(1))
		})
	})

	Context("when instance has no Pod", func() {
		It("should be added to the failing Pods", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						"process-class":   fdbtypes.ProcessClassStorage,
						"fdb-instance-id": "1337",
					},
				},
			}
			cluster := createDefaultCluster()

			failing, _, _, err := validateInstance(clusterReconciler, context.TODO(), cluster, instance, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(failing).To(BeTrue())
		})
	})

	Context("validate instances", func() {
		Context("when Pod for instance is missing", func() {
			It("should be added to the failing Pods", func() {
				cluster := createDefaultCluster()
				status := fdbtypes.FoundationDBClusterStatus{}
				status.Generations.Reconciled = cluster.Status.Generations.Reconciled
				status.IncorrectProcesses = make(map[string]int64)
				status.MissingProcesses = make(map[string]int64)
				// Initialize with the current desired storage servers per Pod
				status.StorageServersPerDisk = []int{cluster.GetStorageServersPerPod()}

				instances := []FdbInstance{
					{
						Metadata: &metav1.ObjectMeta{
							Name: "1337",
							Labels: map[string]string{
								"process-class":   fdbtypes.ProcessClassStorage,
								"fdb-instance-id": "1337",
							},
						},
					},
				}

				configMap := &corev1.ConfigMap{}

				processMap := map[string][]fdbtypes.FoundationDBStatusProcessInfo{}

				err := validateInstances(clusterReconciler, context.TODO(), cluster, &status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(status.FailingPods)).To(Equal(1))
				Expect(status.FailingPods[0]).To(Equal("1337"))
				Expect(len(status.MissingProcesses)).To(Equal(0))
				Expect(len(status.StorageServersPerDisk)).To(Equal(1))
			})
		})
	})
})
