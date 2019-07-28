/*
Copyright 2019 FoundationDB project authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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

	"github.com/onsi/gomega"
	appsv1beta1 "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
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

func createDefaultCluster() *appsv1beta1.FoundationDBCluster {
	clusterID += 1
	return &appsv1beta1.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("operator-test-%d", clusterID),
			Namespace: "default",
			Labels: map[string]string{
				"fdb-label": "value",
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
	g.Eventually(func() (appsv1beta1.GenerationStatus, error) { return reloadClusterGenerations(c, cluster) }, timeout).Should(gomega.Equal(appsv1beta1.GenerationStatus{Reconciled: 5}))

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

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: cluster.Name}, cluster)
		}, timeout).Should(gomega.Succeed())

		g.Expect(cluster.Spec.RedundancyMode).To(gomega.Equal("double"))
		g.Expect(cluster.Spec.StorageEngine).To(gomega.Equal("ssd"))
		g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(16))
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

		g.Expect(cluster.Status.Generations.Reconciled).To(gomega.Equal(int64(5)))
		g.Expect(cluster.Status.ProcessCounts).To(gomega.Equal(appsv1beta1.ProcessCounts{
			Storage:     4,
			Transaction: 4,
			Stateless:   7,
		}))
		g.Expect(cluster.Status.ProcessCounts).To(gomega.Equal(cluster.GetProcessCountsWithDefaults()))
		g.Expect(cluster.Status.IncorrectProcesses).To(gomega.BeNil())
		g.Expect(cluster.Status.MissingProcesses).To(gomega.BeNil())
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

		removedItem := originalPods.Items[3]
		g.Expect(adminClient.ReincludedAddresses).To(gomega.Equal([]string{
			cluster.GetFullAddress(mockPodIP(&removedItem)),
		}))
	})
}

func TestReconcileWithIncreasedProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion := cluster.ObjectMeta.Generation

		originalID := cluster.Spec.NextInstanceID
		cluster.Spec.ProcessCounts.Storage = 5
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 2))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(16))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: cluster.Name}, cluster)
		}, timeout).Should(gomega.Succeed())

		g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(originalID + 1))

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

		originalID := cluster.Spec.NextInstanceID
		cluster.Spec.ProcessCounts.Stateless = 10
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 2))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(18))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: cluster.Name}, cluster)
		}, timeout).Should(gomega.Succeed())

		g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(originalID + 3))

		configMap := &corev1.ConfigMap{}
		configMapName := types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-config", cluster.Name)}
		g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
		expectedConfigMap, _ := GetConfigMap(context.TODO(), cluster, c)
		g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))
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
		g.Expect(cluster.Status.Generations.Reconciled).To(gomega.Equal(int64(8)))
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
			originalPods.Items[0].Name: "",
		}
		err := client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		expectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: "default"}}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 5))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), getListOptions(cluster), pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(15))

		g.Expect(pods.Items[0].Name).To(gomega.Equal(originalPods.Items[1].Name))
		g.Expect(pods.Items[1].Name).To(gomega.Equal(originalPods.Items[2].Name))

		g.Expect(cluster.Spec.PendingRemovals).To(gomega.BeNil())

		adminClient, err := newMockAdminClientUncast(cluster, client)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient).NotTo(gomega.BeNil())
		g.Expect(adminClient.ExcludedAddresses).To(gomega.Equal([]string{}))

		g.Expect(adminClient.ReincludedAddresses).To(gomega.Equal([]string{
			cluster.GetFullAddress(mockPodIP(&originalPods.Items[0])),
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
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 3))

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
			NeedsConfigurationChange: originalVersion + 2,
		}))

		g.Expect(adminClient.DatabaseConfiguration.RedundancyMode).To(gomega.Equal("double"))
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
	}))

	expectedConf, err := GetMonitorConf(cluster, "storage", nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(configMap.Data)).To(gomega.Equal(6))
	g.Expect(configMap.Data["cluster-file"]).To(gomega.Equal("operator-test:asdfasf@127.0.0.1:4501"))
	g.Expect(configMap.Data["fdbmonitor-conf-storage"]).To(gomega.Equal(expectedConf))
	g.Expect(configMap.Data["ca-file"]).To(gomega.Equal(""))

	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(sidecarConf)).To(gomega.Equal(4))
	g.Expect(sidecarConf["COPY_FILES"]).To(gomega.Equal([]interface{}{"fdb.cluster", "ca.pem"}))
	g.Expect(sidecarConf["COPY_BINARIES"]).To(gomega.Equal([]interface{}{"fdbserver", "fdbcli"}))
	g.Expect(sidecarConf["COPY_LIBRARIES"]).To(gomega.Equal([]interface{}{}))
	g.Expect(sidecarConf["INPUT_MONITOR_CONF"]).To(gomega.Equal("fdbmonitor.conf"))
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

func TestGetMonitorConfForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	conf, err := GetMonitorConf(cluster, "storage", nil)
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
		"locality_machineid = $FDB_MACHINE_ID",
		"locality_zoneid = $FDB_ZONE_ID",
	}, "\n")))
}

func TestGetMonitorConfWithTls(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.EnableTLS = true
	conf, err := GetMonitorConf(cluster, "storage", nil)
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
	conf, err := GetMonitorConf(cluster, "storage", nil)
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
		"locality_machineid = $FDB_MACHINE_ID",
		"locality_zoneid = $FDB_ZONE_ID",
		"knob_disable_posix_kernel_aio = 1",
	}, "\n")))
}

func TestGetStartCommandForStoragePod(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}

		err := c.List(context.TODO(), getListOptions(cluster), pods)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		command, err := GetStartCommand(cluster, newFdbInstance(pods.Items[0]))
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[0].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
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

		pod := pods.Items[0]
		pod.Spec.NodeName = "machine1"
		pod.Status.PodIP = "127.0.0.1"
		cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{}

		command, err := GetStartCommand(cluster, newFdbInstance(pod))
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
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

		pod := pods.Items[0]
		pod.Spec.NodeName = "machine1"
		pod.Status.PodIP = "127.0.0.1"
		cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
			Key:   "foundationdb.org/kubernetes-cluster",
			Value: "kc2",
		}

		command, err := GetStartCommand(cluster, newFdbInstance(pod))
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
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

		cluster.Spec.PeerVerificationRules = []string{"S.CN=foundationdb.org"}

		command, err := GetStartCommand(cluster, newFdbInstance(pods.Items[0]))
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[0].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
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
		command, err := GetStartCommand(cluster, newFdbInstance(pods.Items[0]))
		g.Expect(err).NotTo(gomega.HaveOccurred())

		id := pods.Items[0].Labels["fdb-instance-id"]
		g.Expect(command).To(gomega.Equal(strings.Join([]string{
			"/var/dynamic-conf/bin/6.1.8/fdbserver",
			"--class=storage",
			"--cluster_file=/var/fdb/data/fdb.cluster",
			"--datadir=/var/fdb/data",
			fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
			fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
			"--logdir=/var/log/fdb-trace-logs",
			"--loggroup=test-fdb-cluster",
			"--public_address=:4501",
			"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
		}, " ")))
	})
}

func TestGetPodForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pod, err := GetPod(context.TODO(), cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.Namespace).To(gomega.Equal("default"))
	g.Expect(pod.Name).To(gomega.Equal(fmt.Sprintf("%s-1", cluster.Name)))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":  cluster.Name,
		"fdb-process-class": "storage",
		"fdb-instance-id":   "1",
		"fdb-label":         "value",
	}))
	g.Expect(pod.Spec).To(gomega.Equal(*GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))))
}

func TestGetPodSpecForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{}
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
	}))
	g.Expect(initContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.Command).To(gomega.Equal([]string{"sh", "-c"}))
	g.Expect(mainContainer.Args).To(gomega.Equal([]string{
		"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/fdb/fdbmonitor.lockfile",
	}))

	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
	}))

	g.Expect(*mainContainer.Resources.Limits.Cpu()).To(gomega.Equal(resource.MustParse("1")))
	g.Expect(*mainContainer.Resources.Limits.Memory()).To(gomega.Equal(resource.MustParse("1Gi")))
	g.Expect(*mainContainer.Resources.Requests.Cpu()).To(gomega.Equal(resource.MustParse("1")))
	g.Expect(*mainContainer.Resources.Requests.Memory()).To(gomega.Equal(resource.MustParse("1Gi")))

	g.Expect(len(mainContainer.VolumeMounts)).To(gomega.Equal(3))

	g.Expect(mainContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
	}))
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))

	g.Expect(len(spec.Volumes)).To(gomega.Equal(4))
	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name: "data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-1-data", cluster.Name),
		}},
	}))
	g.Expect(spec.Volumes[1]).To(gomega.Equal(corev1.Volume{
		Name:         "dynamic-conf",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
		Name: "config-map",
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
			Items: []corev1.KeyToPath{
				corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
				corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
				corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
				corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"},
			},
		}},
	}))
	g.Expect(spec.Volumes[3]).To(gomega.Equal(corev1.Volume{
		Name:         "fdb-trace-logs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))

	g.Expect(spec.Affinity).To(gomega.Equal(&corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				corev1.WeightedPodAffinityTerm{
					Weight: 1,
					PodAffinityTerm: corev1.PodAffinityTerm{
						TopologyKey: "kubernetes.io/hostname",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"fdb-cluster-name":  cluster.Name,
								"fdb-process-class": "storage",
							},
						},
					},
				},
			},
		},
	}))
}

func TestGetPodSpecForStorageInstanceWithNoVolume(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.VolumeSize = "0"
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))

	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name:         "data",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
}

func TestGetPodSpecWithFaultDomainDisabled(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
		Key: "foundationdb.org/none",
	}
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
	}))

	g.Expect(spec.Affinity).To(gomega.BeNil())
}

func TestGetPodSpecWithCrossKubernetesReplication(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
		Key:   "foundationdb.org/kubernetes-cluster",
		Value: "kc2",
	}
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", Value: "kc2"},
	}))

	g.Expect(spec.Affinity).To(gomega.BeNil())
}

func TestGetPodSpecWithCustomContainers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.InitContainers = []corev1.Container{corev1.Container{
		Name:    "test-container",
		Image:   "foundationdb/" + cluster.Name,
		Command: []string{"echo", "test1"},
	}}
	cluster.Spec.Containers = []corev1.Container{corev1.Container{
		Name:    "test-container",
		Image:   "foundationdb/" + cluster.Name,
		Command: []string{"echo", "test2"},
	}}
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(2))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))

	testInitContainer := spec.InitContainers[1]
	g.Expect(testInitContainer.Name).To(gomega.Equal("test-container"))
	g.Expect(testInitContainer.Image).To(gomega.Equal("foundationdb/" + cluster.Name))
	g.Expect(testInitContainer.Command).To(gomega.Equal([]string{"echo", "test1"}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(3))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))

	testContainer := spec.Containers[2]
	g.Expect(testContainer.Name).To(gomega.Equal("test-container"))
	g.Expect(testContainer.Image).To(gomega.Equal("foundationdb/" + cluster.Name))
	g.Expect(testContainer.Command).To(gomega.Equal([]string{"echo", "test2"}))
}

func TestGetPodSpecWithCustomEnvironment(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.Env = []corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
	}
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
	}))
}

func TestGetPodSpecWithCustomVolumes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.Volumes = []corev1.Volume{corev1.Volume{
		Name: "test-secrets",
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: "test-secrets",
		}},
	}}
	cluster.Spec.VolumeMounts = []corev1.VolumeMount{corev1.VolumeMount{
		Name:      "test-secrets",
		MountPath: "/var/secrets",
	}}
	spec := GetPodSpec(cluster, "storage", fmt.Sprintf("%s-1", cluster.Name))

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		corev1.VolumeMount{Name: "test-secrets", MountPath: "/var/secrets"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))
	g.Expect(len(spec.Volumes)).To(gomega.Equal(5))
	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name: "data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-1-data", cluster.Name),
		}},
	}))
	g.Expect(spec.Volumes[1]).To(gomega.Equal(corev1.Volume{
		Name:         "dynamic-conf",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
		Name: "config-map",
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
			Items: []corev1.KeyToPath{
				corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
				corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
				corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
				corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"},
			},
		}},
	}))
	g.Expect(spec.Volumes[3]).To(gomega.Equal(corev1.Volume{
		Name:         "fdb-trace-logs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[4]).To(gomega.Equal(corev1.Volume{
		Name: "test-secrets",
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: "test-secrets",
		}},
	}))
}

func TestGetPvcForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pvc.Namespace).To(gomega.Equal("default"))
	g.Expect(pvc.Name).To(gomega.Equal(fmt.Sprintf("%s-1-data", cluster.Name)))
	g.Expect(pvc.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":  cluster.Name,
		"fdb-process-class": "storage",
		"fdb-instance-id":   "1",
		"fdb-label":         "value",
	}))
	g.Expect(pvc.Spec).To(gomega.Equal(corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				"storage": resource.MustParse("16G"),
			},
		},
	}))
}

func TestGetPvcWithNoVolume(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.VolumeSize = "0"
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pvc).To(gomega.BeNil())
}

func TestGetPvcWithStorageClass(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	class := "local"
	cluster.Spec.StorageClass = &class
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pvc.Spec.StorageClassName).To(gomega.Equal(&class))
}

func TestGetPvcForStatelessInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pvc, err := GetPvc(cluster, "stateless", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pvc).To(gomega.BeNil())
}
