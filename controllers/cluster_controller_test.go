/*
 * cluster_controller_test.go
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
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"github.com/prometheus/common/expfmt"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

var firstLogIndex = 1
var firstStorageIndex = 13

// reloadCluster reloads fdbcluster object info to client.
// It's necessary in unit test when reconciler changes fdbserver object but client's state is not updated.
// We reloadCluster to bring client state consistent with the latest fdbcluster object
func reloadCluster(cluster *fdbv1beta2.FoundationDBCluster) (int64, error) {
	generations, err := reloadClusterGenerations(cluster)
	if generations.HasPendingRemoval > 0 {
		return 0, err
	}
	return generations.Reconciled, err
}

func reloadClusterGenerations(cluster *fdbv1beta2.FoundationDBCluster) (fdbv1beta2.ClusterGenerationStatus, error) {
	key := client.ObjectKeyFromObject(cluster)
	*cluster = fdbv1beta2.FoundationDBCluster{}
	err := k8sClient.Get(context.TODO(), key, cluster)
	if err != nil {
		return fdbv1beta2.ClusterGenerationStatus{}, err
	}
	return cluster.Status.Generations, err
}

func getListOptions(cluster *fdbv1beta2.FoundationDBCluster) []client.ListOption {
	return []client.ListOption{
		client.InNamespace("my-ns"),
		client.MatchingLabels(map[string]string{
			fdbv1beta2.FDBClusterLabel: cluster.Name,
		}),
	}
}

func sortPodsByName(pods *corev1.PodList) {
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].ObjectMeta.Name < pods.Items[j].ObjectMeta.Name
	})
}

var _ = Describe("cluster_controller", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var fakeConnectionString string

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		fakeConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
	})

	Describe("Reconciliation", func() {
		var originalPods *corev1.PodList
		var originalVersion int64
		var err error
		var generationGap int64
		var shouldCompleteReconciliation bool

		BeforeEach(func() {
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))

			originalVersion = cluster.ObjectMeta.Generation

			originalPods = &corev1.PodList{}
			err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(originalPods.Items)).To(Equal(17))

			sortPodsByName(originalPods)

			generationGap = 1
			shouldCompleteReconciliation = true
		})

		JustBeforeEach(func() {
			result, err := reconcileCluster(cluster)

			if err != nil && !shouldCompleteReconciliation {
				return
			}

			Expect(err).NotTo(HaveOccurred())

			Expect(result.Requeue).To(Equal(!shouldCompleteReconciliation))

			if shouldCompleteReconciliation {
				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(originalVersion + generationGap))
			}
		})

		AfterEach(func() {
			k8sClient.Clear()
		})

		Context("when reconciling a new cluster", func() {
			BeforeEach(func() {
				generationGap = 0
			})

			It("should create pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(17))

				sortPodsByName(pods)

				Expect(pods.Items[0].Name).To(Equal("operator-test-1-cluster-controller-1"))
				Expect(pods.Items[0].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("cluster_controller-1"))
				Expect(pods.Items[0].Annotations[fdbv1beta2.PublicIPSourceAnnotation]).To(Equal("pod"))
				Expect(pods.Items[0].Annotations[fdbv1beta2.PublicIPAnnotation]).To(Equal(""))

				Expect(pods.Items[1].Name).To(Equal("operator-test-1-log-1"))
				Expect(pods.Items[1].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("log-1"))
				Expect(pods.Items[4].Name).To(Equal("operator-test-1-log-4"))
				Expect(pods.Items[4].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("log-4"))
				Expect(pods.Items[5].Name).To(Equal("operator-test-1-stateless-1"))
				Expect(pods.Items[5].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("stateless-1"))
				Expect(pods.Items[12].Name).To(Equal("operator-test-1-stateless-8"))
				Expect(pods.Items[12].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("stateless-8"))
				Expect(pods.Items[13].Name).To(Equal("operator-test-1-storage-1"))
				Expect(pods.Items[13].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("storage-1"))
				Expect(pods.Items[16].Name).To(Equal("operator-test-1-storage-4"))
				Expect(pods.Items[16].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("storage-4"))

				Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
					fdbv1beta2.ProcessClassStorage:           4,
					fdbv1beta2.ProcessClassLog:               4,
					fdbv1beta2.ProcessClassStateless:         8,
					fdbv1beta2.ProcessClassClusterController: 1,
				}))

				pod := &pods.Items[0]
				configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta), pod)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey]).To(Equal(configMapHash))
				Expect(len(cluster.Status.ProcessGroups)).To(Equal(len(pods.Items)))
			})

			It("should not create any services", func() {
				services := &corev1.ServiceList{}
				err := k8sClient.List(context.TODO(), services, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(services.Items)).To(Equal(0))
			})

			It("should fill in the required fields in the configuration", func() {
				Expect(cluster.Status.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))
				Expect(cluster.Status.DatabaseConfiguration.StorageEngine).To(Equal(fdbv1beta2.StorageEngineSSD2))
				Expect(cluster.Status.ConnectionString).NotTo(Equal(""))
			})

			It("should create a config map for the cluster", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				err = k8sClient.Get(context.TODO(), configMapName, configMap)
				Expect(err).NotTo(HaveOccurred())
				expectedConfigMap, _ := internal.GetConfigMap(cluster)
				Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
			})

			It("should send the configuration to the cluster", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())
				Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))
				Expect(adminClient.DatabaseConfiguration.StorageEngine).To(Equal(fdbv1beta2.StorageEngineSSD2))
				Expect(adminClient.DatabaseConfiguration.RoleCounts).To(Equal(fdbv1beta2.RoleCounts{
					Logs:          3,
					Proxies:       3,
					CommitProxies: 0,
					GrvProxies:    0,
					Resolvers:     1,
					RemoteLogs:    -1,
					LogRouters:    -1,
				}))
			})

			It("should send the configuration to the cluster", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())
				Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))
				Expect(adminClient.DatabaseConfiguration.StorageEngine).To(Equal(fdbv1beta2.StorageEngineSSD2))
				Expect(adminClient.DatabaseConfiguration.RoleCounts).To(Equal(fdbv1beta2.RoleCounts{
					Logs:       3,
					Proxies:    3,
					Resolvers:  1,
					RemoteLogs: -1,
					LogRouters: -1,
				}))
			})

			It("should update the status with the reconciliation result", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				Expect(cluster.Status.Generations.Reconciled).To(Equal(int64(1)))

				processCounts := fdbv1beta2.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true)
				Expect(processCounts).To(Equal(fdbv1beta2.ProcessCounts{
					Storage:           4,
					Log:               4,
					Stateless:         8,
					ClusterController: 1,
				}))

				desiredCounts, err := cluster.GetProcessCountsWithDefaults()
				Expect(err).NotTo(HaveOccurred())
				Expect(processCounts).To(Equal(desiredCounts))
				Expect(len(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false))).To(Equal(0))
				Expect(len(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.MissingProcesses, false))).To(Equal(0))

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				configuration := status.Cluster.DatabaseConfiguration.DeepCopy()
				configuration.LogSpill = 0
				Expect(cluster.Status.DatabaseConfiguration).To(Equal(*configuration))

				Expect(cluster.Status.Health).To(Equal(fdbv1beta2.ClusterHealth{
					Available:            true,
					Healthy:              true,
					FullReplication:      true,
					DataMovementPriority: 0,
				}))

				Expect(cluster.Status.StorageServersPerDisk).To(Equal([]int{1}))
				Expect(cluster.Status.ImageTypes).To(Equal([]fdbv1beta2.ImageType{"split"}))
			})
		})

		When("converting a cluster to use unified images", func() {
			BeforeEach(func() {
				// There is a bug in the fake client that when updating the status the spec is updated.
				cluster.Spec.MainContainer.ImageConfigs = nil
				cluster.Spec.SidecarContainer.ImageConfigs = nil
				cluster.Spec.UseUnifiedImage = pointer.Bool(true)
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should update the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

				for _, pod := range pods.Items {
					Expect(pod.Spec.Containers[0].Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes:%s", fdbv1beta2.Versions.Default)))
					Expect(pod.Spec.Containers[1].Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(pod.Spec.Containers[1].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes:%s", fdbv1beta2.Versions.Default)))
				}
			})

			It("should update the status", func() {
				Expect(cluster.Status.ImageTypes).To(Equal([]fdbv1beta2.ImageType{"unified"}))
			})
		})

		When("enabling the DNS names in the cluster file", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range pods.Items {
					env := make(map[string]string)
					for _, envVar := range pod.Spec.InitContainers[0].Env {
						env[envVar.Name] = envVar.Value
					}
					Expect(env["FDB_DNS_NAME"]).To(Equal(internal.GetPodDNSName(cluster, pod.Name)))
				}
			})
		})
		Context("when buggifying a pod to make it crash loop", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1"}
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the crash loop flag", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))
				sortPodsByName(pods)

				pod := pods.Items[firstStorageIndex]
				Expect(pod.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("storage-1"))

				mainContainer := pod.Spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).To(Equal([]string{"crash-loop"}))
			})
		})

		Context("when buggifying an empty fdbmonitor conf", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.EmptyMonitorConf = true
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				// The buggify config causes the reconciliation loop to never finish with the mock admin client.
				shouldCompleteReconciliation = false
			})

			It("should update conf and bounce processes", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))
				sortPodsByName(pods)

				cm, _ := internal.GetConfigMap(cluster)

				for _, file := range cm.Data {
					Expect(file).To(Not(ContainSubstring("fdbserver")))
				}

				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())

				// All of the processes in the cluster should be killed.
				processes := map[string]fdbv1beta2.None{}
				for _, processGroup := range cluster.Status.ProcessGroups {
					for i, addr := range processGroup.Addresses {
						// +1 since the process list uses 1-based indexing.
						fullAddr := cluster.GetFullAddress(addr, i+1)
						processes[fullAddr.String()] = fdbv1beta2.None{}
					}
				}
				Expect(adminClient.KilledAddresses).To(Equal(processes))
			})
		})

		Context("with a decreased process count", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Storage = 3
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))
				sortPodsByName(pods)

				Expect(pods.Items[0].Name).To(Equal(originalPods.Items[0].Name))
				Expect(pods.Items[1].Name).To(Equal(originalPods.Items[1].Name))
				Expect(pods.Items[2].Name).To(Equal(originalPods.Items[2].Name))

				Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
					fdbv1beta2.ProcessClassStorage:           3,
					fdbv1beta2.ProcessClassLog:               4,
					fdbv1beta2.ProcessClassStateless:         8,
					fdbv1beta2.ProcessClassClusterController: 1,
				}))

				Expect(cluster.Spec.ProcessGroupsToRemove).To(BeNil())

				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())
				Expect(adminClient.ExcludedAddresses).To(BeEmpty())

				removedItem := originalPods.Items[16]
				Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
					removedItem.Status.PodIP: true,
				}))
			})
		})

		Context("with an increased process count", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Storage = 5
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add additional pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items) + 1))

				Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
					fdbv1beta2.ProcessClassStorage:           5,
					fdbv1beta2.ProcessClassLog:               4,
					fdbv1beta2.ProcessClassStateless:         8,
					fdbv1beta2.ProcessClassClusterController: 1,
				}))
			})

			It("should update the config map", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				err = k8sClient.Get(context.TODO(), configMapName, configMap)
				Expect(err).NotTo(HaveOccurred())
				expectedConfigMap, _ := internal.GetConfigMap(cluster)
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
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(18))

				Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
					fdbv1beta2.ProcessClassStorage:           4,
					fdbv1beta2.ProcessClassLog:               4,
					fdbv1beta2.ProcessClassStateless:         9,
					fdbv1beta2.ProcessClassClusterController: 1,
				}))
			})

			It("should update the config map", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				err = k8sClient.Get(context.TODO(), configMapName, configMap)
				Expect(err).NotTo(HaveOccurred())
				expectedConfigMap, _ := internal.GetConfigMap(cluster)
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
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(18))

				Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
					fdbv1beta2.ProcessClassStorage:           4,
					fdbv1beta2.ProcessClassLog:               4,
					fdbv1beta2.ProcessClassStateless:         9,
					fdbv1beta2.ProcessClassClusterController: 1,
				}))
			})
		})

		Context("with a negative stateless process count", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Stateless = -1
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove the stateless pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(9))

				Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
					fdbv1beta2.ProcessClassStorage:           4,
					fdbv1beta2.ProcessClassLog:               4,
					fdbv1beta2.ProcessClassClusterController: 1,
				}))
			})

			It("should clear removals from the process group status", func() {
				processGroups := cluster.Status.ProcessGroups
				Expect(len(processGroups)).To(Equal(9))

				for _, group := range processGroups {
					Expect(group.IsMarkedForRemoval()).To(BeFalse())
				}
			})
		})

		Context("with a coordinator replacement", func() {
			var originalConnectionString string

			BeforeEach(func() {
				originalConnectionString = cluster.Status.ConnectionString
			})

			Context("with an entry in the process groups to remove list", func() {
				BeforeEach(func() {
					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
						fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should keep the process counts the same", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
						fdbv1beta2.ProcessClassStorage:           4,
						fdbv1beta2.ProcessClassLog:               4,
						fdbv1beta2.ProcessClassStateless:         8,
						fdbv1beta2.ProcessClassClusterController: 1,
					}))
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByName(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should exclude and re-include the process group", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeEmpty())

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
						originalPods.Items[firstStorageIndex].Status.PodIP: true,
					}))
				})

				It("should not change the connection string", func() {
					Expect(cluster.Status.ConnectionString).To(Equal(originalConnectionString))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.ProcessGroupsToRemove).To(Equal([]fdbv1beta2.ProcessGroupID{
						fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					}))
				})

				It("should clear removals from the process group status", func() {
					processGroups := cluster.Status.ProcessGroups
					Expect(len(processGroups)).To(Equal(len(originalPods.Items)))

					Expect(fdbv1beta2.ContainsProcessGroupID(processGroups, "storage-5")).To(BeTrue())
					oldID := originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]
					Expect(fdbv1beta2.ContainsProcessGroupID(processGroups, fdbv1beta2.ProcessGroupID(oldID))).To(BeFalse())

					for _, group := range processGroups {
						Expect(group.IsMarkedForRemoval()).To(BeFalse())
					}
				})

				Context("with a pod stuck in terminating", func() {
					BeforeEach(func() {
						pod := originalPods.Items[firstStorageIndex]
						for _, processGroup := range cluster.Status.ProcessGroups {
							if processGroup.ProcessGroupID == fdbv1beta2.ProcessGroupID(pod.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]) {
								processGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true, nil, "")
							}
						}

						err = k8sClient.Status().Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())

						err = k8sClient.MockStuckTermination(&pod, true)
						Expect(err).NotTo(HaveOccurred())
						// The returned generation in the test case will be 0 since we have 2 pending removals
						// which is expected.
						generationGap = -1
					})

					It("should set the generation to both reconciled and pending removal", func() {
						_, err = reloadCluster(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(cluster.Status.Generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
							Reconciled:        2,
							HasPendingRemoval: 2,
						}))
					})

					It("should exclude but not re-include the process group", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient).NotTo(BeNil())
						Expect(adminClient.ReincludedAddresses).To(HaveLen(0))
						Expect(adminClient.ExcludedAddresses).To(HaveLen(1))
						Expect(adminClient.ExcludedAddresses).To(HaveKey(
							originalPods.Items[firstStorageIndex].Status.PodIP,
						))
					})
				})

				Context("with a PVC stuck in terminating", func() {
					BeforeEach(func() {
						originalPod := &originalPods.Items[firstStorageIndex]
						for _, processGroup := range cluster.Status.ProcessGroups {
							if processGroup.ProcessGroupID == fdbv1beta2.ProcessGroupID(originalPod.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]) {
								processGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true, nil, "")
							}
						}

						pvc := &corev1.PersistentVolumeClaim{}
						err = k8sClient.Get(context.TODO(), client.ObjectKey{
							Namespace: originalPod.Namespace,
							Name:      fmt.Sprintf("%s-data", originalPod.Name),
						}, pvc)
						Expect(err).NotTo(HaveOccurred())

						err = k8sClient.Status().Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())

						err = k8sClient.MockStuckTermination(pvc, true)
						Expect(err).NotTo(HaveOccurred())
						// The returned generation in the test case will be 0 since we have 2 pending removals
						// which is expected.
						generationGap = -1
					})

					It("should set the generation to both reconciled and pending removal", func() {
						_, err = reloadCluster(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(cluster.Status.Generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
							Reconciled:        2,
							HasPendingRemoval: 2,
						}))
					})

					It("should exclude but not re-include the process group", func() {
						adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient).NotTo(BeNil())
						Expect(adminClient.ReincludedAddresses).To(HaveLen(0))
						Expect(adminClient.ExcludedAddresses).To(HaveLen(1))
						Expect(adminClient.ExcludedAddresses).To(HaveKey(
							originalPods.Items[firstStorageIndex].Status.PodIP,
						))
					})
				})
			})

			Context("with cluster skip enabled", func() {
				BeforeEach(func() {
					cluster.Spec.Skip = true
					// Since we don't Reconcile we don't expect a generationGap
					generationGap = 0
				})

				Context("with an entry in the process groups to remove list", func() {
					BeforeEach(func() {
						cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
							fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
						}
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})

					It("should not replace the pod", func() {
						pods := &corev1.PodList{}
						err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
						Expect(err).NotTo(HaveOccurred())
						Expect(len(pods.Items)).To(Equal(17))

						sortPodsByName(pods)

						for i := 0; i < 4; i++ {
							Expect(pods.Items[firstStorageIndex+i].Name).To(Equal(originalPods.Items[firstStorageIndex+i].Name))
						}
					})
				})
			})

			Context("with a missing pod IP", func() {
				var podIP string

				BeforeEach(func() {
					podIP = originalPods.Items[firstStorageIndex].Status.PodIP

					err := k8sClient.RemovePodIP(&originalPods.Items[firstStorageIndex])
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
						fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByName(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should exclude and re-include the process group", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeEmpty())

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{podIP: true}))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.ProcessGroupsToRemove).To(Equal([]fdbv1beta2.ProcessGroupID{
						fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					}))
				})
			})

			Context("with a removal with no exclusion", func() {
				BeforeEach(func() {
					err := k8sClient.RemovePodIP(&originalPods.Items[firstStorageIndex])
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []fdbv1beta2.ProcessGroupID{
						fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByName(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should not exclude anything", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeEmpty())
					Expect(adminClient.ReincludedAddresses).To(BeEmpty())
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion).To(Equal([]fdbv1beta2.ProcessGroupID{
						fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					}))
				})
			})
		})

		Context("with a missing process", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				adminClient.MockMissingProcessGroup("storage-1", true)

				// Run a single reconciliation to detect the missing process.
				result, err := reconcileObject(clusterReconciler, cluster.ObjectMeta, 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())

				// Tweak the time on the missing process to make it eligible for replacement.
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-1")
				Expect(processGroup).NotTo(BeNil())
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.MissingProcesses))
				Expect(processGroup.ProcessGroupConditions[0].Timestamp).NotTo(Equal(0))
				processGroup.ProcessGroupConditions[0].Timestamp -= 3600
				Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				generationGap = 0
			})

			It("should replace the pod", func() {
				err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, internal.GetSinglePodListOptions(cluster, "storage-1")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(0))

				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(17))

				sortPodsByName(pods)

				Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
				Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
				Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
				Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
			})
		})

		Context("with missing processes and pending exclusion", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
					fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				adminClient.MockMissingProcessGroup("storage-2", true)
				adminClient.MockMissingProcessGroup("storage-3", true)
				shouldCompleteReconciliation = false
				generationGap = 0
			})

			JustBeforeEach(func() {
				generations, err := reloadClusterGenerations(cluster)
				Expect(err).NotTo(HaveOccurred())
				// Since we delay the requeue we are able to choose new
				// coordinators.
				Expect(generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
					Reconciled:          originalVersion,
					NeedsShrink:         originalVersion + 1,
					HasUnhealthyProcess: originalVersion + 1,
				}))
			})

			It("should not exclude or remove the process", func() {
				err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				Expect(adminClient.ExcludedAddresses).To(BeEmpty())
				Expect(adminClient.ReincludedAddresses).To(BeEmpty())

				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, internal.GetSinglePodListOptions(cluster, "storage-2")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(1))

				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(18))
			})
		})

		Context("with multiple replacements", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
					fdbv1beta2.ProcessGroupID(originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
					"storage-5",
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should replace one of the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(17))

				sortPodsByName(pods)

				Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
				Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
				Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
				Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-6"))
			})
		})

		Context("with a pod that gets deleted", func() {
			var pod corev1.Pod
			BeforeEach(func() {
				err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				generationGap = 0

				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, internal.GetSinglePodListOptions(cluster, "storage-1")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(1))
				pod = pods.Items[0]
				err := k8sClient.Delete(context.TODO(), &pod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should replace the pod", func() {
				err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, internal.GetSinglePodListOptions(cluster, "storage-1")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(1))
				Expect(pods.Items[0].ObjectMeta.UID).NotTo(Equal(pod.ObjectMeta.UID))
				Expect(pods.Items[0].Name).To(Equal("operator-test-1-storage-1"))
			})
		})

		Context("with a knob change", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				err = adminClient.FreezeStatus()
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{"knob_disable_posix_kernel_aio=1"}}}
			})

			Context("with bounces enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[cluster.GetFullAddress(pod.Status.PodIP, 1).String()] = fdbv1beta2.None{}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should update the config map", func() {
					configMap := &corev1.ConfigMap{}
					configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
					err = k8sClient.Get(context.TODO(), configMapName, configMap)
					Expect(err).NotTo(HaveOccurred())
					expectedConfigMap, _ := internal.GetConfigMap(cluster)
					Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
				})
			})

			Context("with a substitution variable", func() {
				BeforeEach(func() {
					settings := cluster.Spec.Processes["general"]
					settings.CustomParameters = []fdbv1beta2.FoundationDBCustomParameter{"locality_disk_id=$FDB_INSTANCE_ID"}
					cluster.Spec.Processes["general"] = settings

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[cluster.GetFullAddress(pod.Status.PodIP, 1).String()] = fdbv1beta2.None{}
					}

					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})
			})

			Context("with bounces disabled", func() {
				BeforeEach(func() {
					shouldCompleteReconciliation = false
					var flag = false
					cluster.Spec.AutomationOptions.KillProcesses = &flag
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
						Reconciled:          originalVersion,
						NeedsBounce:         originalVersion + 1,
						HasUnhealthyProcess: originalVersion + 1,
					}))
				})

				It("should not kill any processes", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})

				It("should update the config map", func() {
					configMap := &corev1.ConfigMap{}
					configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
					err = k8sClient.Get(context.TODO(), configMapName, configMap)
					Expect(err).NotTo(HaveOccurred())
					expectedConfigMap, _ := internal.GetConfigMap(cluster)
					Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
				})
			})

			Context("with multiple storage servers per pod", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{}}}
					cluster.Spec.StorageServersPerPod = 2
					adminClient.UnfreezeStatus()
					Expect(err).NotTo(HaveOccurred())
					err = k8sClient.Update(context.TODO(), cluster)

					result, err := reconcileCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Requeue).To(BeFalse())

					generation, err := reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generation).To(Equal(originalVersion + generationGap))
					originalVersion = cluster.ObjectMeta.Generation

					err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(originalPods.Items)).To(Equal(17))

					sortPodsByName(originalPods)

					err = adminClient.FreezeStatus()
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{"knob_disable_posix_kernel_aio=1"}}}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[cluster.GetFullAddress(pod.Status.PodIP, 1).String()] = fdbv1beta2.None{}
						if internal.ProcessClassFromLabels(cluster, pod.ObjectMeta.Labels) == fdbv1beta2.ProcessClassStorage {
							addresses[cluster.GetFullAddress(pod.Status.PodIP, 2).String()] = fdbv1beta2.None{}
						}
					}

					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})
			})
		})

		Context("with a configuration change", func() {
			var adminClient *mock.AdminClient
			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))

				cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeTriple

				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))
			})

			Context("with changes enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should configure the database", func() {
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeTriple))
				})
			})

			Context("with a change to the log version", func() {
				BeforeEach(func() {
					cluster.Spec.DatabaseConfiguration.LogVersion = 3
					cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeDouble
					generationGap = 1
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should configure the database", func() {
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))
					Expect(adminClient.DatabaseConfiguration.LogVersion).To(Equal(3))
				})
			})

			Context("with a change to the log version that is not set in the spec", func() {
				BeforeEach(func() {
					cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeDouble
					cluster.Spec.SeedConnectionString = "touch"

					configuration := cluster.DesiredDatabaseConfiguration()
					configuration.LogVersion = 3
					err = adminClient.ConfigureDatabase(configuration, false, cluster.Spec.Version)
					Expect(err).NotTo(HaveOccurred())

					generationGap = 1
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should not reconfigure the database", func() {
					Expect(adminClient.DatabaseConfiguration.LogVersion).To(Equal(3))
				})
			})

			Context("with changes disabled", func() {
				BeforeEach(func() {
					shouldCompleteReconciliation = false
					cluster.Spec.AutomationOptions.ConfigureDatabase = pointer.Bool(false)
					cluster.Spec.DatabaseConfiguration.RedundancyMode = fdbv1beta2.RedundancyModeTriple

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
						Reconciled:               originalVersion,
						NeedsConfigurationChange: originalVersion + 1,
					}))
				})

				It("should not change the database configuration", func() {
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal(fdbv1beta2.RedundancyModeDouble))
				})
			})
		})

		Context("with a change to pod labels", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value3",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{},
					},
				}}}
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

		Context("with annotations on pod", func() {
			BeforeEach(func() {
				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())
				pod.Annotations["foundationdb.org/existing-annotation"] = "test-value"
				err = k8sClient.Update(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{},
					},
				}}}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the annotations on the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				for _, item := range pods.Items {
					_, id, err := podmanager.ParseProcessGroupID(fdbv1beta2.ProcessGroupID(item.Labels[fdbv1beta2.FDBProcessGroupIDLabel]))
					Expect(err).NotTo(HaveOccurred())

					hash, err := internal.GetPodSpecHash(cluster, internal.ProcessClassFromLabels(cluster, item.Labels), id, nil)
					Expect(err).NotTo(HaveOccurred())

					configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, item.ObjectMeta), &item)
					Expect(err).NotTo(HaveOccurred())
					if item.Labels[fdbv1beta2.FDBProcessGroupIDLabel] == "storage-1" {
						Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
							"foundationdb.org/last-applied-config-map": configMapHash,
							"foundationdb.org/last-applied-spec":       hash,
							"foundationdb.org/public-ip-source":        "pod",
							"foundationdb.org/existing-annotation":     "test-value",
							"fdb-annotation":                           "value1",
						}))
					} else {
						Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
							"foundationdb.org/last-applied-config-map": configMapHash,
							"foundationdb.org/last-applied-spec":       hash,
							"foundationdb.org/public-ip-source":        "pod",
							"fdb-annotation":                           "value1",
						}))
					}
				}
			})

			It("should not set the annotations on other resources", func() {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"foundationdb.org/last-applied-spec": "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
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

		Context("with a change to the PVC labels", func() {
			Context("with the fields from the processes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"fdb-label": "value3",
							},
						},
					}}}
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
		})

		Context("with a change to PVC annotations", func() {
			Context("with the fields from the processes", func() {
				BeforeEach(func() {
					pvc := &corev1.PersistentVolumeClaim{}
					err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1-data"}, pvc)
					Expect(err).NotTo(HaveOccurred())
					pvc.Annotations["foundationdb.org/existing-annotation"] = "test-value"
					err = k8sClient.Update(context.TODO(), pvc)
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"fdb-annotation": "value1",
							},
						},
					}}}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should update the annotations on the PVCs", func() {
					pvcs := &corev1.PersistentVolumeClaimList{}
					err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					for _, item := range pvcs.Items {
						if item.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel] == "storage-1" {
							Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
								"fdb-annotation":                       "value1",
								"foundationdb.org/existing-annotation": "test-value",
								"foundationdb.org/last-applied-spec":   "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
							}))
						} else {
							Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
								"fdb-annotation":                     "value1",
								"foundationdb.org/last-applied-spec": "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
							}))

						}
					}
				})

				It("should not update the annotations on other resources", func() {
					pods := &corev1.PodList{}

					err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
					Expect(err).NotTo(HaveOccurred())

					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					for _, item := range pods.Items {
						_, id, err := podmanager.ParseProcessGroupID(fdbv1beta2.ProcessGroupID(item.Labels[fdbv1beta2.FDBProcessGroupIDLabel]))
						Expect(err).NotTo(HaveOccurred())

						hash, err := internal.GetPodSpecHash(cluster, internal.ProcessClassFromLabels(cluster, item.Labels), id, nil)
						Expect(err).NotTo(HaveOccurred())

						configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, item.ObjectMeta), &item)
						Expect(err).NotTo(HaveOccurred())
						Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
							"foundationdb.org/last-applied-config-map": configMapHash,
							"foundationdb.org/last-applied-spec":       hash,
							"foundationdb.org/public-ip-source":        "pod",
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
				configMap := &corev1.ConfigMap{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-config"}, configMap)
				Expect(err).NotTo(HaveOccurred())
				configMap.Annotations = make(map[string]string)
				configMap.Annotations["foundationdb.org/existing-annotation"] = "test-value"
				err = k8sClient.Update(context.TODO(), configMap)
				Expect(err).NotTo(HaveOccurred())

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
						"fdb-annotation":                       "value1",
						"foundationdb.org/existing-annotation": "test-value",
					}))
				}
			})

			It("should not update the annotations on the other resources", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				for _, item := range pods.Items {
					_, id, err := podmanager.ParseProcessGroupID(fdbv1beta2.ProcessGroupID(item.Labels[fdbv1beta2.FDBProcessGroupIDLabel]))
					Expect(err).NotTo(HaveOccurred())

					hash, err := internal.GetPodSpecHash(cluster, internal.ProcessClassFromLabels(cluster, item.Labels), id, nil)
					Expect(err).NotTo(HaveOccurred())

					configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, item.ObjectMeta), &item)
					Expect(err).NotTo(HaveOccurred())
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"foundationdb.org/last-applied-config-map": configMapHash,
						"foundationdb.org/last-applied-spec":       hash,
						"foundationdb.org/public-ip-source":        "pod",
					}))
				}

				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						"foundationdb.org/last-applied-spec": "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
					}))
				}
			})
		})

		Context("with a change to environment variables", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: fdbv1beta2.MainContainerName,
								Env: []corev1.EnvVar{
									{
										Name:  "TEST_CHANGE",
										Value: "1",
									},
								},
							},
						},
					},
				}}}
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
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(2))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("TEST_CHANGE"))
						Expect(pod.Spec.Containers[0].Env[0].Value).To(Equal("1"))
					}
				})
			})

			Context("with deletion disabled", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.DeletionMode = fdbv1beta2.PodUpdateModeNone
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyDelete
					shouldCompleteReconciliation = false

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
						Reconciled:          originalVersion,
						NeedsPodDeletion:    originalVersion + 1,
						HasUnhealthyProcess: originalVersion + 1,
					}))
				})

				It("should not set the environment variable on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(1))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("FDB_CLUSTER_FILE"))
					}
				})
			})

			Context("with deletion mode none", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.DeletionMode = fdbv1beta2.PodUpdateModeNone
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyDelete
					shouldCompleteReconciliation = false

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
						Reconciled:          originalVersion,
						NeedsPodDeletion:    originalVersion + 1,
						HasUnhealthyProcess: originalVersion + 1,
					}))
				})

				It("should not set the environment variable on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(len(pod.Spec.Containers[0].Env)).To(Equal(1))
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal("FDB_CLUSTER_FILE"))
					}
				})
			})
		})

		Context("with a change to the public IP source", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the public IP annotations", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range pods.Items {
					Expect(pod.Annotations[fdbv1beta2.PublicIPSourceAnnotation]).To(Equal("service"))
				}

				pod := pods.Items[0]
				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, service)
				Expect(err).NotTo(HaveOccurred())

				Expect(pod.Annotations[fdbv1beta2.PublicIPAnnotation]).To(Equal(service.Spec.ClusterIP))
				Expect(pod.Annotations[fdbv1beta2.PublicIPAnnotation]).NotTo(Equal(""))
				Expect(len(service.Spec.Ports)).To(Equal(cluster.GetStorageServersPerPod() * 2))
			})

			It("should create services for the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				services := &corev1.ServiceList{}
				err = k8sClient.List(context.TODO(), services, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(services.Items)).To(Equal(len(pods.Items)))

				service := &corev1.Service{}
				pod := pods.Items[0]
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, service)
				Expect(err).NotTo(HaveOccurred())
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
				Expect(len(service.Spec.Ports)).To(Equal(cluster.GetStorageServersPerPod() * 2))
			})

			It("should replace the old processes", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				originalNames := make([]string, 0, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					originalNames = append(originalNames, pod.Name)
				}

				currentNames := make([]string, 0, len(originalPods.Items))
				for _, pod := range pods.Items {
					currentNames = append(currentNames, pod.Name)
				}

				Expect(currentNames).NotTo(ContainElements(originalNames))
			})
		})

		Context("when enabling explicit listen addresses", func() {
			BeforeEach(func() {
				cluster.Spec.UseExplicitListenAddress = pointer.Bool(false)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				err = k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(cluster), cluster)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.UseExplicitListenAddress = pointer.Bool(true)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				generationGap = 2
			})

			It("should set the FDB_POD_IP on the pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, pod := range pods.Items {
					container := pod.Spec.Containers[1]
					Expect(container.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					var podIPEnv corev1.EnvVar
					for _, env := range container.Env {
						if env.Name == "FDB_POD_IP" {
							podIPEnv = env
						}
					}
					Expect(podIPEnv.Name).To(Equal("FDB_POD_IP"))
					Expect(podIPEnv.ValueFrom).NotTo(BeNil())
					Expect(podIPEnv.ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
				}
			})
		})

		Context("with a change to the public IP source and multiple storage servers per Pod", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Spec.StorageServersPerPod = 2
				err = k8sClient.Update(context.TODO(), cluster)
			})

			It("should set the public IP annotations", func() {
				pods := &corev1.PodList{}
				ContainOriginalPod := func(idx int) gomegatypes.GomegaMatcher {
					return ContainElement(WithTransform(func(pod corev1.Pod) string {
						return pod.Name
					}, Equal(originalPods.Items[idx].Name)))
				}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				sortPodsByName(pods)
				Expect(pods.Items).To(HaveLen(len(originalPods.Items)))

				Expect(pods.Items).NotTo(ContainOriginalPod(13))
				Expect(pods.Items).NotTo(ContainOriginalPod(14))
				Expect(pods.Items).NotTo(ContainOriginalPod(15))
				Expect(pods.Items).NotTo(ContainOriginalPod(16))

				var storagePod corev1.Pod
				for _, pod := range pods.Items {
					Expect(pod.Annotations[fdbv1beta2.PublicIPSourceAnnotation]).To(Equal("service"))

					if internal.ProcessClassFromLabels(cluster, pod.Labels) == fdbv1beta2.ProcessClassStorage && storagePod.Name == "" {
						storagePod = pod
					}
				}

				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: storagePod.Namespace, Name: storagePod.Name}, service)
				Expect(err).NotTo(HaveOccurred())

				Expect(storagePod.Annotations[fdbv1beta2.PublicIPAnnotation]).To(Equal(service.Spec.ClusterIP))
				Expect(storagePod.Annotations[fdbv1beta2.PublicIPAnnotation]).NotTo(Equal(""))
				Expect(len(service.Spec.Ports)).To(Equal(cluster.GetStorageServersPerPod() * 2))
			})

			It("should create services for the pods", func() {
				pods := &corev1.PodList{}
				ContainOriginalPod := func(idx int) gomegatypes.GomegaMatcher {
					return ContainElement(WithTransform(func(pod corev1.Pod) string {
						return pod.Name
					}, Equal(originalPods.Items[idx].Name)))
				}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				sortPodsByName(pods)

				// Exactly as many pods as we started with
				Expect(pods.Items).To(HaveLen(len(originalPods.Items)))
				// But the original storage pods should all be replaced
				// with newly named storage pods
				Expect(pods.Items).NotTo(ContainOriginalPod(13))
				Expect(pods.Items).NotTo(ContainOriginalPod(14))
				Expect(pods.Items).NotTo(ContainOriginalPod(15))
				Expect(pods.Items).NotTo(ContainOriginalPod(16))

				services := &corev1.ServiceList{}
				err = k8sClient.List(context.TODO(), services, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(services.Items)).To(Equal(len(pods.Items)))

				var storagePod corev1.Pod
				for _, pod := range pods.Items {
					if internal.ProcessClassFromLabels(cluster, pod.Labels) == fdbv1beta2.ProcessClassStorage && storagePod.Name == "" {
						storagePod = pod
						break
					}
				}

				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: storagePod.Namespace, Name: storagePod.Name}, service)
				Expect(err).NotTo(HaveOccurred())
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
				Expect(len(service.Spec.Ports)).To(Equal(cluster.GetStorageServersPerPod() * 2))
			})

			It("should replace the old processes", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				originalNames := make([]string, 0, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					originalNames = append(originalNames, pod.Name)
				}

				currentNames := make([]string, 0, len(originalPods.Items))
				for _, pod := range pods.Items {
					currentNames = append(currentNames, pod.Name)
				}

				Expect(currentNames).NotTo(ContainElements(originalNames))
			})
		})

		Context("with a change to TLS settings", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should bounce the processes", func() {
				addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					addresses[fmt.Sprintf("%s:4500:tls", pod.Status.PodIP)] = fdbv1beta2.None{}
				}

				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient.KilledAddresses).To(Equal(addresses))
			})

			It("should change the coordinators to use TLS", func() {
				connectionString, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)

				Expect(err).NotTo(HaveOccurred())
				for _, coordinator := range connectionString.Coordinators {
					Expect(coordinator).To(HaveSuffix("tls"))
				}
			})
		})

		Context("with a conversion to IPv6", func() {
			BeforeEach(func() {
				family := 6
				cluster.Spec.Routing.PodIPFamily = &family
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should make the processes listen on an IPV6 address", func() {
				address1 := cluster.Status.ProcessGroups[1].Addresses[0]
				address2 := cluster.Status.ProcessGroups[2].Addresses[0]
				address3 := cluster.Status.ProcessGroups[3].Addresses[0]
				Expect(cluster.Status.ConnectionString).To(HaveSuffix(fmt.Sprintf("@[%s]:4501,[%s]:4501,[%s]:4501", address1, address2, address3)))
			})
		})

		Context("with a newly created IPv6 cluster", func() {
			BeforeEach(func() {
				k8sClient.Clear()
				mock.ClearMockAdminClients()
				mock.ClearMockLockClients()

				cluster = internal.CreateDefaultCluster()
				family := 6
				cluster.Spec.Routing.PodIPFamily = &family

				err = k8sClient.Create(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(int64(1)))

				originalVersion = cluster.ObjectMeta.Generation

				originalPods = &corev1.PodList{}
				err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(originalPods.Items)).To(Equal(17))

				sortPodsByName(originalPods)

				generationGap = 0
			})

			It("should make the processes listen on an IPV6 address", func() {
				address1 := cluster.Status.ProcessGroups[firstLogIndex].Addresses[0]
				address2 := cluster.Status.ProcessGroups[firstLogIndex+1].Addresses[0]
				address3 := cluster.Status.ProcessGroups[firstLogIndex+2].Addresses[0]
				Expect(cluster.Status.ConnectionString).To(HaveSuffix(fmt.Sprintf("@[%s]:4501,[%s]:4501,[%s]:4501", address1, address2, address3)))
			})
		})

		Context("with only storage processes as coordinator", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     0,
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should only have storage processes as coordinator", func() {
				connectionString, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)
				Expect(err).NotTo(HaveOccurred())

				addressClassMap := map[string]fdbv1beta2.ProcessClass{}
				for _, pGroup := range cluster.Status.ProcessGroups {
					addressClassMap[fmt.Sprintf("%s:4501", pGroup.Addresses[0])] = pGroup.ProcessClass
				}

				for _, coordinator := range connectionString.Coordinators {
					Expect(addressClassMap[coordinator]).To(Equal(fdbv1beta2.ProcessClassStorage))
				}
			})

			When("changing the coordinator selection to only select log processes", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbv1beta2.ProcessClassLog,
							Priority:     0,
						},
					}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
					generationGap++
				})

				It("should only have log processes as coordinator", func() {
					connectionString, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)
					Expect(err).NotTo(HaveOccurred())

					addressClassMap := map[string]fdbv1beta2.ProcessClass{}
					for _, pGroup := range cluster.Status.ProcessGroups {
						addressClassMap[fmt.Sprintf("%s:4501", pGroup.Addresses[0])] = pGroup.ProcessClass
					}

					for _, coordinator := range connectionString.Coordinators {
						Expect(addressClassMap[coordinator]).To(Equal(fdbv1beta2.ProcessClassLog))
					}
				})
			})
		})

		Context("downgrade cluster", func() {
			When("downgrading a cluster to another patch version", func() {
				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.PreviousPatchVersion.String()
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
					requeueLimit = 50
				})

				It("should downgrade the cluster", func() {
					Expect(cluster.Status.Generations.Reconciled).To(Equal(originalVersion + 1))
					Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.PreviousPatchVersion.String()))
				})
			})

			When("downgrading a cluster to another major version", func() {
				BeforeEach(func() {
					shouldCompleteReconciliation = false
					cluster.Spec.Version = fdbv1beta2.Versions.IncompatibleVersion.String()
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should not downgrade the cluster", func() {
					Expect(cluster.Status.Generations.Reconciled).To(Equal(originalVersion))
					Expect(cluster.Status.RunningVersion).To(Equal(fdbv1beta2.Versions.Default.String()))
				})
			})
		})

		Context("with a patch upgrade", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextPatchVersion.String()
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				// Increase the requeue limit here since the operator has do multiple things before being reconciled
				requeueLimit = 50
			})

			Context("with the delete strategy", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyDelete
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should upgrade the cluster", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextPatchVersion.String())))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})
			})

			Context("with the replace transaction strategy", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyTransactionReplacement
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should not bounce the processes", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextPatchVersion.String())))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})

				It("should replace the transaction system Pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if !fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if !fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).NotTo(ContainElements(originalNames))
				})

				It("should not replace the storage Pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).To(ContainElements(originalNames))
				})
			})

			Context("with the replacement strategy", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyReplacement
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should not bounce the processes", func() {
					Expect(adminClient.KilledAddresses).To(BeEmpty())
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextPatchVersion.String())))
					}
				})

				It("should replace the process group", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).NotTo(ContainElements(originalNames))
				})
			})
		})

		When("doing an version incompatible upgrade", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			Context("with the delete strategy", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyDelete
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[fmt.Sprintf("%s:4501", pod.Status.PodIP)] = fdbv1beta2.None{}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextMajorVersion.String())))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})
			})

			Context("with the replace transaction strategy", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyTransactionReplacement
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[fmt.Sprintf("%s:4501", pod.Status.PodIP)] = fdbv1beta2.None{}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextMajorVersion.String())))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})

				It("should replace the transaction system Pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if !fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if !fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).NotTo(ContainElements(originalNames))
				})

				It("should not replace the storage Pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						class := pod.Labels[fdbv1beta2.FDBProcessClassLabel]
						if fdbv1beta2.ProcessClass(class).IsTransaction() {
							continue
						}

						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).To(ContainElements(originalNames))
				})
			})

			Context("with the replacement strategy", func() {
				BeforeEach(func() {
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyReplacement
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[fmt.Sprintf("%s:4501", pod.Status.PodIP)] = fdbv1beta2.None{}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextMajorVersion.String())))
					}
				})

				It("should replace the process group", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).NotTo(ContainElements(originalNames))
				})
			})

			Context("with all upgradable clients", func() {
				BeforeEach(func() {
					adminClient.MockClientVersion(fdbv1beta2.Versions.NextMajorVersion.String(), []string{"127.0.0.2:3687"})
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should not set a message about the client upgradability", func() {
					events := &corev1.EventList{}
					Expect(k8sClient.List(context.TODO(), events)).NotTo(HaveOccurred())
					var matchingEvents []corev1.Event
					for _, event := range events.Items {
						if event.InvolvedObject.UID == cluster.ObjectMeta.UID && event.Reason == "UnsupportedClient" {
							matchingEvents = append(matchingEvents, event)
						}
					}
					Expect(matchingEvents).To(BeEmpty())
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})
			})

			Context("with a non-upgradable client", func() {
				BeforeEach(func() {
					adminClient.MockClientVersion(fdbv1beta2.Versions.NextMajorVersion.String(), []string{"127.0.0.2:3687"})
					adminClient.MockClientVersion(fdbv1beta2.Versions.Default.String(), []string{"127.0.0.3:85891"})
				})

				Context("with the check enabled", func() {
					BeforeEach(func() {
						Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})

					It("should set a message about the client upgradability", func() {
						events := &corev1.EventList{}
						var matchingEvents []corev1.Event
						Expect(k8sClient.List(context.TODO(), events)).NotTo(HaveOccurred())

						for _, event := range events.Items {
							if event.InvolvedObject.UID == cluster.ObjectMeta.UID && event.Reason == "UnsupportedClient" {
								matchingEvents = append(matchingEvents, event)
							}
						}
						Expect(len(matchingEvents)).NotTo(Equal(0))

						Expect(matchingEvents[0].Message).To(Equal(
							fmt.Sprintf("1 clients do not support version %s: 127.0.0.3:85891 (%s)", fdbv1beta2.Versions.NextMajorVersion, cluster.Name),
						))
					})
				})

				Context("with the check disabled", func() {
					BeforeEach(func() {
						cluster.Spec.IgnoreUpgradabilityChecks = true
						Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
					})

					It("should not set a message about the client upgradability", func() {
						events := &corev1.EventList{}
						Expect(k8sClient.List(context.TODO(), events)).NotTo(HaveOccurred())

						var matchingEvents []corev1.Event
						for _, event := range events.Items {
							if event.InvolvedObject.UID == cluster.ObjectMeta.UID && event.Reason == "UnsupportedClient" {
								matchingEvents = append(matchingEvents, event)
							}
						}
						Expect(matchingEvents).To(BeEmpty())
					})

					It("should update the running version", func() {
						Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
					})
				})
			})

			When("one processes is missing the localities", func() {
				BeforeEach(func() {
					adminClient.MockMissingLocalities(fdbv1beta2.ProcessGroupID(originalPods.Items[0].Labels[cluster.GetProcessClassLabel()]), true)
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]fdbv1beta2.None, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[fmt.Sprintf("%s:4501", pod.Status.PodIP)] = fdbv1beta2.None{}
					}
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbv1beta2.Versions.NextMajorVersion.String())))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})
			})
		})

		Context("with a change to the volume size", func() {
			Context("with the fields from the processes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("32Gi"),
								},
							},
						},
					}}}

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should replace the process groups", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					originalNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						processClass := internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta)
						if !processClass.IsStateful() {
							continue
						}
						originalNames = append(originalNames, pod.Name)
					}

					currentNames := make([]string, 0, len(originalPods.Items))
					for _, pod := range pods.Items {
						processClass := internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta)
						if !processClass.IsStateful() {
							continue
						}
						currentNames = append(currentNames, pod.Name)
					}

					Expect(currentNames).NotTo(ContainElements(originalNames))
				})

				It("should set the new volume size on the PVCs", func() {
					pvcs := &corev1.PersistentVolumeClaimList{}
					err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pvc := range pvcs.Items {
						Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("32Gi")))
					}
				})
			})
		})

		Context("with a change to the process group ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessGroupIDPrefix = "dev"
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should replace the process groups", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				originalNames := make([]string, 0, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					originalNames = append(originalNames, pod.Name)
				}

				currentNames := make([]string, 0, len(originalPods.Items))
				for _, pod := range pods.Items {
					currentNames = append(currentNames, pod.Name)
				}

				Expect(currentNames).NotTo(ContainElements(originalNames))
			})

			It("should generate process group IDs with the new prefix", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				sortPodsByName(pods)
				Expect(pods.Items[0].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("dev-cluster_controller-2"))
				Expect(pods.Items[1].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal("dev-log-5"))
			})
		})

		Context("when enabling a headless service", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.HeadlessService = pointer.Bool(true)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should create a service", func() {
				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, service)
				Expect(err).NotTo(HaveOccurred())

				Expect(service.Spec.ClusterIP).To(Equal("None"))
				Expect(len(service.OwnerReferences)).To(Equal(1))
			})
		})

		Context("when disabling a headless service", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.HeadlessService = pointer.Bool(true)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				_, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())

				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(originalVersion + 1))

				*cluster.Spec.Routing.HeadlessService = false
				generationGap = 2
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove a service", func() {
				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, service)
				Expect(err).To(HaveOccurred())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("with a lock deny list", func() {
			BeforeEach(func() {
				cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, fdbv1beta2.LockDenyListEntry{ID: "dc2"})
				cluster.Spec.LockOptions.DisableLocks = pointer.Bool(false)
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the deny list", func() {
				lockClient, err := clusterReconciler.getLockClient(cluster)
				Expect(err).NotTo(HaveOccurred())
				list, err := lockClient.GetDenyList()
				Expect(err).NotTo(HaveOccurred())
				Expect(list).To(Equal([]string{"dc2"}))

				Expect(cluster.Status.Locks.DenyList).To(Equal([]string{"dc2"}))
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
				InitCustomMetrics(clusterReconciler)
			})

			It("should update custom metrics in the registry", func() {
				metricFamilies, err := metrics.Registry.Gather()
				Expect(err).NotTo(HaveOccurred())
				var buf bytes.Buffer
				for _, mf := range metricFamilies {
					if strings.HasPrefix(mf.GetName(), "fdb_operator_cluster_status") {
						_, err := expfmt.MetricFamilyToText(&buf, mf)
						Expect(err).NotTo(HaveOccurred())
					}
				}
				healthMetricOutput := fmt.Sprintf(`\nfdb_operator_cluster_status{name="%s",namespace="%s",status_type="%s"} 1`, cluster.Name, cluster.Namespace, "health")
				availMetricOutput := fmt.Sprintf(`\nfdb_operator_cluster_status{name="%s",namespace="%s",status_type="%s"} 1`, cluster.Name, cluster.Namespace, "available")
				replMetricOutput := fmt.Sprintf(`\nfdb_operator_cluster_status{name="%s",namespace="%s",status_type="%s"} 1`, cluster.Name, cluster.Namespace, "replication")
				for _, re := range []*regexp.Regexp{
					regexp.MustCompile(healthMetricOutput),
					regexp.MustCompile(availMetricOutput),
					regexp.MustCompile(replMetricOutput),
				} {
					Expect(re.Match(buf.Bytes())).To(Equal(true))
				}
			})
		})

		Context("with a failing Pod", func() {
			var recreatedPod corev1.Pod

			BeforeEach(func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				recreatedPod = pods.Items[0]
				err = k8sClient.SetPodIntoFailed(context.Background(), &recreatedPod, "NodeAffinity")
				Expect(err).NotTo(HaveOccurred())
				// We have to sleep 1 second otherwise the creation timestamp will be the same
				time.Sleep(1 * time.Second)

				generationGap = 0
			})

			It("should recreate the Pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range pods.Items {
					if pod.GetName() != recreatedPod.GetName() {
						continue
					}

					Expect(recreatedPod.CreationTimestamp.UnixNano()).To(BeNumerically("<", pod.CreationTimestamp.UnixNano()))
				}
			})
		})

		Context("validating upgrade with ignoreLogGroups", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.MockClientVersion("7.1.0", []string{"127.0.0.2:3687"})
				cluster.Spec.Version = "7.1.0"
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			Context("minor upgrade from 7.1 to 7.2 with non empty ignoreLogGroups", func() {
				BeforeEach(func() {
					cluster.Spec.Version = "7.2.0"
					cluster.Spec.AutomationOptions.IgnoreLogGroupsForUpgrade = []fdbv1beta2.LogGroup{fdbv1beta2.LogGroup(cluster.Name)}
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
					generationGap = 2
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal("foundationdb/foundationdb:7.2.0"))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})
			})

			Context("minor upgrade from 7.1 to 7.2 with empty ignoreLogGroups", func() {
				BeforeEach(func() {
					cluster.Spec.Version = "7.2.0"
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
					shouldCompleteReconciliation = false
				})

				It("should set a message about the client upgradability", func() {
					events := &corev1.EventList{}
					var matchingEvents []corev1.Event

					err = k8sClient.List(context.TODO(), events)
					Expect(err).NotTo(HaveOccurred())

					for _, event := range events.Items {
						if event.InvolvedObject.UID == cluster.ObjectMeta.UID && event.Reason == "UnsupportedClient" {
							matchingEvents = append(matchingEvents, event)
						}
					}
					Expect(len(matchingEvents)).NotTo(Equal(0))

					Expect(matchingEvents[0].Message).To(Equal(
						fmt.Sprintf("1 clients do not support version 7.2.0: 127.0.0.2:3687 (%s)", cluster.Name),
					))
				})
			})
		})

		When("creating a cluster with RocksDB as storage engine", func() {
			When("using rocksdb-v1 engine", func() {
				When("using 6.3.26", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbV1
						cluster.Spec.Version = "6.3.26"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0-rc3", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbV1
						cluster.Spec.Version = "7.1.0-rc3"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbV1
						cluster.Spec.Version = "7.1.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0-rc4", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbV1
						cluster.Spec.Version = "7.1.0-rc4"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
			})

			When("using ssd-rocksdb-experimental", func() {
				When("using 7.1.0-rc4", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbExperimental
						cluster.Spec.Version = "7.1.0-rc4"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbExperimental
						cluster.Spec.Version = "7.1.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0-rc3", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbExperimental
						cluster.Spec.Version = "7.1.0-rc3"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 6.3.24", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbExperimental
						cluster.Spec.Version = "6.3.24"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
			})
			When("using ssd-sharded-rocksdb", func() {
				When("using 7.2.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineShardedRocksDB
						cluster.Spec.Version = "7.2.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineShardedRocksDB
						cluster.Spec.Version = "7.1.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 6.3.24", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineShardedRocksDB
						cluster.Spec.Version = "6.3.24"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
			})

		})

		When("creating a cluster with Redwood as storage engine", func() {
			When("using ssd-redwood-1-experimental", func() {
				When("using 7.2.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRedwood1Experimental
						cluster.Spec.Version = "7.2.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.1.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRedwood1Experimental
						cluster.Spec.Version = "7.1.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 7.0.0", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRedwood1Experimental
						cluster.Spec.Version = "7.0.0"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
				When("using 6.3.24", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRedwood1Experimental
						cluster.Spec.Version = "6.3.24"
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})
					It("generations are not matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).ToNot(Equal(cluster.ObjectMeta.Generation))
					})
				})
			})

		})

		When("When a process have an incorrect commandline", func() {
			var adminClient *mock.AdminClient

			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.MockIncorrectCommandLine("storage-1", true)
				generationGap = 0
				shouldCompleteReconciliation = false
			})

			It("It should report the incorrect commandline in the process groups", func() {
				// We have to reload the cluster because we don't reach the Reconcile state
				_, err = reloadClusterGenerations(cluster)
				Expect(err).NotTo(HaveOccurred())
				incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1"}))
			})

			When("When an additional process have an incorrect commandline", func() {
				BeforeEach(func() {
					adminClient.MockIncorrectCommandLine("storage-2", true)
					generationGap = 0
					shouldCompleteReconciliation = false
				})

				It("It should report the incorrect commandline in the process groups", func() {
					// We have to reload the cluster because we don't reach the Reconcile state
					_, err = reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)
					Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{"storage-1", "storage-2"}))
				})
			})
		})
	})

	Describe("GetMonitorConf", func() {
		var conf string
		var err error

		BeforeEach(func() {
			cluster.Status.ConnectionString = fakeConnectionString
		})

		Context("with a test process group", func() {
			BeforeEach(func() {
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassTest, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the test conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = test",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a basic storage process group", func() {
			BeforeEach(func() {
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a basic storage process group with multiple storage servers per Pod", func() {
			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf with two processes", func() {
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/1",
					"locality_process_id = $FDB_INSTANCE_ID-1",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"[fdbserver.2]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4503",
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/2",
					"locality_process_id = $FDB_INSTANCE_ID-2",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with the public IP from the pod", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with the public IP from the service", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Status.HasListenIPsForAllPods = true
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"listen_address = $FDB_POD_IP:4501",
				}, "\n")))
			})

			Context("with pods without the listen IP environment variable", func() {
				BeforeEach(func() {
					cluster.Status.HasListenIPsForAllPods = false
					conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
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
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						"locality_instance_id = $FDB_INSTANCE_ID",
						"locality_machineid = $FDB_MACHINE_ID",
						"locality_zoneid = $FDB_ZONE_ID",
					}, "\n")))
				})
			})
		})

		Context("with TLS enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
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

				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
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

				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with custom parameters", func() {
			Context("with general parameters", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
						"knob_disable_posix_kernel_aio = 1",
					}}}
					conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						"locality_instance_id = $FDB_INSTANCE_ID",
						"locality_machineid = $FDB_MACHINE_ID",
						"locality_zoneid = $FDB_ZONE_ID",
						"knob_disable_posix_kernel_aio = 1",
					}, "\n")))
				})
			})

			Context("with process-class parameters", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
							"knob_disable_posix_kernel_aio = 1",
						}},
						fdbv1beta2.ProcessClassStorage: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
							"knob_test = test1",
						}},
						fdbv1beta2.ProcessClassStateless: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
							"knob_test = test2",
						}},
					}
					conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						"locality_instance_id = $FDB_INSTANCE_ID",
						"locality_machineid = $FDB_MACHINE_ID",
						"locality_zoneid = $FDB_ZONE_ID",
						"knob_test = test1",
					}, "\n")))
				})
			})
		})

		Context("with an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $RACK",
				}, "\n")))
			})
		})

		Context("with a version that can use binaries from the main container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.Default.String()
				cluster.Status.RunningVersion = fdbv1beta2.Versions.Default.String()
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
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
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = test-fdb-cluster",
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
				}, "\n")))
			})
		})

		Context("with a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"locality_dcid = dc01",
				}, "\n")))
			})
		})
	})

	Describe("ParseProcessGroupID", func() {
		Context("with a storage ID", func() {
			It("can parse the ID", func() {
				prefix, id, err := podmanager.ParseProcessGroupID("storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(id).To(Equal(12))
			})
		})

		Context("with a cluster controller ID", func() {
			It("can parse the ID", func() {
				prefix, id, err := podmanager.ParseProcessGroupID("cluster_controller-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal(fdbv1beta2.ProcessClassClusterController))
				Expect(id).To(Equal(3))
			})
		})

		Context("with a custom prefix", func() {
			It("parses the prefix", func() {
				prefix, id, err := podmanager.ParseProcessGroupID("dc1-storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal(fdbv1beta2.ProcessClass("dc1-storage")))
				Expect(id).To(Equal(12))
			})
		})

		Context("with no prefix", func() {
			It("gives a parsing error", func() {
				_, _, err := podmanager.ParseProcessGroupID("6")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID 6"))
			})
		})

		Context("with no numbers", func() {
			It("gives a parsing error", func() {
				_, _, err := podmanager.ParseProcessGroupID("storage")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID storage"))
			})
		})

		Context("with a text suffix", func() {
			It("gives a parsing error", func() {
				_, _, err := podmanager.ParseProcessGroupID("storage-bad")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID storage-bad"))
			})
		})
	})

	Describe("GetProcessGroupIDFromProcessID", func() {
		It("can parse a process ID", func() {
			Expect(podmanager.GetProcessGroupIDFromProcessID("storage-1-1")).To(Equal("storage-1"))
		})
		It("can parse a process ID with a prefix", func() {
			Expect(podmanager.GetProcessGroupIDFromProcessID("dc1-storage-1-1")).To(Equal("dc1-storage-1"))
		})

		It("can handle a process group ID with no process number", func() {
			Expect(podmanager.GetProcessGroupIDFromProcessID("storage-2")).To(Equal("storage-2"))
		})
	})

	Context("Setting the partial connection string", func() {
		var partialConnectionString fdbv1beta2.ConnectionString

		JustBeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			cluster.Spec.PartialConnectionString = partialConnectionString

			err := k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
		})

		Context("with the database name", func() {
			BeforeEach(func() {
				partialConnectionString.DatabaseName = "foo"
			})

			It("should set the database name", func() {
				Expect(cluster.Status.ConnectionString).NotTo(Equal(""))

				conn, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)
				Expect(err).NotTo(HaveOccurred())

				Expect(conn.DatabaseName).To(Equal("foo"))
			})
		})

		Context("with the database name and generation ID", func() {
			BeforeEach(func() {
				partialConnectionString.DatabaseName = "foo"
				partialConnectionString.GenerationID = "bar"
			})

			It("should set the database name", func() {
				Expect(cluster.Status.ConnectionString).NotTo(Equal(""))

				conn, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)
				Expect(err).NotTo(HaveOccurred())

				Expect(conn.DatabaseName).To(Equal("foo"))
				Expect(conn.GenerationID).To(Equal("bar"))
			})
		})
	})

	Describe("GetPublicIPs", func() {
		var pod *corev1.Pod

		BeforeEach(func() {
			err := internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a default pod", func() {
			BeforeEach(func() {
				var err error
				pod, err = internal.GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
			})

			It("should be the public IP from the pod", func() {
				result := podmanager.GetPublicIPs(pod, log)
				Expect(result).To(Equal([]string{"1.1.1.1"}))
			})
		})

		Context("with a v6 pod IP family configured", func() {
			BeforeEach(func() {
				var err error
				cluster.Spec.Routing.PodIPFamily = pointer.Int(6)
				pod, err = internal.GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
			})

			It("should select the address based on the spec", func() {
				result := podmanager.GetPublicIPs(pod, log)
				Expect(result).To(Equal([]string{"2001:db8::ff00:42:8329"}))
			})

			Context("with no matching IPs in the Pod IP list", func() {
				BeforeEach(func() {
					var err error
					pod, err = internal.GetPod(cluster, "storage", 1)
					Expect(err).NotTo(HaveOccurred())
					pod.Status.PodIPs = []corev1.PodIP{
						{IP: "1.1.1.2"},
					}
				})

				It("should be empty", func() {
					result := podmanager.GetPublicIPs(pod, log)
					Expect(result).To(BeEmpty())
				})
			})
		})

		Context("with a v4 pod IP family configured", func() {
			BeforeEach(func() {
				var err error
				cluster.Spec.Routing.PodIPFamily = pointer.Int(4)
				pod, err = internal.GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.2"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
			})

			It("should select the address based on the spec", func() {
				result := podmanager.GetPublicIPs(pod, log)
				Expect(result).To(Equal([]string{"1.1.1.2"}))
			})
		})

		Context("with no pod", func() {
			It("should be empty", func() {
				result := podmanager.GetPublicIPs(nil, log)
				Expect(result).To(BeEmpty())
			})
		})
	})
})

func getProcessClassMap(cluster *fdbv1beta2.FoundationDBCluster, pods []corev1.Pod) map[fdbv1beta2.ProcessClass]int {
	counts := make(map[fdbv1beta2.ProcessClass]int)
	for _, pod := range pods {
		processClass := internal.ProcessClassFromLabels(cluster, pod.Labels)
		counts[processClass]++
	}
	return counts
}

// getConfigMapHash gets the hash of the data for a cluster's dynamic config.
func getConfigMapHash(cluster *fdbv1beta2.FoundationDBCluster, pClass fdbv1beta2.ProcessClass, pod *corev1.Pod) (string, error) {
	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return "", err
	}

	serversPerPod, err := internal.GetStorageServersPerPodForPod(pod)
	if err != nil {
		return "", err
	}

	imageType := internal.GetImageType(pod)

	return internal.GetDynamicConfHash(configMap, pClass, imageType, serversPerPod)
}
