/*
 * controller.go
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
	ctx "context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/tools/record"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"

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

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return reconcile.Result{}, err
	}

	supportedVersion, err := adminClient.VersionSupported(cluster.Spec.Version)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !supportedVersion {
		return reconcile.Result{}, fmt.Errorf("Version %s is not supported", cluster.Spec.Version)
	}

	subReconcilers := []SubReconciler{
		SetDefaultValues{},
		UpdateStatus{UpdateGenerations: false},
		UpdateSidecarVersions{},
		UpdateConfigMap{},
		UpdateLabels{},
		AddPods{},
		GenerateInitialClusterFile{},
		UpdateDatabaseConfiguration{},
		ChooseRemovals{},
		ExcludeInstances{},
		ChangeCoordinators{},
		RemovePods{},
		IncludeInstances{},
		BounceProcesses{},
		UpdatePods{},
		UpdateStatus{UpdateGenerations: true},
	}

	for _, subReconciler := range subReconcilers {
		canContinue, err := subReconciler.Reconcile(r, context, cluster)
		if err != nil {
			return r.checkRetryableError(err)
		} else if !canContinue {
			return reconcile.Result{Requeue: true, RequeueAfter: subReconciler.RequeueAfter()}, nil
		}
	}

	if cluster.Status.Generations.Reconciled < originalGeneration {
		log.Info("Cluster was not fully reconciled by reconciliation process")
		return reconcile.Result{Requeue: true}, nil
	}

	log.Info("Reconciliation complete", "namespace", cluster.Namespace, "cluster", cluster.Name)

	return reconcile.Result{}, nil
}

func (r *ReconcileFoundationDBCluster) checkRetryableError(err error) (reconcile.Result, error) {
	notReadyError, canCast := err.(ReconciliationNotReadyError)
	if canCast && notReadyError.retryable {
		log.Info("Retrying reconcilation", "reason", notReadyError.message)
		return reconcile.Result{Requeue: true}, nil
	}
	if k8serrors.IsConflict(err) {
		log.Info("Retrying reconcilation", "reason", "Conflict")
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, err
}

func (r *ReconcileFoundationDBCluster) postStatusUpdate(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	if hasStatusSubresource {
		return r.Status().Update(context, cluster)
	} else {
		return r.Update(context, cluster)
	}
}

func (r *ReconcileFoundationDBCluster) updatePodDynamicConf(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (bool, error) {
	_, pendingRemoval := cluster.Spec.PendingRemovals[instance.Metadata.Name]
	if pendingRemoval {
		return true, nil
	}

	client, err := r.getPodClient(context, cluster, instance)
	if err != nil {
		return false, err
	}

	if instance.Pod == nil {
		return false, MissingPodError(instance, cluster)
	}

	conf, err := GetMonitorConf(cluster, instance.Metadata.Labels["fdb-process-class"], instance.Pod, client)
	if err != nil {
		return false, err
	}

	synced, err := UpdateDynamicFiles(client, "fdb.cluster", cluster.Spec.ConnectionString, func(client FdbPodClient) error { return client.CopyFiles() })
	if !synced {
		return synced, err
	}

	synced, err = UpdateDynamicFiles(client, "fdbmonitor.conf", conf, func(client FdbPodClient) error { return client.GenerateMonitorConf() })
	if !synced {
		return synced, err
	}

	synced, err = CheckDynamicFilePresent(client, fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
	if !synced {
		return synced, err
	}

	return true, nil
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
		fullID := id
		if cluster.Spec.InstanceIDPrefix != "" {
			fullID = fmt.Sprintf("%s-%s", cluster.Spec.InstanceIDPrefix, fullID)
		}
		labels["fdb-full-instance-id"] = fullID
	}

	return labels
}

func getPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass string, id string) *client.ListOptions {
	return (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingLabels(getMinimalPodLabels(cluster, processClass, id))
}

func getSinglePodListOptions(cluster *fdbtypes.FoundationDBCluster, name string) *client.ListOptions {
	return (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", name)
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

	substitutionCount := len(cluster.Spec.SidecarVariables)
	substitutionKeys := make([]string, 0, 1+substitutionCount)
	substitutionKeys = append(substitutionKeys, cluster.Spec.SidecarVariables...)
	substitutionKeys = append(substitutionKeys, "FDB_INSTANCE_ID")

	sidecarConf := map[string]interface{}{
		"COPY_BINARIES":            []string{"fdbserver", "fdbcli"},
		"COPY_FILES":               []string{"fdb.cluster", "ca.pem"},
		"COPY_LIBRARIES":           []string{},
		"INPUT_MONITOR_CONF":       "fdbmonitor.conf",
		"ADDITIONAL_SUBSTITUTIONS": substitutionKeys,
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
		fmt.Sprintf("locality_instance_id = $FDB_INSTANCE_ID"),
		fmt.Sprintf("locality_machineid = $FDB_MACHINE_ID"),
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
	spec := GetPodSpec(cluster, processClass, strconv.Itoa(id))
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

func (r *ReconcileFoundationDBCluster) getPodClient(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (FdbPodClient, error) {
	if instance.Pod == nil {
		return nil, MissingPodError(instance, cluster)
	}

	pod := instance.Pod
	client, err := r.PodClientProvider(cluster, pod)
	if err == fdbPodClientErrorNoIP {
		return nil, ReconciliationNotReadyError{message: fmt.Sprintf("Waiting for pod %s/%s/%s to be assigned an IP", cluster.Namespace, cluster.Name, pod.Name), retryable: true}
	} else if err == fdbPodClientErrorNotReady {
		return nil, ReconciliationNotReadyError{message: fmt.Sprintf("Waiting for pod %s/%s/%s to be ready", cluster.Namespace, cluster.Name, pod.Name), retryable: true}
	} else if err != nil {
		return nil, err
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
	return ReconciliationNotReadyError{
		message:   fmt.Sprintf("Instance %s in cluster %s/%s does not have pod defined", instanceName, cluster.Namespace, cluster.Name),
		retryable: true,
	}
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

/**
This type describes a class that does part of the work of reconciliation.
*/
type SubReconciler interface {
	/**
	Reconcile runs the reconciler's work.

	If reconciliation can continue, this should return (true, nil).

	If reconciliation encounters an error, this should return (false, err).

	If reconciliation cannot proceed, or if this method has to make a change
	to the cluster spec, this should return (false, nil).

	This method will only be called once for a given instance of the reconciler,
	so you can safely store
	*/
	Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error)

	/**
	RequeueAfter returns the delay before we should run the reconciliation
	again.
	*/
	RequeueAfter() time.Duration
}
