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
	"os"
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
	return AddReconciler(mgr, newReconciler(mgr))
}

var hasStatusSubresource = os.Getenv("HAS_STATUS_SUBRESOURCE") != "0"

const LastPodSpecKey = "org.foundationdb/last-applied-pod-spec"

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFoundationDBCluster{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetRecorder("foundationdbcluster-controller"),
		Scheme:              mgr.GetScheme(),
		PodLifecycleManager: StandardPodLifecycleManager{},
		PodClientProvider:   NewFdbPodClient,
		AdminClientProvider: NewCliAdminClient,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func AddReconciler(mgr manager.Manager, r reconcile.Reconciler) error {
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
	Recorder            record.EventRecorder
	Scheme              *runtime.Scheme
	InSimulation        bool
	PodLifecycleManager PodLifecycleManager
	PodClientProvider   func(*fdbtypes.FoundationDBCluster, *corev1.Pod) (FdbPodClient, error)
	AdminClientProvider func(*fdbtypes.FoundationDBCluster, client.Client) (AdminClient, error)
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

	originalGeneration := cluster.ObjectMeta.Generation

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if originalGeneration == cluster.Status.Generations.Reconciled && hasStatusSubresource {
		return reconcile.Result{}, err
	}

	err = r.setDefaultValues(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updateStatus(context, cluster, false)
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

	err = r.updateLabels(context, cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.addPods(context, cluster)
	if err != nil {
		if r.checkRetryableError(err) {
			return r.Reconcile(request)
		}
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

	err = r.updatePods(context, cluster)
	if err != nil {
		if r.checkRetryableError(err) {
			return r.Reconcile(request)
		}
		return reconcile.Result{}, err
	}

	err = r.updateStatus(context, cluster, true)
	if err != nil {
		return reconcile.Result{}, err
	}

	if cluster.Status.Generations.Reconciled < originalGeneration {
		return reconcile.Result{}, errors.New("Cluster was not fully reconciled by reconciliation process")
	}

	log.Info("Reconcilation complete", "namespace", cluster.Namespace, "cluster", cluster.Name)

	return reconcile.Result{}, nil
}

func (r *ReconcileFoundationDBCluster) checkRetryableError(err error) bool {
	castError, canCast := err.(ReconciliationNotReadyError)
	return canCast && castError.retryable && r.InSimulation
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

func (r *ReconcileFoundationDBCluster) updateStatus(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, updateGenerations bool) error {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.IncorrectProcesses = make(map[string]int64)
	status.MissingProcesses = make(map[string]int64)

	var databaseStatus *fdbtypes.FoundationDBStatus
	processMap := make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)

	if cluster.Spec.Configured {
		adminClient, err := r.AdminClientProvider(cluster, r)
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

		status.DatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.FillInDefaultsFromStatus()
	} else {
		databaseStatus = nil
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return err
	}

	for _, instance := range instances {
		processClass := instance.Metadata.Labels["fdb-process-class"]

		_, pendingRemoval := cluster.Spec.PendingRemovals[instance.Metadata.Name]
		if !pendingRemoval {
			status.ProcessCounts.IncreaseCount(processClass, 1)
		}

		var processStatus []fdbtypes.FoundationDBStatusProcessInfo
		if instance.Pod == nil {
			processStatus = nil
		} else {
			podClient, err := r.getPodClient(context, cluster, instance)
			if err != nil {
				return err
			}
			ip := podClient.GetPodIP()
			processStatus = processMap[ip]
		}
		if len(processStatus) == 0 {
			existingTime, exists := cluster.Status.MissingProcesses[instance.Metadata.Name]
			if exists {
				status.MissingProcesses[instance.Metadata.Name] = existingTime
			} else {
				status.MissingProcesses[instance.Metadata.Name] = time.Now().Unix()
			}
		} else {
			podClient, err := r.getPodClient(context, cluster, instance)
			if err != nil {
				return err
			}

			for _, process := range processStatus {
				commandLine, err := GetStartCommand(cluster, instance, podClient)
				if err != nil {
					return err
				}
				if commandLine != process.CommandLine {
					existingTime, exists := cluster.Status.IncorrectProcesses[instance.Metadata.Name]
					if exists {
						status.IncorrectProcesses[instance.Metadata.Name] = existingTime
					} else {
						status.IncorrectProcesses[instance.Metadata.Name] = time.Now().Unix()
					}
				}
			}
		}
	}

	reconciled := cluster.Spec.Configured &&
		len(cluster.Spec.PendingRemovals) == 0 &&
		cluster.GetProcessCountsWithDefaults().CountsAreSatisfied(status.ProcessCounts) &&
		len(status.IncorrectProcesses) == 0 &&
		databaseStatus != nil &&
		reflect.DeepEqual(status.DatabaseConfiguration, cluster.DesiredDatabaseConfiguration())

	if reconciled && updateGenerations {
		status.Generations = fdbtypes.GenerationStatus{Reconciled: cluster.ObjectMeta.Generation}
	} else {
		status.Generations = cluster.Status.Generations
	}

	if databaseStatus != nil {
		status.Health.Available = databaseStatus.Client.DatabaseStatus.Available
		status.Health.Healthy = databaseStatus.Client.DatabaseStatus.Healthy
		status.Health.FullReplication = databaseStatus.Cluster.FullReplication
		status.Health.DataMovementPriority = databaseStatus.Cluster.Data.MovingData.HighestPriority
	}

	cluster.Status = status
	err = r.postStatusUpdate(context, cluster)
	if err != nil {
		log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) postStatusUpdate(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	if hasStatusSubresource {
		return r.Status().Update(context, cluster)
	} else {
		return r.Update(context, cluster)
	}
}

func (r *ReconcileFoundationDBCluster) updateConfigMap(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return err
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		err = r.Create(context, configMap)
		return err
	} else if err != nil {
		return err
	}

	if !reflect.DeepEqual(existing.Data, configMap.Data) || !reflect.DeepEqual(existing.Labels, configMap.Labels) {
		log.Info("Updating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		r.Recorder.Event(cluster, "Normal", "UpdatingConfigMap", "")
		existing.ObjectMeta.Labels = configMap.ObjectMeta.Labels
		existing.Data = configMap.Data
		err = r.Update(context, existing)
		if err != nil {
			return err
		}
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return err
	}

	podUpdates := make([]chan error, len(instances))
	for index := range instances {
		podUpdates[index] = make(chan error)
		go r.updatePodDynamicConf(context, cluster, instances[index], podUpdates[index])
	}

	for _, podUpdate := range podUpdates {
		err = <-podUpdate
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) updateLabels(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if instance.Pod != nil {
			processClass := instance.Metadata.Labels["fdb-process-class"]
			instanceId := instance.Metadata.Labels["fdb-instance-id"]

			labels := getPodLabels(cluster, processClass, instanceId)
			if !reflect.DeepEqual(instance.Pod.ObjectMeta.Labels, labels) {
				instance.Pod.ObjectMeta.Labels = labels
				err = r.Update(context, instance.Pod)
				if err != nil {
					return err
				}
			}
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, getPodListOptions(cluster, "", ""), pvcs)
	if err != nil {
		return err
	}
	for _, pvc := range pvcs.Items {
		processClass := pvc.ObjectMeta.Labels["fdb-process-class"]
		instanceId := pvc.ObjectMeta.Labels["fdb-instance-id"]

		labels := getPodLabels(cluster, processClass, instanceId)
		if !reflect.DeepEqual(pvc.ObjectMeta.Labels, labels) {
			pvc.ObjectMeta.Labels = labels
			err = r.Update(context, &pvc)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) updatePodDynamicConf(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, signal chan error) {
	client, err := r.getPodClient(context, cluster, instance)
	if err != nil {
		signal <- err
		return
	}

	if instance.Pod == nil {
		signal <- MissingPodError(instance, cluster)
		return
	}

	podClient, err := r.getPodClient(context, cluster, instance)
	if err != nil {
		signal <- err
		return
	}

	conf, err := GetMonitorConf(cluster, instance.Metadata.Labels["fdb-process-class"], instance.Pod, podClient)
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
	labels := getMinimalPodLabels(cluster, processClass, id)

	for label, value := range cluster.Spec.PodLabels {
		labels[label] = value
	}

	return labels
}

func getMinimalPodLabels(cluster *fdbtypes.FoundationDBCluster, processClass string, id string) map[string]string {
	labels := map[string]string{}

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
	return (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingLabels(getMinimalPodLabels(cluster, processClass, id))
}

func getSinglePodListOptions(cluster *fdbtypes.FoundationDBCluster, name string) *client.ListOptions {
	return (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", name)
}

func (r *ReconcileFoundationDBCluster) addPods(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	hasNewPods := false
	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()

	err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	if err != nil {
		return err
	}

	currentPods := &corev1.PodList{}
	err = r.List(context, getPodListOptions(cluster, "", ""), currentPods)
	if err != nil {
		return err
	}
	for _, pod := range currentPods.Items {
		if pod.DeletionTimestamp != nil {
			return ReconciliationNotReadyError{message: "Cluster has pod that is pending deletion", retryable: true}
		}
	}

	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		if desiredCount < 0 {
			desiredCount = 0
		}
		newCount := desiredCount - currentCounts[processClass]
		if newCount > 0 {
			r.Recorder.Event(cluster, "Normal", "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))

			pvcs := &corev1.PersistentVolumeClaimList{}
			r.List(context, getPodListOptions(cluster, processClass, ""), pvcs)
			reusablePvcs := make(map[int]bool, len(pvcs.Items))
			for index, pvc := range pvcs.Items {
				if pvc.Status.Phase == "Bound" && pvc.ObjectMeta.DeletionTimestamp == nil {
					matchingInstances, err := r.PodLifecycleManager.GetInstances(
						r, context,
						getPodListOptions(cluster, processClass, pvc.Labels["fdb-instance-id"]),
					)
					if err != nil {
						return err
					}
					if len(matchingInstances) == 0 {
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

				err = r.PodLifecycleManager.CreateInstance(r, context, pod)
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
					owner, err := buildOwnerReference(context, cluster, r)
					if err != nil {
						return err
					}
					pvc.ObjectMeta.OwnerReferences = owner

					err = r.Create(context, pvc)
					if err != nil {
						return err
					}
				}

				pod, err := GetPod(context, cluster, processClass, id, r)
				if err != nil {
					return err
				}
				err = r.PodLifecycleManager.CreateInstance(r, context, pod)
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
		log.Info("Generating initial cluster file", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing initial coordinators")
		instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "storage", ""))
		if err != nil {
			return err
		}
		count := cluster.DesiredCoordinatorCount()
		if len(instances) < count {
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
			go r.getPodClientAsync(context, cluster, instances[i], clientChan, errChan)
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
	adminClient, err := r.AdminClientProvider(cluster, r)

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
		var enabled = cluster.Spec.AutomationOptions.ConfigureDatabase
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but configuration changes are disabled", configurationString))
			cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
			err = r.postStatusUpdate(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return ReconciliationNotReadyError{message: "Database configuration changes are disabled"}
		}
		log.Info("Configuring database", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ConfiguringDatabase",
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
		log.Info("Configured database", "namespace", cluster.Namespace, "cluster", cluster.Name)
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
		instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, processClass, ""))
		if err != nil {
			return err
		}

		if desiredCount < 0 {
			desiredCount = 0
		}

		removedCount := currentCounts[processClass] - desiredCount
		if removedCount > 0 {
			err = sortInstancesByID(instances)
			if err != nil {
				return err
			}

			r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing %d %s processes", removedCount, processClass))
			for indexOfPod := 0; indexOfPod < removedCount; indexOfPod++ {
				instance := instances[len(instances)-1-indexOfPod]
				podClient, err := r.getPodClient(context, cluster, instance)
				if err != nil {
					return err
				}
				removals[instance.Metadata.Name] = podClient.GetPodIP()
			}
			hasNewRemovals = true
			cluster.Status.ProcessCounts.IncreaseCount(processClass, -1*removedCount)
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		for podName := range cluster.Spec.PendingRemovals {
			if removals[podName] == "" {
				instances, err := r.PodLifecycleManager.GetInstances(r, context, client.InNamespace(cluster.Namespace).MatchingField("metadata.name", podName))
				if err != nil {
					return err
				}
				if len(instances) == 0 {
					delete(removals, podName)
				} else {
					podClient, err := r.getPodClient(context, cluster, instances[0])
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
	adminClient, err := r.AdminClientProvider(cluster, r)
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
		r.Recorder.Event(cluster, "Normal", "ExcludingProcesses", fmt.Sprintf("Excluding %v", addresses))
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
			log.Info("Waiting for exclusions to complete", "namespace", cluster.Namespace, "cluster", cluster.Name, "remainingServers", remaining)
			time.Sleep(time.Second)
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) changeCoordinators(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	if !cluster.Spec.Configured {
		return nil
	}

	adminClient, err := r.AdminClientProvider(cluster, r)
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
		log.Info("Changing coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing new coordinators")
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
	r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", cluster.Spec.PendingRemovals))
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

func (r *ReconcileFoundationDBCluster) removePod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceName string, signal chan error) {
	instanceListOptions := getSinglePodListOptions(cluster, instanceName)
	instances, err := r.PodLifecycleManager.GetInstances(r, context, instanceListOptions)
	if err != nil {
		signal <- err
		return
	}
	if len(instances) == 0 {
		signal <- nil
		return
	}

	err = r.PodLifecycleManager.DeleteInstance(r, context, instances[0])

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", instanceName))
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
		instances, err = r.PodLifecycleManager.GetInstances(r, context, instanceListOptions)
		if err != nil {
			signal <- err
			return
		}
		if len(instances) == 0 {
			break
		}

		log.Info("Waiting for instance get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		time.Sleep(time.Second)
	}

	pods := &corev1.PodList{}
	for {
		err = r.List(context, instanceListOptions, pods)
		if err != nil {
			signal <- err
			return
		}
		if len(pods.Items) == 0 {
			break
		}

		log.Info("Waiting for pod get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
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

		log.Info("Waiting for volume claim get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "name", pvcs.Items[0].Name)
		time.Sleep(time.Second)
	}

	signal <- nil
	return
}

func (r *ReconcileFoundationDBCluster) includeInstances(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, cluster.GetFullAddress(address))
	}

	if len(addresses) > 0 {
		r.Recorder.Event(cluster, "Normal", "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))
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
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return err
	}
	upgraded := false
	image := cluster.Spec.SidecarContainer.ImageName
	if image == "" {
		image = "foundationdb/foundationdb-kubernetes-sidecar"
	}
	image = fmt.Sprintf("%s:%s", image, cluster.GetFullSidecarVersion())
	for _, instance := range instances {
		if instance.Pod == nil {
			return MissingPodError(instance, cluster)
		}
		for containerIndex, container := range instance.Pod.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != image {
				log.Info("Upgrading sidecar", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instance.Pod.Name, "oldImage", container.Image, "newImage", image)
				instance.Pod.Spec.Containers[containerIndex].Image = image
				err := r.Update(context, instance.Pod)
				if err != nil {
					return err
				}
				upgraded = true
			}
		}
	}
	if upgraded {
		r.Recorder.Event(cluster, "Normal", "SidecarUpgraded", fmt.Sprintf("New version: %s", cluster.Spec.Version))
	}
	return nil
}

func (r *ReconcileFoundationDBCluster) bounceProcesses(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Status.IncorrectProcesses))
	for instanceName := range cluster.Status.IncorrectProcesses {
		instances, err := r.PodLifecycleManager.GetInstances(r, context, getSinglePodListOptions(cluster, instanceName))
		if err != nil {
			return err
		}
		if len(instances) == 0 {
			return MissingPodErrorByName(instanceName, cluster)
		}

		podClient, err := r.getPodClient(context, cluster, instances[0])
		if err != nil {
			return err
		}
		addresses = append(addresses, cluster.GetFullAddress(podClient.GetPodIP()))

		signal := make(chan error)
		go r.updatePodDynamicConf(context, cluster, instances[0], signal)
		err = <-signal
		if err != nil {
			return err
		}
	}

	if len(addresses) > 0 {
		var enabled = cluster.Spec.AutomationOptions.KillProcesses
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsBounce",
				fmt.Sprintf("Spec require a bounce of some processes, but killing processes is disabled"))
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.postStatusUpdate(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return ReconciliationNotReadyError{message: "Kills are disabled"}
		}

		log.Info("Bouncing instances", "namespace", cluster.Namespace, "cluster", cluster.Name, "addresses", addresses)
		r.Recorder.Event(cluster, "Normal", "BouncingInstances", fmt.Sprintf("Bouncing processes: %v", addresses))
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

func (r *ReconcileFoundationDBCluster) updatePods(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return err
	}

	updates := make(map[string][]FdbInstance)

	for _, instance := range instances {
		if instance.Pod == nil {
			continue
		}
		spec := GetPodSpec(cluster, instance.Metadata.Labels["fdb-process-class"], fmt.Sprintf("%s-%s", cluster.ObjectMeta.Name, instance.Metadata.Labels["fdb-instance-id"]))
		specBytes, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		if instance.Metadata.Annotations[LastPodSpecKey] != string(specBytes) {
			podClient, err := r.getPodClient(context, cluster, instance)
			if err != nil {
				return err
			}
			substitutions, err := podClient.GetVariableSubstitutions()
			if err != nil {
				return err
			}
			zone := substitutions["FDB_ZONE_ID"]
			if updates[zone] == nil {
				updates[zone] = make([]FdbInstance, 0)
			}
			updates[zone] = append(updates[zone], instance)
		}
	}

	if len(updates) > 0 {
		var enabled = cluster.Spec.AutomationOptions.DeletePods
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsPodsDeletion",
				fmt.Sprintf("Spec require deleting some pods, but deleting pods is disabled"))
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.postStatusUpdate(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return ReconciliationNotReadyError{message: "Pod deletion is disabled"}
		}
	}

	for zone, zoneInstances := range updates {
		log.Info("Deleting pods", "namespace", cluster.Namespace, "cluster", cluster.Name, "zone", zone, "count", len(zoneInstances))
		r.Recorder.Event(cluster, "Normal", "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))
		ready, err := r.PodLifecycleManager.CanDeletePods(r, context, cluster)
		if err != nil {
			return err
		}
		if !ready {
			return ReconciliationNotReadyError{message: "Reconciliation requires deleting pods, but deletion is not currently safe"}
		}
		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, zoneInstances)
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
				conf, err := GetMonitorConf(cluster, processClass, nil, nil)
				if err != nil {
					return nil, err
				}
				data[filename] = conf
			}
		}
	}

	sidecarConf := map[string]interface{}{
		"COPY_BINARIES":            []string{"fdbserver", "fdbcli"},
		"COPY_FILES":               []string{"fdb.cluster", "ca.pem"},
		"COPY_LIBRARIES":           []string{},
		"INPUT_MONITOR_CONF":       "fdbmonitor.conf",
		"ADDITIONAL_SUBSTITUTIONS": cluster.Spec.SidecarVariables,
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
			Namespace:       cluster.Namespace,
			Name:            fmt.Sprintf("%s-config", cluster.Name),
			Labels:          getPodLabels(cluster, "", ""),
			OwnerReferences: owner,
		},
		Data: data,
	}, nil
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod, podClient FdbPodClient) (string, error) {
	if cluster.Spec.ConnectionString == "" {
		return "", nil
	}

	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)
	confLines = append(confLines, "[fdbserver.1]")
	commands, err := getStartCommandLines(cluster, processClass, pod, podClient)
	if err != nil {
		return "", err
	}
	confLines = append(confLines, commands...)
	return strings.Join(confLines, "\n"), nil
}

func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, podClient FdbPodClient) (string, error) {
	if instance.Pod == nil {
		return "", MissingPodError(instance, cluster)
	}

	lines, err := getStartCommandLines(cluster, instance.Metadata.Labels["fdb-process-class"], instance.Pod, podClient)
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

func getStartCommandLines(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod, podClient FdbPodClient) ([]string, error) {
	confLines := make([]string, 0, 20)

	var substitutions map[string]string

	if podClient == nil {
		substitutions = map[string]string{}
	} else {
		subs, err := podClient.GetVariableSubstitutions()
		if err != nil {
			return nil, err
		}
		substitutions = subs
	}

	logGroup := cluster.Spec.LogGroup
	if logGroup == "" {
		logGroup = cluster.Name
	}

	var zoneVariable string
	if strings.HasPrefix(cluster.Spec.FaultDomain.ValueFrom, "$") {
		zoneVariable = cluster.Spec.FaultDomain.ValueFrom
	} else {
		zoneVariable = "$FDB_ZONE_ID"
	}

	confLines = append(confLines,
		fmt.Sprintf("command = /var/dynamic-conf/bin/%s/fdbserver", cluster.Spec.Version),
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		fmt.Sprintf("public_address = %s", cluster.GetFullAddress("$FDB_PUBLIC_IP")),
		fmt.Sprintf("class = %s", processClass),
		"datadir = /var/fdb/data",
		"logdir = /var/log/fdb-trace-logs",
		fmt.Sprintf("loggroup = %s", logGroup),
		fmt.Sprintf("locality_machineid = %s", "$FDB_MACHINE_ID"),
		fmt.Sprintf("locality_zoneid = %s", zoneVariable),
	)

	if cluster.Spec.DataCenter != "" {
		confLines = append(confLines, fmt.Sprintf("locality_dcid = %s", cluster.Spec.DataCenter))
	}

	if cluster.Spec.MainContainer.PeerVerificationRules != "" {
		confLines = append(confLines, fmt.Sprintf("tls_verify_peers = %s", cluster.Spec.MainContainer.PeerVerificationRules))
	}

	confLines = append(confLines, cluster.Spec.CustomParameters...)

	for index, _ := range confLines {
		for key, value := range substitutions {
			confLines[index] = strings.Replace(confLines[index], "$"+key, value, -1)
		}
	}
	return confLines, nil
}

// GetPod builds a pod for a new instance
func GetPod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, processClass string, id int, kubeClient client.Client) (*corev1.Pod, error) {
	name := fmt.Sprintf("%s-%d", cluster.ObjectMeta.Name, id)

	owner, err := buildOwnerReference(context, cluster, kubeClient)
	if err != nil {
		return nil, err
	}
	spec := GetPodSpec(cluster, processClass, name)
	specJson, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			Labels:          getPodLabels(cluster, processClass, strconv.Itoa(id)),
			OwnerReferences: owner,
			Annotations: map[string]string{
				LastPodSpecKey: string(specJson),
			},
		},
		Spec: *spec,
	}, nil
}

func customizeContainer(container *corev1.Container, overrides fdbtypes.ContainerOverrides) {
	envOverrides := make(map[string]bool)
	fullEnv := []corev1.EnvVar{}

	for _, envVar := range overrides.Env {
		fullEnv = append(fullEnv, *envVar.DeepCopy())
		envOverrides[envVar.Name] = true
	}

	for _, envVar := range container.Env {
		if !envOverrides[envVar.Name] {
			fullEnv = append(fullEnv, envVar)
		}
	}

	container.Env = fullEnv

	for _, volume := range overrides.VolumeMounts {
		container.VolumeMounts = append(container.VolumeMounts, *volume.DeepCopy())
	}

	container.SecurityContext = overrides.SecurityContext
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass string, podID string) *corev1.PodSpec {
	imageName := cluster.Spec.MainContainer.ImageName
	if imageName == "" {
		imageName = "foundationdb/foundationdb"
	}
	mainContainer := corev1.Container{
		Name:  "foundationdb",
		Image: fmt.Sprintf("%s:%s", imageName, cluster.Spec.Version),
		Env: []corev1.EnvVar{
			corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
			corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
		},
		Command: []string{"sh", "-c"},
		Args: []string{
			"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
				" --lockfile /var/fdb/fdbmonitor.lockfile",
		},
		Resources: *cluster.Spec.Resources,
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
			corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		},
	}

	customizeContainer(&mainContainer, cluster.Spec.MainContainer)

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
		if !strings.HasPrefix(faultDomainSource, "$") {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: faultDomainSource},
			}})
		}
	}

	sidecarImageName := cluster.Spec.SidecarContainer.ImageName
	if sidecarImageName == "" {
		sidecarImageName = "foundationdb/foundationdb-kubernetes-sidecar"
	}

	initContainer := corev1.Container{
		Name:  "foundationdb-kubernetes-init",
		Image: fmt.Sprintf("%s:%s", sidecarImageName, cluster.GetFullSidecarVersion()),
		Env:   sidecarEnv,
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
		},
	}

	customizeContainer(&initContainer, cluster.Spec.SidecarContainer)

	sidecarEnv = append(sidecarEnv,
		corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: cluster.Spec.SidecarContainer.PeerVerificationRules})
	sidecarEnv = append(sidecarEnv,
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"})

	sidecarContainer := corev1.Container{
		Name:  "foundationdb-kubernetes-sidecar",
		Image: initContainer.Image,
		Env:   sidecarEnv[1:],
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.IntOrString{IntVal: 8080},
				},
			},
		},
	}

	if cluster.Spec.SidecarContainer.EnableTLS {
		sidecarContainer.Args = []string{"--tls"}
	}

	customizeContainer(&sidecarContainer, cluster.Spec.SidecarContainer)

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

	for _, volume := range cluster.Spec.Volumes {
		volumes = append(volumes, *volume.DeepCopy())
	}

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
		InitContainers:  initContainers,
		Containers:      containers,
		Volumes:         volumes,
		Affinity:        affinity,
		SecurityContext: cluster.Spec.PodSecurityContext,
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

func (r *ReconcileFoundationDBCluster) getPodClient(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (FdbPodClient, error) {
	if instance.Pod == nil {
		return nil, MissingPodError(instance, cluster)
	}

	pod := instance.Pod
	client, err := r.PodClientProvider(cluster, pod)
	for err != nil {
		if err == fdbPodClientErrorNoIP {
			log.Info("Waiting for pod to be assigned an IP", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", pod.Name)
		} else if err == fdbPodClientErrorNotReady {
			log.Info("Waiting for pod to be ready", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", pod.Name)
		} else {
			return nil, err
		}
		time.Sleep(time.Second)
		err = r.Get(context, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)
		if err != nil {
			return nil, err
		}
		client, err = r.PodClientProvider(cluster, pod)
	}
	return client, nil
}

func (r *ReconcileFoundationDBCluster) getPodClientAsync(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, clientChan chan FdbPodClient, errorChan chan error) {
	client, err := r.getPodClient(context, cluster, instance)
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

func sortInstancesByID(instances []FdbInstance) error {
	var err error
	sort.Slice(instances, func(i, j int) bool {
		id1, err1 := strconv.Atoi(instances[i].Metadata.Labels["fdb-instance-id"])
		id2, err2 := strconv.Atoi(instances[j].Metadata.Labels["fdb-instance-id"])
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

// FdbInstance represents an instance of FDB that has been configured in
// Kubernetes.
type FdbInstance struct {
	Metadata *metav1.ObjectMeta
	Pod      *corev1.Pod
}

// PodLifecycleManager provides an abstraction around created pods to allow
// using intermediary replication controllers that will manager the basic pod
// lifecycle.
type PodLifecycleManager interface {
	// GetInstances lists the instances in the cluster
	GetInstances(*ReconcileFoundationDBCluster, ctx.Context, *client.ListOptions) ([]FdbInstance, error)

	// CreateInstance creates a new instance based on a pod definition
	CreateInstance(*ReconcileFoundationDBCluster, ctx.Context, *corev1.Pod) error

	// DeleteInstance shuts down an instance
	DeleteInstance(*ReconcileFoundationDBCluster, ctx.Context, FdbInstance) error

	// CanDeletePods checks whether it is safe to delete pods.
	CanDeletePods(*ReconcileFoundationDBCluster, ctx.Context, *fdbtypes.FoundationDBCluster) (bool, error)

	// UpdatePods updates a list of pods to match the latest specs.
	UpdatePods(*ReconcileFoundationDBCluster, ctx.Context, *fdbtypes.FoundationDBCluster, []FdbInstance) error
}

// StandardPodLifecycleManager provides an implementation of PodLifecycleManager
// that directly creates pods.
type StandardPodLifecycleManager struct {
}

func newFdbInstance(pod corev1.Pod) FdbInstance {
	return FdbInstance{Metadata: &pod.ObjectMeta, Pod: &pod}
}

// NamespacedName gets the name of an instance along with its namespace
func (instance FdbInstance) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: instance.Metadata.Namespace, Name: instance.Metadata.Name}
}

// GetInstances returns a list of instances for FDB pods that have been
// created.
func (manager StandardPodLifecycleManager) GetInstances(r *ReconcileFoundationDBCluster, context ctx.Context, options *client.ListOptions) ([]FdbInstance, error) {
	pods := &corev1.PodList{}
	err := r.List(context, options, pods)
	if err != nil {
		return nil, err
	}
	instances := make([]FdbInstance, len(pods.Items))
	for index, pod := range pods.Items {
		instances[index] = newFdbInstance(pod)
	}

	return instances, nil
}

// CreateInstance creates a new instance based on a pod definition
func (manager StandardPodLifecycleManager) CreateInstance(r *ReconcileFoundationDBCluster, context ctx.Context, pod *corev1.Pod) error {
	return r.Create(context, pod)
}

// DeleteInstance shuts down an instance
func (manager StandardPodLifecycleManager) DeleteInstance(r *ReconcileFoundationDBCluster, context ctx.Context, instance FdbInstance) error {
	return r.Delete(context, instance.Pod)
}

// CanDeletePods checks whether it is safe to delete pods.
func (manager StandardPodLifecycleManager) CanDeletePods(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}
	return status.Client.DatabaseStatus.Healthy, nil
}

// UpdatePods updates a list of pods to match the latest specs.
func (manager StandardPodLifecycleManager) UpdatePods(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instances []FdbInstance) error {
	for _, instance := range instances {
		err := r.Delete(context, instance.Pod)
		if err != nil {
			return err
		}
	}
	if len(instances) > 0 {
		return ReconciliationNotReadyError{message: "Need to restart reconciliation to recreate pods", retryable: true}
	}
	return nil
}

// MissingPodError creates an error that can be thrown when an instance does not
// have an associated pod.
func MissingPodError(instance FdbInstance, cluster *fdbtypes.FoundationDBCluster) error {
	return MissingPodErrorByName(instance.Metadata.Name, cluster)
}

// MissingPodErrorByName creates an error that can be thrown when an instance
// does not have an associated pod.
func MissingPodErrorByName(instanceName string, cluster *fdbtypes.FoundationDBCluster) error {
	return fmt.Errorf("Instance %s in cluster %s/%s does not have pod defined", instanceName, cluster.Namespace, cluster.Name)
}

// ReconciliationNotReadyError is returned when reconciliation cannot proceed
// because of a temporary condition or because automation is disabled
type ReconciliationNotReadyError struct {
	message   string
	retryable bool
}

func (err ReconciliationNotReadyError) Error() string {
	return err.message
}
