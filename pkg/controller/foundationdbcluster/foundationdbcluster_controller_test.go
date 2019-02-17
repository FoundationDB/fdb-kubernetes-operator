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
var podListOptions = (&client.ListOptions{}).InNamespace("default").MatchingLabels(map[string]string{
	"fdb-cluster-name": "operator-test",
})

const timeout = time.Second * 5

var defaultCluster = &appsv1beta1.FoundationDBCluster{
	ObjectMeta: metav1.ObjectMeta{Name: "operator-test", Namespace: "default"},
	Spec: appsv1beta1.FoundationDBClusterSpec{
		Version: "6.0.18",
	},
}

func TestReconcileWithNewCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := defaultCluster.DeepCopy()

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the FoundationDBCluster object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), cluster)
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	configMap := &corev1.ConfigMap{}
	configMapName := types.NamespacedName{Namespace: "default", Name: "operator-test-config"}
	g.Eventually(func() error { return c.Get(context.TODO(), configMapName, configMap) }, timeout).Should(gomega.Succeed())
	expectedConfigMap, _ := GetConfigMap(cluster)
	g.Expect(configMap.Data).To(gomega.Equal(expectedConfigMap.Data))

	pods := &corev1.PodList{}
	g.Eventually(func() (int, error) {
		err := c.List(context.TODO(), podListOptions, pods)
		return len(pods.Items), err
	}, timeout).Should(gomega.Equal(1))

	g.Eventually(func() error {
		return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "operator-test"}, cluster)
	}, timeout).Should(gomega.Succeed())

	g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(2))
}

func TestGetConfigMap(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configMap, err := GetConfigMap(defaultCluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configMap.Namespace).To(gomega.Equal("default"))
	g.Expect(configMap.Name).To(gomega.Equal("operator-test-config"))
	g.Expect(configMap.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name": "operator-test",
	}))

	g.Expect(len(configMap.Data)).To(gomega.Equal(3))
	g.Expect(configMap.Data["cluster-file"]).To(gomega.Equal("init:init@127.0.0.1:4500"))
	g.Expect(configMap.Data["fdbmonitor-conf-storage"]).To(gomega.Equal(GetMonitorConf(defaultCluster, "storage")))
	sidecarConf := make(map[string]interface{})
	err = json.Unmarshal([]byte(configMap.Data["sidecar-conf"]), &sidecarConf)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(sidecarConf)).To(gomega.Equal(4))
	g.Expect(sidecarConf["COPY_FILES"]).To(gomega.Equal([]interface{}{"fdb.cluster"}))
	g.Expect(sidecarConf["COPY_BINARIES"]).To(gomega.Equal([]interface{}{"fdbserver", "fdbcli"}))
	g.Expect(sidecarConf["COPY_LIBRARIES"]).To(gomega.Equal([]interface{}{}))
	g.Expect(sidecarConf["INPUT_MONITOR_CONF"]).To(gomega.Equal("fdbmonitor.conf"))
}

func TestGetMonitorConfForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	conf := GetMonitorConf(defaultCluster, "storage")
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
	pod := GetPod(defaultCluster, "storage", 1)
	g.Expect(pod.Namespace).To(gomega.Equal("default"))
	g.Expect(pod.Name).To(gomega.Equal("operator-test-1"))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":  "operator-test",
		"fdb-process-class": "storage",
		"fdb-instance-id":   "1",
	}))
	g.Expect(pod.Spec).To(gomega.Equal(*GetPodSpec(defaultCluster, "storage")))
}

func TestGetPodSpecForStorageInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	spec := GetPodSpec(defaultCluster, "storage")

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
