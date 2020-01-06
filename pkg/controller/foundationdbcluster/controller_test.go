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

package foundationdbcluster

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	appsv1beta1 "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

const timeout = time.Second * 5

var clusterID = 0
var firstStorageIndex = 11

func createDefaultCluster() *appsv1beta1.FoundationDBCluster {
	clusterID += 1
	return &appsv1beta1.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("operator-test-%d", clusterID),
			Namespace: "default",
			Labels: map[string]string{
				"fdb-label": "value1",
			},
		},
		Spec: appsv1beta1.FoundationDBClusterSpec{
			Version:          "6.1.8",
			ConnectionString: "operator-test:asdfasf@127.0.0.1:4501",
			ProcessCounts: appsv1beta1.ProcessCounts{
				Storage: 4,
			},
			FaultDomain: appsv1beta1.FoundationDBClusterFaultDomain{
				Key: "foundationdb.org/none",
			},
			VolumeSize: "16G",
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"memory": resource.MustParse("1Gi"),
					"cpu":    resource.MustParse("1"),
				},
				Requests: corev1.ResourceList{
					"memory": resource.MustParse("1Gi"),
					"cpu":    resource.MustParse("1"),
				},
			},
			PodLabels: map[string]string{
				"fdb-label": "value2",
			},
		},
	}
}

func reloadCluster(client client.Client, cluster *appsv1beta1.FoundationDBCluster) (int64, error) {
	generations, err := reloadClusterGenerations(client, cluster)
	return generations.Reconciled, err
}

func reloadClusterGenerations(client client.Client, cluster *appsv1beta1.FoundationDBCluster) (appsv1beta1.GenerationStatus, error) {
	err := c.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	if err != nil {
		return appsv1beta1.GenerationStatus{}, err
	}
	return cluster.Status.Generations, err
}

func getListOptions(cluster *appsv1beta1.FoundationDBCluster) *client.ListOptions {
	return (&client.ListOptions{}).InNamespace("default").MatchingLabels(map[string]string{
		"fdb-cluster-name": cluster.Name,
	})
}

func cleanupCluster(cluster *appsv1beta1.FoundationDBCluster, g *gomega.GomegaWithT) {
	err := c.Delete(context.TODO(), cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	pods := &corev1.PodList{}
	err = c.List(context.TODO(), getListOptions(cluster), pods)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, item := range pods.Items {
		err = c.Delete(context.TODO(), &item)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	configMaps := &corev1.ConfigMapList{}
	err = c.List(context.TODO(), getListOptions(cluster), configMaps)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, item := range configMaps.Items {
		err = c.Delete(context.TODO(), &item)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = c.List(context.TODO(), getListOptions(cluster), configMaps)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, item := range pvcs.Items {
		err = c.Delete(context.TODO(), &item)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}
}

func runReconciliation(t *testing.T, testFunction func(*gomega.GomegaWithT, *appsv1beta1.FoundationDBCluster, client.Client, chan reconcile.Request)) {
	cluster := createDefaultCluster()
	cluster.Spec.ConnectionString = ""
	runReconciliationOnCluster(t, cluster, testFunction)
}

func runReconciliationOnCluster(t *testing.T, cluster *appsv1beta1.FoundationDBCluster, testFunction func(*gomega.GomegaWithT, *appsv1beta1.FoundationDBCluster, client.Client, chan reconcile.Request)) {
	g := gomega.NewGomegaWithT(t)
	ClearMockAdminClients()

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(t, newTestReconciler(mgr))
	g.Expect(AddReconciler(mgr, recFn)).NotTo(gomega.HaveOccurred())

	defer cleanupCluster(cluster, g)

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the FoundationDBCluster object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
	g.Eventually(func() (appsv1beta1.GenerationStatus, error) { return reloadClusterGenerations(c, cluster) }, timeout).Should(gomega.Equal(appsv1beta1.GenerationStatus{Reconciled: 4}))

	err = c.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testFunction(g, cluster, c, requests)
}

func TestReconcileWithNewCluster(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(15))

		sortPodsByID(pods)

		g.Expect(pods.Items[0].Name).To(gomega.Equal("operator-test-1-log-1"))
		g.Expect(pods.Items[0].Labels["fdb-instance-id"]).To(gomega.Equal("log-1"))
		g.Expect(pods.Items[3].Name).To(gomega.Equal("operator-test-1-log-4"))
		g.Expect(pods.Items[3].Labels["fdb-instance-id"]).To(gomega.Equal("log-4"))
		g.Expect(pods.Items[4].Name).To(gomega.Equal("operator-test-1-stateless-1"))
		g.Expect(pods.Items[4].Labels["fdb-instance-id"]).To(gomega.Equal("stateless-1"))
		g.Expect(pods.Items[10].Name).To(gomega.Equal("operator-test-1-stateless-7"))
		g.Expect(pods.Items[10].Labels["fdb-instance-id"]).To(gomega.Equal("stateless-7"))
		g.Expect(pods.Items[11].Name).To(gomega.Equal("operator-test-1-storage-1"))
		g.Expect(pods.Items[11].Labels["fdb-instance-id"]).To(gomega.Equal("storage-1"))
		g.Expect(pods.Items[14].Name).To(gomega.Equal("operator-test-1-storage-4"))
		g.Expect(pods.Items[14].Labels["fdb-instance-id"]).To(gomega.Equal("storage-4"))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: cluster.Name}, cluster)
		}, timeout).Should(gomega.Succeed())

		g.Expect(cluster.Spec.RedundancyMode).To(gomega.Equal("double"))
		g.Expect(cluster.Spec.StorageEngine).To(gomega.Equal("ssd"))
		g.Expect(cluster.Spec.ConnectionString).NotTo(gomega.Equal(""))

		configMap := &corev1.ConfigMap{}
		configMapName := types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-config", cluster.Name)}
		g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
		expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, c)
		g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient).NotTo(gomega.BeNil())
		g.Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(gomega.Equal("double"))
		g.Expect(adminClient.DatabaseConfiguration.StorageEngine).To(gomega.Equal("ssd-2"))
		g.Expect(adminClient.DatabaseConfiguration.RoleCounts).To(gomega.Equal(appsv1beta1.RoleCounts{
			Logs:       3,
			Proxies:    3,
			Resolvers:  1,
			RemoteLogs: -1,
			LogRouters: -1,
		}))

		g.Expect(cluster.Status.Generations.Reconciled).To(gomega.Equal(int64(4)))
		g.Expect(cluster.Status.ProcessCounts).To(gomega.Equal(appsv1beta1.ProcessCounts{
			Storage:   4,
			Log:       4,
			Stateless: 7,
		}))
		g.Expect(cluster.Status.ProcessCounts).To(gomega.Equal(cluster.GetProcessCountsWithDefaults()))
		g.Expect(cluster.Status.IncorrectProcesses).To(gomega.BeNil())
		g.Expect(cluster.Status.MissingProcesses).To(gomega.BeNil())
		g.Expect(cluster.Status.DatabaseConfiguration).To(gomega.Equal(*adminClient.DatabaseConfiguration))
		g.Expect(cluster.Status.Health).To(gomega.Equal(appsv1beta1.ClusterHealth{
			Available:            true,
			Healthy:              true,
			FullReplication:      true,
			DataMovementPriority: 0,
		}))

	})
}

func TestReconcileWithDecreasedProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalPods := &corev1.PodList{}

		originalVersion := cluster.ObjectMeta.Generation

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), originalPods)
			return len(originalPods.Items), err
		}, timeout).Should(gomega.Equal(15))

		sortPodsByID(originalPods)

		cluster.Spec.ProcessCounts.Storage = 3
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 3))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(14))
		sortPodsByID(pods)

		g.Expect(pods.Items[0].Name).To(gomega.Equal(originalPods.Items[0].Name))
		g.Expect(pods.Items[1].Name).To(gomega.Equal(originalPods.Items[1].Name))
		g.Expect(pods.Items[2].Name).To(gomega.Equal(originalPods.Items[2].Name))

		g.Expect(cluster.Spec.PendingRemovals).To(gomega.BeNil())

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient).NotTo(gomega.BeNil())
		g.Expect(adminClient.ExcludedAddresses).To(gomega.Equal([]string{}))

		removedItem := originalPods.Items[14]
		g.Expect(adminClient.ReincludedAddresses).To(gomega.Equal([]string{
			cluster.GetFullAddress(mockPodIP(&removedItem)),
		}))
	})
}

func TestReconcileWithIncreasedProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		cluster.Spec.ProcessCounts.Storage = 5
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 1))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(16))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: cluster.Name}, cluster)
		}, timeout).Should(gomega.Succeed())

		configMap := &corev1.ConfigMap{}
		configMapName := types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-config", cluster.Name)}
		g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
		expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, c)
		g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))
	})
}

func TestReconcileWithIncreasedStatelessProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		cluster.Spec.ProcessCounts.Stateless = 10
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 1))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(18))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: cluster.Name}, cluster)
		}, timeout).Should(gomega.Succeed())

		configMap := &corev1.ConfigMap{}
		configMapName := types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-config", cluster.Name)}
		g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
		expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, c)
		g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))
	})
}

func TestReconcileWithExplicitClusterControllerProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		cluster.Spec.ProcessCounts.ClusterController = 1
		cluster.Spec.ProcessCounts.Stateless = 7
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 1))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(16))
	})
}

func TestReconcileWithNoStatelessProcesses(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalPods := &corev1.PodList{}

		originalVersion := cluster.ObjectMeta.Generation

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), originalPods)
			return len(originalPods.Items), err
		}, timeout).Should(gomega.Equal(15))

		cluster.Spec.ProcessCounts.Stateless = -1
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 3))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(8))

		g.Expect(cluster.Spec.PendingRemovals).To(gomega.BeNil())
		g.Expect(cluster.Status.Generations.Reconciled).To(gomega.Equal(int64(7)))
	})
}

func TestReconcileWithCoordinatorReplacement(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalPods := &corev1.PodList{}

		originalVersion := cluster.ObjectMeta.Generation

		originalConnectionString := cluster.Spec.ConnectionString

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), originalPods)
			return len(originalPods.Items), err
		}, timeout).Should(gomega.Equal(15))

		cluster.Spec.PendingRemovals = map[string]string{
			originalPods.Items[firstStorageIndex].Name: "",
		}
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 4))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(15))

		g.Expect(pods.Items[firstStorageIndex].Name).To(gomega.Equal(originalPods.Items[firstStorageIndex+1].Name))
		g.Expect(pods.Items[firstStorageIndex+1].Name).To(gomega.Equal(originalPods.Items[firstStorageIndex+2].Name))

		g.Expect(cluster.Spec.PendingRemovals).To(gomega.BeNil())

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient).NotTo(gomega.BeNil())
		g.Expect(adminClient.ExcludedAddresses).To(gomega.Equal([]string{}))

		g.Expect(adminClient.ReincludedAddresses).To(gomega.Equal([]string{
			cluster.GetFullAddress(mockPodIP(&originalPods.Items[firstStorageIndex])),
		}))
		g.Expect(cluster.Spec.ConnectionString).NotTo(gomega.Equal(originalConnectionString))
	})
}

func TestReconcileWithKnobChange(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalPods := &corev1.PodList{}

		originalVersion := cluster.ObjectMeta.Generation

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), originalPods)
			return len(originalPods.Items), err
		}, timeout).Should(gomega.Equal(15))

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		adminClient.FreezeStatus()
		cluster.Spec.CustomParameters = []string{"knob_disable_posix_kernel_aio=1"}

		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 1))

		addresses := make([]string, 0, len(originalPods.Items))
		for _, pod := range originalPods.Items {
			addresses = append(addresses, cluster.GetFullAddress(mockPodIP(&pod)))
		}

		sort.Slice(adminClient.KilledAddresses, func(i, j int) bool {
			return strings.Compare(adminClient.KilledAddresses[i], adminClient.KilledAddresses[j]) < 0
		})
		sort.Slice(addresses, func(i, j int) bool {
			return strings.Compare(addresses[i], addresses[j]) < 0
		})
		g.Expect(adminClient.KilledAddresses).To(gomega.Equal(addresses))
	})
}

func TestReconcileWithKnobChangeWithBouncesDisabled(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalPods := &corev1.PodList{}

		originalVersion := cluster.ObjectMeta.Generation

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), originalPods)
			return len(originalPods.Items), err
		}, timeout).Should(gomega.Equal(15))

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		adminClient.FreezeStatus()
		var flag = false
		cluster.Spec.AutomationOptions.KillProcesses = &flag
		cluster.Spec.CustomParameters = []string{"knob_disable_posix_kernel_aio=1"}

		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (appsv1beta1.GenerationStatus, error) { return reloadClusterGenerations(c, cluster) }, timeout).Should(gomega.Equal(appsv1beta1.GenerationStatus{
			Reconciled:  originalVersion,
			NeedsBounce: originalVersion + 1,
		}))

		g.Expect(adminClient.KilledAddresses).To(gomega.BeNil())
	})
}

func TestReconcileWithConfigurationChange(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"
		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 2))

		g.Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(gomega.Equal("triple"))
	})
}

func TestReconcileWithConfigurationChangeWithChangesDisabled(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var flag = false
		cluster.Spec.AutomationOptions.ConfigureDatabase = &flag
		cluster.Spec.DatabaseConfiguration.RedundancyMode = "triple"

		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (appsv1beta1.GenerationStatus, error) { return reloadClusterGenerations(c, cluster) }, timeout).Should(gomega.Equal(appsv1beta1.GenerationStatus{
			Reconciled:               originalVersion,
			NeedsConfigurationChange: originalVersion + 1,
		}))

		g.Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(gomega.Equal("double"))
	})
}

func TestReconcileWithLabelChange(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		originalVersion := cluster.ObjectMeta.Generation

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(15))

		cluster.Spec.PodLabels = map[string]string{
			"fdb-label": "value3",
		}
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 1))

		err = c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		for _, item := range pods.Items {
			g.Expect(item.ObjectMeta.Labels["fdb-label"]).To(gomega.Equal("value3"))
		}

		pvcs := &corev1.PersistentVolumeClaimList{}
		err = c.List(context.TODO(), getListOptions(cluster), pvcs)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		for _, item := range pvcs.Items {
			g.Expect(item.ObjectMeta.Labels["fdb-label"]).To(gomega.Equal("value3"))
		}

		configMaps := &corev1.ConfigMapList{}
		err = c.List(context.TODO(), getListOptions(cluster), configMaps)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		for _, item := range configMaps.Items {
			g.Expect(item.ObjectMeta.Labels["fdb-label"]).To(gomega.Equal("value3"))
		}
	})
}

func TestReconcileWithEnvironmentVariableChange(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		pods := &corev1.PodList{}
		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		cluster.Spec.MainContainer.Env = append(cluster.Spec.MainContainer.Env, corev1.EnvVar{
			Name:  "TEST_CHANGE",
			Value: "1",
		})
		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, 60).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, 60).Should(gomega.Not(gomega.Equal(originalVersion)))

		err = c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		for _, pod := range pods.Items {
			g.Expect(len(pod.Spec.Containers[0].Env)).To(gomega.Equal(3))
			g.Expect(pod.Spec.Containers[0].Env[0].Name).To(gomega.Equal("TEST_CHANGE"))
			g.Expect(pod.Spec.Containers[0].Env[0].Value).To(gomega.Equal("1"))
		}
	})
}

func TestReconcileWithEnvironmentVariableChangeWithDeletionDisabled(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		pods := &corev1.PodList{}
		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		cluster.Spec.MainContainer.Env = append(cluster.Spec.MainContainer.Env, corev1.EnvVar{
			Name:  "TEST_CHANGE",
			Value: "1",
		})
		var flag = false
		cluster.Spec.AutomationOptions.DeletePods = &flag

		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (appsv1beta1.GenerationStatus, error) { return reloadClusterGenerations(c, cluster) }, timeout).Should(gomega.Equal(appsv1beta1.GenerationStatus{
			Reconciled:       originalVersion,
			NeedsPodDeletion: originalVersion + 1,
		}))

		err = c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		for _, pod := range pods.Items {
			g.Expect(len(pod.Spec.Containers[0].Env)).To(gomega.Equal(2))
		}
	})
}

func TestGetConfigMap(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	configMap, err := GetConfigMap(context.TODO(), cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configMap.Namespace).To(gomega.Equal("default"))
	g.Expect(configMap.Name).To(gomega.Equal(fmt.Sprintf("%s-config", cluster.Name)))
	g.Expect(configMap.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name": cluster.Name,
		"fdb-label":        "value2",
	}))

	expectedConf, err := GetMonitorConf(cluster, "storage", nil, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(configMap.Data)).To(gomega.Equal(6))
	g.Expect(configMap.Data["cluster-file"]).To(gomega.Equal("operator-test:asdfasf@127.0.0.1:4501"))
	g.Expect(configMap.Data["fdbmonitor-conf-storage"]).To(gomega.Equal(expectedConf))
	g.Expect(configMap.Data["ca-file"]).To(gomega.Equal(""))

	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(sidecarConf)).To(gomega.Equal(5))
	g.Expect(sidecarConf["COPY_FILES"]).To(gomega.Equal([]interface{}{"fdb.cluster", "ca.pem"}))
	g.Expect(sidecarConf["COPY_BINARIES"]).To(gomega.Equal([]interface{}{"fdbserver", "fdbcli"}))
	g.Expect(sidecarConf["COPY_LIBRARIES"]).To(gomega.Equal([]interface{}{}))
	g.Expect(sidecarConf["INPUT_MONITOR_CONF"]).To(gomega.Equal("fdbmonitor.conf"))
	g.Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(gomega.Equal([]interface{}{"FDB_INSTANCE_ID"}))
}

func TestGetConfigMapWithCustomCA(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.TrustedCAs = []string{
		"-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC",
		"---CERT2----",
	}
	configMap, err := GetConfigMap(context.TODO(), cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configMap.Data["ca-file"]).To(gomega.Equal("-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC\n---CERT2----"))
}

func TestGetConfigMapWithEmptyConnectionString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.ConnectionString = ""
	configMap, err := GetConfigMap(context.TODO(), cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configMap.Data["cluster-file"]).To(gomega.Equal(""))
	g.Expect(configMap.Data["fdbmonitor-conf-storage"]).To(gomega.Equal(""))
}

func TestGetConfigMapWithCustomSidecarSubstitutions(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}

	configMap, err := GetConfigMap(context.TODO(), cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(gomega.Equal([]interface{}{"FAULT_DOMAIN", "ZONE", "FDB_INSTANCE_ID"}))
}

func TestGetConfigMapWithImplicitInstanceIdSubstitution(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.Version = Versions.WithSidecarInstanceIdSubstitution.String()
	configMap, err := GetConfigMap(context.TODO(), cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(gomega.BeNil())
}

func TestGetConfigMapWithExplicitInstanceIdSubstitution(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.Version = Versions.WithoutSidecarInstanceIdSubstitution.String()
	configMap, err := GetConfigMap(context.TODO(), cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(sidecarConf["ADDITIONAL_SUBSTITUTIONS"]).To(gomega.Equal([]interface{}{"FDB_INSTANCE_ID"}))

}

func TestGetMonitorConfForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	conf, err := GetMonitorConf(cluster, "storage", nil, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(conf).To(gomega.Equal(strings.Join([]string{
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
		"[fdbserver.1]",
		"command = /var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetMonitorConfWithTls(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.MainContainer.EnableTLS = true
	conf, err := GetMonitorConf(cluster, "storage", nil, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(conf).To(gomega.Equal(strings.Join([]string{
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
		"[fdbserver.1]",
		"command = /var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetMonitorConfWithCustomParameters(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.CustomParameters = []string{
		"knob_disable_posix_kernel_aio = 1",
	}
	conf, err := GetMonitorConf(cluster, "storage", nil, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(conf).To(gomega.Equal(strings.Join([]string{
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
		"[fdbserver.1]",
		"command = /var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetMonitorConfWithAlternativeFaultDomainVariable(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
		Key:       "rack",
		ValueFrom: "$RACK",
	}
	conf, err := GetMonitorConf(cluster, "storage", nil, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(conf).To(gomega.Equal(strings.Join([]string{
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
		"[fdbserver.1]",
		"command = /var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetStartCommandForStoragePod(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		instance := newFdbInstance(pods.Items[firstStorageIndex])
		podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pods.Items[firstStorageIndex]}
		command, err := GetStartCommand(cluster, instance, podClient)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[firstStorageIndex].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetStartCommandForStoragePodWithHostReplication(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		pod := pods.Items[firstStorageIndex]
		pod.Spec.NodeName = "machine1"
		pod.Status.PodIP = "127.0.0.1"
		cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{}

		podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
		command, err := GetStartCommand(cluster, newFdbInstance(pod), podClient)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetStartCommandForStoragePodWithCrossKubernetesReplication(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		pod := pods.Items[firstStorageIndex]
		pod.Spec.NodeName = "machine1"
		pod.Status.PodIP = "127.0.0.1"
		cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
			Key:   "foundationdb.org/kubernetes-cluster",
			Value: "kc2",
		}

		podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pod}
		command, err := GetStartCommand(cluster, newFdbInstance(pod), podClient)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
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
}

func TestGetStartCommandWithPeerVerificationRules(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		sortPodsByID(pods)

		cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"

		podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pods.Items[firstStorageIndex]}
		command, err := GetStartCommand(cluster, newFdbInstance(pods.Items[firstStorageIndex]), podClient)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[firstStorageIndex].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
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
			"--tls_verify_peers=S.CN=foundationdb.org",
		}, " ")))
	})
}

func TestGetStartCommandWithCustomLogGroup(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		sortPodsByID(pods)

		cluster.Spec.LogGroup = "test-fdb-cluster"
		podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pods.Items[firstStorageIndex]}
		command, err := GetStartCommand(cluster, newFdbInstance(pods.Items[firstStorageIndex]), podClient)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[firstStorageIndex].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
			fmt.Sprintf("--locality_instance_id=%s", id),
			fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
			fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
			"--logdir=/var/log/fdb-trace-logs",
			"--loggroup=test-fdb-cluster",
			"--public_address=:4501",
			"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
		}, " ")))
	})
}

func TestGetStartCommandWithDataCenter(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		cluster.Spec.DataCenter = "dc01"

		instance := newFdbInstance(pods.Items[firstStorageIndex])
		podClient := &mockFdbPodClient{Cluster: cluster, Pod: &pods.Items[firstStorageIndex]}
		command, err := GetStartCommand(cluster, instance, podClient)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[firstStorageIndex].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
			"--locality_dcid=dc01",
			fmt.Sprintf("--locality_instance_id=%s", id),
			fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
			fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
			"--logdir=/var/log/fdb-trace-logs",
			"--loggroup=" + cluster.Name,
			"--public_address=:4501",
			"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
		}, " ")))
	})
}

func TestParseInstanceID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	prefix, id, err := ParseInstanceID("storage-12")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(prefix).To(gomega.Equal("storage"))
	g.Expect(id).To(gomega.Equal(12))

	prefix, id, err = ParseInstanceID("cluster_controller-3")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(prefix).To(gomega.Equal("cluster_controller"))
	g.Expect(id).To(gomega.Equal(3))

	prefix, id, err = ParseInstanceID("dc1-storage-12")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(prefix).To(gomega.Equal("dc1-storage"))
	g.Expect(id).To(gomega.Equal(12))

	prefix, id, err = ParseInstanceID("6")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(prefix).To(gomega.Equal(""))
	g.Expect(id).To(gomega.Equal(6))

	prefix, id, err = ParseInstanceID("storage")
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.Equal("Could not parse instance ID storage"))

	prefix, id, err = ParseInstanceID("storage-bad")
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.Equal("Could not parse instance ID storage-bad"))
}
