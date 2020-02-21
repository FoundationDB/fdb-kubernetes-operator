/*
 * controller_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2019 Apple Inc. and the FoundationDB project authors
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/prometheus/common/expfmt"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var firstStorageIndex = 13

func reloadCluster(client client.Client, cluster *fdbtypes.FoundationDBCluster) (int64, error) {
	generations, err := reloadClusterGenerations(client, cluster)
	return generations.Reconciled, err
}

func reloadClusterGenerations(client client.Client, cluster *fdbtypes.FoundationDBCluster) (fdbtypes.GenerationStatus, error) {
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	if err != nil {
		return fdbtypes.GenerationStatus{}, err
	}
	return cluster.Status.Generations, err
}

func getListOptions(cluster *fdbtypes.FoundationDBCluster) []client.ListOption {
	return []client.ListOption{
		client.InNamespace("my-ns"),
		client.MatchingLabels(map[string]string{
			"fdb-cluster-name": cluster.Name,
		}),
	}
}

var _ = Describe("controller", func() {
	var cluster *fdbtypes.FoundationDBCluster
	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createDefaultCluster()
	})

	Describe("Reconciliation", func() {
		var originalPods *corev1.PodList
		var originalVersion int64
		var err error
		var generationGap int64
		var timeout time.Duration

		BeforeEach(func() {
			cluster.Spec.ConnectionString = ""
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			timeout = time.Second * 5
			Eventually(func() (int64, error) {
				generations, err := reloadClusterGenerations(k8sClient, cluster)
				return generations.Reconciled, err
			}, timeout).ShouldNot(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
			originalVersion = cluster.ObjectMeta.Generation

			originalPods = &corev1.PodList{}
			Eventually(func() (int, error) {
				err := k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
				return len(originalPods.Items), err
			}, timeout).Should(Equal(17))

			sortPodsByID(originalPods)

			generationGap = 1
		})

		JustBeforeEach(func() {
			Eventually(func() (int64, error) { return reloadCluster(k8sClient, cluster) }, timeout).Should(Equal(originalVersion + generationGap))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanupCluster(cluster)
		})

		Context("when reconciling a new cluster", func() {
			BeforeEach(func() {
				generationGap = 0
			})

			It("should create pods", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, timeout).Should(Equal(17))

				sortPodsByID(pods)

				Expect(pods.Items[0].Name).To(Equal("operator-test-1-cluster-controller-1"))
				Expect(pods.Items[0].Labels["fdb-instance-id"]).To(Equal("cluster_controller-1"))
				Expect(pods.Items[1].Name).To(Equal("operator-test-1-log-1"))
				Expect(pods.Items[1].Labels["fdb-instance-id"]).To(Equal("log-1"))
				Expect(pods.Items[4].Name).To(Equal("operator-test-1-log-4"))
				Expect(pods.Items[4].Labels["fdb-instance-id"]).To(Equal("log-4"))
				Expect(pods.Items[5].Name).To(Equal("operator-test-1-stateless-1"))
				Expect(pods.Items[5].Labels["fdb-instance-id"]).To(Equal("stateless-1"))
				Expect(pods.Items[12].Name).To(Equal("operator-test-1-stateless-8"))
				Expect(pods.Items[12].Labels["fdb-instance-id"]).To(Equal("stateless-8"))
				Expect(pods.Items[13].Name).To(Equal("operator-test-1-storage-1"))
				Expect(pods.Items[13].Labels["fdb-instance-id"]).To(Equal("storage-1"))
				Expect(pods.Items[16].Name).To(Equal("operator-test-1-storage-4"))
				Expect(pods.Items[16].Labels["fdb-instance-id"]).To(Equal("storage-4"))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
					"storage":            4,
					"log":                4,
					"stateless":          8,
					"cluster_controller": 1,
				}))
			})

			It("should fill in the required fields in the configuration", func() {
				Expect(cluster.Spec.RedundancyMode).To(Equal("double"))
				Expect(cluster.Spec.StorageEngine).To(Equal("ssd"))
				Expect(cluster.Spec.ConnectionString).NotTo(Equal(""))
			})

			It("should create a config map for the cluster", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				Eventually(func() error { return k8sClient.Get(context.TODO(), configMapName, configMap) }, timeout).Should(Succeed())
				expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, k8sClient)
				Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
			})

			It("should send the configuration to the cluster", func() {
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())
				Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("double"))
				Expect(adminClient.DatabaseConfiguration.StorageEngine).To(Equal("ssd-2"))
				Expect(adminClient.DatabaseConfiguration.RoleCounts).To(Equal(fdbtypes.RoleCounts{
					Logs:       3,
					Proxies:    3,
					Resolvers:  1,
					RemoteLogs: -1,
					LogRouters: -1,
				}))
			})

			It("should update the status with the reconciliation result", func() {
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				Expect(cluster.Status.Generations.Reconciled).To(Equal(int64(4)))
				Expect(cluster.Status.ProcessCounts).To(Equal(fdbtypes.ProcessCounts{
					Storage:           4,
					Log:               4,
					Stateless:         8,
					ClusterController: 1,
				}))

				desiredCounts, err := cluster.GetProcessCountsWithDefaults()
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Status.ProcessCounts).To(Equal(desiredCounts))
				Expect(cluster.Status.IncorrectProcesses).To(BeNil())
				Expect(cluster.Status.MissingProcesses).To(BeNil())
				Expect(cluster.Status.DatabaseConfiguration).To(Equal(*adminClient.DatabaseConfiguration))
				Expect(cluster.Status.Health).To(Equal(fdbtypes.ClusterHealth{
					Available:            true,
					Healthy:              true,
					FullReplication:      true,
					DataMovementPriority: 0,
				}))
			})
		})

		Context("with a decreased process count", func() {
			BeforeEach(func() {
				generationGap = 3

				cluster.Spec.ProcessCounts.Storage = 3
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove the pods", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, timeout).Should(Equal(len(originalPods.Items) - 1))
				sortPodsByID(pods)

				Expect(pods.Items[0].Name).To(Equal(originalPods.Items[0].Name))
				Expect(pods.Items[1].Name).To(Equal(originalPods.Items[1].Name))
				Expect(pods.Items[2].Name).To(Equal(originalPods.Items[2].Name))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
					"storage":            3,
					"log":                4,
					"stateless":          8,
					"cluster_controller": 1,
				}))

				Expect(cluster.Spec.PendingRemovals).To(BeNil())
				Expect(cluster.Spec.InstancesToRemove).To(BeNil())

				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())
				Expect(adminClient.ExcludedAddresses).To(Equal([]string{}))

				removedItem := originalPods.Items[16]
				Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
					cluster.GetFullAddress(MockPodIP(&removedItem)): true,
				}))
			})
		})

		Context("with an increased process count", func() {
			BeforeEach(func() {
				generationGap = 1
				cluster.Spec.ProcessCounts.Storage = 5
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add additional pods", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, timeout).Should(Equal(len(originalPods.Items) + 1))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
					"storage":            5,
					"log":                4,
					"stateless":          8,
					"cluster_controller": 1,
				}))
			})

			It("should update the config map", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				Eventually(func() error { return k8sClient.Get(context.TODO(), configMapName, configMap) }, timeout).Should(Succeed())
				expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, k8sClient)
				Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
			})
		})

		Context("with an increased stateless process count", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Stateless = 9
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the additional pods", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, timeout).Should(Equal(18))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
					"storage":            4,
					"log":                4,
					"stateless":          9,
					"cluster_controller": 1,
				}))
			})

			It("should update the config map", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				Eventually(func() error { return k8sClient.Get(context.TODO(), configMapName, configMap) }, timeout).Should(Succeed())
				expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, k8sClient)
				Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
			})
		})

		Context("with an explicit cluster controller process count", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.ClusterController = 1
				cluster.Spec.ProcessCounts.Stateless = 9
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the pods", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, timeout).Should(Equal(18))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
					"storage":            4,
					"log":                4,
					"stateless":          9,
					"cluster_controller": 1,
				}))
			})
		})

		Context("with a negative stateless process count", func() {
			BeforeEach(func() {
				generationGap = 3
				cluster.Spec.ProcessCounts.Stateless = -1
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove the stateless pods", func() {
				pods := &corev1.PodList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					return len(pods.Items), err
				}, timeout).Should(Equal(9))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
					"storage":            4,
					"log":                4,
					"cluster_controller": 1,
				}))

				Expect(cluster.Spec.PendingRemovals).To(BeNil())
				Expect(cluster.Spec.InstancesToRemove).To(BeNil())
			})
		})

		Context("with a coordinator replacement", func() {
			var originalConnectionString string

			BeforeEach(func() {
				generationGap = 4
				originalConnectionString = cluster.Spec.ConnectionString
			})

			Context("with an entry in the pending removals map", func() {
				BeforeEach(func() {
					cluster.Spec.PendingRemovals = map[string]string{
						originalPods.Items[firstStorageIndex].Name: "",
					}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should keep the process counts the same", func() {
					pods := &corev1.PodList{}
					Eventually(func() (int, error) {
						err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
						return len(pods.Items), err
					}, timeout).Should(Equal(17))

					Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
						"storage":            4,
						"log":                4,
						"stateless":          8,
						"cluster_controller": 1,
					}))
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					Eventually(func() (int, error) {
						err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
						return len(pods.Items), err
					}, timeout).Should(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
				})

				It("should exclude and re-include the process", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(Equal([]string{}))

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
						cluster.GetFullAddress(MockPodIP(&originalPods.Items[firstStorageIndex])): true,
					}))
				})

				It("should change the connection string", func() {
					Expect(cluster.Spec.ConnectionString).NotTo(Equal(originalConnectionString))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemove).To(BeNil())
				})
			})

			Context("with an entry in the instances to remove list", func() {
				BeforeEach(func() {
					cluster.Spec.InstancesToRemove = []string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels["fdb-instance-id"],
					}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should keep the process counts the same", func() {
					pods := &corev1.PodList{}
					Eventually(func() (int, error) {
						err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
						return len(pods.Items), err
					}, timeout).Should(Equal(17))

					Expect(getProcessClassMap(pods.Items)).To(Equal(map[string]int{
						"storage":            4,
						"log":                4,
						"stateless":          8,
						"cluster_controller": 1,
					}))
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					Eventually(func() (int, error) {
						err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
						return len(pods.Items), err
					}, timeout).Should(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
				})

				It("should exclude and re-include the process", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(Equal([]string{}))

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
						cluster.GetFullAddress(MockPodIP(&originalPods.Items[firstStorageIndex])): true,
					}))
				})

				It("should change the connection string", func() {
					Expect(cluster.Spec.ConnectionString).NotTo(Equal(originalConnectionString))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemove).To(BeNil())
				})
			})
		})

		Context("with a knob change", func() {
			var adminClient *MockAdminClient

			BeforeEach(func() {

				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.FreezeStatus()
				cluster.Spec.CustomParameters = []string{"knob_disable_posix_kernel_aio=1"}
			})

			Context("with bounces enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses = append(addresses, cluster.GetFullAddress(MockPodIP(&pod)))
					}

					sort.Slice(adminClient.KilledAddresses, func(i, j int) bool {
						return strings.Compare(adminClient.KilledAddresses[i], adminClient.KilledAddresses[j]) < 0
					})
					sort.Slice(addresses, func(i, j int) bool {
						return strings.Compare(addresses[i], addresses[j]) < 0
					})
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should update the config map", func() {
					configMap := &corev1.ConfigMap{}
					configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
					Eventually(func() error { return k8sClient.Get(context.TODO(), configMapName, configMap) }, timeout).Should(Succeed())
					expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, k8sClient)
					Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
				})
			})

			Context("with bounces disabled", func() {
				BeforeEach(func() {
					generationGap = 0
					var flag = false
					cluster.Spec.AutomationOptions.KillProcesses = &flag
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					Eventually(func() (fdbtypes.GenerationStatus, error) { return reloadClusterGenerations(k8sClient, cluster) }, timeout).Should(Equal(fdbtypes.GenerationStatus{
						Reconciled:             originalVersion,
						NeedsBounce:            originalVersion + 1,
						NeedsMonitorConfUpdate: originalVersion + 1,
					}))
				})

				It("should not kill any processes", func() {
					Expect(adminClient.KilledAddresses).To(BeNil())
				})

				It("should update the config map", func() {
					configMap := &corev1.ConfigMap{}
					configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
					Eventually(func() error { return k8sClient.Get(context.TODO(), configMapName, configMap) }, timeout).Should(Succeed())
					expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, k8sClient)
					Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
				})
			})
		})

		Context("with a configuration change", func() {
			var adminClient *MockAdminClient
			BeforeEach(func() {
				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"
			})

			Context("with changes enabled", func() {

				BeforeEach(func() {
					generationGap = 2
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should configure the database", func() {
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("triple"))
				})

			})

			Context("with changes disabled", func() {
				BeforeEach(func() {
					generationGap = 0
					var flag = false
					cluster.Spec.AutomationOptions.ConfigureDatabase = &flag
					cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					Eventually(func() (fdbtypes.GenerationStatus, error) { return reloadClusterGenerations(k8sClient, cluster) }, timeout).Should(Equal(fdbtypes.GenerationStatus{
						Reconciled:               originalVersion,
						NeedsConfigurationChange: originalVersion + 1,
					}))
				})

				It("should not change the database configuration", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("double"))
				})
			})
		})

		Context("with a change to pod labels", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value3",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{},
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the labels on the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal("value3"))
				}
			})

			It("should not update the labels on other resources", func() {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal(""))
				}

				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal(""))
				}
			})
		})

		Context("with a change to pod labels with a deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.PodLabels = map[string]string{
					"fdb-label": "value3",
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the labels on all the resources", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal("value3"))
				}

				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal("value3"))
				}

				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal("value3"))
				}
			})
		})

		Context("with annotations on pod", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{},
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the annotations on the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					_, id, err := ParseInstanceID(item.Labels["fdb-instance-id"])
					Expect(err).NotTo(HaveOccurred())

					hash, err := GetPodSpecHash(cluster, item.Labels["fdb-process-class"], id, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"org.foundationdb/last-applied-pod-spec-hash": hash,
						"fdb-annotation": "value1",
					}))
				}
			})

			It("should not set the annotations on other resources", func() {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(BeNil())
				}

				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Annotations).To(BeNil())
				}
			})
		})

		Context("with a change to the PVC labels", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value3",
						},
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the labels on the PVCs", func() {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal("value3"))
				}
			})

			It("should not update the labels on other resources", func() {
				pods := &corev1.PodList{}

				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal(""))
				}

				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal(""))
				}
			})
		})

		Context("with a change to PVC annotations", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the annotations on the PVCs", func() {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"fdb-annotation": "value1",
					}))
				}
			})

			It("should not update the annotations on other resources", func() {
				pods := &corev1.PodList{}

				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					_, id, err := ParseInstanceID(item.Labels["fdb-instance-id"])
					Expect(err).NotTo(HaveOccurred())

					hash, err := GetPodSpecHash(cluster, item.Labels["fdb-process-class"], id, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"org.foundationdb/last-applied-pod-spec-hash": hash,
					}))
				}

				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Annotations).To(BeNil())
				}
			})
		})

		Context("with a change to config map labels", func() {
			BeforeEach(func() {

				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value3",
						},
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the labels on the config map", func() {
				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal("value3"))
				}
			})

			It("should not update the labels on other resources", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal(""))
				}

				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Labels["fdb-label"]).To(Equal(""))
				}
			})
		})

		Context("with a change to config map annotations", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
				}
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the annotations on the config map", func() {
				configMaps := &corev1.ConfigMapList{}
				err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range configMaps.Items {
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"fdb-annotation": "value1",
					}))
				}
			})

			It("should not update the annotations on the other resources", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pods.Items {
					_, id, err := ParseInstanceID(item.Labels["fdb-instance-id"])
					Expect(err).NotTo(HaveOccurred())

					hash, err := GetPodSpecHash(cluster, item.Labels["fdb-process-class"], id, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"org.foundationdb/last-applied-pod-spec-hash": hash,
					}))
				}

				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(BeNil())
				}
			})
		})

		Context("with a change to environment variables", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							corev1.Container{
								Name: "foundationdb",
								Env: []corev1.EnvVar{
									corev1.EnvVar{
										Name:  "TEST_CHANGE",
										Value: "1",
									},
								},
							},
						},
					},
				}

				timeout = 120 * time.Second
			})

			Context("with deletion enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should set the environment variable on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(3))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("TEST_CHANGE"))
						Expect(pod.Spec.Containers[0].Env[0].Value).To(Equal("1"))
					}
				})
			})

			Context("with deletion disabled", func() {
				BeforeEach(func() {
					var flag = false
					cluster.Spec.AutomationOptions.DeletePods = &flag

					generationGap = 0

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					Eventually(func() (fdbtypes.GenerationStatus, error) { return reloadClusterGenerations(k8sClient, cluster) }, timeout).Should(Equal(fdbtypes.GenerationStatus{
						Reconciled:       originalVersion,
						NeedsPodDeletion: originalVersion + 1,
					}))
				})

				It("should not set the environment variable on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(2))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("FDB_CLUSTER_FILE"))
						Expect(pod.Spec.Containers[0].Env[1].Name).To(Equal("FDB_TLS_CA_FILE"))
					}
				})
			})
		})

		Context("with a change to environment variables with the deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.Env = append(cluster.Spec.MainContainer.Env, corev1.EnvVar{
					Name:  "TEST_CHANGE",
					Value: "1",
				})

				timeout = 60 * time.Second
			})

			Context("with deletion enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should set the environment variable on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(3))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("TEST_CHANGE"))
						Expect(pod.Spec.Containers[0].Env[0].Value).To(Equal("1"))
					}
				})
			})

			Context("with deletion disabled", func() {
				BeforeEach(func() {
					var flag = false
					cluster.Spec.AutomationOptions.DeletePods = &flag

					generationGap = 0

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					Eventually(func() (fdbtypes.GenerationStatus, error) { return reloadClusterGenerations(k8sClient, cluster) }, timeout).Should(Equal(fdbtypes.GenerationStatus{
						Reconciled:       originalVersion,
						NeedsPodDeletion: originalVersion + 1,
					}))
				})

				It("should not set the environment variable on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(2))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("FDB_CLUSTER_FILE"))
						Expect(pod.Spec.Containers[0].Env[1].Name).To(Equal("FDB_TLS_CA_FILE"))
					}
				})
			})
		})

		Context("with a change to TLS settings", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				generationGap = 2
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should bounce the processes", func() {
				addresses := make(map[string]bool, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					addresses[fmt.Sprintf("%s:4500:tls", MockPodIP(&pod))] = true
				}

				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				killedAddresses := make(map[string]bool, len(adminClient.KilledAddresses))
				for _, address := range adminClient.KilledAddresses {
					killedAddresses[address] = true
				}
				Expect(killedAddresses).To(Equal(addresses))
			})

			It("should change the coordinators to use TLS", func() {
				connectionString, err := fdbtypes.ParseConnectionString(cluster.Spec.ConnectionString)

				Expect(err).NotTo(HaveOccurred())
				for _, coordinator := range connectionString.Coordinators {
					address, err := fdbtypes.ParseProcessAddress(coordinator)
					Expect(err).NotTo(HaveOccurred())
					Expect(address.Flags["tls"]).To(BeTrue())
				}
			})
		})

		Context("downgrade cluster", func() {
			BeforeEach(func() {
				generationGap = 0
				IncompatibleVersion := Versions.Default
				IncompatibleVersion.Patch--
				cluster.Spec.Version = IncompatibleVersion.String()
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not downgrade cluster", func() {
				Expect(cluster.Status.Generations.Reconciled).To(Equal(originalVersion))
				Expect(cluster.Spec.RunningVersion).To(Equal(Versions.Default.String()))
			})
		})

		Context("custom metrics for a cluster", func() {
			BeforeEach(func() {
				generationGap = 0
				metricFamilies, err := metrics.Registry.Gather()
				Expect(err).NotTo(HaveOccurred())
				for _, metricFamily := range metricFamilies {
					metricFamily.Reset()
				}
				InitCustomMetrics(reconciler)
			})

			It("should update custom metrics in the registry", func() {
				metricFamilies, err := metrics.Registry.Gather()
				Expect(err).NotTo(HaveOccurred())
				var buf bytes.Buffer
				for _, mf := range metricFamilies {
					if strings.HasPrefix(mf.GetName(), "fdb_cluster_status") {
						_, err := expfmt.MetricFamilyToText(&buf, mf)
						Expect(err).NotTo(HaveOccurred())
					}
				}
				healthMetricOutput := fmt.Sprintf(`\nfdb_cluster_status{name="%s",namespace="%s",status_type="%s"} 1`,cluster.Name,cluster.Namespace,"health")
				availMetricOutput := fmt.Sprintf(`\nfdb_cluster_status{name="%s",namespace="%s",status_type="%s"} 1`,cluster.Name,cluster.Namespace,"available")
				replMetricOutput := fmt.Sprintf(`\nfdb_cluster_status{name="%s",namespace="%s",status_type="%s"} 1`,cluster.Name,cluster.Namespace,"replication")
				for _, re := range []*regexp.Regexp{
					regexp.MustCompile(healthMetricOutput),
					regexp.MustCompile(availMetricOutput),
					regexp.MustCompile(replMetricOutput),
				} {
					Expect(re.Match(buf.Bytes())).To(Equal(true))
				}
			})
		})
	})

	Describe("GetConfigMap", func() {
		var configMap *corev1.ConfigMap
		var err error

		JustBeforeEach(func() {
			configMap, err = GetConfigMap(context.TODO(), cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			It("should populate the metadata", func() {
				Expect(configMap.Namespace).To(Equal("my-ns"))
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-config", cluster.Name)))
				Expect(configMap.Labels).To(Equal(map[string]string{
					"fdb-cluster-name": cluster.Name,
				}))
				Expect(configMap.Annotations).To(BeNil())
			})

			It("should have the basic files", func() {
				expectedConf, err := GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())

				Expect(len(configMap.Data)).To(Equal(7))
				Expect(configMap.Data["cluster-file"]).To(Equal("operator-test:asdfasf@127.0.0.1:4501"))
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(expectedConf))
				Expect(configMap.Data["ca-file"]).To(Equal(""))
			})

			It("should have the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(sidecarConf)).To(Equal(5))
				Expect(sidecarConf["COPY_FILES"]).To(Equal([]interface{}{"fdb.cluster", "ca.pem"}))
				Expect(sidecarConf["COPY_BINARIES"]).To(Equal([]interface{}{"fdbserver", "fdbcli"}))
				Expect(sidecarConf["COPY_LIBRARIES"]).To(Equal([]interface{}{}))
				Expect(sidecarConf["INPUT_MONITOR_CONF"]).To(Equal("fdbmonitor.conf"))
				Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(BeNil())
			})
		})

		Context("with a custom CA", func() {
			BeforeEach(func() {
				cluster.Spec.TrustedCAs = []string{
					"-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC",
					"---CERT2----",
				}
			})

			It("should populate the CA file", func() {
				Expect(configMap.Data["ca-file"]).To(Equal("-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC\n---CERT2----"))
			})
		})

		Context("with an empty connection string", func() {
			BeforeEach(func() {
				cluster.Spec.ConnectionString = ""
			})

			It("should empty the monitor conf and cluster file", func() {
				Expect(configMap.Data["cluster-file"]).To(Equal(""))
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(""))
			})
		})

		Context("with custom sidecar substitutions", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}
			})

			It("should put the substitutions in the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())
				Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(Equal([]interface{}{"FAULT_DOMAIN", "ZONE"}))
			})
		})

		Context("with implicit instance ID substitution", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithSidecarInstanceIDSubstitution.String()
				cluster.Spec.RunningVersion = Versions.WithSidecarInstanceIDSubstitution.String()
			})

			It("should not include any substitutions in the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())

				Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(BeNil())
			})
		})

		Context("with explicit instance ID substitution", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithoutSidecarInstanceIDSubstitution.String()
				cluster.Spec.RunningVersion = Versions.WithoutSidecarInstanceIDSubstitution.String()
			})

			It("should include the instance ID in the substitutions in the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())

				Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(Equal([]interface{}{"FDB_INSTANCE_ID"}))
			})
		})

		Context("with a custom label", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value1",
						},
					},
				}
			})

			It("should put the label on the config map", func() {
				Expect(configMap.Labels).To(Equal(map[string]string{
					"fdb-cluster-name": cluster.Name,
					"fdb-label":        "value1",
				}))
			})
		})

		Context("with a custom label with the deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.PodLabels = map[string]string{
					"fdb-label": "value1",
				}
			})

			It("should put the label on the config map", func() {
				Expect(configMap.Labels).To(Equal(map[string]string{
					"fdb-cluster-name": cluster.Name,
					"fdb-label":        "value1",
				}))
			})
		})

		Context("with a custom annotation", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
				}
			})

			It("should put the annotation on the config map", func() {
				Expect(configMap.Annotations).To(Equal(map[string]string{
					"fdb-annotation": "value1",
				}))
			})
		})

		Context("with a custom configmap", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name: "name1",
					},
				}
			})

			It("should use the configmap name as suffix", func() {
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, "name1")))
			})
		})

		Context("without a configmap", func() {
			It("should use the default suffix", func() {
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, "config")))
			})
		})

		Context("with configmap having items", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					Data: map[string]string{
						"itemKey": "itemVal",
					},
				}
			})

			It("should have items from the clusterSpec", func() {
				Expect(configMap.Data["itemKey"]).To(Equal("itemVal"))
			})
		})
	})

	Describe("GetMonitorConf", func() {
		var conf string
		var err error

		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with TLS enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the TLS flag in the address", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4500:tls",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a transition to TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true

				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include both addresses", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4500:tls,$FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a transition to non-TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = false
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true

				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include both addresses", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501,$FDB_PUBLIC_IP:4500:tls",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with custom parameters", func() {
			BeforeEach(func() {
				cluster.Spec.CustomParameters = []string{
					"knob_disable_posix_kernel_aio = 1",
				}
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the custom parameters", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"knob_disable_posix_kernel_aio = 1",
				}, "\n")))
			})
		})

		Context("with an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should use the variable as the zone ID", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $RACK",
				}, "\n")))
			})
		})

		Context("with a version that can use binaries from the main container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithBinariesFromMainContainer.String()
				cluster.Spec.RunningVersion = Versions.WithBinariesFromMainContainer.String()
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())

			})

			It("should use the binaries from the BINARY_DIR", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a version with binaries from the sidecar container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithoutBinariesFromMainContainer.String()
				cluster.Spec.RunningVersion = Versions.WithoutBinariesFromMainContainer.String()
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should use the binaries from the dynamic conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = /var/dynamic-conf/bin/6.2.11/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the verification rules", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"tls_verify_peers = S.CN=foundationdb.org",
				}, "\n")))
			})
		})

		Context("with a custom log group", func() {
			BeforeEach(func() {
				cluster.Spec.LogGroup = "test-fdb-cluster"
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the log group", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = test-fdb-cluster",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
				conf, err = GetMonitorConf(cluster, "storage", nil, nil)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the log group", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = storage",
					"datadir = /var/fdb/data",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"locality_dcid = dc01",
				}, "\n")))
			})
		})
	})

	Describe("GetStartCommand", func() {
		var cluster *fdbtypes.FoundationDBCluster
		var pods *corev1.PodList
		var command string
		var err error

		BeforeEach(func() {
			cluster = createDefaultCluster()
			cluster.Spec.ConnectionString = ""
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			timeout := time.Second * 5
			Eventually(func() (fdbtypes.GenerationStatus, error) { return reloadClusterGenerations(k8sClient, cluster) }, timeout).Should(Equal(fdbtypes.GenerationStatus{Reconciled: 4}))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			pods = &corev1.PodList{}
			Eventually(func() (int, error) {
				err := k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				return len(pods.Items), err
			}, timeout).Should(Equal(17))

			sortPodsByID(pods)

			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanupCluster(cluster)
		})

		Context("for a basic storage process", func() {
			It("should substitute the variables in the start command", func() {
				instance := newFdbInstance(pods.Items[firstStorageIndex])
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: instance.Pod}
				command, err = GetStartCommand(cluster, instance, podClient)
				Expect(err).NotTo(HaveOccurred())

				id := instance.GetInstanceID()
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with host replication", func() {
			BeforeEach(func() {
				pod := pods.Items[firstStorageIndex]
				pod.Spec.NodeName = "machine1"
				pod.Status.PodIP = "127.0.0.1"
				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{}

				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should provide the host information in the start command", func() {
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					"--locality_instance_id=storage-1",
					"--locality_machineid=machine1",
					"--locality_zoneid=machine1",
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=127.0.0.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with cross-Kubernetes replication", func() {
			BeforeEach(func() {
				pod := pods.Items[firstStorageIndex]
				pod.Spec.NodeName = "machine1"
				pod.Status.PodIP = "127.0.0.1"

				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{
					Key:   "foundationdb.org/kubernetes-cluster",
					Value: "kc2",
				}

				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the zone ID in the start command", func() {
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					"--locality_instance_id=storage-1",
					"--locality_machineid=machine1",
					"--locality_zoneid=kc2",
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=127.0.0.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with binaries from the main container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithBinariesFromMainContainer.String()
				cluster.Spec.RunningVersion = Versions.WithBinariesFromMainContainer.String()
				pod := pods.Items[firstStorageIndex]
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("includes the binary path in the start command", func() {
				id := pods.Items[firstStorageIndex].Labels["fdb-instance-id"]
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with binaries from the sidecar container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithoutBinariesFromMainContainer.String()
				cluster.Spec.RunningVersion = Versions.WithoutBinariesFromMainContainer.String()
				pod := pods.Items[firstStorageIndex]
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("includes the binary path in the start command", func() {
				id := pods.Items[firstStorageIndex].Labels["fdb-instance-id"]
				Expect(command).To(Equal(strings.Join([]string{
					"/var/dynamic-conf/bin/6.2.11/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})
	})

	Describe("ParseInstanceID", func() {
		Context("with a storage ID", func() {
			It("can parse the ID", func() {
				prefix, id, err := ParseInstanceID("storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal("storage"))
				Expect(id).To(Equal(12))
			})
		})

		Context("with a cluster controller ID", func() {
			It("can parse the ID", func() {
				prefix, id, err := ParseInstanceID("cluster_controller-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal("cluster_controller"))
				Expect(id).To(Equal(3))
			})
		})

		Context("with a custom prefix", func() {
			It("parses the prefix", func() {
				prefix, id, err := ParseInstanceID("dc1-storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal("dc1-storage"))
				Expect(id).To(Equal(12))
			})
		})

		Context("with no prefix", func() {
			It("leaves the prefix blank", func() {
				prefix, id, err := ParseInstanceID("6")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal(""))
				Expect(id).To(Equal(6))
			})
		})

		Context("with no numbers", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseInstanceID("storage")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Could not parse instance ID storage"))
			})
		})

		Context("with a text suffix", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseInstanceID("storage-bad")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Could not parse instance ID storage-bad"))
			})
		})
	})
})

func cleanupCluster(cluster *fdbtypes.FoundationDBCluster) {
	err := k8sClient.Delete(context.TODO(), cluster)
	Expect(err).NotTo(HaveOccurred())

	pods := &corev1.PodList{}
	err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range pods.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}

	configMaps := &corev1.ConfigMapList{}
	err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range configMaps.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range pvcs.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}
}

func getProcessClassMap(pods []corev1.Pod) map[string]int {
	counts := make(map[string]int)
	for _, pod := range pods {
		ProcessClass := pod.Labels["fdb-process-class"]
		counts[ProcessClass]++
	}
	return counts
}
