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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	appsv1beta1 "github.com/brownleej/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "operator-test", Namespace: "default"}}
var depKey = types.NamespacedName{Name: "operator-test", Namespace: "default"}
var listOptions = (&client.ListOptions{}).InNamespace("default").MatchingLabels(map[string]string{
	"fdb-cluster-name": "operator-test",
})

const timeout = time.Second * 5

func createDefaultCluster() *appsv1beta1.FoundationDBCluster {
	return &appsv1beta1.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-test", Namespace: "default"},
		Spec: appsv1beta1.FoundationDBClusterSpec{
			Version:          "6.0.18",
			ConnectionString: "operator-test:asdfasf@127.0.0.1:4500",
			ProcessCounts: map[string]int{
				"storage": 4,
			},
		},
	}
}

func reloadCluster(client client.Client, cluster *appsv1beta1.FoundationDBCluster) (int64, error) {
	err := c.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	if err != nil {
		return 0, err
	}
	version, err := strconv.ParseInt(cluster.ResourceVersion, 10, 16)
	return version, err
}

func cleanupCluster(cluster *appsv1beta1.FoundationDBCluster, g *gomega.GomegaWithT) {
	err := c.Delete(context.TODO(), cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	pods := &corev1.PodList{}
	err = c.List(context.TODO(), listOptions, pods)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, item := range pods.Items {
		err = c.Delete(context.TODO(), &item)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	configMaps := &corev1.ConfigMapList{}
	err = c.List(context.TODO(), listOptions, configMaps)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, item := range configMaps.Items {
		err = c.Delete(context.TODO(), &item)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}
}

func runReconciliation(t *testing.T, testFunction func(*gomega.GomegaWithT, *appsv1beta1.FoundationDBCluster, client.Client, chan reconcile.Request)) {
	g := gomega.NewGomegaWithT(t)
	ClearMockAdminClients()

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(t, newTestReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	cluster := createDefaultCluster()
	cluster.Spec.ConnectionString = ""

	defer cleanupCluster(cluster, g)

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the FoundationDBCluster object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	err = c.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testFunction(g, cluster, c, requests)
}

func TestReconcileWithNewCluster(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), listOptions, pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(4))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "operator-test"}, cluster)
		}, timeout).Should(gomega.Succeed())

		g.Expect(cluster.Spec.ReplicationMode).To(gomega.Equal("double"))
		g.Expect(cluster.Spec.StorageEngine).To(gomega.Equal("ssd"))
		g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(5))
		g.Expect(cluster.Spec.ConnectionString).NotTo(gomega.Equal(""))

		configMap := &corev1.ConfigMap{}
		configMapName := types.NamespacedName{Namespace: "default", Name: "operator-test-config"}
		g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
		expectedConfigMap, _ := GetConfigMap(cluster, c)
		g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))

		adminClient, err := newMockAdminClientUncast(cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient).NotTo(gomega.BeNil())
		g.Expect(adminClient.DatabaseConfiguration.ReplicationMode).To(gomega.Equal("double"))
		g.Expect(adminClient.DatabaseConfiguration.StorageEngine).To(gomega.Equal("ssd"))
	})
}

func TestReconcileWithDecreasedProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalPods := &corev1.PodList{}

		originalVersion, err := strconv.ParseInt(cluster.ResourceVersion, 10, 16)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), listOptions, originalPods)
			return len(originalPods.Items), err
		}, timeout).Should(gomega.Equal(4))

		cluster.Spec.ProcessCounts["storage"] = 3
		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 5))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), listOptions, pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(3))

		g.Expect(pods.Items[0].Name).To(gomega.Equal(originalPods.Items[0].Name))
		g.Expect(pods.Items[1].Name).To(gomega.Equal(originalPods.Items[1].Name))
		g.Expect(pods.Items[2].Name).To(gomega.Equal(originalPods.Items[2].Name))

		g.Expect(cluster.Spec.PendingRemovals).To(gomega.BeNil())

		adminClient, err := newMockAdminClientUncast(cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient).NotTo(gomega.BeNil())
		g.Expect(adminClient.ExcludedAddresses).To(gomega.Equal([]string{}))

		podClient, err := NewMockFdbPodClient(cluster, &originalPods.Items[3])
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(adminClient.ReincludedAddresses).To(gomega.Equal([]string{podClient.GetPodIP()}))
	})
}

func TestReconcileWithIncreasedProcessCount(t *testing.T) {
	runReconciliation(t, func(g *gomega.GomegaWithT, cluster *appsv1beta1.FoundationDBCluster, client client.Client, requests chan reconcile.Request) {
		originalVersion, err := strconv.ParseInt(cluster.ResourceVersion, 10, 16)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		cluster.Spec.ProcessCounts["storage"] = 5
		err = client.Update(context.TODO(), cluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
		g.Eventually(func() (int64, error) { return reloadCluster(c, cluster) }, timeout).Should(gomega.Equal(originalVersion + 3))

		pods := &corev1.PodList{}
		g.Eventually(func() (int, error) {
			err := c.List(context.TODO(), listOptions, pods)
			return len(pods.Items), err
		}, timeout).Should(gomega.Equal(5))

		g.Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "operator-test"}, cluster)
		}, timeout).Should(gomega.Succeed())

		g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(6))

		configMap := &corev1.ConfigMap{}
		configMapName := types.NamespacedName{Namespace: "default", Name: "operator-test-config"}
		g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
		expectedConfigMap, _ := GetConfigMap(cluster, c)
		g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))
	})
}
func TestGetConfigMap(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	configMap, err := GetConfigMap(cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configMap.Namespace).To(gomega.Equal("default"))
	g.Expect(configMap.Name).To(gomega.Equal("operator-test-config"))
	g.Expect(configMap.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name": "operator-test",
	}))

	g.Expect(len(configMap.Data)).To(gomega.Equal(3))
	g.Expect(configMap.Data["cluster-file"]).To(gomega.Equal("operator-test:asdfasf@127.0.0.1:4500"))
	g.Expect(configMap.Data["fdbmonitor-conf-storage"]).To(gomega.Equal(GetMonitorConf(cluster, "storage")))
	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(sidecarConf)).To(gomega.Equal(4))
	g.Expect(sidecarConf["COPY_FILES"]).To(gomega.Equal([]interface{}{"fdb.cluster"}))
	g.Expect(sidecarConf["COPY_BINARIES"]).To(gomega.Equal([]interface{}{"fdbserver", "fdbcli"}))
	g.Expect(sidecarConf["COPY_LIBRARIES"]).To(gomega.Equal([]interface{}{}))
	g.Expect(sidecarConf["INPUT_MONITOR_CONF"]).To(gomega.Equal("fdbmonitor.conf"))
}

func TestGetConfigMapWithEmptyConnectionString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.ConnectionString = ""
	configMap, err := GetConfigMap(cluster, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configMap.Data["cluster-file"]).To(gomega.Equal(""))
	g.Expect(configMap.Data["fdbmonitor-conf-storage"]).To(gomega.Equal(""))
}

func TestGetMonitorConfForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	conf := GetMonitorConf(cluster, "storage")
	g.Expect(conf).To(gomega.Equal(strings.Join([]string{
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
		"[fdbserver.1]",
		"command = /var/dynamic-conf/bin/6.0.18/fdbserver",
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		"public_address = $FDB_PUBLIC_IP:4500",
		"class = storage",
		"datadir = /var/fdb/data",
		"logdir = /var/log/fdb-trace-logs",
		"loggroup = operator-test",
		"locality_machineid = $HOSTNAME",
		"locality_zoneid = $HOSTNAME",
	}, "\n")))
}

func TestGetPodForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pod, err := GetPod(cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.Namespace).To(gomega.Equal("default"))
	g.Expect(pod.Name).To(gomega.Equal("operator-test-1"))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":  "operator-test",
		"fdb-process-class": "storage",
		"fdb-instance-id":   "1",
	}))
	g.Expect(pod.Spec).To(gomega.Equal(*GetPodSpec(cluster, "storage")))
}

func TestGetPodSpecForStorageInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	spec := GetPodSpec(cluster, "storage")

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.0.18"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
	}))
	g.Expect(initContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.0.18"))
	g.Expect(mainContainer.Command).To(gomega.Equal([]string{"sh", "-c"}))
	g.Expect(mainContainer.Args).To(gomega.Equal([]string{
		"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/fdb/fdbmonitor.lockfile",
	}))
	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
	}))

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
	}))
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))

	g.Expect(len(spec.Volumes)).To(gomega.Equal(4))
	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name:         "data",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[1]).To(gomega.Equal(corev1.Volume{
		Name:         "dynamic-conf",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
		Name: "config-map",
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "operator-test-config"},
			Items: []corev1.KeyToPath{
				corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
				corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
				corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"},
			},
		}},
	}))
	g.Expect(spec.Volumes[3]).To(gomega.Equal(corev1.Volume{
		Name:         "fdb-trace-logs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
}
