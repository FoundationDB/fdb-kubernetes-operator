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
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/tools/record"

	"github.com/apple/foundationdb/bindings/go/src/fdb"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller")

// Add creates a new FoundationDBCluster Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	fdb.MustAPIVersion(510)
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFoundationDBCluster{
		Client:              mgr.GetClient(),
		recorder:            mgr.GetRecorder("foundationdbcluster-controller"),
		scheme:              mgr.GetScheme(),
		podClientProvider:   NewFdbPodClient,
		adminClientProvider: NewAdminClient,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	log.Info("Adding controller")

	mgr.GetFieldIndexer().IndexField(&corev1.Pod{}, "metadata.name", func(o runtime.Object) []string {
		return []string{o.(*corev1.Pod).Name}
	})

	mgr.GetFieldIndexer().IndexField(&corev1.PersistentVolumeClaim{}, "metadata.name", func(o runtime.Object) []string {
		return []string{o.(*corev1.PersistentVolumeClaim).Name}
	})

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
	recorder            record.EventRecorder
	scheme              *runtime.Scheme
	podClientProvider   func(*fdbtypes.FoundationDBCluster, *corev1.Pod) (FdbPodClient, error)
	adminClientProvider func(*fdbtypes.FoundationDBCluster, client.Client) (AdminClient, error)
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

	err = r.setDefaultValues(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateStatus(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateSidecarVersions(cluster)
	if err != nil {
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

	err = r.generateInitialClusterFile(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateDatabaseConfiguration(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.chooseRemovals(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.excludeInstances(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.removePods(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.includeInstances(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.bounceProcesses(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateStatus(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !cluster.Status.FullyReconciled {
		return reconcile.Result{}, errors.New("Cluster was not fully reconciled by reconciliation process")
	}

	log.Info("Reconciliation complete", "namespace", cluster.Namespace, "name", cluster.Name)

	return reconcile.Result{}, nil
}

func (r *ReconcileFoundationDBCluster) setDefaultValues(cluster *fdbtypes.FoundationDBCluster) error {
	changed := false
	if cluster.Spec.ReplicationMode == "" {
		cluster.Spec.ReplicationMode = "double"
		changed = true
	}
	if cluster.Spec.StorageEngine == "" {
		cluster.Spec.StorageEngine = "ssd"
		changed = true
	}
	if cluster.Spec.Resources == nil {
		cluster.Spec.Resources = &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"memory": resource.MustParse("1Gi"),
				"cpu":    resource.MustParse("1"),
			},
			Requests: corev1.ResourceList{
				"memory": resource.MustParse("1Gi"),
				"cpu":    resource.MustParse("1"),
			},
		}
		changed = true
	}
	if cluster.ApplyDefaultRoleCounts() {
		changed = true
	}
	if cluster.ApplyDefaultProcessCounts() {
		changed = true
	}
	if changed {
		err := r.Update(context.TODO(), cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) updateStatus(cluster *fdbtypes.FoundationDBCluster) error {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.IncorrectProcesses = make(map[string]int64)
	status.MissingProcesses = make(map[string]int64)

	var databaseStatus *fdbtypes.FoundationDBStatus
	processMap := make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)

	if cluster.Spec.Configured {
		adminClient, err := r.adminClientProvider(cluster, r)
		if err != nil {
			return err
		}
		databaseStatus, err = adminClient.GetStatus()
		if err != nil {
			return err
		}
		for _, process := range databaseStatus.Cluster.Processes {
			address := strings.Split(process.Address, ":")
			processMap[address[0]] = append(processMap[address[0]], process)
		}
	} else {
		databaseStatus = nil
	}

	existingPods := &corev1.PodList{}
	err := r.List(
		context.TODO(),
		getPodListOptions(cluster, ""),
		existingPods)
	if err != nil {
		return err
	}

	for _, pod := range existingPods.Items {
		processClass := pod.Labels["fdb-process-class"]

		_, pendingRemoval := cluster.Spec.PendingRemovals[pod.Name]
		if !pendingRemoval {
			status.ProcessCounts.IncreaseCount(processClass, 1)
		}

		podClient, err := r.getPodClient(cluster, &pod)
		if err != nil {
			return err
		}
		ip := podClient.GetPodIP()
		processStatus := processMap[ip]
		if len(processStatus) == 0 {
			existingTime, exists := cluster.Status.MissingProcesses[pod.Name]
			if exists {
				status.MissingProcesses[pod.Name] = existingTime
			} else {
				status.MissingProcesses[pod.Name] = time.Now().Unix()
			}
		} else {
			for _, process := range processStatus {
				commandLine := GetStartCommand(cluster, &pod)
				if commandLine != process.CommandLine {
					existingTime, exists := cluster.Status.IncorrectProcesses[pod.Name]
					if exists {
						status.IncorrectProcesses[pod.Name] = existingTime
					} else {
						status.IncorrectProcesses[pod.Name] = time.Now().Unix()
					}
				}
			}
		}
	}

	status.FullyReconciled = cluster.Spec.Configured &&
		len(cluster.Spec.PendingRemovals) == 0 &&
		cluster.Spec.ProcessCounts.CountsAreSatisfied(status.ProcessCounts) &&
		len(status.IncorrectProcesses) == 0
	cluster.Status = status

	r.Status().Update(context.TODO(), cluster)
	return nil
}

func (r *ReconcileFoundationDBCluster) updateConfigMap(cluster *fdbtypes.FoundationDBCluster) error {
	configMap, err := GetConfigMap(cluster, r)
	if err != nil {
		return err
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context.TODO(), types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "name", configMap.Name)
		err = r.Create(context.TODO(), configMap)
		return err
	} else if err != nil {
		return err
	}

	if !reflect.DeepEqual(existing.Data, configMap.Data) || !reflect.DeepEqual(existing.Labels, configMap.Labels) {
		log.Info("Updating config map", "namespace", configMap.Namespace, "name", configMap.Name)
		r.recorder.Event(cluster, "Normal", "UpdatingConfigMap", "")
		existing.ObjectMeta.Labels = configMap.ObjectMeta.Labels
		existing.Data = configMap.Data
		err = r.Update(context.TODO(), existing)
		if err != nil {
			return err
		}
	}

	pods := &corev1.PodList{}
	err = r.List(context.TODO(), getPodListOptions(cluster, ""), pods)
	if err != nil {
		return err
	}

	podUpdates := make([]chan error, len(pods.Items))
	for index := range pods.Items {
		podUpdates[index] = make(chan error)
		go r.updatePodDynamicConf(cluster, &pods.Items[index], podUpdates[index])
	}

	for _, podUpdate := range podUpdates {
		err = <-podUpdate
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) updatePodDynamicConf(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod, signal chan error) {
	client, err := r.getPodClient(cluster, pod)

	pendingUpdates := 3
	updateSignals := make(chan error, pendingUpdates)
	go UpdateDynamicFiles(client, "fdb.cluster", cluster.Spec.ConnectionString, updateSignals, func(client FdbPodClient, clientError chan error) { client.CopyFiles(clientError) })

	if !cluster.Spec.Configured {
		err = <-updateSignals
		if err != nil {
			signal <- err
		}
		pendingUpdates--
	}

	go UpdateDynamicFiles(client, "fdbmonitor.conf", GetMonitorConf(cluster, pod.ObjectMeta.Labels["fdb-process-class"], pod), updateSignals, func(client FdbPodClient, clientError chan error) { client.GenerateMonitorConf(clientError) })
	go CheckDynamicFilePresent(client, fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version), updateSignals)

	for pendingUpdates > 0 {
		err = <-updateSignals
		if err != nil {
			signal <- err
		}
		pendingUpdates--
	}

	signal <- nil
}

func getPodLabels(cluster *fdbtypes.FoundationDBCluster, processClass string, id int) map[string]string {
	labels := map[string]string{
		"fdb-cluster-name": cluster.ObjectMeta.Name,
	}

	if processClass != "" {
		labels["fdb-process-class"] = processClass
	}

	if id != 0 {
		labels["fdb-instance-id"] = strconv.Itoa(id)
	}

	return labels
}

func getPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass string) *client.ListOptions {
	return (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingLabels(getPodLabels(cluster, processClass, 0))
}

func (r *ReconcileFoundationDBCluster) addPods(cluster *fdbtypes.FoundationDBCluster) error {
	hasNewPods := false
	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.Spec.ProcessCounts.Map()
	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		if desiredCount < 0 {
			desiredCount = 0
		}
		newCount := desiredCount - currentCounts[processClass]
		if newCount > 0 {
			r.recorder.Event(cluster, "Normal", "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))
			id := cluster.Spec.NextInstanceID
			if id < 1 {
				id = 1
			}
			for i := 0; i < newCount; i++ {
				pvc, err := GetPvc(cluster, processClass, id)
				if err != nil {
					return err
				}
				if pvc != nil {
					err = r.Create(context.TODO(), pvc)
					if err != nil {
						return err
					}
				}

				pod, err := GetPod(cluster, processClass, id, r)
				if err != nil {
					return err
				}
				err = r.Create(context.TODO(), pod)
				if err != nil {
					return err
				}

				id++
			}
			cluster.Spec.NextInstanceID = id
			cluster.Status.ProcessCounts.IncreaseCount(processClass, newCount)
			hasNewPods = true
		}
	}
	if hasNewPods {
		err := r.Update(context.TODO(), cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) generateInitialClusterFile(cluster *fdbtypes.FoundationDBCluster) error {
	if cluster.Spec.ConnectionString == "" {
		log.Info("Generating initial cluster file", "namespace", cluster.Namespace, "name", cluster.Name)
		r.recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing initial coordinators")
		pods := &corev1.PodList{}
		err := r.List(context.TODO(), getPodListOptions(cluster, "storage"), pods)
		if err != nil {
			return err
		}
		count := cluster.DesiredCoordinatorCount()
		if len(pods.Items) < count {
			return errors.New("Cannot find enough pods to recruit coordinators")
		}

		clientChan := make(chan FdbPodClient, count)
		errChan := make(chan error)

		for i := 0; i < count; i++ {
			go r.getPodClientAsync(cluster, &pods.Items[i], clientChan, errChan)
		}
		clusterName := connectionStringNameRegex.ReplaceAllString(cluster.Name, "_")
		connectionString := fmt.Sprintf("%s:init", clusterName)
		for i := 0; i < count; i++ {
			select {
			case client := <-clientChan:
				if i == 0 {
					connectionString = connectionString + "@"
				} else {
					connectionString = connectionString + ","
				}
				connectionString = connectionString + client.GetPodIP() + ":4500"
			case err := <-errChan:
				return err
			}
		}
		cluster.Spec.ConnectionString = connectionString

		err = r.Update(context.TODO(), cluster)
		if err != nil {
			return err
		}

		return r.updateConfigMap(cluster)
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) updateDatabaseConfiguration(cluster *fdbtypes.FoundationDBCluster) error {
	log.Info("Configuring database", "cluster", cluster.Name)
	adminClient, err := r.adminClientProvider(cluster, r)

	if !cluster.Spec.Configured {
		r.recorder.Event(cluster, "Normal", "ChangingConfiguration", "Setting initial database configuration")
	}
	if err != nil {
		return err
	}
	err = adminClient.ConfigureDatabase(DatabaseConfiguration{
		ReplicationMode: cluster.Spec.ReplicationMode,
		StorageEngine:   cluster.Spec.StorageEngine,
		RoleCounts:      cluster.Spec.RoleCounts,
	}, !cluster.Spec.Configured)
	if err != nil {
		return err
	}
	if !cluster.Spec.Configured {
		cluster.Spec.Configured = true
		err = r.Update(context.TODO(), cluster)
		if err != nil {
			return err
		}
	}
	log.Info("Configured database", "cluster", cluster.Name)

	return nil
}

func (r *ReconcileFoundationDBCluster) chooseRemovals(cluster *fdbtypes.FoundationDBCluster) error {
	hasNewRemovals := false

	var removals = cluster.Spec.PendingRemovals
	if removals == nil {
		removals = make(map[string]string)
	}

	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.Spec.ProcessCounts.Map()
	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		existingPods := &corev1.PodList{}
		err := r.List(
			context.TODO(),
			getPodListOptions(cluster, processClass),
			existingPods)
		if err != nil {
			return err
		}

		if desiredCount < 0 {
			desiredCount = 0
		}

		removedCount := currentCounts[processClass] - desiredCount
		if removedCount > 0 {
			r.recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing %d %s processes", removedCount, processClass))
			for indexOfPod := 0; indexOfPod < removedCount; indexOfPod++ {
				pod := existingPods.Items[len(existingPods.Items)-1-indexOfPod]
				podClient, err := r.getPodClient(cluster, &pod)
				if err != nil {
					return err
				}
				removals[pod.Name] = podClient.GetPodIP()
			}
			hasNewRemovals = true
			cluster.Status.ProcessCounts.IncreaseCount(processClass, -1*removedCount)
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		for podName := range cluster.Spec.PendingRemovals {
			if removals[podName] == "" {
				podList := &corev1.PodList{}
				err := r.List(context.TODO(), client.InNamespace(cluster.Namespace).MatchingField("metadata.name", podName), podList)
				if err != nil {
					return err
				}
				if len(podList.Items) == 0 {
					delete(removals, podName)
				} else {
					podClient, err := r.getPodClient(cluster, &podList.Items[0])
					if err != nil {
						return err
					}
					removals[podName] = podClient.GetPodIP()
				}
				hasNewRemovals = true
			}
		}
	}

	if hasNewRemovals {
		cluster.Spec.PendingRemovals = removals
		err := r.Update(context.TODO(), cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) excludeInstances(cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.adminClientProvider(cluster, r)
	if err != nil {
		return err
	}

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, address)
	}

	if len(addresses) > 0 {
		err = adminClient.ExcludeInstances(addresses)
		r.recorder.Event(cluster, "Normal", "ExcludingProcesses", fmt.Sprintf("Excluding %v", addresses))
		if err != nil {
			return err
		}
	}

	remaining := addresses
	for len(remaining) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return err
		}
		if len(remaining) > 0 {
			log.Info("Waiting for exclusions to complete", "remainingServers", remaining)
			time.Sleep(time.Second)
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) removePods(cluster *fdbtypes.FoundationDBCluster) error {
	if len(cluster.Spec.PendingRemovals) == 0 {
		return nil
	}
	r.recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", cluster.Spec.PendingRemovals))
	updateSignals := make(chan error, len(cluster.Spec.PendingRemovals))
	for id := range cluster.Spec.PendingRemovals {
		go r.removePod(cluster, id, updateSignals)
	}

	for range cluster.Spec.PendingRemovals {
		err := <-updateSignals
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) removePod(cluster *fdbtypes.FoundationDBCluster, podName string, signal chan error) {
	podListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", podName)
	pods := &corev1.PodList{}
	err := r.List(context.TODO(), podListOptions, pods)
	if err != nil {
		signal <- err
		return
	}
	if len(pods.Items) == 0 {
		signal <- nil
		return
	}

	err = r.Delete(context.TODO(), &pods.Items[0])
	if err != nil {
		signal <- err
		return
	}

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", podName))
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context.TODO(), pvcListOptions, pvcs)
	if err != nil {
		signal <- err
		return
	}
	if len(pvcs.Items) > 0 {
		err = r.Delete(context.TODO(), &pvcs.Items[0])
	}

	for {
		err := r.List(context.TODO(), podListOptions, pods)
		if err != nil {
			signal <- err
			return
		}
		if len(pods.Items) == 0 {
			break
		}

		log.Info("Waiting for pod get torn down", "pod", podName)
		time.Sleep(time.Second)
	}

	for {
		err := r.List(context.TODO(), pvcListOptions, pvcs)
		if err != nil {
			signal <- err
			return
		}
		if len(pvcs.Items) == 0 {
			break
		}

		log.Info("Waiting for volume claim get torn down", "name", pvcs.Items[0].Name)
		time.Sleep(time.Second)
	}

	signal <- nil
	return
}

func (r *ReconcileFoundationDBCluster) includeInstances(cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.adminClientProvider(cluster, r)
	if err != nil {
		return err
	}

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, address)
	}

	if len(addresses) > 0 {
		r.recorder.Event(cluster, "Normal", "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))
	}

	err = adminClient.IncludeInstances(addresses)
	if err != nil {
		return err
	}

	cluster.Spec.PendingRemovals = nil
	r.Update(context.TODO(), cluster)

	return nil
}

func (r *ReconcileFoundationDBCluster) updateSidecarVersions(cluster *fdbtypes.FoundationDBCluster) error {
	pods := &corev1.PodList{}
	err := r.List(context.TODO(), getPodListOptions(cluster, ""), pods)
	if err != nil {
		return err
	}
	upgraded := false
	image := fmt.Sprintf("%s/foundationdb-kubernetes-sidecar:%s", DockerImageRoot, cluster.Spec.Version)
	for _, pod := range pods.Items {
		for containerIndex, container := range pod.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != image {
				log.Info("Upgrading sidecar", "namespace", cluster.Namespace, "pod", pod.Name, "oldImage", container.Image, "newImage", image)
				pod.Spec.Containers[containerIndex].Image = image
				err := r.Update(context.TODO(), &pod)
				if err != nil {
					return err
				}
				upgraded = true
			}
		}
	}
	if upgraded {
		r.recorder.Event(cluster, "Normal", "SidecarUpgraded", fmt.Sprintf("New version: %s", cluster.Spec.Version))
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) bounceProcesses(cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.adminClientProvider(cluster, r)
	if err != nil {
		return err
	}

	addresses := make([]string, 0, len(cluster.Status.IncorrectProcesses))
	for podName := range cluster.Status.IncorrectProcesses {
		pod := &corev1.Pod{}
		err := r.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: podName}, pod)
		if err != nil {
			return err
		}
		podClient, err := r.podClientProvider(cluster, pod)
		if err != nil {
			return err
		}
		addresses = append(addresses, podClient.GetPodIP())

		signal := make(chan error)
		go r.updatePodDynamicConf(cluster, pod, signal)
		err = <-signal
		if err != nil {
			return err
		}
	}

	if len(addresses) > 0 {
		log.Info("Bouncing instances", "namespace", cluster.Namespace, "cluster", cluster.Name, "addresses", addresses)
		r.recorder.Event(cluster, "Normal", "BouncingInstances", fmt.Sprintf("Bouncing processes: %v", addresses))
		err = adminClient.KillInstances(addresses)
		if err != nil {
			return err
		}
	}

	return nil
}

func buildOwnerReference(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) ([]metav1.OwnerReference, error) {
	reloadedCluster := &fdbtypes.FoundationDBCluster{}
	kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, reloadedCluster)
	var isController = true
	return []metav1.OwnerReference{metav1.OwnerReference{
		APIVersion: reloadedCluster.APIVersion,
		Kind:       reloadedCluster.Kind,
		Name:       reloadedCluster.Name,
		UID:        reloadedCluster.UID,
		Controller: &isController,
	}}, nil
}

// DockerImageRoot is the prefix for our docker image paths
var DockerImageRoot = "foundationdb"

// GetConfigMap builds a config map for a cluster's dynamic config
func GetConfigMap(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	connectionString := cluster.Spec.ConnectionString
	data["cluster-file"] = connectionString

	desiredCounts := cluster.Spec.ProcessCounts.Map()
	for processClass, count := range desiredCounts {
		if count > 0 {
			filename := fmt.Sprintf("fdbmonitor-conf-%s", processClass)
			if connectionString == "" {
				data[filename] = ""
			} else {
				data[filename] = GetMonitorConf(cluster, processClass, nil)
			}
		}
	}

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

	owner, err := buildOwnerReference(cluster, kubeClient)
	if err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      fmt.Sprintf("%s-config", cluster.Name),
			Labels: map[string]string{
				"fdb-cluster-name": cluster.Name,
			},
			OwnerReferences: owner,
		},
		Data: data,
	}, nil
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod) string {
	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)
	confLines = append(confLines, "[fdbserver.1]")
	confLines = append(confLines, getStartCommandLines(cluster, processClass, pod)...)
	return strings.Join(confLines, "\n")
}

// GetStartCommand builds the expected start command for a pod.
func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) string {
	lines := getStartCommandLines(cluster, pod.Labels["fdb-process-class"], pod)
	regex := regexp.MustCompile("^(\\w+)\\s*=\\s*(.*)")
	firstComponents := regex.FindStringSubmatch(lines[0])
	command := firstComponents[2]
	sort.Slice(lines, func(i, j int) bool {
		return strings.Compare(lines[i], lines[j]) < 0
	})
	for _, line := range lines {
		components := regex.FindStringSubmatch(line)
		if components[1] == "command" {
			continue
		}
		command += " --" + components[1] + "=" + components[2]
	}
	return command
}

func getStartCommandLines(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod) []string {
	confLines := make([]string, 0, 20)
	var publicIP string
	var machineID string

	if pod == nil {
		publicIP = "$FDB_PUBLIC_IP"
		machineID = "$FDB_MACHINE_ID"
	} else {
		publicIP = pod.Status.PodIP
		machineID = pod.Name
	}
	confLines = append(confLines,
		fmt.Sprintf("command = /var/dynamic-conf/bin/%s/fdbserver", cluster.Spec.Version),
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		fmt.Sprintf("public_address = %s:4500", publicIP),
		fmt.Sprintf("class = %s", processClass),
		"datadir = /var/fdb/data",
		"logdir = /var/log/fdb-trace-logs",
		fmt.Sprintf("loggroup = %s", cluster.Name),
		fmt.Sprintf("locality_machineid = %s", machineID),
		fmt.Sprintf("locality_zoneid = %s", machineID),
	)
	confLines = append(confLines, cluster.Spec.CustomParameters...)
	return confLines
}

// GetPod builds a pod for a new instance
func GetPod(cluster *fdbtypes.FoundationDBCluster, processClass string, id int, kubeClient client.Client) (*corev1.Pod, error) {
	name := fmt.Sprintf("%s-%d", cluster.ObjectMeta.Name, id)

	owner, err := buildOwnerReference(cluster, kubeClient)
	if err != nil {
		return nil, err
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			Labels:          getPodLabels(cluster, processClass, id),
			OwnerReferences: owner,
		},
		Spec: *GetPodSpec(cluster, processClass, name),
	}, nil
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass string, podID string) *corev1.PodSpec {
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
		Resources: *cluster.Spec.Resources,
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
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(8080),
			}},
		},
	}

	var mainVolumeSource corev1.VolumeSource
	if usePvc(cluster, processClass) {
		mainVolumeSource.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-data", podID),
		}
	} else {
		mainVolumeSource.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}

	volumes := []corev1.Volume{
		corev1.Volume{Name: "data", VolumeSource: mainVolumeSource},
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

func usePvc(cluster *fdbtypes.FoundationDBCluster, processClass string) bool {
	return cluster.Spec.VolumeSize != "0" && isStateful(processClass)
}

func isStateful(processClass string) bool {
	return processClass == "storage" || processClass == "log" || processClass == "transaction"
}

// GetPvc builds a persistent volume claim for a FoundationDB instance.
func GetPvc(cluster *fdbtypes.FoundationDBCluster, processClass string, id int) (*corev1.PersistentVolumeClaim, error) {
	if !usePvc(cluster, processClass) {
		return nil, nil
	}
	name := fmt.Sprintf("%s-%d-data", cluster.ObjectMeta.Name, id)
	size, err := resource.ParseQuantity(cluster.Spec.VolumeSize)
	if err != nil {
		return nil, err
	}
	spec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.ResourceRequirements{
			Requests: v1.ResourceList{"storage": size},
		},
		StorageClassName: cluster.Spec.StorageClass,
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      name,
			Labels:    getPodLabels(cluster, processClass, id),
		},
		Spec: spec,
	}, nil
}

func (r *ReconcileFoundationDBCluster) getPodClient(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (FdbPodClient, error) {
	client, err := r.podClientProvider(cluster, pod)
	for err != nil {
		if err == fdbPodClientErrorNoIP {
			log.Info("Waiting for pod to be assigned an IP", "pod", pod.Name)
		} else if err == fdbPodClientErrorNotReady {
			log.Info("Waiting for pod to be ready", "pod", pod.Name)
		} else {
			return nil, err
		}
		time.Sleep(time.Second)
		err = r.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)
		if err != nil {
			return nil, err
		}
		client, err = r.podClientProvider(cluster, pod)
	}
	return client, nil
}

func (r *ReconcileFoundationDBCluster) getPodClientAsync(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod, clientChan chan FdbPodClient, errorChan chan error) {
	client, err := r.getPodClient(cluster, pod)
	if err != nil {
		errorChan <- err
	} else {
		clientChan <- client
	}
}

var connectionStringNameRegex, _ = regexp.Compile("[^A-Za-z0-9_]")
