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
	"strconv"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	gomegatypes "github.com/onsi/gomega/types"
	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/net"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var firstStorageIndex = 13

// reloadCluster reloads FoundationDBCluster object info to its local copy.
// It's necessary in unit test when reconciler changes FoundationDBCluster object but the local's state is not updated.
// We reloadCluster to bring client state consistent with the latest FoundationDBCluster object
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

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
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

				Expect(pods.Items[0].Name).To(HavePrefix("operator-test-1-cluster-controller-"))
				Expect(pods.Items[0].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("cluster_controller-"))
				Expect(pods.Items[0].Labels[fdbv1beta2.FDBProcessClassLabel]).To(Equal(string(fdbv1beta2.ProcessClassClusterController)))
				Expect(pods.Items[0].Annotations[fdbv1beta2.PublicIPSourceAnnotation]).To(Equal("pod"))
				Expect(pods.Items[0].Annotations[fdbv1beta2.PublicIPAnnotation]).To(Equal(""))

				Expect(pods.Items[1].Name).To(HavePrefix("operator-test-1-log-"))
				Expect(pods.Items[1].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("log-"))
				Expect(pods.Items[4].Name).To(HavePrefix("operator-test-1-log-"))
				Expect(pods.Items[4].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("log-"))
				Expect(pods.Items[5].Name).To(HavePrefix("operator-test-1-stateless-"))
				Expect(pods.Items[5].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("stateless-"))
				Expect(pods.Items[12].Name).To(HavePrefix("operator-test-1-stateless-"))
				Expect(pods.Items[12].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("stateless-"))
				Expect(pods.Items[13].Name).To(HavePrefix("operator-test-1-storage-"))
				Expect(pods.Items[13].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("storage-"))
				Expect(pods.Items[16].Name).To(HavePrefix("operator-test-1-storage-"))
				Expect(pods.Items[16].Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(HavePrefix("storage-"))

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

			It("should create an headless service", func() {
				services := &corev1.ServiceList{}
				Expect(k8sClient.List(context.TODO(), services, getListOptions(cluster)...)).NotTo(HaveOccurred())
				Expect(services.Items).To(HaveLen(1))
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
				Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)).To(HaveLen(0))
				Expect(fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.MissingProcesses, false)).To(HaveLen(0))

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				configuration := status.Cluster.DatabaseConfiguration.DeepCopy()
				configuration.LogSpill = 0
				cluster.ClearUnsetDatabaseConfigurationKnobs(configuration)
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
				imageType := fdbv1beta2.ImageTypeUnified
				cluster.Spec.ImageType = &imageType
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should update the pods and the status", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

				for _, pod := range pods.Items {
					Expect(pod.Spec.Containers[0].Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(pod.Spec.Containers[0].Image).To(And(HavePrefix(fdbv1beta2.FoundationDBKubernetesBaseImage), HaveSuffix(fdbv1beta2.Versions.Default.String())))
					Expect(pod.Spec.Containers[1].Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(pod.Spec.Containers[1].Image).To(And(HavePrefix(fdbv1beta2.FoundationDBKubernetesBaseImage), HaveSuffix(fdbv1beta2.Versions.Default.String())))
				}

				Expect(cluster.Status.ImageTypes).To(Equal([]fdbv1beta2.ImageType{"unified"}))
			})
		})

		When("disabling the DNS names in the cluster file", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(false)
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should disable DNS names for the pods", func() {
				pods := &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
				for _, pod := range pods.Items {
					env := make(map[string]string)
					for _, envVar := range pod.Spec.InitContainers[0].Env {
						env[envVar.Name] = envVar.Value
					}
					Expect(env).NotTo(HaveKey(fdbv1beta2.EnvNameDNSName))
				}
			})
		})

		Context("when buggifying a pod to make it crash loop", func() {
			var processGroupID fdbv1beta2.ProcessGroupID

			BeforeEach(func() {
				processGroupID = internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0].ProcessGroupID
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{processGroupID}
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should add the crash loop flag", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(pods.Items).To(HaveLen(len(originalPods.Items)))
				sortPodsByName(pods)

				pod := pods.Items[firstStorageIndex]
				Expect(pod.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal(string(processGroupID)))

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
				Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
			})

			It("should remove the pods", func() {
				pods := &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).To(Succeed())
				Expect(pods.Items).To(HaveLen(len(originalPods.Items) - 1))
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
				exclusionString := fmt.Sprintf("%s:%s", fdbv1beta2.FDBLocalityExclusionPrefix, removedItem.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel])
				Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
					exclusionString: true,
				}))
			})
		})

		Context("with an increased process count", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Storage = 5
				Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
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

		When("a coordinator is replaced", func() {
			var originalConnectionString string
			var replacedProcessGroup *fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				originalConnectionString = cluster.Status.ConnectionString

				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())

				coordinatorSet, err := adminClient.GetCoordinatorSet()
				Expect(err).NotTo(HaveOccurred())

				for _, processGroup := range cluster.Status.ProcessGroups {
					if _, ok := coordinatorSet[string(processGroup.ProcessGroupID)]; !ok {
						continue
					}

					replacedProcessGroup = processGroup
					break
				}

				Expect(replacedProcessGroup).NotTo(BeNil())
				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{replacedProcessGroup.ProcessGroupID}
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			When("the pod is in a healthy state", func() {
				It("should replace the targeted Pod and update the cluster status", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
					Expect(pods.Items).To(HaveLen(17))

					Expect(getProcessClassMap(cluster, pods.Items)).To(Equal(map[fdbv1beta2.ProcessClass]int{
						fdbv1beta2.ProcessClassStorage:           4,
						fdbv1beta2.ProcessClassLog:               4,
						fdbv1beta2.ProcessClassStateless:         8,
						fdbv1beta2.ProcessClassClusterController: 1,
					}))

					processGroupIDs := make([]string, 17)
					for idx, pod := range pods.Items {
						processGroupIDs[idx] = pod.Labels[fdbv1beta2.FDBProcessGroupIDLabel]
					}
					Expect(processGroupIDs).NotTo(ContainElements(string(replacedProcessGroup.ProcessGroupID)))

					// Should change the connection string
					Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))

					// Should remove the process group
					Expect(fdbv1beta2.ContainsProcessGroupID(cluster.Status.ProcessGroups, replacedProcessGroup.ProcessGroupID)).To(BeFalse())

					// Should exclude and re-include the process
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeEmpty())
					Expect(adminClient.ReincludedAddresses).To(HaveKeyWithValue(replacedProcessGroup.Addresses[0], true))
				})
			})

			When("a pod is stuck in terminating", func() {
				BeforeEach(func() {
					replacedProcessGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
					Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())

					pod := &corev1.Pod{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: replacedProcessGroup.GetPodName(cluster), Namespace: cluster.Namespace}, pod)).NotTo(HaveOccurred())
					Expect(k8sClient.MockStuckTermination(pod, true)).NotTo(HaveOccurred())
					// The returned generation in the test case will be 0 since we have 2 pending removals
					// which is expected.
					generationGap = -1
				})

				It("should set the generation to both reconciled and pending removal and exclude the process", func() {
					_, err = reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.Generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
						Reconciled:        2,
						HasPendingRemoval: 2,
					}))

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ReincludedAddresses).To(HaveLen(0))
					Expect(adminClient.ExcludedAddresses).To(HaveLen(2))
					Expect(adminClient.ExcludedAddresses).To(HaveKey(
						replacedProcessGroup.Addresses[0],
					))
				})
			})

			When("a PVC is stuck in terminating", func() {
				BeforeEach(func() {
					replacedProcessGroup.UpdateCondition(fdbv1beta2.MissingProcesses, true)
					Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())

					pvc := &corev1.PersistentVolumeClaim{}
					err = k8sClient.Get(context.TODO(), client.ObjectKey{
						Namespace: cluster.Namespace,
						Name:      fmt.Sprintf("%s-data", replacedProcessGroup.GetPodName(cluster)),
					}, pvc)
					Expect(err).NotTo(HaveOccurred())
					Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())
					Expect(k8sClient.MockStuckTermination(pvc, true)).NotTo(HaveOccurred())
					// The returned generation in the test case will be 0 since we have 2 pending removals
					// which is expected.
					generationGap = -1
				})

				It("should set the generation to both reconciled and pending removal and exclude the process", func() {
					_, err = reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(cluster.Status.Generations).To(Equal(fdbv1beta2.ClusterGenerationStatus{
						Reconciled:        2,
						HasPendingRemoval: 2,
					}))

					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ReincludedAddresses).To(HaveLen(0))
					Expect(adminClient.ExcludedAddresses).To(HaveLen(2))
					Expect(adminClient.ExcludedAddresses).To(HaveKey(
						replacedProcessGroup.Addresses[0],
					))
				})
			})

			When("the cluster skip setting is enabled", func() {
				BeforeEach(func() {
					cluster.Spec.Skip = true
					Expect(k8sClient.Update(context.Background(), cluster)).NotTo(HaveOccurred())
					generationGap = 0
				})

				It("should not replace the pod", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
					Expect(pods.Items).To(HaveLen(17))

					sortPodsByName(pods)

					for i := 0; i < 17; i++ {
						Expect(pods.Items[i].Name).To(Equal(originalPods.Items[i].Name))
					}
				})
			})

			When("the pod has a missing pod IP", func() {
				var podIP string

				BeforeEach(func() {
					pod := &corev1.Pod{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: replacedProcessGroup.GetPodName(cluster), Namespace: cluster.Namespace}, pod))
					podIP = pod.Status.PodIP
					Expect(k8sClient.RemovePodIP(pod)).NotTo(HaveOccurred())
				})

				It("should replace the pod and exclude and include it", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
					Expect(pods.Items).To(HaveLen(17))

					processGroupIDs := make([]string, 17)
					for idx, pod := range pods.Items {
						processGroupIDs[idx] = pod.Labels[fdbv1beta2.FDBProcessGroupIDLabel]
					}
					Expect(processGroupIDs).NotTo(ContainElements(string(replacedProcessGroup.ProcessGroupID)))

					// Check for the exclusion and inclusion
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeEmpty())
					Expect(adminClient.ReincludedAddresses).To(HaveKeyWithValue(podIP, true))
				})
			})

			When("the pod should be removed without an exclusion", func() {
				BeforeEach(func() {
					pod := &corev1.Pod{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: replacedProcessGroup.GetPodName(cluster), Namespace: cluster.Namespace}, pod))
					Expect(k8sClient.RemovePodIP(pod)).NotTo(HaveOccurred())
					cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = []fdbv1beta2.ProcessGroupID{replacedProcessGroup.ProcessGroupID}
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
					generationGap++
				})

				It("should replace the pod", func() {
					pods := &corev1.PodList{}
					Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
					Expect(pods.Items).To(HaveLen(17))

					processGroupIDs := make([]string, 17)
					for idx, pod := range pods.Items {
						processGroupIDs[idx] = pod.Labels[fdbv1beta2.FDBProcessGroupIDLabel]
					}
					Expect(processGroupIDs).NotTo(ContainElements(string(replacedProcessGroup.ProcessGroupID)))

					// Check for the exclusion and inclusion
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeEmpty())
					Expect(adminClient.ReincludedAddresses).To(BeEmpty())
				})
			})
		})

		When("a process is missing", func() {
			var adminClient *mock.AdminClient
			var processGroupID fdbv1beta2.ProcessGroupID
			var podName string

			BeforeEach(func() {
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				pickedProcessGroup := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
				processGroupID = pickedProcessGroup.ProcessGroupID
				podName = pickedProcessGroup.GetPodName(cluster)
				adminClient.MockMissingProcessGroup(processGroupID, true)

				// Run a single reconciliation to detect the missing process.
				result, err := reconcileObject(clusterReconciler, cluster.ObjectMeta, 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())

				// Tweak the time on the missing process to make it eligible for replacement.
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				processGroup := fdbv1beta2.FindProcessGroupByID(cluster.Status.ProcessGroups, processGroupID)
				Expect(processGroup).NotTo(BeNil())
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbv1beta2.MissingProcesses))
				Expect(processGroup.ProcessGroupConditions[0].Timestamp).NotTo(Equal(0))
				processGroup.ProcessGroupConditions[0].Timestamp -= 3600
				Expect(k8sClient.Status().Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				generationGap = 0
			})

			It("should replace the pod", func() {
				pod := &corev1.Pod{}
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: podName, Namespace: cluster.Namespace}, pod)
				Expect(err).To(HaveOccurred())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())

				pods := &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
				Expect(pods.Items).To(HaveLen(17))
			})
		})

		When("multiple replacements are present", func() {
			var replacedPods []string

			BeforeEach(func() {
				picked := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)
				cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{
					picked[0].ProcessGroupID,
					picked[1].ProcessGroupID,
				}
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())

				replacedPods = []string{picked[0].GetPodName(cluster), picked[1].GetPodName(cluster)}
			})

			It("should replace the pods", func() {
				pods := &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())
				Expect(pods.Items).To(HaveLen(17))

				for _, podName := range replacedPods {
					pod := &corev1.Pod{}
					err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: podName, Namespace: cluster.Namespace}, pod)
					Expect(err).To(HaveOccurred())
					Expect(k8serrors.IsNotFound(err)).To(BeTrue())
				}
			})
		})

		When("a pod gets deleted", func() {
			var pod *corev1.Pod

			BeforeEach(func() {
				generationGap = 0

				picked := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
				pod = &corev1.Pod{}
				Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: picked.GetPodName(cluster), Namespace: cluster.Namespace}, pod)).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(context.TODO(), pod)).NotTo(HaveOccurred())
			})

			It("should replace the pod", func() {
				newPod := &corev1.Pod{}
				Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pod.Name, Namespace: cluster.Namespace}, newPod)).NotTo(HaveOccurred())
				Expect(pod.UID).NotTo(Equal(newPod.UID))
				Expect(pod.Name).To(Equal(newPod.Name))
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
					Expect(err).NotTo(HaveOccurred())

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

			Context("with multiple log servers per pod", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {CustomParameters: fdbv1beta2.FoundationDBCustomParameters{}}}
					cluster.Spec.LogServersPerPod = 2
					adminClient.UnfreezeStatus()
					Expect(err).NotTo(HaveOccurred())
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())

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
						if internal.ProcessClassFromLabels(cluster, pod.ObjectMeta.Labels) == fdbv1beta2.ProcessClassLog {
							addresses[cluster.GetFullAddress(pod.Status.PodIP, 2).String()] = fdbv1beta2.None{}
						}
					}

					Expect(adminClient.KilledAddresses).To(Equal(addresses))
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
			var pickedPod string

			BeforeEach(func() {
				picked := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
				pickedPod = picked.GetPodName(cluster)
				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: pickedPod}, pod)
				Expect(err).NotTo(HaveOccurred())
				pod.Annotations["foundationdb.org/existing-annotation"] = "test-value"
				Expect(k8sClient.Update(context.TODO(), pod)).NotTo(HaveOccurred())

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
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should set the annotations on the pod", func() {
				pods := &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

				for _, pod := range pods.Items {
					hash, err := internal.GetPodSpecHash(cluster, &fdbv1beta2.ProcessGroupStatus{
						ProcessGroupID: fdbv1beta2.ProcessGroupID(pod.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
						ProcessClass:   internal.ProcessClassFromLabels(cluster, pod.Labels),
					}, nil)
					Expect(err).NotTo(HaveOccurred())

					configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta), &pod)
					Expect(err).NotTo(HaveOccurred())
					if pod.Name == pickedPod {
						Expect(pod.ObjectMeta.Annotations).To(Equal(map[string]string{
							fdbv1beta2.LastConfigMapKey:            configMapHash,
							fdbv1beta2.LastSpecKey:                 hash,
							fdbv1beta2.PublicIPSourceAnnotation:    "pod",
							"foundationdb.org/existing-annotation": "test-value",
							"fdb-annotation":                       "value1",
							fdbv1beta2.NodeAnnotation:              pod.Spec.NodeName,
							fdbv1beta2.ImageTypeAnnotation:         string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation:          strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
						}))
						continue
					}

					Expect(pod.ObjectMeta.Annotations).To(Equal(map[string]string{
						fdbv1beta2.LastConfigMapKey:         configMapHash,
						fdbv1beta2.LastSpecKey:              hash,
						fdbv1beta2.PublicIPSourceAnnotation: "pod",
						"fdb-annotation":                    "value1",
						fdbv1beta2.NodeAnnotation:           pod.Spec.NodeName,
						fdbv1beta2.ImageTypeAnnotation:      string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:       strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					}))
				}
			})

			It("should not set the annotations on other resources", func() {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						fdbv1beta2.LastSpecKey: "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
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
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
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
				var pickedPvc string

				BeforeEach(func() {
					picked := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 1)[0]
					pickedPvc = picked.GetPodName(cluster) + "-data"
					pvc := &corev1.PersistentVolumeClaim{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: pickedPvc}, pvc)).NotTo(HaveOccurred())
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
					Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
				})

				It("should update the annotations on the PVCs", func() {
					pvcs := &corev1.PersistentVolumeClaimList{}
					err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					for _, item := range pvcs.Items {
						if item.Name == pickedPvc {
							Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
								"fdb-annotation":                       "value1",
								"foundationdb.org/existing-annotation": "test-value",
								fdbv1beta2.LastSpecKey:                 "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
							}))
							continue
						}
						Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
							"fdb-annotation":       "value1",
							fdbv1beta2.LastSpecKey: "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
						}))

					}
				})

				It("should not update the annotations on other resources", func() {
					pods := &corev1.PodList{}

					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					for _, item := range pods.Items {
						hash, err := internal.GetPodSpecHash(cluster, &fdbv1beta2.ProcessGroupStatus{
							ProcessGroupID: fdbv1beta2.ProcessGroupID(item.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
							ProcessClass:   internal.ProcessClassFromLabels(cluster, item.Labels),
						}, nil)
						Expect(err).NotTo(HaveOccurred())

						configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, item.ObjectMeta), &item)
						Expect(err).NotTo(HaveOccurred())
						Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
							fdbv1beta2.LastConfigMapKey:         configMapHash,
							fdbv1beta2.LastSpecKey:              hash,
							fdbv1beta2.PublicIPSourceAnnotation: "pod",
							fdbv1beta2.NodeAnnotation:           item.Spec.NodeName,
							fdbv1beta2.ImageTypeAnnotation:      string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation:       strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
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
					hash, err := internal.GetPodSpecHash(cluster, &fdbv1beta2.ProcessGroupStatus{
						ProcessGroupID: fdbv1beta2.ProcessGroupID(item.Labels[fdbv1beta2.FDBProcessGroupIDLabel]),
						ProcessClass:   internal.ProcessClassFromLabels(cluster, item.Labels),
					}, nil)
					Expect(err).NotTo(HaveOccurred())

					configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(cluster, item.ObjectMeta), &item)
					Expect(err).NotTo(HaveOccurred())
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						fdbv1beta2.LastConfigMapKey:         configMapHash,
						fdbv1beta2.LastSpecKey:              hash,
						fdbv1beta2.PublicIPSourceAnnotation: "pod",
						fdbv1beta2.NodeAnnotation:           item.Spec.NodeName,
						fdbv1beta2.ImageTypeAnnotation:      string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:       strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					}))
				}

				pvcs := &corev1.PersistentVolumeClaimList{}
				err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				for _, item := range pvcs.Items {
					Expect(item.ObjectMeta.Annotations).To(Equal(map[string]string{
						fdbv1beta2.LastSpecKey: "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
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
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal(fdbv1beta2.EnvNameClusterFile))
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
						Expect(pod.Spec.Containers[0].Env[0].Name).To(Equal(fdbv1beta2.EnvNameClusterFile))
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
				Expect(services.Items).To(HaveLen(len(pods.Items) + 1))

				service := &corev1.Service{}
				pod := pods.Items[0]
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, service)).To(Succeed())
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
						if env.Name == fdbv1beta2.EnvNamePodIP {
							podIPEnv = env
						}
					}
					Expect(podIPEnv.Name).To(Equal(fdbv1beta2.EnvNamePodIP))
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
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).To(Succeed())
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
				Expect(k8sClient.List(context.TODO(), services, getListOptions(cluster)...)).To(Succeed())
				Expect(services.Items).To(HaveLen(len(pods.Items) + 1))

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
				cluster.Spec.Routing.PodIPFamily = pointer.Int(6)
				Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
			})

			It("should make the processes listen on an IPV6 address", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).To(Succeed())

				status, err := adminClient.GetStatus()
				Expect(err).To(Succeed())

				for _, process := range status.Cluster.Processes {
					Expect(net.IsIPv6(process.Address.IPAddress)).To(BeTrue())
				}
			})
		})

		Context("with a newly created IPv6 cluster", func() {
			BeforeEach(func() {
				k8sClient.Clear()
				mock.ClearMockAdminClients()
				mock.ClearMockLockClients()

				cluster = internal.CreateDefaultCluster()
				cluster.Spec.Routing.PodIPFamily = pointer.Int(6)

				Expect(k8sClient.Create(context.TODO(), cluster)).To(Succeed())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(int64(1)))

				originalVersion = cluster.ObjectMeta.Generation

				originalPods = &corev1.PodList{}
				Expect(k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)).To(Succeed())
				Expect(originalPods.Items).To(HaveLen(17))

				sortPodsByName(originalPods)

				generationGap = 0
			})

			It("should make the processes listen on an IPV6 address", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).To(Succeed())

				status, err := adminClient.GetStatus()
				Expect(err).To(Succeed())

				Expect(status.Cluster.Processes).To(HaveLen(17))
				for _, process := range status.Cluster.Processes {
					Expect(net.IsIPv6(process.Address.IPAddress)).To(BeTrue())
				}
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
				Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
			})

			It("should only have storage processes as coordinator", func() {
				adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).To(Succeed())

				status, err := adminClient.GetStatus()
				Expect(err).To(Succeed())

				coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
				var validatedCoordinators int
				for _, process := range status.Cluster.Processes {
					if _, ok := coordinators[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]]; !ok {
						continue
					}
					Expect(process.ProcessClass).To(Equal(fdbv1beta2.ProcessClassStorage))
					validatedCoordinators++
				}

				Expect(coordinators).To(HaveLen(validatedCoordinators))
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
					adminClient, err := mock.NewMockAdminClientUncast(cluster, k8sClient)
					Expect(err).To(Succeed())

					status, err := adminClient.GetStatus()
					Expect(err).To(Succeed())

					coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
					var validatedCoordinators int
					for _, process := range status.Cluster.Processes {
						if _, ok := coordinators[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]]; !ok {
							continue
						}
						Expect(process.ProcessClass).To(Equal(fdbv1beta2.ProcessClassLog))
						validatedCoordinators++
					}

					Expect(coordinators).To(HaveLen(validatedCoordinators))
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
				Expect(k8sClient.Update(context.TODO(), cluster)).NotTo(HaveOccurred())
			})

			It("should replace the process groups", func() {
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
				for _, processGroup := range cluster.Status.ProcessGroups {
					Expect(string(processGroup.ProcessGroupID)).To(HavePrefix("dev"))
				}
			})
		})

		When("enabling a headless service", func() {
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

		When("disabling a headless service", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.HeadlessService = pointer.Bool(true)
				Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())

				_, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())

				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(originalVersion + 1))

				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(false)
				cluster.Spec.Routing.HeadlessService = pointer.Bool(false)
				generationGap = 2
				Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
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
				lockClient, err := clusterReconciler.getLockClient(testLogger, cluster)
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
				Expect(k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)).NotTo(HaveOccurred())

				recreatedPod = pods.Items[0]
				Expect(k8sClient.SetPodIntoFailed(context.Background(), &recreatedPod, "NodeAffinity")).NotTo(HaveOccurred())
				generationGap = 0
			})

			It("should recreate the Pod", func() {
				pod := &corev1.Pod{}
				Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: recreatedPod.Namespace, Name: recreatedPod.Name}, pod)).NotTo(HaveOccurred())
				Expect(pod.UID).NotTo(Equal(recreatedPod.UID))
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

		When("changing the storage engine to RocksDB", func() {
			When("using rocksdb-v1 engine", func() {
				When("using the default version", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbV1
						Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
					})

					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
			})

			When("using ssd-rocksdb-experimental", func() {
				When("using the default version", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRocksDbExperimental
						Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
					})

					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})
			})

			When("using ssd-sharded-rocksdb", func() {
				When("using a version that supports sharded-rocksdb", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineShardedRocksDB
						cluster.Spec.Version = fdbv1beta2.Versions.SupportsShardedRocksDB.String()
						Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
						Expect(err).NotTo(HaveOccurred())
					})

					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})

				When("using the default version", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineShardedRocksDB
						Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
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

		When("changing the storage engine to Redwood", func() {
			When("using ssd-redwood-1-experimental", func() {
				When("using a version that supports redwood-v1", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRedwood1Experimental
						cluster.Spec.Version = fdbv1beta2.Versions.SupportsRedwood1.String()
						Expect(k8sClient.Update(context.TODO(), cluster)).To(Succeed())
					})
					It("generations are matching", func() {
						generations, err := reloadClusterGenerations(cluster)
						Expect(err).NotTo(HaveOccurred())
						Expect(generations.Reconciled).To(Equal(cluster.ObjectMeta.Generation))
					})
				})

				When("using the default version", func() {
					BeforeEach(func() {
						cluster.Spec.DatabaseConfiguration.StorageEngine = fdbv1beta2.StorageEngineRedwood1Experimental
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
		})

		When("a process have an incorrect commandline", func() {
			var adminClient *mock.AdminClient
			var first, second *fdbv1beta2.ProcessGroupStatus

			BeforeEach(func() {
				picked := internal.PickProcessGroups(cluster, fdbv1beta2.ProcessClassStorage, 2)
				first = picked[0]
				second = picked[1]
				adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.MockIncorrectCommandLine(first.ProcessGroupID, true)
				generationGap = 0
				shouldCompleteReconciliation = false
			})

			It("It should report the incorrect commandline in the process groups", func() {
				// We have to reload the cluster because we don't reach the Reconcile state
				_, err = reloadClusterGenerations(cluster)
				Expect(err).NotTo(HaveOccurred())
				incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{first.ProcessGroupID}))
			})

			When("When an additional process have an incorrect commandline", func() {
				BeforeEach(func() {
					adminClient.MockIncorrectCommandLine(second.ProcessGroupID, true)
					generationGap = 0
					shouldCompleteReconciliation = false
				})

				It("It should report the incorrect commandline in the process groups", func() {
					// We have to reload the cluster because we don't reach the Reconcile state
					_, err = reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					incorrectProcesses := fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, fdbv1beta2.IncorrectCommandLine, false)
					Expect(incorrectProcesses).To(Equal([]fdbv1beta2.ProcessGroupID{first.ProcessGroupID, second.ProcessGroupID}))
				})
			})
		})
	})

	Describe("GetMonitorConf", func() {
		var conf string
		var err error

		BeforeEach(func() {
			cluster.Status.ConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
			format.MaxLength = 5000
			format.MaxDepth = 100
			format.TruncatedDiff = false
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
				}, "\n")))
			})
		})

		Context("with a basic log process group with multiple log servers per Pod", func() {
			BeforeEach(func() {
				cluster.Spec.LogServersPerPod = 2
				conf, err = internal.GetMonitorConf(cluster, fdbv1beta2.ProcessClassLog, nil, cluster.GetLogServersPerPod())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the log conf with two processes", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4501",
					"class = log",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/1",
					"locality_process_id = $FDB_INSTANCE_ID-1",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"locality_dns_name = $FDB_DNS_NAME",
					"[fdbserver.2]",
					"command = $BINARY_DIR/fdbserver",
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					"public_address = $FDB_PUBLIC_IP:4503",
					"class = log",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/2",
					"locality_process_id = $FDB_INSTANCE_ID-2",
					"locality_instance_id = $FDB_INSTANCE_ID",
					"locality_machineid = $FDB_MACHINE_ID",
					"locality_zoneid = $FDB_ZONE_ID",
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
						"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
						"locality_dns_name = $FDB_DNS_NAME",
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
						"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
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
					"locality_dns_name = $FDB_DNS_NAME",
				}, "\n")))
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
				pod, err = internal.GetPod(cluster, &fdbv1beta2.ProcessGroupStatus{
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
					ProcessGroupID: "storage-1",
				})
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
			})

			It("should be the public IP from the pod", func() {
				result := podmanager.GetPublicIPs(pod, globalControllerLogger)
				Expect(result).To(Equal([]string{"1.1.1.1"}))
			})
		})

		Context("with a v6 pod IP family configured", func() {
			BeforeEach(func() {
				var err error
				cluster.Spec.Routing.PodIPFamily = pointer.Int(6)
				pod, err = internal.GetPod(cluster, &fdbv1beta2.ProcessGroupStatus{
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
					ProcessGroupID: "storage-1",
				})
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
			})

			It("should select the address based on the spec", func() {
				result := podmanager.GetPublicIPs(pod, globalControllerLogger)
				Expect(result).To(Equal([]string{"2001:db8::ff00:42:8329"}))
			})

			Context("with no matching IPs in the Pod IP list", func() {
				BeforeEach(func() {
					var err error
					pod, err = internal.GetPod(cluster, &fdbv1beta2.ProcessGroupStatus{
						ProcessClass:   fdbv1beta2.ProcessClassStorage,
						ProcessGroupID: "storage-1",
					})
					Expect(err).NotTo(HaveOccurred())
					pod.Status.PodIPs = []corev1.PodIP{
						{IP: "1.1.1.2"},
					}
				})

				It("should be empty", func() {
					result := podmanager.GetPublicIPs(pod, globalControllerLogger)
					Expect(result).To(BeEmpty())
				})
			})
		})

		Context("with a v4 pod IP family configured", func() {
			BeforeEach(func() {
				var err error
				cluster.Spec.Routing.PodIPFamily = pointer.Int(4)
				pod, err = internal.GetPod(cluster, &fdbv1beta2.ProcessGroupStatus{
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
					ProcessGroupID: "storage-1",
				})
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.2"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
			})

			It("should select the address based on the spec", func() {
				result := podmanager.GetPublicIPs(pod, globalControllerLogger)
				Expect(result).To(Equal([]string{"1.1.1.2"}))
			})
		})

		Context("with no pod", func() {
			It("should be empty", func() {
				result := podmanager.GetPublicIPs(nil, globalControllerLogger)
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

	serversPerPod, err := internal.GetServersPerPodForPod(pod, pClass)
	if err != nil {
		return "", err
	}

	imageType := internal.GetImageType(pod)

	return internal.GetDynamicConfHash(configMap, pClass, imageType, serversPerPod)
}
