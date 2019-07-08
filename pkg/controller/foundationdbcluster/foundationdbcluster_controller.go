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
	ctx "context"
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

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

// Add creates a new FoundationDBCluster Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFoundationDBCluster{
		Client:              mgr.GetClient(),
		recorder:            mgr.GetRecorder("foundationdbcluster-controller"),
		scheme:              mgr.GetScheme(),
		podClientProvider:   NewFdbPodClient,
		adminClientProvider: NewCliAdminClient,
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
	context := ctx.Background()
	err := r.Get(context, request.NamespacedName, cluster)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = r.setDefaultValues(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateStatus(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateSidecarVersions(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateConfigMap(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.addPods(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.generateInitialClusterFile(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateDatabaseConfiguration(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.chooseRemovals(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.excludeInstances(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.changeCoordinators(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.removePods(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.includeInstances(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.bounceProcesses(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateStatus(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !cluster.Status.FullyReconciled {
		return reconcile.Result{}, errors.New("Cluster was not fully reconciled by reconciliation process")
	}

	log.Info("Reconciliation complete", "namespace", cluster.Namespace, "name", cluster.Name)

	return reconcile.Result{}, nil
}

func (r *ReconcileFoundationDBCluster) setDefaultValues(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	changed := false
	if cluster.Spec.RedundancyMode == "" {
		cluster.Spec.RedundancyMode = "double"
		changed = true
	}
	if cluster.Spec.StorageEngine == "" {
		cluster.Spec.StorageEngine = "ssd"
		changed = true
	}
	if cluster.Spec.UsableRegions == 0 {
		cluster.Spec.UsableRegions = 1
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
	if cluster.Spec.RunningVersion == "" {
		cluster.Spec.RunningVersion = cluster.Spec.Version
		changed = true
	}
	if changed {
		err := r.Update(context, cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) updateStatus(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.IncorrectProcesses = make(map[string]int64)
	status.MissingProcesses = make(map[string]int64)

	var databaseStatus *fdbtypes.FoundationDBStatus
	processMap := make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)

	var currentDatabaseConfiguration fdbtypes.DatabaseConfiguration

	if cluster.Spec.Configured {
		adminClient, err := r.adminClientProvider(cluster, r)
		if err != nil {
			return err
		}
		defer adminClient.Close()
		databaseStatus, err = adminClient.GetStatus()
		if err != nil {
			return err
		}
		for _, process := range databaseStatus.Cluster.Processes {
			address := strings.Split(process.Address, ":")
			processMap[address[0]] = append(processMap[address[0]], process)
		}

		currentDatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.FillInDefaultsFromStatus()

	} else {
		databaseStatus = nil
	}

	existingPods := &corev1.PodList{}
	err := r.List(
		context,
		getPodListOptions(cluster, "", ""),
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

		podClient, err := r.getPodClient(context, cluster, &pod)
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
				commandLine, err := GetStartCommand(cluster, &pod)
				if err != nil {
					return err
				}
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
		cluster.GetProcessCountsWithDefaults().CountsAreSatisfied(status.ProcessCounts) &&
		len(status.IncorrectProcesses) == 0 &&
		databaseStatus != nil &&
		reflect.DeepEqual(currentDatabaseConfiguration, cluster.DesiredDatabaseConfiguration())
	cluster.Status = status

	r.Status().Update(context, cluster)
	return nil
}

func (r *ReconcileFoundationDBCluster) updateConfigMap(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return err
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "name", configMap.Name)
		err = r.Create(context, configMap)
		return err
	} else if err != nil {
		return err
	}

	if !reflect.DeepEqual(existing.Data, configMap.Data) || !reflect.DeepEqual(existing.Labels, configMap.Labels) {
		log.Info("Updating config map", "namespace", configMap.Namespace, "name", configMap.Name)
		r.recorder.Event(cluster, "Normal", "UpdatingConfigMap", "")
		existing.ObjectMeta.Labels = configMap.ObjectMeta.Labels
		existing.Data = configMap.Data
		err = r.Update(context, existing)
		if err != nil {
			return err
		}
	}

	pods := &corev1.PodList{}
	err = r.List(context, getPodListOptions(cluster, "", ""), pods)
	if err != nil {
		return err
	}

	podUpdates := make([]chan error, len(pods.Items))
	for index := range pods.Items {
		podUpdates[index] = make(chan error)
		go r.updatePodDynamicConf(context, cluster, &pods.Items[index], podUpdates[index])
	}

	for _, podUpdate := range podUpdates {
		err = <-podUpdate
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) updatePodDynamicConf(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod, signal chan error) {
	client, err := r.getPodClient(context, cluster, pod)
	if err != nil {
		signal <- err
		return
	}

	conf, err := GetMonitorConf(cluster, pod.ObjectMeta.Labels["fdb-process-class"], pod)
	if err != nil {
		signal <- err
		return
	}

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

	go UpdateDynamicFiles(client, "fdbmonitor.conf", conf, updateSignals, func(client FdbPodClient, clientError chan error) { client.GenerateMonitorConf(clientError) })
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

func getPodLabels(cluster *fdbtypes.FoundationDBCluster, processClass string, id string) map[string]string {
	labels := map[string]string{}

	for label, value := range cluster.ObjectMeta.Labels {
		labels[label] = value
	}

	labels["fdb-cluster-name"] = cluster.ObjectMeta.Name

	if processClass != "" {
		labels["fdb-process-class"] = processClass
	}

	if id != "" {
		labels["fdb-instance-id"] = id
	}

	return labels
}

func getPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass string, id string) *client.ListOptions {
	return (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingLabels(getPodLabels(cluster, processClass, id))
}

func (r *ReconcileFoundationDBCluster) addPods(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	hasNewPods := false
	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()
	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		if desiredCount < 0 {
			desiredCount = 0
		}
		newCount := desiredCount - currentCounts[processClass]
		if newCount > 0 {
			r.recorder.Event(cluster, "Normal", "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))

			pvcs := &corev1.PersistentVolumeClaimList{}
			r.List(context, getPodListOptions(cluster, processClass, ""), pvcs)
			reusablePvcs := make(map[int]bool, len(pvcs.Items))
			for index, pvc := range pvcs.Items {
				if pvc.Status.Phase == "Bound" && pvc.ObjectMeta.DeletionTimestamp == nil {
					matchingPods := &corev1.PodList{}
					err := r.List(context, getPodListOptions(cluster, processClass, pvc.Labels["fdb-instance-id"]), matchingPods)
					if err != nil {
						return err
					}
					if len(matchingPods.Items) == 0 {
						reusablePvcs[index] = true
					}
				}
			}

			addedCount := 0
			for index := range reusablePvcs {
				if newCount <= 0 {
					break
				}
				id, err := strconv.Atoi(pvcs.Items[index].Labels["fdb-instance-id"])
				if err != nil {
					return err
				}

				pod, err := GetPod(context, cluster, processClass, id, r)
				if err != nil {
					return err
				}
				err = r.Create(context, pod)
				if err != nil {
					return err
				}
				addedCount++
				newCount--
			}

			id := cluster.Spec.NextInstanceID
			if id < 1 {
				id = 1
			}
			for i := 0; i < newCount; i++ {
				for id > 0 {
					pvcs := &corev1.PersistentVolumeClaimList{}
					err := r.List(context, getPodListOptions(cluster, "", strconv.Itoa(id)), pvcs)
					if err != nil {
						return err
					}
					if len(pvcs.Items) == 0 {
						break
					}
					id++
				}

				pvc, err := GetPvc(cluster, processClass, id)
				if err != nil {
					return err
				}
				if pvc != nil {
					err = r.Create(context, pvc)
					if err != nil {
						return err
					}
				}

				pod, err := GetPod(context, cluster, processClass, id, r)
				if err != nil {
					return err
				}
				err = r.Create(context, pod)
				if err != nil {
					return err
				}

				addedCount++
				id++
			}
			cluster.Spec.NextInstanceID = id
			cluster.Status.ProcessCounts.IncreaseCount(processClass, addedCount)
			hasNewPods = true
		}
	}
	if hasNewPods {
		err := r.Update(context, cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) generateInitialClusterFile(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	if cluster.Spec.ConnectionString == "" {
		log.Info("Generating initial cluster file", "namespace", cluster.Namespace, "name", cluster.Name)
		r.recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing initial coordinators")
		pods := &corev1.PodList{}
		err := r.List(context, getPodListOptions(cluster, "storage", ""), pods)
		if err != nil {
			return err
		}
		count := cluster.DesiredCoordinatorCount()
		if len(pods.Items) < count {
			return errors.New("Cannot find enough pods to recruit coordinators")
		}

		clusterName := connectionStringNameRegex.ReplaceAllString(cluster.Name, "_")
		connectionString := fdbtypes.ConnectionString{DatabaseName: clusterName}
		err = connectionString.GenerateNewGenerationID()
		if err != nil {
			return err
		}

		clientChan := make(chan FdbPodClient, count)
		errChan := make(chan error)

		for i := 0; i < count; i++ {
			go r.getPodClientAsync(context, cluster, &pods.Items[i], clientChan, errChan)
		}
		for i := 0; i < count; i++ {
			select {
			case client := <-clientChan:
				connectionString.Coordinators = append(connectionString.Coordinators, cluster.GetFullAddress(client.GetPodIP()))
			case err := <-errChan:
				return err
			}
		}
		cluster.Spec.ConnectionString = connectionString.String()

		err = r.Update(context, cluster)
		if err != nil {
			return err
		}

		return r.updateConfigMap(context, cluster)
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) updateDatabaseConfiguration(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.adminClientProvider(cluster, r)

	if err != nil {
		return err
	}
	defer adminClient.Close()

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	desiredConfiguration.RoleCounts.Storage = 0
	needsChange := false
	if !cluster.Spec.Configured {
		needsChange = true
	} else {
		status, err := adminClient.GetStatus()
		if err != nil {
			return err
		}

		needsChange = !reflect.DeepEqual(desiredConfiguration, status.Cluster.DatabaseConfiguration.FillInDefaultsFromStatus())
	}

	if needsChange {
		configurationString, _ := desiredConfiguration.GetConfigurationString()
		log.Info("Configuring database", "cluster", cluster.Name)
		r.recorder.Event(cluster, "Normal", "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(cluster.DesiredDatabaseConfiguration(), !cluster.Spec.Configured)
		if err != nil {
			return err
		}
		if !cluster.Spec.Configured {
			cluster.Spec.Configured = true
			err = r.Update(context, cluster)
			if err != nil {
				return err
			}
		}
		log.Info("Configured database", "cluster", cluster.Name)
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) chooseRemovals(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	hasNewRemovals := false

	var removals = cluster.Spec.PendingRemovals
	if removals == nil {
		removals = make(map[string]string)
	}

	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()
	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		existingPods := &corev1.PodList{}
		err := r.List(
			context,
			getPodListOptions(cluster, processClass, ""),
			existingPods)
		if err != nil {
			return err
		}

		if desiredCount < 0 {
			desiredCount = 0
		}

		removedCount := currentCounts[processClass] - desiredCount
		if removedCount > 0 {
			err = sortPodsByID(existingPods)
			if err != nil {
				return err
			}

			r.recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing %d %s processes", removedCount, processClass))
			for indexOfPod := 0; indexOfPod < removedCount; indexOfPod++ {
				pod := existingPods.Items[len(existingPods.Items)-1-indexOfPod]
				podClient, err := r.getPodClient(context, cluster, &pod)
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
				err := r.List(context, client.InNamespace(cluster.Namespace).MatchingField("metadata.name", podName), podList)
				if err != nil {
					return err
				}
				if len(podList.Items) == 0 {
					delete(removals, podName)
				} else {
					podClient, err := r.getPodClient(context, cluster, &podList.Items[0])
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
		err := r.Update(context, cluster)
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
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, cluster.GetFullAddress(address))
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

func (r *ReconcileFoundationDBCluster) changeCoordinators(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	if !cluster.Spec.Configured {
		return nil
	}

	adminClient, err := r.adminClientProvider(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		return err
	}
	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	if len(coordinatorStatus) == 0 {
		return errors.New("Unable to get coordinator status")
	}

	for _, process := range status.Cluster.Processes {
		_, isCoordinator := coordinatorStatus[process.Address]
		if isCoordinator && !process.Excluded {
			coordinatorStatus[process.Address] = true
		}
	}

	needsChange := len(coordinatorStatus) != cluster.DesiredCoordinatorCount()
	for _, healthy := range coordinatorStatus {
		needsChange = needsChange || !healthy
	}

	if needsChange {
		log.Info("Changing coordinators", "namespace", cluster.Namespace, "name", cluster.Name)
		r.recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing new coordinators")
		coordinatorCount := cluster.DesiredCoordinatorCount()
		coordinators := make([]string, 0, coordinatorCount)
		for _, process := range status.Cluster.Processes {
			eligible := !process.Excluded && isStateful(process.ProcessClass)
			if eligible {
				coordinators = append(coordinators, process.Address)
			}
			if len(coordinators) >= coordinatorCount {
				break
			}
		}
		if len(coordinators) < coordinatorCount {
			return errors.New("Unable to recruit new coordinators")
		}
		connectionString, err := adminClient.ChangeCoordinators(coordinators)
		if err != nil {
			return err
		}
		cluster.Spec.ConnectionString = connectionString
		err = r.Update(context, cluster)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) removePods(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	if len(cluster.Spec.PendingRemovals) == 0 {
		return nil
	}
	r.recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", cluster.Spec.PendingRemovals))
	updateSignals := make(chan error, len(cluster.Spec.PendingRemovals))
	for id := range cluster.Spec.PendingRemovals {
		go r.removePod(context, cluster, id, updateSignals)
	}

	for range cluster.Spec.PendingRemovals {
		err := <-updateSignals
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) removePod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, podName string, signal chan error) {
	podListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", podName)
	pods := &corev1.PodList{}
	err := r.List(context, podListOptions, pods)
	if err != nil {
		signal <- err
		return
	}
	if len(pods.Items) == 0 {
		signal <- nil
		return
	}

	err = r.Delete(context, &pods.Items[0])
	if err != nil {
		signal <- err
		return
	}

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", podName))
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcListOptions, pvcs)
	if err != nil {
		signal <- err
		return
	}
	if len(pvcs.Items) > 0 {
		err = r.Delete(context, &pvcs.Items[0])
	}

	for {
		err := r.List(context, podListOptions, pods)
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
		err := r.List(context, pvcListOptions, pvcs)
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

func (r *ReconcileFoundationDBCluster) includeInstances(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.adminClientProvider(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, cluster.GetFullAddress(address))
	}

	if len(addresses) > 0 {
		r.recorder.Event(cluster, "Normal", "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))
	}

	err = adminClient.IncludeInstances(addresses)
	if err != nil {
		return err
	}

	cluster.Spec.PendingRemovals = nil
	r.Update(context, cluster)

	return nil
}

func (r *ReconcileFoundationDBCluster) updateSidecarVersions(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	pods := &corev1.PodList{}
	err := r.List(context, getPodListOptions(cluster, "", ""), pods)
	if err != nil {
		return err
	}
	upgraded := false
	image := fmt.Sprintf("%s/foundationdb-kubernetes-sidecar:%s-1", DockerImageRoot, cluster.Spec.Version)
	for _, pod := range pods.Items {
		for containerIndex, container := range pod.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != image {
				log.Info("Upgrading sidecar", "namespace", cluster.Namespace, "pod", pod.Name, "oldImage", container.Image, "newImage", image)
				pod.Spec.Containers[containerIndex].Image = image
				err := r.Update(context, &pod)
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

func (r *ReconcileFoundationDBCluster) bounceProcesses(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.adminClientProvider(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Status.IncorrectProcesses))
	for podName := range cluster.Status.IncorrectProcesses {
		pod := &corev1.Pod{}
		err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: podName}, pod)
		if err != nil {
			return err
		}
		podClient, err := r.podClientProvider(cluster, pod)
		if err != nil {
			return err
		}
		addresses = append(addresses, cluster.GetFullAddress(podClient.GetPodIP()))

		signal := make(chan error)
		go r.updatePodDynamicConf(context, cluster, pod, signal)
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

	if cluster.Spec.RunningVersion != cluster.Spec.Version {
		cluster.Spec.RunningVersion = cluster.Spec.Version
		err = r.Update(context, cluster)
		if err != nil {
			return err
		}
	}

	return nil
}

func buildOwnerReference(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) ([]metav1.OwnerReference, error) {
	reloadedCluster := &fdbtypes.FoundationDBCluster{}
	kubeClient.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, reloadedCluster)
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
func GetConfigMap(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	connectionString := cluster.Spec.ConnectionString
	data["cluster-file"] = connectionString

	caFile := ""
	for _, ca := range cluster.Spec.TrustedCAs {
		if caFile != "" {
			caFile += "\n"
		}
		caFile += ca
	}

	data["ca-file"] = caFile

	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()
	for processClass, count := range desiredCounts {
		if count > 0 {
			filename := fmt.Sprintf("fdbmonitor-conf-%s", processClass)
			if connectionString == "" {
				data[filename] = ""
			} else {
				conf, err := GetMonitorConf(cluster, processClass, nil)
				if err != nil {
					return nil, err
				}
				data[filename] = conf
			}
		}
	}

	sidecarConf := map[string]interface{}{
		"COPY_BINARIES":      []string{"fdbserver", "fdbcli"},
		"COPY_FILES":         []string{"fdb.cluster", "ca.pem"},
		"COPY_LIBRARIES":     []string{},
		"INPUT_MONITOR_CONF": "fdbmonitor.conf",
	}
	sidecarConfData, err := json.Marshal(sidecarConf)
	if err != nil {
		return nil, err
	}
	data["sidecar-conf"] = string(sidecarConfData)

	owner, err := buildOwnerReference(context, cluster, kubeClient)
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
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod) (string, error) {
	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)
	confLines = append(confLines, "[fdbserver.1]")
	commands, err := getStartCommandLines(cluster, processClass, pod)
	if err != nil {
		return "", err
	}
	confLines = append(confLines, commands...)
	return strings.Join(confLines, "\n"), nil
}

// GetStartCommand builds the expected start command for a pod.
func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (string, error) {
	lines, err := getStartCommandLines(cluster, pod.Labels["fdb-process-class"], pod)
	if err != nil {
		return "", err
	}

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
	return command, nil
}

func getStartCommandLines(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod) ([]string, error) {
	confLines := make([]string, 0, 20)
	var publicIP string
	var machineID string
	var zoneID string

	if pod == nil {
		publicIP = "$FDB_PUBLIC_IP"
		machineID = "$FDB_MACHINE_ID"
		zoneID = "$FDB_ZONE_ID"
	} else {
		publicIP = pod.Status.PodIP
		if cluster.Spec.FaultDomain.Key == "foundationdb.org/none" {
			machineID = pod.Name
			zoneID = pod.Name
		} else if cluster.Spec.FaultDomain.Key == "foundationdb.org/kubernetes-cluster" {
			machineID = pod.Spec.NodeName
			zoneID = cluster.Spec.FaultDomain.Value
		} else {
			faultDomainSource := cluster.Spec.FaultDomain.ValueFrom
			if faultDomainSource == "" {
				faultDomainSource = "spec.nodeName"
			}
			machineID = pod.Spec.NodeName

			if faultDomainSource == "spec.nodeName" {
				zoneID = pod.Spec.NodeName
			} else {
				return nil, fmt.Errorf("Unsupported fault domain source %s", faultDomainSource)
			}
		}
	}

	logGroup := cluster.Spec.LogGroup
	if logGroup == "" {
		logGroup = cluster.Name
	}

	confLines = append(confLines,
		fmt.Sprintf("command = /var/dynamic-conf/bin/%s/fdbserver", cluster.Spec.Version),
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		fmt.Sprintf("public_address = %s", cluster.GetFullAddress(publicIP)),
		fmt.Sprintf("class = %s", processClass),
		"datadir = /var/fdb/data",
		"logdir = /var/log/fdb-trace-logs",
		fmt.Sprintf("loggroup = %s", logGroup),
		fmt.Sprintf("locality_machineid = %s", machineID),
		fmt.Sprintf("locality_zoneid = %s", zoneID),
	)

	for _, rule := range cluster.Spec.PeerVerificationRules {
		confLines = append(confLines, fmt.Sprintf("tls_verify_peers = %s", rule))
	}

	confLines = append(confLines, cluster.Spec.CustomParameters...)
	return confLines, nil
}

// GetPod builds a pod for a new instance
func GetPod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, processClass string, id int, kubeClient client.Client) (*corev1.Pod, error) {
	name := fmt.Sprintf("%s-%d", cluster.ObjectMeta.Name, id)

	owner, err := buildOwnerReference(context, cluster, kubeClient)
	if err != nil {
		return nil, err
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			Labels:          getPodLabels(cluster, processClass, strconv.Itoa(id)),
			OwnerReferences: owner,
		},
		Spec: *GetPodSpec(cluster, processClass, name),
	}, nil
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass string, podID string) *corev1.PodSpec {
	mainEnv := []corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
	}

	envOverrides := make(map[string]bool)
	for _, envVar := range cluster.Spec.Env {
		envOverrides[envVar.Name] = true
	}

	if !envOverrides["FDB_TLS_CA_FILE"] {
		mainEnv = append(mainEnv, corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"})
	}

	mainEnv = append(mainEnv, cluster.Spec.Env...)
	mainVolumeMounts := []corev1.VolumeMount{
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	}
	mainVolumeMounts = append(mainVolumeMounts, cluster.Spec.VolumeMounts...)

	mainContainer := corev1.Container{
		Name:    "foundationdb",
		Image:   fmt.Sprintf("%s/foundationdb:%s", DockerImageRoot, cluster.Spec.Version),
		Env:     mainEnv,
		Command: []string{"sh", "-c"},
		Args: []string{
			"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
				" --lockfile /var/fdb/fdbmonitor.lockfile",
		},
		VolumeMounts: mainVolumeMounts,
		Resources:    *cluster.Spec.Resources,
	}

	sidecarEnv := make([]corev1.EnvVar, 0, 5)
	sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "COPY_ONCE", Value: "1"})
	sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"})
	sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
	}})

	faultDomainKey := cluster.Spec.FaultDomain.Key
	if faultDomainKey == "" {
		faultDomainKey = "kubernetes.io/hostname"
	}

	faultDomainSource := cluster.Spec.FaultDomain.ValueFrom
	if faultDomainSource == "" {
		faultDomainSource = "spec.nodeName"
	}

	if faultDomainKey == "foundationdb.org/none" {
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
	} else if faultDomainKey == "foundationdb.org/kubernetes-cluster" {
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", Value: cluster.Spec.FaultDomain.Value})
	} else {
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: faultDomainSource},
		}})
	}

	initContainer := corev1.Container{
		Name:  "foundationdb-kubernetes-init",
		Image: fmt.Sprintf("%s/foundationdb-kubernetes-sidecar:%s-1", DockerImageRoot, cluster.Spec.Version),
		Env:   sidecarEnv,
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
		},
	}

	sidecarContainer := corev1.Container{
		Name:         "foundationdb-kubernetes-sidecar",
		Image:        initContainer.Image,
		Env:          sidecarEnv[1:],
		VolumeMounts: initContainer.VolumeMounts,
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
				corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
				corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"},
			},
		}}},
		corev1.Volume{Name: "fdb-trace-logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	volumes = append(volumes, cluster.Spec.Volumes...)

	var affinity *corev1.Affinity

	if faultDomainKey != "foundationdb.org/none" && faultDomainKey != "foundationdb.org/kubernetes-cluster" {
		affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					corev1.WeightedPodAffinityTerm{
						Weight: 1,
						PodAffinityTerm: corev1.PodAffinityTerm{
							TopologyKey: faultDomainKey,
							LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
								"fdb-cluster-name":  cluster.ObjectMeta.Name,
								"fdb-process-class": processClass,
							}},
						},
					},
				},
			},
		}
	}

	initContainers := []corev1.Container{initContainer}
	initContainers = append(initContainers, cluster.Spec.InitContainers...)

	containers := []corev1.Container{mainContainer, sidecarContainer}
	containers = append(containers, cluster.Spec.Containers...)

	return &corev1.PodSpec{
		InitContainers: initContainers,
		Containers:     containers,
		Volumes:        volumes,
		Affinity:       affinity,
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

	var idLabel string
	if id > 0 {
		idLabel = strconv.Itoa(id)
	} else {
		idLabel = ""
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      name,
			Labels:    getPodLabels(cluster, processClass, idLabel),
		},
		Spec: spec,
	}, nil
}

func (r *ReconcileFoundationDBCluster) getPodClient(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (FdbPodClient, error) {
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
		err = r.Get(context, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)
		if err != nil {
			return nil, err
		}
		client, err = r.podClientProvider(cluster, pod)
	}
	return client, nil
}

func (r *ReconcileFoundationDBCluster) getPodClientAsync(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod, clientChan chan FdbPodClient, errorChan chan error) {
	client, err := r.getPodClient(context, cluster, pod)
	if err != nil {
		errorChan <- err
	} else {
		clientChan <- client
	}
}

func sortPodsByID(pods *corev1.PodList) error {
	var err error
	sort.Slice(pods.Items, func(i, j int) bool {
		id1, err1 := strconv.Atoi(pods.Items[i].Labels["fdb-instance-id"])
		id2, err2 := strconv.Atoi(pods.Items[j].Labels["fdb-instance-id"])
		if err1 != nil {
			err = err1
		}
		if err2 != nil {
			err = err2
		}
		return id1 < id2
	})
	return err
}

var connectionStringNameRegex, _ = regexp.Compile("[^A-Za-z0-9_]")
