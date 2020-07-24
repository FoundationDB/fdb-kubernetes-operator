/*
 * cluster_controller.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	ctx "context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var instanceIDRegex = regexp.MustCompile("^([\\w-]+)-(\\d+)")

// FoundationDBClusterReconciler reconciles a FoundationDBCluster object
type FoundationDBClusterReconciler struct {
	client.Client
	Recorder            record.EventRecorder
	Log                 logr.Logger
	Scheme              *runtime.Scheme
	InSimulation        bool
	PodLifecycleManager PodLifecycleManager
	PodClientProvider   func(*fdbtypes.FoundationDBCluster, *corev1.Pod) (FdbPodClient, error)
	PodIPProvider       func(*corev1.Pod) string
	AdminClientProvider func(*fdbtypes.FoundationDBCluster, client.Client) (AdminClient, error)
	LockClientProvider  LockClientProvider
	lockClients         map[string]LockClient
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch

// Reconcile runs the reconciliation logic.
func (r *FoundationDBClusterReconciler) Reconcile(request ctrl.Request) (ctrl.Result, error) {
	// your logic here

	cluster := &fdbtypes.FoundationDBCluster{}
	context := ctx.Background()

	err := r.Get(context, request.NamespacedName, cluster)

	originalGeneration := cluster.ObjectMeta.Generation

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer adminClient.Close()

	supportedVersion, err := adminClient.VersionSupported(cluster.Spec.Version)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supportedVersion {
		return ctrl.Result{}, fmt.Errorf("Version %s is not supported", cluster.Spec.Version)
	}

	subReconcilers := []ClusterSubReconciler{
		UpdateStatus{},
		CheckClientCompatibility{},
		CheckInstancesToRemove{},
		ReplaceMisconfiguredPods{},
		AddServices{},
		AddPods{},
		GenerateInitialClusterFile{},
		UpdateSidecarVersions{},
		UpdateConfigMap{},
		UpdateLabels{},
		UpdateDatabaseConfiguration{},
		ChooseRemovals{},
		ChangeCoordinators{},
		ExcludeInstances{},
		BounceProcesses{},
		UpdatePods{},
		RemoveServices{},
		RemovePods{},
		IncludeInstances{},
		UpdateStatus{},
	}

	for _, subReconciler := range subReconcilers {
		canContinue, err := subReconciler.Reconcile(r, context, cluster)
		if !canContinue || err != nil {
			log.Info("Reconciliation terminated early", "namespace", cluster.Namespace, "name", cluster.Name, "lastAction", fmt.Sprintf("%T", subReconciler))
		}

		if err != nil {
			result, err := r.checkRetryableError(err)
			if err != nil {
				log.Error(err, "Error in reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler), "namespace", cluster.Namespace, "cluster", cluster.Name)
				return ctrl.Result{}, err
			}
			return result, nil
		} else if cluster.ObjectMeta.Generation != originalGeneration {
			log.Info("Ending reconciliation early because cluster has been updated")
			return ctrl.Result{}, nil
		} else if !canContinue {
			log.Info("Requeuing reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler), "namespace", cluster.Namespace, "cluster", cluster.Name)
			return ctrl.Result{Requeue: true, RequeueAfter: subReconciler.RequeueAfter()}, nil
		}
	}

	if cluster.Status.Generations.Reconciled < originalGeneration {
		log.Info("Cluster was not fully reconciled by reconciliation process")

		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("Reconciliation complete", "namespace", cluster.Namespace, "cluster", cluster.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mgr.GetFieldIndexer().IndexField(&corev1.Pod{}, "metadata.name", func(o runtime.Object) []string {
		return []string{o.(*corev1.Pod).Name}
	})

	mgr.GetFieldIndexer().IndexField(&corev1.Service{}, "metadata.name", func(o runtime.Object) []string {
		return []string{o.(*corev1.Service).Name}
	})

	mgr.GetFieldIndexer().IndexField(&corev1.PersistentVolumeClaim{}, "metadata.name", func(o runtime.Object) []string {
		return []string{o.(*corev1.PersistentVolumeClaim).Name}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&fdbtypes.FoundationDBCluster{}).
		Complete(r)
}

func (r *FoundationDBClusterReconciler) checkRetryableError(err error) (ctrl.Result, error) {
	notReadyError, canCast := err.(ReconciliationNotReadyError)
	if canCast && notReadyError.retryable {
		log.Info("Retrying reconcilation", "reason", notReadyError.message)
		return ctrl.Result{Requeue: true}, nil
	}
	if k8serrors.IsConflict(err) {
		log.Info("Retrying reconcilation", "reason", "Conflict")
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, err
}

func (r *FoundationDBClusterReconciler) updatePodDynamicConf(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (bool, error) {
	if cluster.InstanceIsBeingRemoved(instance.GetInstanceID()) {
		return true, nil
	}
	client, err := r.getPodClient(context, cluster, instance)
	if err != nil {
		return false, err
	}

	if instance.Pod == nil {
		return false, MissingPodError(instance, cluster)
	}

	conf, err := GetMonitorConf(cluster, instance.GetProcessClass(), instance.Pod, client)
	if err != nil {
		return false, err
	}

	synced, err := UpdateDynamicFiles(client, "fdb.cluster", cluster.Status.ConnectionString, func(client FdbPodClient) error { return client.CopyFiles() })
	if !synced {
		return synced, err
	}

	synced, err = UpdateDynamicFiles(client, "fdbmonitor.conf", conf, func(client FdbPodClient) error { return client.GenerateMonitorConf() })
	if !synced {
		return synced, err
	}

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return false, err
	}

	if !version.SupportsUsingBinariesFromMainContainer() || cluster.IsBeingUpgraded() {
		synced, err = CheckDynamicFilePresent(client, fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
		if !synced {
			return synced, err
		}
	}

	return true, nil
}

func getPodMetadata(cluster *fdbtypes.FoundationDBCluster, processClass string, id string, specHash string) metav1.ObjectMeta {
	var customMetadata *metav1.ObjectMeta

	processSettings := cluster.GetProcessSettings(processClass)
	if cluster.Spec.PodTemplate != nil {
		customMetadata = &cluster.Spec.PodTemplate.ObjectMeta
	} else if processSettings.PodTemplate != nil {
		customMetadata = &processSettings.PodTemplate.ObjectMeta
	} else {
		customMetadata = nil
	}

	metadata := getObjectMetadata(cluster, customMetadata, processClass, id)

	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[LastSpecKey] = specHash

	return metadata
}

func getPvcMetadata(cluster *fdbtypes.FoundationDBCluster, processClass string, id string) metav1.ObjectMeta {
	var customMetadata *metav1.ObjectMeta

	processSettings := cluster.GetProcessSettings(processClass)
	if cluster.Spec.VolumeClaim != nil {
		customMetadata = &cluster.Spec.VolumeClaim.ObjectMeta
	} else if processSettings.VolumeClaim != nil {
		customMetadata = &processSettings.VolumeClaim.ObjectMeta
	} else {
		customMetadata = nil
	}
	return getObjectMetadata(cluster, customMetadata, processClass, id)
}

func getConfigMapMetadata(cluster *fdbtypes.FoundationDBCluster) metav1.ObjectMeta {
	var metadata metav1.ObjectMeta
	if cluster.Spec.ConfigMap != nil {
		metadata = getObjectMetadata(cluster, &cluster.Spec.ConfigMap.ObjectMeta, "", "")
	} else {
		metadata = getObjectMetadata(cluster, nil, "", "")
	}

	if metadata.Name == "" {
		metadata.Name = fmt.Sprintf("%s-config", cluster.Name)
	} else {
		metadata.Name = fmt.Sprintf("%s-%s", cluster.Name, metadata.Name)
	}

	return metadata
}

func getObjectMetadata(cluster *fdbtypes.FoundationDBCluster, base *metav1.ObjectMeta, processClass string, id string) metav1.ObjectMeta {
	var metadata *metav1.ObjectMeta

	if base != nil {
		metadata = base.DeepCopy()
	} else {
		metadata = &metav1.ObjectMeta{}
	}
	metadata.Namespace = cluster.Namespace

	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string)
	}
	for label, value := range getMinimalPodLabels(cluster, processClass, id) {
		metadata.Labels[label] = value
	}
	for label, value := range cluster.Spec.PodLabels {
		metadata.Labels[label] = value
	}

	return *metadata
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

func getPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass string, id string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(getMinimalPodLabels(cluster, processClass, id))}
}

func getSinglePodListOptions(cluster *fdbtypes.FoundationDBCluster, instanceID string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(map[string]string{"fdb-instance-id": instanceID})}
}

func buildOwnerReference(context ctx.Context, ownerType metav1.TypeMeta, ownerMetadata metav1.ObjectMeta, kubeClient client.Client) ([]metav1.OwnerReference, error) {
	var isController = true
	return []metav1.OwnerReference{metav1.OwnerReference{
		APIVersion: ownerType.APIVersion,
		Kind:       ownerType.Kind,
		Name:       ownerMetadata.Name,
		UID:        ownerMetadata.UID,
		Controller: &isController,
	}}, nil
}

// GetConfigMap builds a config map for a cluster's dynamic config
func GetConfigMap(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	connectionString := cluster.Status.ConnectionString
	data["cluster-file"] = connectionString
	data["running-version"] = cluster.Status.RunningVersion

	caFile := ""
	for _, ca := range cluster.Spec.TrustedCAs {
		if caFile != "" {
			caFile += "\n"
		}
		caFile += ca
	}

	if caFile != "" {
		data["ca-file"] = caFile
	}

	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return nil, err
	}
	desiredCounts := desiredCountStruct.Map()

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

	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return nil, err
	}
	needsInstanceIDSubstitution := !version.HasInstanceIDInSidecarSubstitutions()

	substitutionCount := len(cluster.Spec.SidecarVariables)
	if needsInstanceIDSubstitution {
		substitutionCount++
	}

	var substitutionKeys []string

	if substitutionCount > 0 {
		substitutionKeys = make([]string, 0, substitutionCount)
		substitutionKeys = append(substitutionKeys, cluster.Spec.SidecarVariables...)

		if needsInstanceIDSubstitution {
			substitutionKeys = append(substitutionKeys, "FDB_INSTANCE_ID")
		}
	}

	filesToCopy := []string{"fdb.cluster"}

	if len(cluster.Spec.TrustedCAs) > 0 {
		filesToCopy = append(filesToCopy, "ca.pem")
	}

	needsSidecarConf := !version.PrefersCommandLineArgumentsInSidecar() ||
		cluster.Status.NeedsSidecarConfInConfigMap

	if needsSidecarConf {
		sidecarConf := map[string]interface{}{
			"COPY_BINARIES":            []string{"fdbserver", "fdbcli"},
			"COPY_FILES":               filesToCopy,
			"COPY_LIBRARIES":           []string{},
			"INPUT_MONITOR_CONF":       "fdbmonitor.conf",
			"ADDITIONAL_SUBSTITUTIONS": substitutionKeys,
		}
		sidecarConfData, err := json.Marshal(sidecarConf)
		if err != nil {
			return nil, err
		}
		data["sidecar-conf"] = string(sidecarConfData)
	}

	if cluster.Status.PendingRemovals != nil {
		pendingRemovalData, err := json.Marshal(cluster.Status.PendingRemovals)
		if err != nil {
			return nil, err
		}
		data["pending-removals"] = string(pendingRemovalData)
	}

	if cluster.Spec.ConfigMap != nil {
		for k, v := range cluster.Spec.ConfigMap.Data {
			data[k] = v
		}
	}

	owner, err := buildOwnerReference(context, cluster.TypeMeta, cluster.ObjectMeta, kubeClient)
	if err != nil {
		return nil, err
	}

	metadata := getConfigMapMetadata(cluster)
	metadata.OwnerReferences = owner

	return &corev1.ConfigMap{
		ObjectMeta: metadata,
		Data:       data,
	}, nil
}

// GetConfigMapHash gets the hash of the data for a cluster's dynamic config.
func GetConfigMapHash(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (string, error) {
	configMap, err := GetConfigMap(context, cluster, kubeClient)
	if err != nil {
		return "", err
	}
	return GetJSONHash(configMap.Data)
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass string, pod *corev1.Pod, podClient FdbPodClient) (string, error) {
	if cluster.Status.ConnectionString == "" {
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

// GetStartCommand builds the expected start command for an instance.
func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, podClient FdbPodClient) (string, error) {
	if instance.Pod == nil {
		return "", MissingPodError(instance, cluster)
	}

	lines, err := getStartCommandLines(cluster, instance.GetProcessClass(), instance.Pod, podClient)
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

	var binaryDir string

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return nil, err
	}

	if version.SupportsUsingBinariesFromMainContainer() {
		binaryDir = "$BINARY_DIR"
	} else {
		binaryDir = fmt.Sprintf("/var/dynamic-conf/bin/%s", cluster.Spec.Version)
	}

	confLines = append(confLines,
		fmt.Sprintf("command = %s/fdbserver", binaryDir),
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		fmt.Sprintf("public_address = %s", cluster.GetFullAddressList("$FDB_PUBLIC_IP", false)),
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

	if cluster.Spec.DataHall != "" {
		confLines = append(confLines, fmt.Sprintf("locality_data_hall = %s", cluster.Spec.DataHall))
	}

	if cluster.Spec.MainContainer.PeerVerificationRules != "" {
		confLines = append(confLines, fmt.Sprintf("tls_verify_peers = %s", cluster.Spec.MainContainer.PeerVerificationRules))
	}

	podSettings := cluster.GetProcessSettings(processClass)

	if podSettings.CustomParameters != nil {
		confLines = append(confLines, *podSettings.CustomParameters...)
	}

	confLines = append(confLines, cluster.Spec.CustomParameters...)

	for index := range confLines {
		for key, value := range substitutions {
			confLines[index] = strings.Replace(confLines[index], "$"+key, value, -1)
		}
	}
	return confLines, nil
}

// GetPodSpecHash builds the hash of the expected spec for a pod.
func GetPodSpecHash(cluster *fdbtypes.FoundationDBCluster, processClass string, id int, spec *corev1.PodSpec) (string, error) {
	var err error
	if spec == nil {
		spec, err = GetPodSpec(cluster, processClass, id)
		if err != nil {
			return "", err
		}
	}

	return GetJSONHash(spec)
}

// GetJSONHash serializes an object to JSON and takes a hash of the resulting
// JSON.
func GetJSONHash(object interface{}) (string, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	err := encoder.Encode(object)
	if err != nil {
		return "", err
	}
	specHash := hash.Sum(make([]byte, 0))
	return hex.EncodeToString(specHash), nil
}

// GetDynamicConfHash gets a hash of the data from the config map holding the
// cluster's dynamic conf.
//
// This will omit keys that we do not expect the pods to reference.
func GetDynamicConfHash(configMap *corev1.ConfigMap) (string, error) {
	var data = make(map[string]string, len(configMap.Data))
	for key, value := range configMap.Data {
		if key != "pending-removals" {
			data[key] = value
		}
	}
	return GetJSONHash(data)
}

func (r *FoundationDBClusterReconciler) getPodClient(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (FdbPodClient, error) {
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

func (r *FoundationDBClusterReconciler) getLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	if r.lockClients == nil {
		r.lockClients = make(map[string]LockClient)
	}

	cacheKey := fmt.Sprintf("%s/%s", cluster.ObjectMeta.Namespace, cluster.ObjectMeta.Name)
	client, present := r.lockClients[cacheKey]
	var err error
	if !present {
		client, err = r.LockClientProvider(cluster)
		if err != nil {
			return nil, err
		}

		r.lockClients[cacheKey] = client
	}
	return client, nil
}

// getPendingRemovalState builds pending removal state for an instance we want
// to remove.
func (r *FoundationDBClusterReconciler) getPendingRemovalState(instance FdbInstance) fdbtypes.PendingRemovalState {
	state := fdbtypes.PendingRemovalState{
		PodName: instance.Metadata.Name,
	}
	if instance.Pod != nil {
		var ip string
		if r.PodIPProvider == nil {
			ip = instance.Pod.Status.PodIP
		} else {
			ip = r.PodIPProvider(instance.Pod)
		}
		state.Address = ip
	}
	return state
}

// clearPendingRemovalsFromSpec removes the pending removals from the cluster
// spec.
func (r *FoundationDBClusterReconciler) clearPendingRemovalsFromSpec(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	modifiedCluster := &fdbtypes.FoundationDBCluster{}
	err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, modifiedCluster)
	if err != nil {
		return err
	}
	modifiedCluster.Spec.PendingRemovals = nil
	err = r.Update(context, modifiedCluster)
	return err
}

// updatePendingRemovals processes an update to the pending removals for the
// cluster.
//
// This will update both the status and the config map.
func (r *FoundationDBClusterReconciler) updatePendingRemovals(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	err := r.Status().Update(context, cluster)
	if err != nil {
		return err
	}

	metadata := getConfigMapMetadata(cluster)
	configMap := &corev1.ConfigMap{}

	err = r.Get(context, types.NamespacedName{Namespace: metadata.Namespace, Name: metadata.Name}, configMap)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	} else {
		if cluster.Status.PendingRemovals == nil {
			configMap.Data["pending-removals"] = ""
		} else {
			pendingRemovalData, err := json.Marshal(cluster.Status.PendingRemovals)
			if err != nil {
				return err
			}
			configMap.Data["pending-removals"] = string(pendingRemovalData)
		}

		err = r.Update(context, configMap)
		if err != nil {
			return err
		}
	}

	return nil
}

func sortPodsByID(pods *corev1.PodList) error {
	var err error
	sort.Slice(pods.Items, func(i, j int) bool {
		return GetInstanceIDFromMeta(pods.Items[i].ObjectMeta) < GetInstanceIDFromMeta(pods.Items[j].ObjectMeta)
	})
	return err
}

func sortInstancesByID(instances []FdbInstance) error {
	var err error
	sort.Slice(instances, func(i, j int) bool {
		prefix1, id1, err1 := ParseInstanceID(instances[i].GetInstanceID())
		prefix2, id2, err2 := ParseInstanceID(instances[j].GetInstanceID())
		if err1 != nil {
			err = err1
			return false
		}
		if err2 != nil {
			err = err2
			return false
		}
		if prefix1 != prefix2 {
			return prefix1 < prefix2
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
	GetInstances(*FoundationDBClusterReconciler, *fdbtypes.FoundationDBCluster, ctx.Context, ...client.ListOption) ([]FdbInstance, error)

	// CreateInstance creates a new instance based on a pod definition
	CreateInstance(*FoundationDBClusterReconciler, ctx.Context, *corev1.Pod) error

	// DeleteInstance shuts down an instance
	DeleteInstance(*FoundationDBClusterReconciler, ctx.Context, FdbInstance) error

	// CanDeletePods checks whether it is safe to delete pods.
	CanDeletePods(*FoundationDBClusterReconciler, ctx.Context, *fdbtypes.FoundationDBCluster) (bool, error)

	// UpdatePods updates a list of pods to match the latest specs.
	UpdatePods(*FoundationDBClusterReconciler, ctx.Context, *fdbtypes.FoundationDBCluster, []FdbInstance) error

	// UpdateImageVersion updates a container's image.
	UpdateImageVersion(*FoundationDBClusterReconciler, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance, int, string) error

	// UpdateMetadata updates an instance's metadata.
	UpdateMetadata(*FoundationDBClusterReconciler, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance) error

	// InstanceIsUpdated determines whether an instance is up to date.
	//
	// This does not need to check the metadata or the pod spec hash. This only
	// needs to check aspects of the rollout that are not available in the
	// instance metadata.
	InstanceIsUpdated(*FoundationDBClusterReconciler, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance) (bool, error)
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

// GetInstanceID fetches the instance ID from an instance's metadata.
func (instance FdbInstance) GetInstanceID() string {
	return GetInstanceIDFromMeta(*instance.Metadata)
}

// GetInstanceIDFromMeta fetches the instance ID from an object's metadata.
func GetInstanceIDFromMeta(metadata metav1.ObjectMeta) string {
	return metadata.Labels["fdb-instance-id"]
}

// GetProcessClass fetches the process class from an instance's metadata.
func (instance FdbInstance) GetProcessClass() string {
	return GetProcessClassFromMeta(*instance.Metadata)
}

// GetProcessClassFromMeta fetches the process class from an object's metadata.
func GetProcessClassFromMeta(metadata metav1.ObjectMeta) string {
	return metadata.Labels["fdb-process-class"]
}

// GetInstances returns a list of instances for FDB pods that have been
// created.
func (manager StandardPodLifecycleManager) GetInstances(r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster, context ctx.Context, options ...client.ListOption) ([]FdbInstance, error) {
	pods := &corev1.PodList{}
	err := r.List(context, pods, options...)
	if err != nil {
		return nil, err
	}
	instances := make([]FdbInstance, 0, len(pods.Items))
	for _, pod := range pods.Items {
		ownedByCluster := false
		for _, reference := range pod.ObjectMeta.OwnerReferences {
			if reference.UID == cluster.UID {
				ownedByCluster = true
				break
			}
		}
		if ownedByCluster {
			instances = append(instances, newFdbInstance(pod))
		}
	}

	return instances, nil
}

// CreateInstance creates a new instance based on a pod definition
func (manager StandardPodLifecycleManager) CreateInstance(r *FoundationDBClusterReconciler, context ctx.Context, pod *corev1.Pod) error {
	return r.Create(context, pod)
}

// DeleteInstance shuts down an instance
func (manager StandardPodLifecycleManager) DeleteInstance(r *FoundationDBClusterReconciler, context ctx.Context, instance FdbInstance) error {
	return r.Delete(context, instance.Pod)
}

// CanDeletePods checks whether it is safe to delete pods.
func (manager StandardPodLifecycleManager) CanDeletePods(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}
	return status.Client.DatabaseStatus.Healthy, nil
}

// UpdatePods updates a list of pods to match the latest specs.
func (manager StandardPodLifecycleManager) UpdatePods(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instances []FdbInstance) error {
	for _, instance := range instances {
		err := r.Delete(context, instance.Pod)
		if err != nil {
			return err
		}
	}
	if len(instances) > 0 && !r.InSimulation {
		return ReconciliationNotReadyError{message: "Need to restart reconciliation to recreate pods", retryable: true}
	}
	return nil
}

// UpdateImageVersion updates a container's image.
func (manager StandardPodLifecycleManager) UpdateImageVersion(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, containerIndex int, image string) error {
	instance.Pod.Spec.Containers[containerIndex].Image = image
	return r.Update(context, instance.Pod)
}

// UpdateMetadata updates an instance's metadata.
func (manager StandardPodLifecycleManager) UpdateMetadata(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) error {
	instance.Pod.ObjectMeta = *instance.Metadata
	return r.Update(context, instance.Pod)
}

// InstanceIsUpdated determines whether an instance is up to date.
//
// This does not need to check the metadata or the pod spec hash. This only
// needs to check aspects of the rollout that are not available in the
// instance metadata.
func (manager StandardPodLifecycleManager) InstanceIsUpdated(*FoundationDBClusterReconciler, ctx.Context, *fdbtypes.FoundationDBCluster, FdbInstance) (bool, error) {
	return true, nil
}

// ParseInstanceID extracts the components of an instance ID.
func ParseInstanceID(id string) (string, int, error) {
	result := instanceIDRegex.FindStringSubmatch(id)
	if result == nil {
		return "", 0, fmt.Errorf("Could not parse instance ID %s", id)
	}
	prefix := result[1]
	number, err := strconv.Atoi(result[2])
	if err != nil {
		return "", 0, err
	}
	return prefix, number, nil
}

// MissingPodError creates an error that can be thrown when an instance does not
// have an associated pod.
func MissingPodError(instance FdbInstance, cluster *fdbtypes.FoundationDBCluster) error {
	return MissingPodErrorByName(instance.GetInstanceID(), cluster)
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

// ClusterSubReconciler describes a class that does part of the work of
// reconciliation for a cluster.
type ClusterSubReconciler interface {
	/**
	Reconcile runs the reconciler's work.

	If reconciliation can continue, this should return (true, nil).

	If reconciliation encounters an error, this should return (false, err).

	If reconciliation cannot proceed, or if this method has to make a change
	to the cluster spec, this should return (false, nil).

	This method will only be called once for a given instance of the reconciler,
	so you can safely store
	*/
	Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error)

	/**
	RequeueAfter returns the delay before we should run the reconciliation
	again.
	*/
	RequeueAfter() time.Duration
}

// MinimumFDBVersion defines the minimum supported FDB version.
func MinimumFDBVersion() fdbtypes.FdbVersion {
	return fdbtypes.FdbVersion{Major: 6, Minor: 1, Patch: 12}
}

// localityInfo captures information about a process for the purposes of
// choosing diverse locality.
type localityInfo struct {
	// The instance ID
	ID string

	// The process's public address.
	Address string

	// The locality map.
	LocalityData map[string]string
}

// localityInfoForProcess converts the process information from the JSON status
// into locality info for selecting processes.
func localityInfoForProcess(process fdbtypes.FoundationDBStatusProcessInfo) localityInfo {
	return localityInfo{
		ID:           process.Locality["instance_id"],
		Address:      process.Address,
		LocalityData: process.Locality,
	}
}

// localityInfoForProcess converts the process information from the sidecar's
// context into locality info for selecting processes.
func localityInfoFromSidecar(cluster *fdbtypes.FoundationDBCluster, client FdbPodClient) (localityInfo, error) {
	substitutions, err := client.GetVariableSubstitutions()
	if err != nil {
		return localityInfo{}, err
	}

	address := cluster.GetFullAddress(substitutions["FDB_PUBLIC_IP"])
	return localityInfo{
		ID:      substitutions["FDB_INSTANCE_ID"],
		Address: address,
		LocalityData: map[string]string{
			"zoneid": substitutions["FDB_ZONE_ID"],
		},
	}, nil
}

// notEnoughProcessesError is returned when we cannot recruit enough processes.
type notEnoughProcessesError struct {
	// desired defines the number of processes we wanted to recruit.
	Desired int

	// chosen defines the number of processes we were able to recruit.
	Chosen int

	// options defines the processes that we were selecting from.
	Options []localityInfo
}

// Error formats an error message.
func (err notEnoughProcessesError) Error() string {
	return fmt.Sprintf("Could only select %d processes, but %d are required", err.Chosen, err.Desired)
}

// processSelectionConstraint defines constraints on how we choose processes
// in chooseDistributedProcesses
type processSelectionConstraint struct {
	// Fields defines the locality fields we should consider when selecting
	// processes.
	Fields []string

	// HardLimits defines a maximum number of processes to recruit on any single
	// value for a given locality field.
	HardLimits map[string]int
}

// chooseDistributedProcesses recruits a maximally well-distributed set
// of processes from a set of potential workers.
func chooseDistributedProcesses(processes []localityInfo, count int, constraint processSelectionConstraint) ([]localityInfo, error) {
	chosen := make([]localityInfo, 0, count)
	chosenIDs := make(map[string]bool, count)

	fields := constraint.Fields
	if len(fields) == 0 {
		fields = []string{"zoneid", "dcid"}
	}

	chosenCounts := make(map[string]map[string]int, len(fields))
	hardLimits := make(map[string]int, len(fields))
	currentLimits := make(map[string]int, len(fields))

	if constraint.HardLimits != nil {
		for field, limit := range constraint.HardLimits {
			hardLimits[field] = limit
		}
	}

	for _, field := range fields {
		chosenCounts[field] = make(map[string]int)
		if hardLimits[field] == 0 {
			hardLimits[field] = count
		}
		currentLimits[field] = 1
	}

	for len(chosen) < count {
		choseAny := false

		for _, process := range processes {
			if !chosenIDs[process.ID] {
				eligible := true
				for _, field := range fields {
					value := process.LocalityData[field]
					if chosenCounts[field][value] >= currentLimits[field] {
						eligible = false
						break
					}
				}
				if eligible {
					chosen = append(chosen, process)
					chosenIDs[process.ID] = true

					choseAny = true

					for _, field := range fields {
						value := process.LocalityData[field]
						chosenCounts[field][value]++
					}

					if len(chosen) == count {
						break
					}
				}
			}
		}

		if !choseAny {
			incrementedLimits := false
			for indexOfField := len(fields) - 1; indexOfField >= 0; indexOfField-- {
				field := fields[indexOfField]
				if currentLimits[field] < hardLimits[field] {
					currentLimits[field]++
					incrementedLimits = true
					break
				}
			}
			if !incrementedLimits {
				return nil, notEnoughProcessesError{Desired: count, Chosen: len(chosen), Options: processes}
			}
		}
	}

	return chosen, nil
}

// checkCoordinatorValidity determines if the cluster's current coordinators
// meet the fault tolerance requirements.
//
// The first return value will be whether the coordinators are valid.
// The second return value will be whether the processes have their TLS flags
// matching the cluster spec.
// The third return value will hold any errors encountered when checking the
// coordinators.
func checkCoordinatorValidity(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus) (bool, bool, error) {
	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	if len(coordinatorStatus) == 0 {
		return false, false, errors.New("Unable to get coordinator status")
	}

	allAddressesValid := true

	coordinatorZones := make(map[string]int, len(coordinatorStatus))
	coordinatorDCs := make(map[string]int, len(coordinatorStatus))

	removals := cluster.Status.PendingRemovals
	if removals == nil {
		removals = make(map[string]fdbtypes.PendingRemovalState)
	}

	for _, process := range status.Cluster.Processes {
		_, isCoordinator := coordinatorStatus[process.Address]
		_, pendingRemoval := removals[process.Locality["instance_id"]]
		if isCoordinator && !process.Excluded && !pendingRemoval {
			coordinatorStatus[process.Address] = true
		}

		if isCoordinator {
			coordinatorZones[process.Locality["zoneid"]]++
			coordinatorDCs[process.Locality["dcid"]]++
		}

		if process.Address == "" {
			continue
		}
		address, err := fdbtypes.ParseProcessAddress(process.Address)
		if err != nil {
			return false, false, err
		}

		if address.Flags["tls"] != cluster.Spec.MainContainer.EnableTLS {
			allAddressesValid = false
		}
	}

	desiredCount := cluster.DesiredCoordinatorCount()
	hasEnoughZones := len(coordinatorZones) == desiredCount
	if !hasEnoughZones {
		log.Info("Cluster does not have coordinators in the correct number of zones", "namespace", cluster.Namespace, "name", cluster.Name, "desiredCount", desiredCount, "coordinatorZones", coordinatorZones)
	}

	maxCoordinatorsPerDC := desiredCount

	hasEnoughDCs := true

	if cluster.Spec.UsableRegions > 1 {
		maxCoordinatorsPerDC = int(math.Floor(float64(desiredCount) / 2.0))

		for dc, count := range coordinatorDCs {
			if count > maxCoordinatorsPerDC {
				log.Info("Cluster has too many coordinators in a single DC", "namespace", cluster.Namespace, "name", cluster.Name, "DC", dc, "count", count, "max", maxCoordinatorsPerDC)
				hasEnoughDCs = false
			}
		}
	}

	allHealthy := true
	for address, healthy := range coordinatorStatus {
		allHealthy = allHealthy && healthy

		if !healthy {
			log.Info("Cluster has an unhealthy coordinator", "namespace", cluster.Namespace, "name", cluster.Name, "address", address)
		}
	}

	coordinatorsValid := hasEnoughZones && hasEnoughDCs && allHealthy

	return coordinatorsValid, allAddressesValid, nil
}
