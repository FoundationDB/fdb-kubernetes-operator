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
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"github.com/prometheus/common/expfmt"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var firstStorageIndex = 13

func reloadCluster(cluster *fdbtypes.FoundationDBCluster) (int64, error) {
	generations, err := reloadClusterGenerations(cluster)
	if generations.HasPendingRemoval > 0 {
		return 0, err
	}
	return generations.Reconciled, err
}

func reloadClusterGenerations(cluster *fdbtypes.FoundationDBCluster) (fdbtypes.ClusterGenerationStatus, error) {
	objectKey := types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}
	*cluster = fdbtypes.FoundationDBCluster{}
	err := k8sClient.Get(context.TODO(), objectKey, cluster)
	if err != nil {
		return fdbtypes.ClusterGenerationStatus{}, err
	}
	return cluster.Status.Generations, err
}

func getListOptions(cluster *fdbtypes.FoundationDBCluster) []client.ListOption {
	return []client.ListOption{
		client.InNamespace("my-ns"),
		client.MatchingLabels(map[string]string{
			fdbtypes.FDBClusterLabel: cluster.Name,
		}),
	}
}

var _ = Describe(string(fdbtypes.ProcessClassClusterController), func() {
	var cluster *fdbtypes.FoundationDBCluster
	var fakeConnectionString string

	BeforeEach(func() {
		cluster = createDefaultCluster()
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

			sortPodsByID(originalPods)

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

				sortPodsByID(pods)

				Expect(pods.Items[0].Name).To(Equal("operator-test-1-cluster-controller-1"))
				Expect(pods.Items[0].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("cluster_controller-1"))
				Expect(pods.Items[0].Annotations[fdbtypes.PublicIPSourceAnnotation]).To(Equal("pod"))
				Expect(pods.Items[0].Annotations[fdbtypes.PublicIPAnnotation]).To(Equal(""))

				Expect(pods.Items[1].Name).To(Equal("operator-test-1-log-1"))
				Expect(pods.Items[1].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("log-1"))
				Expect(pods.Items[4].Name).To(Equal("operator-test-1-log-4"))
				Expect(pods.Items[4].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("log-4"))
				Expect(pods.Items[5].Name).To(Equal("operator-test-1-stateless-1"))
				Expect(pods.Items[5].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("stateless-1"))
				Expect(pods.Items[12].Name).To(Equal("operator-test-1-stateless-8"))
				Expect(pods.Items[12].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("stateless-8"))
				Expect(pods.Items[13].Name).To(Equal("operator-test-1-storage-1"))
				Expect(pods.Items[13].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("storage-1"))
				Expect(pods.Items[16].Name).To(Equal("operator-test-1-storage-4"))
				Expect(pods.Items[16].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("storage-4"))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
					fdbtypes.ProcessClassStorage:           4,
					fdbtypes.ProcessClassLog:               4,
					fdbtypes.ProcessClassStateless:         8,
					fdbtypes.ProcessClassClusterController: 1,
				}))

				pod := &pods.Items[0]
				configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(pod.ObjectMeta), pod)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.ObjectMeta.Annotations[fdbtypes.LastConfigMapKey]).To(Equal(configMapHash))
				Expect(len(cluster.Status.ProcessGroups)).To(Equal(len(pods.Items)))
			})

			It("should not create any services", func() {
				services := &corev1.ServiceList{}
				err := k8sClient.List(context.TODO(), services, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(services.Items)).To(Equal(0))
			})

			It("should fill in the required fields in the configuration", func() {
				Expect(cluster.Status.DatabaseConfiguration.RedundancyMode).To(Equal("double"))
				Expect(cluster.Status.DatabaseConfiguration.StorageEngine).To(Equal("ssd-2"))
				Expect(cluster.Status.ConnectionString).NotTo(Equal(""))
			})

			It("should create a config map for the cluster", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				err = k8sClient.Get(context.TODO(), configMapName, configMap)
				Expect(err).NotTo(HaveOccurred())
				expectedConfigMap, _ := GetConfigMap(cluster)
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

				Expect(cluster.Status.Generations.Reconciled).To(Equal(int64(1)))

				processCounts := fdbtypes.CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, true)
				Expect(processCounts).To(Equal(fdbtypes.ProcessCounts{
					Storage:           4,
					Log:               4,
					Stateless:         8,
					ClusterController: 1,
				}))

				desiredCounts, err := cluster.GetProcessCountsWithDefaults()
				Expect(err).NotTo(HaveOccurred())
				Expect(processCounts).To(Equal(desiredCounts))
				Expect(len(fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, fdbtypes.IncorrectCommandLine, false))).To(Equal(0))
				Expect(len(fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, fdbtypes.MissingProcesses, false))).To(Equal(0))

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				configuration := status.Cluster.DatabaseConfiguration.DeepCopy()
				configuration.LogSpill = 0
				Expect(cluster.Status.DatabaseConfiguration).To(Equal(*configuration))

				Expect(cluster.Status.Health).To(Equal(fdbtypes.ClusterHealth{
					Available:            true,
					Healthy:              true,
					FullReplication:      true,
					DataMovementPriority: 0,
				}))
			})
		})

		Context("when buggifying a pod to make it crash loop", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []string{"storage-1"}
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the crash loop flag", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))
				sortPodsByID(pods)

				pod := pods.Items[firstStorageIndex]
				Expect(pod.ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("storage-1"))

				mainContainer := pod.Spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
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
				sortPodsByID(pods)

				cm, _ := GetConfigMap(cluster)

				for _, file := range cm.Data {
					Expect(file).To(Not(ContainSubstring("fdbserver")))
				}

				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())

				// Converted the killed servers into a set since they may have been killed more than once during
				// reconciliation.
				killedSet := map[string]struct{}{}
				for _, addr := range adminClient.KilledAddresses {
					killedSet[addr] = struct{}{}
				}

				// All of the processes in the cluster should be killed.
				processes := map[string]struct{}{}
				for _, processGroup := range cluster.Status.ProcessGroups {
					for i, addr := range processGroup.Addresses {
						// +1 since the process list uses 1-based indexing.
						fullAddr := cluster.GetFullAddress(addr, i+1)
						processes[fullAddr] = struct{}{}
					}
				}

				Expect(killedSet).To(Equal(processes))
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
				sortPodsByID(pods)

				Expect(pods.Items[0].Name).To(Equal(originalPods.Items[0].Name))
				Expect(pods.Items[1].Name).To(Equal(originalPods.Items[1].Name))
				Expect(pods.Items[2].Name).To(Equal(originalPods.Items[2].Name))

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
					fdbtypes.ProcessClassStorage:           3,
					fdbtypes.ProcessClassLog:               4,
					fdbtypes.ProcessClassStateless:         8,
					fdbtypes.ProcessClassClusterController: 1,
				}))

				Expect(cluster.Spec.PendingRemovals).To(BeNil())
				Expect(cluster.Spec.InstancesToRemove).To(BeNil())
				Expect(cluster.Status.PendingRemovals).To(BeNil())

				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(adminClient).NotTo(BeNil())
				Expect(adminClient.ExcludedAddresses).To(BeNil())

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

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
					fdbtypes.ProcessClassStorage:           5,
					fdbtypes.ProcessClassLog:               4,
					fdbtypes.ProcessClassStateless:         8,
					fdbtypes.ProcessClassClusterController: 1,
				}))
			})

			It("should update the config map", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				err = k8sClient.Get(context.TODO(), configMapName, configMap)
				Expect(err).NotTo(HaveOccurred())
				expectedConfigMap, _ := GetConfigMap(cluster)
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

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
					fdbtypes.ProcessClassStorage:           4,
					fdbtypes.ProcessClassLog:               4,
					fdbtypes.ProcessClassStateless:         9,
					fdbtypes.ProcessClassClusterController: 1,
				}))
			})

			It("should update the config map", func() {
				configMap := &corev1.ConfigMap{}
				configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
				err = k8sClient.Get(context.TODO(), configMapName, configMap)
				Expect(err).NotTo(HaveOccurred())
				expectedConfigMap, _ := GetConfigMap(cluster)
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

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
					fdbtypes.ProcessClassStorage:           4,
					fdbtypes.ProcessClassLog:               4,
					fdbtypes.ProcessClassStateless:         9,
					fdbtypes.ProcessClassClusterController: 1,
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

				Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
					fdbtypes.ProcessClassStorage:           4,
					fdbtypes.ProcessClassLog:               4,
					fdbtypes.ProcessClassClusterController: 1,
				}))

				Expect(cluster.Spec.PendingRemovals).To(BeNil())
				Expect(cluster.Spec.InstancesToRemove).To(BeNil())
				Expect(cluster.Status.PendingRemovals).To(BeNil())
			})

			It("should clear removals from the process group status", func() {
				processGroups := cluster.Status.ProcessGroups
				Expect(len(processGroups)).To(Equal(9))

				for _, group := range processGroups {
					Expect(group.Remove).To(BeFalse())
				}
			})
		})

		Context("with a coordinator replacement", func() {
			var originalConnectionString string

			BeforeEach(func() {
				originalConnectionString = cluster.Status.ConnectionString
			})

			Context("with an entry in the instances to remove list", func() {
				BeforeEach(func() {
					cluster.Spec.InstancesToRemove = []string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
					}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should keep the process counts the same", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
						fdbtypes.ProcessClassStorage:           4,
						fdbtypes.ProcessClassLog:               4,
						fdbtypes.ProcessClassStateless:         8,
						fdbtypes.ProcessClassClusterController: 1,
					}))
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should exclude and re-include the instance", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeNil())

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
						originalPods.Items[firstStorageIndex].Status.PodIP: true,
					}))
				})

				It("should change the connection string", func() {
					Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Status.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemove).To(Equal([]string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
					}))
				})

				It("should clear removals from the process group status", func() {
					processGroups := cluster.Status.ProcessGroups
					Expect(len(processGroups)).To(Equal(len(originalPods.Items)))

					Expect(fdbtypes.ContainsProcessGroupID(processGroups, "storage-5")).To(BeTrue())
					oldID := originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel]
					Expect(fdbtypes.ContainsProcessGroupID(processGroups, oldID)).To(BeFalse())

					for _, group := range processGroups {
						Expect(group.Remove).To(BeFalse())
					}
				})

				Context("with a pod stuck in terminating", func() {
					BeforeEach(func() {
						pod := originalPods.Items[firstStorageIndex]
						for _, processGroup := range cluster.Status.ProcessGroups {
							if processGroup.ProcessGroupID == pod.ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel] {
								processGroup.UpdateCondition(fdbtypes.MissingProcesses, true, nil, "")
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
						Expect(cluster.Status.Generations).To(Equal(fdbtypes.ClusterGenerationStatus{
							Reconciled:        2,
							HasPendingRemoval: 2,
						}))
					})

					It("should exclude but not re-include the instance", func() {
						adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient).NotTo(BeNil())
						Expect(adminClient.ReincludedAddresses).To(HaveLen(0))
						Expect(adminClient.ExcludedAddresses).To(Equal([]string{
							originalPods.Items[firstStorageIndex].Status.PodIP,
						}))
					})
				})

				Context("with a PVC stuck in terminating", func() {
					BeforeEach(func() {
						originalPod := &originalPods.Items[firstStorageIndex]
						for _, processGroup := range cluster.Status.ProcessGroups {
							if processGroup.ProcessGroupID == originalPod.ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel] {
								processGroup.UpdateCondition(fdbtypes.MissingProcesses, true, nil, "")
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
						Expect(cluster.Status.Generations).To(Equal(fdbtypes.ClusterGenerationStatus{
							Reconciled:        2,
							HasPendingRemoval: 2,
						}))
					})

					It("should exclude but not re-include the instance", func() {
						adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())
						Expect(adminClient).NotTo(BeNil())
						Expect(adminClient.ReincludedAddresses).To(HaveLen(0))
						Expect(adminClient.ExcludedAddresses).To(Equal([]string{
							originalPods.Items[firstStorageIndex].Status.PodIP,
						}))
					})
				})
			})

			Context("with cluster skip enabled", func() {
				BeforeEach(func() {
					cluster.Spec.Skip = true
					// Since we don't reconcile we don't expect a generationGap
					generationGap = 0
				})

				Context("with an entry in the instances to remove list", func() {
					BeforeEach(func() {
						cluster.Spec.InstancesToRemove = []string{
							originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
						}
						err := k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})

					It("should not replace the pod", func() {
						pods := &corev1.PodList{}
						err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
						Expect(err).NotTo(HaveOccurred())
						Expect(len(pods.Items)).To(Equal(17))

						sortPodsByID(pods)

						for i := 0; i < 4; i++ {
							Expect(pods.Items[firstStorageIndex+i].Name).To(Equal(originalPods.Items[firstStorageIndex+i].Name))
						}
					})
				})
			})

			Context("with an entry in the pendingRemovals map", func() {
				BeforeEach(func() {
					pod := originalPods.Items[firstStorageIndex]
					cluster.Status.PendingRemovals = map[string]fdbtypes.PendingRemovalState{
						pod.ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel]: {
							PodName: pod.Name,
							Address: pod.Status.PodIP,
						},
					}
					err := k8sClient.Status().Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.SeedConnectionString = "touch"
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should keep the process counts the same", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
						fdbtypes.ProcessClassStorage:           4,
						fdbtypes.ProcessClassLog:               4,
						fdbtypes.ProcessClassStateless:         8,
						fdbtypes.ProcessClassClusterController: 1,
					}))
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should exclude and re-include the instance", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeNil())

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
						originalPods.Items[firstStorageIndex].Status.PodIP: true,
					}))
				})

				It("should change the connection string", func() {
					Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Status.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemove).To(BeNil())
				})
			})

			Context("with an entry in the pendingRemovals in the spec", func() {
				BeforeEach(func() {
					pod := originalPods.Items[firstStorageIndex]
					cluster.Spec.PendingRemovals = map[string]string{
						pod.Name: pod.Status.PodIP,
					}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
					generationGap = 2
				})

				It("should keep the process counts the same", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					Expect(len(pods.Items)).To(Equal(17))

					Expect(getProcessClassMap(pods.Items)).To(Equal(map[fdbtypes.ProcessClass]int{
						fdbtypes.ProcessClassStorage:           4,
						fdbtypes.ProcessClassLog:               4,
						fdbtypes.ProcessClassStateless:         8,
						fdbtypes.ProcessClassClusterController: 1,
					}))
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should exclude and re-include the instance", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeNil())

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{
						originalPods.Items[firstStorageIndex].Status.PodIP: true,
					}))
				})

				It("should change the connection string", func() {
					Expect(cluster.Status.ConnectionString).NotTo(Equal(originalConnectionString))
				})

				It("should clear the removal list", func() {
					_, err = reloadCluster(cluster)
					Expect(err).NotTo(HaveOccurred())

					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Status.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemove).To(BeNil())
				})
			})

			Context("with a missing pod IP", func() {
				var podIP string

				BeforeEach(func() {
					podIP = originalPods.Items[firstStorageIndex].Status.PodIP

					err := k8sClient.RemovePodIP(&originalPods.Items[firstStorageIndex])
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.InstancesToRemove = []string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
					}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should exclude and re-include the instance", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeNil())

					Expect(adminClient.ReincludedAddresses).To(Equal(map[string]bool{podIP: true}))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemove).To(Equal([]string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
					}))
				})
			})

			Context("with a removal with no exclusion", func() {
				BeforeEach(func() {
					err := k8sClient.RemovePodIP(&originalPods.Items[firstStorageIndex])
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.InstancesToRemoveWithoutExclusion = []string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
					}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should replace one of the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(pods.Items)).To(Equal(17))

					sortPodsByID(pods)

					Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
					Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
					Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
					Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
				})

				It("should not exclude anything", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(adminClient).NotTo(BeNil())
					Expect(adminClient.ExcludedAddresses).To(BeNil())
					Expect(len(adminClient.ReincludedAddresses)).To(Equal(0))
				})

				It("should clear the removal list", func() {
					Expect(cluster.Spec.PendingRemovals).To(BeNil())
					Expect(cluster.Spec.InstancesToRemoveWithoutExclusion).To(Equal([]string{
						originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
					}))
				})
			})
		})

		Context("with a missing process", func() {
			var adminClient *MockAdminClient

			BeforeEach(func() {
				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				adminClient.MockMissingProcessGroup("storage-1", true)

				// Run a single reconciliation to detect the missing process.
				result, err := reconcileObject(clusterReconciler, cluster.ObjectMeta, 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())

				// Tweak the time on the missing process to make it eligible for replacement.
				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				processGroup := fdbtypes.FindProcessGroupByID(cluster.Status.ProcessGroups, "storage-1")
				Expect(processGroup).NotTo(BeNil())
				Expect(len(processGroup.ProcessGroupConditions)).To(Equal(1))
				Expect(processGroup.ProcessGroupConditions[0].ProcessGroupConditionType).To(Equal(fdbtypes.MissingProcesses))
				Expect(processGroup.ProcessGroupConditions[0].Timestamp).NotTo(Equal(0))
				processGroup.ProcessGroupConditions[0].Timestamp -= 3600
				err = k8sClient.Status().Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				generationGap = 0
			})

			It("should replace the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getSinglePodListOptions(cluster, "storage-1")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(0))

				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(17))

				sortPodsByID(pods)

				Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
				Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
				Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
				Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-5"))
			})
		})

		Context("with a missing process and pending exclusion", func() {
			var adminClient *MockAdminClient

			BeforeEach(func() {
				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.InstancesToRemove = []string{
					originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				adminClient.MockMissingProcessGroup("storage-2", true)
				shouldCompleteReconciliation = false
				generationGap = 0
			})

			JustBeforeEach(func() {
				generations, err := reloadClusterGenerations(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generations).To(Equal(fdbtypes.ClusterGenerationStatus{
					Reconciled:             originalVersion,
					NeedsCoordinatorChange: originalVersion + 1,
					NeedsShrink:            originalVersion + 1,
					HasUnhealthyProcess:    originalVersion + 1,
				}))
			})

			It("should not exclude or remove the process", func() {
				Expect(adminClient.ExcludedAddresses).To(BeNil())
				Expect(len(adminClient.ReincludedAddresses)).To(Equal(0))

				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getSinglePodListOptions(cluster, "storage-2")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(1))

				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(18))
			})
		})

		Context("with multiple replacements", func() {
			BeforeEach(func() {
				cluster.Spec.InstancesToRemove = []string{
					originalPods.Items[firstStorageIndex].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
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

				sortPodsByID(pods)

				Expect(pods.Items[firstStorageIndex].Name).To(Equal(originalPods.Items[firstStorageIndex+1].Name))
				Expect(pods.Items[firstStorageIndex+1].Name).To(Equal(originalPods.Items[firstStorageIndex+2].Name))
				Expect(pods.Items[firstStorageIndex+2].Name).To(Equal(originalPods.Items[firstStorageIndex+3].Name))
				Expect(pods.Items[firstStorageIndex+3].Name).To(Equal("operator-test-1-storage-6"))
			})
		})

		Context("with a pod that gets deleted", func() {
			var pod corev1.Pod
			BeforeEach(func() {
				generationGap = 0

				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getSinglePodListOptions(cluster, "storage-1")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(1))
				pod = pods.Items[0]
				err := k8sClient.Delete(context.TODO(), &pod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should replace the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getSinglePodListOptions(cluster, "storage-1")...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(1))
				Expect(pods.Items[0].ObjectMeta.UID).NotTo(Equal(pod.ObjectMeta.UID))
				Expect(pods.Items[0].Name).To(Equal("operator-test-1-storage-1"))
			})
		})

		Context("with a knob change", func() {
			var adminClient *MockAdminClient

			BeforeEach(func() {
				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				err = adminClient.FreezeStatus()
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{"knob_disable_posix_kernel_aio=1"}}}
			})

			Context("with bounces enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						// TODO: johscheuer get real process number !
						addresses = append(addresses, cluster.GetFullAddress(pod.Status.PodIP, 1))
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
					err = k8sClient.Get(context.TODO(), configMapName, configMap)
					Expect(err).NotTo(HaveOccurred())
					expectedConfigMap, _ := GetConfigMap(cluster)
					Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
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
					Expect(generations).To(Equal(fdbtypes.ClusterGenerationStatus{
						Reconciled:          originalVersion,
						NeedsBounce:         originalVersion + 1,
						HasUnhealthyProcess: originalVersion + 1,
					}))
				})

				It("should not kill any processes", func() {
					Expect(adminClient.KilledAddresses).To(BeNil())
				})

				It("should update the config map", func() {
					configMap := &corev1.ConfigMap{}
					configMapName := types.NamespacedName{Namespace: "my-ns", Name: fmt.Sprintf("%s-config", cluster.Name)}
					err = k8sClient.Get(context.TODO(), configMapName, configMap)
					Expect(err).NotTo(HaveOccurred())
					expectedConfigMap, _ := GetConfigMap(cluster)
					Expect(configMap.Data).To(Equal(expectedConfigMap.Data))
				})
			})

			Context("with multiple storage servers per pod", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{}}}
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

					sortPodsByID(originalPods)

					err = adminClient.FreezeStatus()
					Expect(err).NotTo(HaveOccurred())

					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{"knob_disable_posix_kernel_aio=1"}}}
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make([]string, 0, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses = append(addresses, cluster.GetFullAddress(pod.Status.PodIP, 1))
						if internal.ProcessClassFromLabels(pod.ObjectMeta.Labels) == fdbtypes.ProcessClassStorage {
							addresses = append(addresses, cluster.GetFullAddress(pod.Status.PodIP, 2))
						}
					}

					sort.Slice(adminClient.KilledAddresses, func(i, j int) bool {
						return strings.Compare(adminClient.KilledAddresses[i], adminClient.KilledAddresses[j]) < 0
					})
					sort.Slice(addresses, func(i, j int) bool {
						return strings.Compare(addresses[i], addresses[j]) < 0
					})
					Expect(adminClient.KilledAddresses).To(Equal(addresses))
				})
			})
		})

		Context("with a configuration change", func() {
			var adminClient *MockAdminClient
			BeforeEach(func() {
				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.DatabaseConfiguration.RedundancyMode).To(Equal("double"))

				cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"

				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.DatabaseConfiguration.RedundancyMode).To(Equal("double"))
			})

			Context("with changes enabled", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should configure the database", func() {
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("triple"))
				})
			})

			Context("with a change to the log version", func() {
				BeforeEach(func() {
					cluster.Spec.DatabaseConfiguration.LogVersion = 3
					cluster.Spec.DatabaseConfiguration.RedundancyMode = "double"
					generationGap = 1
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should configure the database", func() {
					Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(Equal("double"))
					Expect(adminClient.DatabaseConfiguration.LogVersion).To(Equal(3))
				})
			})

			Context("with a change to the log version that is not set in the spec", func() {
				BeforeEach(func() {
					cluster.Spec.DatabaseConfiguration.RedundancyMode = "double"
					cluster.Spec.SeedConnectionString = "touch"

					configuration := cluster.DesiredDatabaseConfiguration()
					configuration.LogVersion = 3
					err = adminClient.ConfigureDatabase(configuration, false)
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
					var flag = false
					cluster.Spec.AutomationOptions.ConfigureDatabase = &flag
					cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbtypes.ClusterGenerationStatus{
						Reconciled:               originalVersion,
						NeedsConfigurationChange: originalVersion + 1,
						NeedsCoordinatorChange:   originalVersion + 1,
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
				cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
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

				cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
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
					_, id, err := ParseInstanceID(item.Labels[fdbtypes.FDBInstanceIDLabel])
					Expect(err).NotTo(HaveOccurred())

					hash, err := GetPodSpecHash(cluster, internal.ProcessClassFromLabels(item.Labels), id, nil)
					Expect(err).NotTo(HaveOccurred())

					configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(item.ObjectMeta), &item)
					Expect(err).NotTo(HaveOccurred())
					if item.Labels[fdbtypes.FDBInstanceIDLabel] == "storage-1" {
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
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
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

					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
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
						if item.ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel] == "storage-1" {
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
						_, id, err := ParseInstanceID(item.Labels[fdbtypes.FDBInstanceIDLabel])
						Expect(err).NotTo(HaveOccurred())

						hash, err := GetPodSpecHash(cluster, internal.ProcessClassFromLabels(item.Labels), id, nil)
						Expect(err).NotTo(HaveOccurred())

						configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(item.ObjectMeta), &item)
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
					_, id, err := ParseInstanceID(item.Labels[fdbtypes.FDBInstanceIDLabel])
					Expect(err).NotTo(HaveOccurred())

					hash, err := GetPodSpecHash(cluster, internal.ProcessClassFromLabels(item.Labels), id, nil)
					Expect(err).NotTo(HaveOccurred())

					configMapHash, err := getConfigMapHash(cluster, internal.GetProcessClassFromMeta(item.ObjectMeta), &item)
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
				cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "foundationdb",
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

			Context("with the replacement strategy", func() {
				BeforeEach(func() {
					cluster.Spec.UpdatePodsByReplacement = true
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

				It("should replace the instance", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					replacements := make(map[string]bool, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						replacements[pod.Status.PodIP] = true
					}

					Expect(adminClient.ReincludedAddresses).To(Equal(replacements))
				})
			})

			Context("with deletion disabled", func() {
				BeforeEach(func() {
					var flag = false
					cluster.Spec.AutomationOptions.DeletePods = &flag

					shouldCompleteReconciliation = false

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbtypes.ClusterGenerationStatus{
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

		Context("with a change to environment variables with the deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.Env = append(cluster.Spec.MainContainer.Env, corev1.EnvVar{
					Name:  "TEST_CHANGE",
					Value: "1",
				})
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
					var flag = false
					cluster.Spec.AutomationOptions.DeletePods = &flag

					shouldCompleteReconciliation = false

					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					generations, err := reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					Expect(generations).To(Equal(fdbtypes.ClusterGenerationStatus{
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
				source := fdbtypes.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the public IP annotations", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range pods.Items {
					Expect(pod.Annotations[fdbtypes.PublicIPSourceAnnotation]).To(Equal("service"))
				}

				pod := pods.Items[0]
				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, service)
				Expect(err).NotTo(HaveOccurred())

				Expect(pod.Annotations[fdbtypes.PublicIPAnnotation]).To(Equal(service.Spec.ClusterIP))
				Expect(pod.Annotations[fdbtypes.PublicIPAnnotation]).NotTo(Equal(""))
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
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				replacements := make(map[string]bool, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					replacements[pod.Status.PodIP] = true
				}

				Expect(adminClient.ReincludedAddresses).To(Equal(replacements))
			})
		})

		Context("with a change to the public IP source and multiple storage servers per Pod", func() {
			BeforeEach(func() {
				source := fdbtypes.PublicIPSourceService
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
				sortPodsByID(pods)
				Expect(pods.Items).To(HaveLen(len(originalPods.Items)))

				Expect(pods.Items).NotTo(ContainOriginalPod(13))
				Expect(pods.Items).NotTo(ContainOriginalPod(14))
				Expect(pods.Items).NotTo(ContainOriginalPod(15))
				Expect(pods.Items).NotTo(ContainOriginalPod(16))

				var storagePod corev1.Pod
				for _, pod := range pods.Items {
					Expect(pod.Annotations[fdbtypes.PublicIPSourceAnnotation]).To(Equal("service"))

					if internal.ProcessClassFromLabels(pod.Labels) == fdbtypes.ProcessClassStorage && storagePod.Name == "" {
						storagePod = pod
					}
				}

				service := &corev1.Service{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: storagePod.Namespace, Name: storagePod.Name}, service)
				Expect(err).NotTo(HaveOccurred())

				Expect(storagePod.Annotations[fdbtypes.PublicIPAnnotation]).To(Equal(service.Spec.ClusterIP))
				Expect(storagePod.Annotations[fdbtypes.PublicIPAnnotation]).NotTo(Equal(""))
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
				sortPodsByID(pods)

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
					if internal.ProcessClassFromLabels(pod.Labels) == fdbtypes.ProcessClassStorage && storagePod.Name == "" {
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
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				replacements := make(map[string]bool, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					replacements[pod.Status.PodIP] = true
				}

				Expect(adminClient.ReincludedAddresses).To(Equal(replacements))
			})
		})

		Context("with a change to TLS settings", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should bounce the processes", func() {
				addresses := make(map[string]bool, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					addresses[fmt.Sprintf("%s:4500:tls", pod.Status.PodIP)] = true
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
				connectionString, err := fdbtypes.ParseConnectionString(cluster.Status.ConnectionString)

				Expect(err).NotTo(HaveOccurred())
				for _, coordinator := range connectionString.Coordinators {
					address, err := fdbtypes.ParseProcessAddress(coordinator)
					Expect(err).NotTo(HaveOccurred())
					Expect(address.Flags["tls"]).To(BeTrue())
				}
			})
		})

		Context("with only storage processes as coordinator", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbtypes.ProcessClassStorage,
						Priority:     0,
					},
				}
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should only have storage processes as coordinator", func() {
				connectionString, err := fdbtypes.ParseConnectionString(cluster.Status.ConnectionString)
				Expect(err).NotTo(HaveOccurred())

				addressClassMap := map[string]fdbtypes.ProcessClass{}
				for _, pGroup := range cluster.Status.ProcessGroups {
					addressClassMap[pGroup.Addresses[0]] = pGroup.ProcessClass
				}

				for _, coordinator := range connectionString.Coordinators {
					addr := coordinator[:strings.LastIndex(coordinator, ":")]
					Expect(addressClassMap[addr]).To(Equal(fdbtypes.ProcessClassStorage))
				}
			})

			When("changing the coordinator selection to only select log processes", func() {
				BeforeEach(func() {
					cluster.Spec.CoordinatorSelection = []fdbtypes.CoordinatorSelectionSetting{
						{
							ProcessClass: fdbtypes.ProcessClassLog,
							Priority:     0,
						},
					}
					err := k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
					generationGap++
				})

				It("should only have log processes as coordinator", func() {
					connectionString, err := fdbtypes.ParseConnectionString(cluster.Status.ConnectionString)
					Expect(err).NotTo(HaveOccurred())

					addressClassMap := map[string]fdbtypes.ProcessClass{}
					for _, pGroup := range cluster.Status.ProcessGroups {
						addressClassMap[pGroup.Addresses[0]] = pGroup.ProcessClass
					}

					for _, coordinator := range connectionString.Coordinators {
						addr := coordinator[:strings.LastIndex(coordinator, ":")]
						Expect(addressClassMap[addr]).To(Equal(fdbtypes.ProcessClassLog))
					}
				})
			})
		})

		Context("downgrade cluster", func() {
			BeforeEach(func() {
				shouldCompleteReconciliation = false
				IncompatibleVersion := fdbtypes.Versions.Default
				IncompatibleVersion.Patch--
				cluster.Spec.Version = IncompatibleVersion.String()
				err := k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not downgrade cluster", func() {
				Expect(cluster.Status.Generations.Reconciled).To(Equal(originalVersion))
				Expect(cluster.Status.RunningVersion).To(Equal(fdbtypes.Versions.Default.String()))
			})
		})

		Context("with an upgrade", func() {
			var adminClient *MockAdminClient

			BeforeEach(func() {
				cluster.Spec.Version = fdbtypes.Versions.NextMajorVersion.String()

				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			Context("with the default strategy", func() {
				BeforeEach(func() {
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]bool, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[fmt.Sprintf("%s:4501", pod.Status.PodIP)] = true
					}

					killedAddresses := make(map[string]bool, len(adminClient.KilledAddresses))
					for _, address := range adminClient.KilledAddresses {
						killedAddresses[address] = true
					}
					Expect(killedAddresses).To(Equal(addresses))
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbtypes.Versions.NextMajorVersion.String())))
					}
				})

				It("should update the running version", func() {
					Expect(cluster.Status.RunningVersion).To(Equal(cluster.Spec.Version))
				})
			})

			Context("with the replacement strategy", func() {
				BeforeEach(func() {
					cluster.Spec.UpdatePodsByReplacement = true
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should bounce the processes", func() {
					addresses := make(map[string]bool, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						addresses[fmt.Sprintf("%s:4501", pod.Status.PodIP)] = true
					}

					killedAddresses := make(map[string]bool, len(adminClient.KilledAddresses))
					for _, address := range adminClient.KilledAddresses {
						killedAddresses[address] = true
					}
					Expect(killedAddresses).To(Equal(addresses))
				})

				It("should set the image on the pods", func() {
					pods := &corev1.PodList{}
					err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
					Expect(err).NotTo(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", fdbtypes.Versions.NextMajorVersion.String())))
					}
				})

				It("should replace the instances", func() {
					replacements := make(map[string]bool, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						replacements[pod.Status.PodIP] = true
					}

					Expect(adminClient.ReincludedAddresses).To(Equal(replacements))
				})
			})

			Context("with all upgradable clients", func() {
				BeforeEach(func() {
					adminClient.MockClientVersion(fdbtypes.Versions.NextMajorVersion.String(), []string{"127.0.0.2:3687"})
					err = k8sClient.Update(context.TODO(), cluster)
					Expect(err).NotTo(HaveOccurred())

				})

				It("should not set a message about the client upgradability", func() {
					events := &corev1.EventList{}
					err = k8sClient.List(context.TODO(), events)
					Expect(err).NotTo(HaveOccurred())
					matchingEvents := []corev1.Event{}
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
					adminClient.MockClientVersion(fdbtypes.Versions.NextMajorVersion.String(), []string{"127.0.0.2:3687"})
					adminClient.MockClientVersion(fdbtypes.Versions.Default.String(), []string{"127.0.0.3:85891"})
				})

				Context("with the check enabled", func() {
					BeforeEach(func() {
						err = k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
						shouldCompleteReconciliation = false
					})

					It("should set a message about the client upgradability", func() {
						events := &corev1.EventList{}
						matchingEvents := []corev1.Event{}

						err = k8sClient.List(context.TODO(), events)
						Expect(err).NotTo(HaveOccurred())

						for _, event := range events.Items {
							if event.InvolvedObject.UID == cluster.ObjectMeta.UID && event.Reason == "UnsupportedClient" {
								matchingEvents = append(matchingEvents, event)
							}
						}
						Expect(len(matchingEvents)).NotTo(Equal(0))

						Expect(matchingEvents[0].Message).To(Equal(
							fmt.Sprintf("1 clients do not support version %s: 127.0.0.3:85891", fdbtypes.Versions.NextMajorVersion),
						))
					})
				})

				Context("with the check disabled", func() {
					BeforeEach(func() {
						cluster.Spec.IgnoreUpgradabilityChecks = true
						err = k8sClient.Update(context.TODO(), cluster)
						Expect(err).NotTo(HaveOccurred())
					})

					It("should not set a message about the client upgradability", func() {
						events := &corev1.EventList{}
						err = k8sClient.List(context.TODO(), events)
						Expect(err).NotTo(HaveOccurred())
						matchingEvents := []corev1.Event{}
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
		})

		Context("with a change to the volume size", func() {
			Context("with the fields from the processes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
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

				It("should replace the instances", func() {
					adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					replacements := make(map[string]bool, len(originalPods.Items))
					for _, pod := range originalPods.Items {
						processClass := internal.GetProcessClassFromMeta(pod.ObjectMeta)
						if processClass.IsStateful() {
							replacements[pod.Status.PodIP] = true
						}
					}

					Expect(adminClient.ReincludedAddresses).To(Equal(replacements))
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

		Context("with a change to the instance ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.InstanceIDPrefix = "my-instances"

				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should replace the instances", func() {
				adminClient, err := newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				replacements := make(map[string]bool, len(originalPods.Items))
				for _, pod := range originalPods.Items {
					replacements[pod.Status.PodIP] = true
				}

				Expect(adminClient.ReincludedAddresses).To(Equal(replacements))
			})

			It("should generate instance IDs with the new prefix", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())

				sortPodsByID(pods)
				Expect(pods.Items[0].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("my-instances-cluster_controller-2"))
				Expect(pods.Items[1].Labels[fdbtypes.FDBInstanceIDLabel]).To(Equal("my-instances-log-5"))
			})
		})

		Context("when enabling a headless service", func() {
			BeforeEach(func() {
				var flag = true
				cluster.Spec.Routing.HeadlessService = &flag
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
				var flag = true
				cluster.Spec.Routing.HeadlessService = &flag
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
				cluster.Spec.LockOptions.DenyList = append(cluster.Spec.LockOptions.DenyList, fdbtypes.LockDenyListEntry{ID: "dc2"})
				var locksDisabled = false
				cluster.Spec.LockOptions.DisableLocks = &locksDisabled
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

		When("When a process have an incorrect commandline", func() {
			var adminClient *MockAdminClient

			BeforeEach(func() {
				adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())
				adminClient.MockIncorrectCommandLine("storage-1", true)
				generationGap = 0
				shouldCompleteReconciliation = false
			})

			It("It should report the incorrect commandline in the process groups", func() {
				// We have to reload the cluster because we don't reach the reconcile state
				_, err = reloadClusterGenerations(cluster)
				Expect(err).NotTo(HaveOccurred())
				incorrectProcesses := fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, fdbtypes.IncorrectCommandLine, false)
				Expect(incorrectProcesses).To(Equal([]string{"storage-1"}))
			})

			When("When an additional process have an incorrect commandline", func() {
				BeforeEach(func() {
					adminClient.MockIncorrectCommandLine("storage-2", true)
					generationGap = 0
					shouldCompleteReconciliation = false
				})

				It("It should report the incorrect commandline in the process groups", func() {
					// We have to reload the cluster because we don't reach the reconcile state
					_, err = reloadClusterGenerations(cluster)
					Expect(err).NotTo(HaveOccurred())
					incorrectProcesses := fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, fdbtypes.IncorrectCommandLine, false)
					Expect(incorrectProcesses).To(Equal([]string{"storage-1", "storage-2"}))
				})
			})
		})
	})

	Describe("GetConfigMap", func() {
		var configMap *corev1.ConfigMap
		var err error

		BeforeEach(func() {
			cluster.Status.ConnectionString = fakeConnectionString
			cluster.Status.RunningVersion = cluster.Spec.Version
			err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			configMap, err = GetConfigMap(cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			It("should populate the metadata", func() {
				Expect(configMap.Namespace).To(Equal("my-ns"))
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-config", cluster.Name)))
				Expect(configMap.Labels).To(Equal(map[string]string{
					fdbtypes.FDBClusterLabel: cluster.Name,
				}))
				Expect(configMap.Annotations).To(BeNil())
			})

			It("should have the basic files", func() {
				expectedConf, err := GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())

				Expect(configMap.Data[clusterFileKey]).To(Equal("operator-test:asdfasf@127.0.0.1:4501"))
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(expectedConf))
				Expect(configMap.Data["running-version"]).To(Equal(fdbtypes.Versions.Default.String()))
				Expect(configMap.Data["sidecar-conf"]).To(Equal(""))
			})
		})

		Context("with a version that requires sidecar conf", func() {
			BeforeEach(func() {
				cluster.Status.RunningVersion = fdbtypes.Versions.WithEnvironmentVariablesForSidecar.String()
			})

			It("should have the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(sidecarConf)).To(Equal(5))
				Expect(sidecarConf["COPY_FILES"]).To(Equal([]interface{}{"fdb.cluster"}))
				Expect(sidecarConf["COPY_BINARIES"]).To(Equal([]interface{}{"fdbserver", "fdbcli"}))
				Expect(sidecarConf["COPY_LIBRARIES"]).To(Equal([]interface{}{}))
				Expect(sidecarConf["INPUT_MONITOR_CONF"]).To(Equal("fdbmonitor.conf"))
			})
		})

		Context("with the sidecar conf enabled in the status", func() {
			BeforeEach(func() {
				cluster.Status.NeedsSidecarConfInConfigMap = true
			})

			It("should have the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(sidecarConf)).To(Equal(5))
				Expect(sidecarConf["COPY_FILES"]).To(Equal([]interface{}{"fdb.cluster"}))
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

			Context("with a version that uses sidecar command-line arguments", func() {
				BeforeEach(func() {
					cluster.Status.RunningVersion = fdbtypes.Versions.WithCommandLineVariablesForSidecar.String()
				})

				It("should populate the CA file", func() {
					Expect(configMap.Data["ca-file"]).To(Equal("-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC\n---CERT2----"))
				})
			})

			Context("with a version that uses sidecar environment variables", func() {
				BeforeEach(func() {
					cluster.Status.RunningVersion = fdbtypes.Versions.WithEnvironmentVariablesForSidecar.String()
				})

				It("should populate the CA file", func() {
					Expect(configMap.Data["ca-file"]).To(Equal("-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC\n---CERT2----"))
				})

				It("should copy the CA file in the sidecar conf", func() {
					sidecarConf := make(map[string]interface{})
					err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
					Expect(err).NotTo(HaveOccurred())
					Expect(sidecarConf["COPY_FILES"]).To(Equal([]interface{}{"fdb.cluster", "ca.pem"}))
				})
			})
		})

		Context("with an empty connection string", func() {
			BeforeEach(func() {
				cluster.Status.ConnectionString = ""
			})

			It("should empty the monitor conf and cluster file", func() {
				Expect(configMap.Data[clusterFileKey]).To(Equal(""))
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(""))
			})
		})

		Context("with custom sidecar substitutions", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}
				cluster.Status.RunningVersion = fdbtypes.Versions.WithEnvironmentVariablesForSidecar.String()
			})

			It("should put the substitutions in the sidecar conf", func() {
				sidecarConf := make(map[string]interface{})
				err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
				Expect(err).NotTo(HaveOccurred())
				Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(Equal([]interface{}{"FAULT_DOMAIN", "ZONE", "FDB_INSTANCE_ID"}))
			})
		})

		Context("with explicit instance ID substitution", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbtypes.Versions.WithoutSidecarInstanceIDSubstitution.String()
				cluster.Status.RunningVersion = fdbtypes.Versions.WithoutSidecarInstanceIDSubstitution.String()
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
					fdbtypes.FDBClusterLabel: cluster.Name,
					"fdb-label":              "value1",
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

		BeforeEach(func() {
			cluster.Status.ConnectionString = fakeConnectionString
		})

		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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

		Context("with a basic storage instance with multiple storage servers per Pod", func() {
			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
				source := fdbtypes.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, 1)
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
				source := fdbtypes.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, 1)
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
		})

		Context("with TLS enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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

				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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

				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{
						"knob_disable_posix_kernel_aio = 1",
					}}}
					conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
					cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
						fdbtypes.ProcessClassGeneral: {CustomParameters: &[]string{
							"knob_disable_posix_kernel_aio = 1",
						}},
						fdbtypes.ProcessClassStorage: {CustomParameters: &[]string{
							"knob_test = test1",
						}},
						fdbtypes.ProcessClassStateless: {CustomParameters: &[]string{
							"knob_test = test2",
						}},
					}
					conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
				cluster.Spec.Version = fdbtypes.Versions.WithBinariesFromMainContainer.String()
				cluster.Status.RunningVersion = fdbtypes.Versions.WithBinariesFromMainContainer.String()
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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

		Context("with a version with binaries from the sidecar container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbtypes.Versions.WithoutBinariesFromMainContainer.String()
				cluster.Status.RunningVersion = fdbtypes.Versions.WithoutBinariesFromMainContainer.String()
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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
				conf, err = GetMonitorConf(cluster, fdbtypes.ProcessClassStorage, nil, cluster.GetStorageServersPerPod())
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

	Describe("GetStartCommand", func() {
		var pods *corev1.PodList
		var command string
		var err error

		BeforeEach(func() {
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo((HaveOccurred()))
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(int64(1)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			pods = &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())

			Expect(len(pods.Items)).To(Equal(17))

			sortPodsByID(pods)

			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("for a basic storage process", func() {
			It("should substitute the variables in the start command", func() {
				instance := newFdbInstance(pods.Items[firstStorageIndex])
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: instance.Pod}
				command, err = GetStartCommand(cluster, instance, podClient, 1, 1)
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
					"--public_address=1.1.1.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("for a basic storage process with multiple storage servers per Pod", func() {
			It("should substitute the variables in the start command", func() {
				instance := newFdbInstance(pods.Items[firstStorageIndex])
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: instance.Pod}
				command, err = GetStartCommand(cluster, instance, podClient, 1, 2)
				Expect(err).NotTo(HaveOccurred())

				id := instance.GetInstanceID()
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data/1",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_process_id=%s-1", id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=1.1.1.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))

				command, err = GetStartCommand(cluster, instance, podClient, 2, 2)
				Expect(err).NotTo(HaveOccurred())
				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--class=storage",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--datadir=/var/fdb/data/2",
					fmt.Sprintf("--locality_instance_id=%s", id),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
					fmt.Sprintf("--locality_process_id=%s-2", id),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--public_address=1.1.1.1:4503",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with host replication", func() {
			BeforeEach(func() {
				pod := pods.Items[firstStorageIndex]
				pod.Spec.NodeName = "machine1"
				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{}

				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient, 1, 1)
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
					"--public_address=1.1.1.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with cross-Kubernetes replication", func() {
			BeforeEach(func() {
				pod := pods.Items[firstStorageIndex]
				pod.Spec.NodeName = "machine1"

				cluster.Spec.FaultDomain = fdbtypes.FoundationDBClusterFaultDomain{
					Key:   "foundationdb.org/kubernetes-cluster",
					Value: "kc2",
				}

				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient, 1, 1)
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
					"--public_address=1.1.1.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with binaries from the main container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbtypes.Versions.WithBinariesFromMainContainer.String()
				cluster.Status.RunningVersion = fdbtypes.Versions.WithBinariesFromMainContainer.String()
				pod := pods.Items[firstStorageIndex]
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("includes the binary path in the start command", func() {
				id := pods.Items[firstStorageIndex].Labels[fdbtypes.FDBInstanceIDLabel]
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
					"--public_address=1.1.1.1:4501",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				}, " ")))
			})
		})

		Context("with binaries from the sidecar container", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbtypes.Versions.WithoutBinariesFromMainContainer.String()
				cluster.Status.RunningVersion = fdbtypes.Versions.WithoutBinariesFromMainContainer.String()
				pod := pods.Items[firstStorageIndex]
				podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
				command, err = GetStartCommand(cluster, newFdbInstance(pod), podClient, 1, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("includes the binary path in the start command", func() {
				id := pods.Items[firstStorageIndex].Labels[fdbtypes.FDBInstanceIDLabel]
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
					"--public_address=1.1.1.1:4501",
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
				Expect(prefix).To(Equal(fdbtypes.ProcessClassStorage))
				Expect(id).To(Equal(12))
			})
		})

		Context("with a cluster controller ID", func() {
			It("can parse the ID", func() {
				prefix, id, err := ParseInstanceID("cluster_controller-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal(fdbtypes.ProcessClassClusterController))
				Expect(id).To(Equal(3))
			})
		})

		Context("with a custom prefix", func() {
			It("parses the prefix", func() {
				prefix, id, err := ParseInstanceID("dc1-storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(prefix).To(Equal(fdbtypes.ProcessClass("dc1-storage")))
				Expect(id).To(Equal(12))
			})
		})

		Context("with no prefix", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseInstanceID("6")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse instance ID 6"))
			})
		})

		Context("with no numbers", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseInstanceID("storage")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse instance ID storage"))
			})
		})

		Context("with a text suffix", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseInstanceID("storage-bad")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse instance ID storage-bad"))
			})
		})
	})

	Describe("GetInstanceIDFromProcessID", func() {
		It("can parse a process ID", func() {
			Expect(GetInstanceIDFromProcessID("storage-1-1")).To(Equal("storage-1"))
		})
		It("can parse a process ID with a prefix", func() {
			Expect(GetInstanceIDFromProcessID("dc1-storage-1-1")).To(Equal("dc1-storage-1"))
		})

		It("can handle a process group ID with no process number", func() {
			Expect(GetInstanceIDFromProcessID("storage-2")).To(Equal("storage-2"))
		})
	})

	Describe("chooseDistributedProcesses", func() {
		var candidates []localityInfo
		var result []localityInfo
		var err error

		Context("with a flat set of processes", func() {
			BeforeEach(func() {
				candidates = []localityInfo{
					{ID: "p1", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p2", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p3", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p4", LocalityData: map[string]string{"zoneid": "z3"}},
					{ID: "p5", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p6", LocalityData: map[string]string{"zoneid": "z4"}},
					{ID: "p7", LocalityData: map[string]string{"zoneid": "z5"}},
				}
				result, err = chooseDistributedProcesses(cluster, candidates, 5, processSelectionConstraint{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should recruit the processes across multiple zones", func() {
				Expect(len(result)).To(Equal(5))
				Expect(result[0].ID).To(Equal("p1"))
				Expect(result[1].ID).To(Equal("p3"))
				Expect(result[2].ID).To(Equal("p4"))
				Expect(result[3].ID).To(Equal("p6"))
				Expect(result[4].ID).To(Equal("p7"))
			})
		})

		Context("with fewer zones than desired processes", func() {
			BeforeEach(func() {
				candidates = []localityInfo{
					{ID: "p1", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p2", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p3", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p4", LocalityData: map[string]string{"zoneid": "z3"}},
					{ID: "p5", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p6", LocalityData: map[string]string{"zoneid": "z4"}},
				}
			})

			Context("with no hard limit", func() {
				It("should only re-use zones as necessary", func() {
					result, err = chooseDistributedProcesses(cluster, candidates, 5, processSelectionConstraint{})
					Expect(err).NotTo(HaveOccurred())

					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p3"))
					Expect(result[2].ID).To(Equal("p4"))
					Expect(result[3].ID).To(Equal("p6"))
					Expect(result[4].ID).To(Equal("p2"))
				})
			})

			Context("with a hard limit", func() {
				It("should give an error", func() {
					result, err = chooseDistributedProcesses(cluster, candidates, 5, processSelectionConstraint{
						HardLimits: map[string]int{"zoneid": 1},
					})
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("Could only select 4 processes, but 5 are required"))
				})
			})
		})

		Context("with multiple data centers", func() {
			BeforeEach(func() {
				candidates = []localityInfo{
					{ID: "p1", LocalityData: map[string]string{"zoneid": "z1", "dcid": "dc1"}},
					{ID: "p2", LocalityData: map[string]string{"zoneid": "z1", "dcid": "dc1"}},
					{ID: "p3", LocalityData: map[string]string{"zoneid": "z2", "dcid": "dc1"}},
					{ID: "p4", LocalityData: map[string]string{"zoneid": "z3", "dcid": "dc1"}},
					{ID: "p5", LocalityData: map[string]string{"zoneid": "z2", "dcid": "dc1"}},
					{ID: "p6", LocalityData: map[string]string{"zoneid": "z4", "dcid": "dc1"}},
					{ID: "p7", LocalityData: map[string]string{"zoneid": "z5", "dcid": "dc1"}},
					{ID: "p8", LocalityData: map[string]string{"zoneid": "z6", "dcid": "dc2"}},
					{ID: "p9", LocalityData: map[string]string{"zoneid": "z7", "dcid": "dc2"}},
					{ID: "p10", LocalityData: map[string]string{"zoneid": "z8", "dcid": "dc2"}},
				}
			})

			Context("with the default constraints", func() {
				BeforeEach(func() {
					result, err = chooseDistributedProcesses(cluster, candidates, 5, processSelectionConstraint{})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should recruit the processes across multiple zones and data centers", func() {
					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p10"))
					Expect(result[2].ID).To(Equal("p3"))
					Expect(result[3].ID).To(Equal("p8"))
					Expect(result[4].ID).To(Equal("p4"))
				})
			})

			Context("when only distributing across data centers", func() {

				BeforeEach(func() {
					result, err = chooseDistributedProcesses(cluster, candidates, 5, processSelectionConstraint{
						Fields: []string{"dcid"},
					})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should recruit the processes across data centers", func() {
					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p10"))
					Expect(result[2].ID).To(Equal("p2"))
					Expect(result[3].ID).To(Equal("p8"))
					Expect(result[4].ID).To(Equal("p3"))
				})
			})
		})
	})

	Describe("checkCoordinatorValidity", func() {
		var status *fdbtypes.FoundationDBStatus
		var adminClient AdminClient
		var err error

		BeforeEach(func() {
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			generations, err := reloadClusterGenerations(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generations.Reconciled).To(Equal(int64(1)))

			adminClient, err = NewMockAdminClient(cluster, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with the default configuration", func() {
			It("should report the coordinators as valid", func() {
				coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
				for _, coordinator := range status.Client.Coordinators.Coordinators {
					coordinatorStatus[coordinator.Address] = false
				}

				coordinatorsValid, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeTrue())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with too few coordinators", func() {
			BeforeEach(func() {
				status.Client.Coordinators.Coordinators = status.Client.Coordinators.Coordinators[0:2]
			})

			It("should report the coordinators as not valid", func() {
				coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
				for _, coordinator := range status.Client.Coordinators.Coordinators {
					coordinatorStatus[coordinator.Address] = false
				}

				coordinatorsValid, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with too few zones", func() {
			BeforeEach(func() {
				zone := ""
				for _, process := range status.Cluster.Processes {
					if process.Address == status.Client.Coordinators.Coordinators[0].Address {
						zone = process.Locality[fdbtypes.FDBLocalityZoneIDKey]
					}
				}
				for _, process := range status.Cluster.Processes {
					if process.Address == status.Client.Coordinators.Coordinators[1].Address {
						process.Locality["zoneid"] = zone
					}
				}
			})

			It("should report the coordinators as not valid", func() {
				coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
				for _, coordinator := range status.Client.Coordinators.Coordinators {
					coordinatorStatus[coordinator.Address] = false
				}

				coordinatorsValid, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
				Expect(coordinatorsValid).To(BeFalse())
				Expect(addressesValid).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		Context("with multiple regions", func() {
			BeforeEach(func() {
				coordinatorCount := 9
				cluster.Spec.ProcessCounts.Storage = coordinatorCount

				err = k8sClient.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				generation, err := reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(generation).To(Equal(int64(2)))

				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.UsableRegions = 2

				coordinators := make([]fdbtypes.FoundationDBStatusCoordinator, 0, coordinatorCount)
				dc := 0
				for _, process := range status.Cluster.Processes {
					if process.ProcessClass == fdbtypes.ProcessClassStorage {
						coordinators = append(coordinators, fdbtypes.FoundationDBStatusCoordinator{
							Address:   process.Address,
							Reachable: true,
						})
						dc++
						process.Locality["dcid"] = fmt.Sprintf("dc%d", dc)
						dc = dc % 3
					}
				}
				status.Client.Coordinators.Coordinators = coordinators
			})

			Context("with coordinators divided across three DCs", func() {
				It("should report the coordinators as valid", func() {
					coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
					for _, coordinator := range status.Client.Coordinators.Coordinators {
						coordinatorStatus[coordinator.Address] = false
					}

					coordinatorsValid, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
					Expect(coordinatorsValid).To(BeTrue())
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})
			})

			Context("with coordinators divided across two DCs", func() {
				BeforeEach(func() {
					for _, process := range status.Cluster.Processes {
						if process.Locality["dcid"] == "dc3" {
							process.Locality["dcid"] = "dc1"
						}
					}
				})
				It("should report the coordinators as not valid", func() {
					coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
					for _, coordinator := range status.Client.Coordinators.Coordinators {
						coordinatorStatus[coordinator.Address] = false
					}

					coordinatorsValid, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
					Expect(coordinatorsValid).To(BeFalse())
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})
			})
		})

		When("changing the TLS setting", func() {
			BeforeEach(func() {
				cluster.Status.RequiredAddresses = fdbtypes.RequiredAddressSet{
					TLS:    true,
					NonTLS: true,
				}
			})

			When("TLS is disabled", func() {
				BeforeEach(func() {
					cluster.Spec.MainContainer = fdbtypes.ContainerOverrides{
						EnableTLS: false,
					}
					cluster.Spec.SidecarContainer = fdbtypes.ContainerOverrides{
						EnableTLS: false,
					}

					adminClient, err = NewMockAdminClient(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					status, err = adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should report the coordinators addresses as valid", func() {
					coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
					for _, coordinator := range status.Client.Coordinators.Coordinators {
						coordinatorStatus[coordinator.Address] = false
					}

					_, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})

				When("converting back to TLS", func() {
					BeforeEach(func() {
						cluster.Spec.MainContainer = fdbtypes.ContainerOverrides{
							EnableTLS: true,
						}
						cluster.Spec.SidecarContainer = fdbtypes.ContainerOverrides{
							EnableTLS: true,
						}

						adminClient, err = NewMockAdminClient(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						status, err = adminClient.GetStatus()
						Expect(err).NotTo(HaveOccurred())
					})

					It("should report the coordinators addresses as valid", func() {
						coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
						for _, coordinator := range status.Client.Coordinators.Coordinators {
							coordinatorStatus[coordinator.Address] = false
						}

						_, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
						Expect(addressesValid).To(BeTrue())
						Expect(err).To(BeNil())
					})
				})
			})

			When("TLS is enabled", func() {
				BeforeEach(func() {
					cluster.Spec.MainContainer = fdbtypes.ContainerOverrides{
						EnableTLS: true,
					}
					cluster.Spec.SidecarContainer = fdbtypes.ContainerOverrides{
						EnableTLS: true,
					}

					adminClient, err = NewMockAdminClient(cluster, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					status, err = adminClient.GetStatus()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should report the coordinators addresses as valid", func() {
					coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
					for _, coordinator := range status.Client.Coordinators.Coordinators {
						coordinatorStatus[coordinator.Address] = false
					}

					_, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
					Expect(addressesValid).To(BeTrue())
					Expect(err).To(BeNil())
				})

				When("converting back to non-TLS", func() {
					BeforeEach(func() {
						cluster.Spec.MainContainer = fdbtypes.ContainerOverrides{
							EnableTLS: false,
						}

						cluster.Spec.SidecarContainer = fdbtypes.ContainerOverrides{
							EnableTLS: false,
						}

						adminClient, err = NewMockAdminClient(cluster, k8sClient)
						Expect(err).NotTo(HaveOccurred())

						status, err = adminClient.GetStatus()
						Expect(err).NotTo(HaveOccurred())
					})

					It("should report the coordinators addresses as valid", func() {
						coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
						for _, coordinator := range status.Client.Coordinators.Coordinators {
							coordinatorStatus[coordinator.Address] = false
						}

						_, addressesValid, err := checkCoordinatorValidity(cluster, status, coordinatorStatus)
						Expect(addressesValid).To(BeTrue())
						Expect(err).To(BeNil())
					})
				})
			})
		})

		When("Generating the locality info based on a process", func() {
			BeforeEach(func() {
				cluster.Status.RequiredAddresses = fdbtypes.RequiredAddressSet{
					TLS:    true,
					NonTLS: true,
				}

				cluster.Spec.MainContainer = fdbtypes.ContainerOverrides{
					EnableTLS: true,
				}
				cluster.Spec.SidecarContainer = fdbtypes.ContainerOverrides{
					EnableTLS: true,
				}

				adminClient, err = NewMockAdminClient(cluster, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				status, err = adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
			})

			When("TLS is disabled", func() {
				It("the locality address should not contain tls", func() {
					var proc fdbtypes.FoundationDBStatusProcessInfo
					for _, p := range status.Cluster.Processes {
						proc = p
						// Select the first process Info
						break
					}

					info, err := localityInfoForProcess(proc, false)
					Expect(err).NotTo(HaveOccurred())
					Expect(info.Address).NotTo(HaveSuffix("tls"))
				})
			})

			When("TLS is enabled", func() {
				It("the locality address should contain tls", func() {
					var proc fdbtypes.FoundationDBStatusProcessInfo
					for _, p := range status.Cluster.Processes {
						proc = p
						// Select the first process Info
						break
					}

					info, err := localityInfoForProcess(proc, true)
					Expect(err).NotTo(HaveOccurred())
					Expect(info.Address).To(HaveSuffix("tls"))
				})
			})
		})
	})

	Context("Setting the partial connection string", func() {
		var partialConnectionString fdbtypes.ConnectionString

		JustBeforeEach(func() {
			cluster = createDefaultCluster()
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

				conn, err := fdbtypes.ParseConnectionString(cluster.Status.ConnectionString)
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

				conn, err := fdbtypes.ParseConnectionString(cluster.Status.ConnectionString)
				Expect(err).NotTo(HaveOccurred())

				Expect(conn.DatabaseName).To(Equal("foo"))
				Expect(conn.GenerationID).To(Equal("bar"))
			})
		})
	})

	Describe("GetPublicIPs", func() {
		var instance FdbInstance

		BeforeEach(func() {
			err := internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
			instance = FdbInstance{}
		})

		Context("with a default pod", func() {
			BeforeEach(func() {
				pod, err := GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
				instance.Pod = pod
				instance.Metadata = &pod.ObjectMeta
			})

			It("should be the public IP from the pod", func() {
				result := instance.GetPublicIPs()
				Expect(result).To(Equal([]string{"1.1.1.1"}))
			})
		})

		Context("with a v6 pod IP family configured", func() {
			BeforeEach(func() {
				family := 6
				cluster.Spec.Routing.PodIPFamily = &family
				pod, err := GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
				instance.Pod = pod
				instance.Metadata = &pod.ObjectMeta
			})

			It("should select the address based on the spec", func() {
				result := instance.GetPublicIPs()
				Expect(result).To(Equal([]string{"2001:db8::ff00:42:8329"}))
			})

			Context("with no matching IPs in the Pod IP list", func() {
				BeforeEach(func() {
					instance.Pod.Status.PodIPs = []corev1.PodIP{
						{IP: "1.1.1.2"},
					}
				})

				It("should be empty", func() {
					result := instance.GetPublicIPs()
					Expect(result).To(BeEmpty())
				})
			})
		})

		Context("with a v4 pod IP family configured", func() {
			BeforeEach(func() {
				family := 4
				cluster.Spec.Routing.PodIPFamily = &family
				pod, err := GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				pod.Status.PodIP = "1.1.1.1"
				pod.Status.PodIPs = []corev1.PodIP{
					{IP: "1.1.1.2"},
					{IP: "2001:db8::ff00:42:8329"},
				}
				instance.Pod = pod
				instance.Metadata = &pod.ObjectMeta
			})

			It("should select the address based on the spec", func() {
				result := instance.GetPublicIPs()
				Expect(result).To(Equal([]string{"1.1.1.2"}))
			})
		})

		Context("with no pod", func() {
			BeforeEach(func() {
				pod, err := GetPod(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				instance.Metadata = &pod.ObjectMeta
			})

			It("should be empty", func() {
				result := instance.GetPublicIPs()
				Expect(result).To(BeEmpty())
			})
		})
	})
})

func getProcessClassMap(pods []corev1.Pod) map[fdbtypes.ProcessClass]int {
	counts := make(map[fdbtypes.ProcessClass]int)
	for _, pod := range pods {
		ProcessClass := internal.ProcessClassFromLabels(pod.Labels)
		counts[ProcessClass]++
	}
	return counts
}

// getConfigMapHash gets the hash of the data for a cluster's dynamic config.
func getConfigMapHash(cluster *fdbtypes.FoundationDBCluster, pClass fdbtypes.ProcessClass, pod *corev1.Pod) (string, error) {
	configMap, err := GetConfigMap(cluster)
	if err != nil {
		return "", err
	}

	serversPerPod, err := getStorageServersPerPodForPod(pod)
	if err != nil {
		return "", err
	}

	return getDynamicConfHash(configMap, pClass, serversPerPod)
}
