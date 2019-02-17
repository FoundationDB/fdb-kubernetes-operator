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
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	fdbtypes "github.com/brownleej/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller")

var processClasses = []string{"storage"}

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new FoundationDBCluster Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFoundationDBCluster{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	log.Info("Adding controller", "docker root", DockerImageRoot)
	c, err := controller.New("foundationdbcluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to FoundationDBCluster
	err = c.Watch(&source.Kind{Type: &fdbtypes.FoundationDBCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to pods owned by a FoundationDBCluster
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &fdbtypes.FoundationDBCluster{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileFoundationDBCluster{}

// ReconcileFoundationDBCluster reconciles a FoundationDBCluster object
type ReconcileFoundationDBCluster struct {
	client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a FoundationDBCluster object and makes changes based on the state read
// and what is in the FoundationDBCluster.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  The scaffolding writes
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;watch;list;create;update;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch
func (r *ReconcileFoundationDBCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the FoundationDBCluster instance
	cluster := &fdbtypes.FoundationDBCluster{}
	err := r.Get(context.TODO(), request.NamespacedName, cluster)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = r.updateConfigMap(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.addPods(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileFoundationDBCluster) updateConfigMap(cluster *fdbtypes.FoundationDBCluster) error {
	configMap, err := GetConfigMap(cluster)
	if err != nil {
		return err
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context.TODO(), types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "name", configMap.Name)
		err = r.Create(context.TODO(), configMap)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if !reflect.DeepEqual(existing, configMap) {
		log.Info("Updating config map", "namespace", configMap.Namespace, "name", configMap.Name)
		err = r.Update(context.TODO(), configMap)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) addPods(cluster *fdbtypes.FoundationDBCluster) error {
	for _, processClass := range processClasses {
		existingPods := &corev1.PodList{}
		podListLabels := map[string]string{
			"fdb-cluster-name":  cluster.ObjectMeta.Name,
			"fdb-process-class": processClass}
		err := r.List(
			context.TODO(),
			(&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingLabels(podListLabels),
			existingPods)
		if err != nil {
			return err
		}

		desiredCount := cluster.DesiredProcessCount(processClass)
		newCount := desiredCount - len(existingPods.Items)
		if newCount > 0 {
			id := cluster.Spec.NextInstanceID
			if id < 1 {
				id = 1
			}
			for i := 0; i < newCount; i++ {
				err := r.Create(context.TODO(), GetPod(cluster, processClass, id))
				if err != nil {
					return err
				}
				id++
			}
			cluster.Spec.NextInstanceID = id

			r.Update(context.TODO(), cluster)
		}
	}
	return nil
}

func buildOwnerReference(cluster *fdbtypes.FoundationDBCluster) []metav1.OwnerReference {
	var isController = true
	return []metav1.OwnerReference{metav1.OwnerReference{
		APIVersion: cluster.APIVersion,
		Kind:       cluster.Kind,
		Name:       cluster.Name,
		UID:        cluster.UID,
		Controller: &isController,
	}}
}

// DockerImageRoot is the prefix for our docker image paths
var DockerImageRoot = "foundationdb"

// GetConfigMap builds a config map for a cluster's dynamic config
func GetConfigMap(cluster *fdbtypes.FoundationDBCluster) (*corev1.ConfigMap, error) {
	data := make(map[string]string)
	for _, processClass := range processClasses {
		data[fmt.Sprintf("fdbmonitor-conf-%s", processClass)] = GetMonitorConf(cluster, processClass)
	}

	connectionString := cluster.Spec.ConnectionString
	if connectionString == "" {
		connectionString = "init:init@127.0.0.1:4500"
	}
	data["cluster-file"] = connectionString

	sidecarConf := map[string]interface{}{
		"COPY_BINARIES":      []string{"fdbserver", "fdbcli"},
		"COPY_FILES":         []string{"fdb.cluster"},
		"COPY_LIBRARIES":     []string{},
		"INPUT_MONITOR_CONF": "fdbmonitor.conf",
	}
	sidecarConfData, err := json.Marshal(sidecarConf)
	if err != nil {
		return nil, err
	}
	data["sidecar-conf"] = string(sidecarConfData)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      fmt.Sprintf("%s-config", cluster.Name),
			Labels: map[string]string{
				"fdb-cluster-name": cluster.Name,
			},
			OwnerReferences: buildOwnerReference(cluster),
		},
		Data: data,
	}, nil
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass string) string {
	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)
	confLines = append(confLines,
		"[fdbserver.1]",
		fmt.Sprintf("command = /var/dynamic-conf/bin/%s/fdbserver", cluster.Spec.Version),
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		"public_address = $FDB_PUBLIC_IP:4500",
		fmt.Sprintf("class = %s", processClass),
		"datadir = /var/fdb/data",
		"logdir = /var/log/fdb-trace-logs",
		fmt.Sprintf("loggroup = %s", cluster.Name),
		"locality_machineid = $HOSTNAME",
		"locality_zoneid = $HOSTNAME",
	)
	return strings.Join(confLines, "\n")
}

// GetPod builds a pod for a new instance
func GetPod(cluster *fdbtypes.FoundationDBCluster, processClass string, id int) *corev1.Pod {
	name := fmt.Sprintf("%s-%d", cluster.ObjectMeta.Name, id)
	podLabels := map[string]string{
		"fdb-cluster-name":  cluster.Name,
		"fdb-process-class": processClass,
		"fdb-instance-id":   strconv.Itoa(id),
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			Labels:          podLabels,
			OwnerReferences: buildOwnerReference(cluster),
		},
		Spec: *GetPodSpec(cluster, processClass),
	}
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass string) *corev1.PodSpec {
	mainContainer := corev1.Container{
		Name:  "foundationdb",
		Image: fmt.Sprintf("%s/foundationdb:%s", DockerImageRoot, cluster.Spec.Version),
		Env: []corev1.EnvVar{
			corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
		},
		Command: []string{"sh", "-c"},
		Args: []string{
			"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
				" --lockfile /var/fdb/fdbmonitor.lockfile",
		},
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
			corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		},
	}
	initContainer := corev1.Container{
		Name:  "foundationdb-kubernetes-init",
		Image: fmt.Sprintf("%s/foundationdb-kubernetes-sidecar:%s", DockerImageRoot, cluster.Spec.Version),
		Env: []corev1.EnvVar{
			corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
			corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		},
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
		},
	}
	sidecarContainer := corev1.Container{
		Name:         "foundationdb-kubernetes-sidecar",
		Image:        initContainer.Image,
		Env:          initContainer.Env[1:],
		VolumeMounts: initContainer.VolumeMounts,
	}

	volumes := []corev1.Volume{
		corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: "config-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
			Items: []corev1.KeyToPath{
				corev1.KeyToPath{Key: fmt.Sprintf("fdbmonitor-conf-%s", processClass), Path: "fdbmonitor.conf"},
				corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
				corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"},
			},
		}}},
		corev1.Volume{Name: "fdb-trace-logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	return &corev1.PodSpec{
		InitContainers: []corev1.Container{initContainer},
		Containers:     []corev1.Container{mainContainer, sidecarContainer},
		Volumes:        volumes,
	}
}
