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
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("update_status", func() {
	Describe("getStorageServersPerPodForInstance", func() {
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
	})

	Describe("validateInstance", func() {
		Context("when instance has no Pod", func() {
			It("should be added to the failing Pods", func() {
				instance := FdbInstance{
					Metadata: &metav1.ObjectMeta{
						Labels: map[string]string{
							FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
							FDBInstanceIDLabel:   "1337",
						},
					},
				}
				cluster := createDefaultCluster()
				processGroupStatus := fdbtypes.NewProcessGroupStatus("1337", fdbtypes.ProcessClassStorage, []string{"1.1.1.1"})

				_, err := validateInstance(clusterReconciler, context.TODO(), cluster, instance, "", processGroupStatus)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroupStatus.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbtypes.MissingPod))
			})
		})
	})

	Context("validate instances", func() {
		var cluster *fdbtypes.FoundationDBCluster
		var configMap *corev1.ConfigMap
		var adminClient *MockAdminClient
		var instances []FdbInstance
		var processMap map[string][]fdbtypes.FoundationDBStatusProcessInfo
		var err error

		BeforeEach(func() {
			cluster = createDefaultCluster()
			err = setupClusterForTest(cluster)
			Expect(err).NotTo(HaveOccurred())

			instances, err = clusterReconciler.PodLifecycleManager.GetInstances(clusterReconciler, cluster, context.TODO(), getSinglePodListOptions(cluster, "storage-1")...)
			Expect(err).NotTo(HaveOccurred())

			for _, container := range instances[0].Pod.Spec.Containers {
				instances[0].Pod.Status.ContainerStatuses = append(instances[0].Pod.Status.ContainerStatuses, corev1.ContainerStatus{Ready: true, Name: container.Name})
			}

			adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			configMap = &corev1.ConfigMap{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-config"}, configMap)
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			databaseStatus, err := adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
			processMap = make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)
			for _, process := range databaseStatus.Cluster.Processes {
				processID, ok := process.Locality["process_id"]
				// if the processID is not set we fall back to the instanceID
				if !ok {
					processID = process.Locality["instance_id"]
				}
				processMap[processID] = append(processMap[processID], process)
			}
		})

		Context("when an instance is fine", func() {
			It("should not get any condition assigned", func() {
				processGroupStatus, err := validateInstances(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroupStatus[0].ProcessGroupConditions)).To(Equal(0))
			})
		})

		Context("when the pod for instance is missing", func() {
			It("should get a condition assigned", func() {
				instances[0].Pod = nil
				Expect(err).NotTo(HaveOccurred())

				processGroupStatus, err := validateInstances(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.MissingPod)
				Expect(missingProcesses).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		Context("when an instance has the wrong command line", func() {
			BeforeEach(func() {
				adminClient.MockIncorrectCommandLine("storage-1", true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateInstances(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())

				incorrectProcesses := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.IncorrectCommandLine)
				Expect(incorrectProcesses).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		Context("when an instance is not reporting to the cluster", func() {
			BeforeEach(func() {
				adminClient.MockMissingProcessGroup("storage-1", true)
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateInstances(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())

				missingProcesses := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.MissingProcesses)
				Expect(missingProcesses).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		Context("when the pod has the wrong spec", func() {
			BeforeEach(func() {
				instances[0].Metadata.Annotations[LastSpecKey] = "bad"
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateInstances(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())

				incorrectPods := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.IncorrectPodSpec)
				Expect(incorrectPods).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})

		Context("when the pod is failing to launch", func() {
			BeforeEach(func() {
				instances[0].Pod.Status.ContainerStatuses[0].Ready = false
			})

			It("should get a condition assigned", func() {
				processGroupStatus, err := validateInstances(clusterReconciler, context.TODO(), cluster, &cluster.Status, processMap, instances, configMap)
				Expect(err).NotTo(HaveOccurred())

				failingPods := fdbtypes.FilterByCondition(processGroupStatus, fdbtypes.PodFailing)
				Expect(failingPods).To(Equal([]string{"storage-1"}))

				Expect(len(processGroupStatus)).To(BeNumerically(">", 4))
				processGroup := processGroupStatus[len(processGroupStatus)-4]
				Expect(processGroup.ProcessGroupID).To(Equal("storage-1"))
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
			})
		})
	})
})
