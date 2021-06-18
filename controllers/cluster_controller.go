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

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"golang.org/x/net/context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var instanceIDRegex = regexp.MustCompile(`^([\w-]+)-(\d+)`)
var processIDRegex = regexp.MustCompile(`^([\w-]+-\d)-\d$`)

// FoundationDBClusterReconciler reconciles a FoundationDBCluster object
type FoundationDBClusterReconciler struct {
	client.Client
	Recorder            record.EventRecorder
	Log                 logr.Logger
	InSimulation        bool
	PodLifecycleManager PodLifecycleManager
	PodClientProvider   func(*fdbtypes.FoundationDBCluster, *corev1.Pod) (FdbPodClient, error)

	DatabaseClientProvider DatabaseClientProvider

	Namespace          string
	DeprecationOptions internal.DeprecationOptions
	RequeueOnNotFound  bool

	// Deprecated: Use DatabaseClientProvider instead
	AdminClientProvider func(*fdbtypes.FoundationDBCluster, client.Client) (AdminClient, error)

	// Deprecated: Use DatabaseClientProvider instead
	LockClientProvider LockClientProvider
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods;configmaps;persistentvolumeclaims;events;secrets;services,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs the reconciliation logic.
func (r *FoundationDBClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	cluster := &fdbtypes.FoundationDBCluster{}

	err := r.Get(ctx, request.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			r.getDatabaseClientProvider().CleanUpCache(request.Namespace, request.Name)

			if r.RequeueOnNotFound {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil

		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	curLogger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name)

	if cluster.Spec.Skip {
		curLogger.Info("Skipping cluster with skip value true", "skip", cluster.Spec.Skip)
		// Don't requeue
		return ctrl.Result{}, nil
	}

	err = internal.NormalizeClusterSpec(&cluster.Spec, r.DeprecationOptions)
	if err != nil {
		return ctrl.Result{}, err
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer adminClient.Close()

	supportedVersion, err := adminClient.VersionSupported(cluster.Spec.Version)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supportedVersion {
		return ctrl.Result{}, fmt.Errorf("version %s is not supported", cluster.Spec.Version)
	}

	subReconcilers := []ClusterSubReconciler{
		UpdateStatus{},
		UpdateLockConfiguration{},
		UpdateConfigMap{},
		CheckClientCompatibility{},
		ReplaceMisconfiguredPods{},
		ReplaceFailedPods{},
		DeletePodsForBuggification{},
		AddProcessGroups{},
		AddServices{},
		AddPVCs{},
		AddPods{},
		GenerateInitialClusterFile{},
		UpdateSidecarVersions{},
		UpdatePodConfig{},
		UpdateLabels{},
		UpdateDatabaseConfiguration{},
		ChooseRemovals{},
		ExcludeInstances{},
		ChangeCoordinators{},
		BounceProcesses{},
		UpdatePods{},
		RemoveServices{},
		RemovePods{},
		UpdateStatus{},
	}

	originalGeneration := cluster.ObjectMeta.Generation
	normalizedSpec := cluster.Spec.DeepCopy()

	for _, subReconciler := range subReconcilers {
		// We have to set the normalized spec here again otherwise any call to Update() for the status of the cluster
		// will reset all normalized fields...
		cluster.Spec = *(normalizedSpec.DeepCopy())
		curLogger.Info("Attempting to run sub-reconciler", "subReconciler", fmt.Sprintf("%T", subReconciler))

		canContinue, err := subReconciler.Reconcile(r, ctx, cluster)
		if !canContinue || err != nil {
			curLogger.Info("Reconciliation terminated early", "lastAction", fmt.Sprintf("%T", subReconciler))
		}

		if err != nil {
			result, err := r.checkRetryableError(err)
			if err != nil {
				curLogger.Error(err, "Error in reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler))
				return ctrl.Result{}, err
			}

			return result, nil
		} else if cluster.ObjectMeta.Generation != originalGeneration {
			curLogger.Info("Ending reconciliation early because cluster has been updated", "lastAction", fmt.Sprintf("%T", subReconciler))
			return ctrl.Result{}, nil
		} else if !canContinue {
			curLogger.Info("Requeuing reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler), "requeueAfter", subReconciler.RequeueAfter())
			return ctrl.Result{Requeue: true, RequeueAfter: subReconciler.RequeueAfter()}, nil
		}
	}

	if cluster.Status.Generations.Reconciled < originalGeneration {
		curLogger.Info("Cluster was not fully reconciled by reconciliation process", "status", cluster.Status)

		return ctrl.Result{Requeue: true}, nil
	}

	curLogger.Info("Reconciliation complete", "generation", cluster.Status.Generations.Reconciled)

	return ctrl.Result{}, nil
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBClusterReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int, watchedObjects ...client.Object) error {
	err := mgr.GetFieldIndexer().IndexField(ctx.Background(), &corev1.Pod{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.Pod).Name}
	})
	if err != nil {
		return err
	}

	err = mgr.GetFieldIndexer().IndexField(ctx.Background(), &corev1.Service{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.Service).Name}
	})
	if err != nil {
		return err
	}

	err = mgr.GetFieldIndexer().IndexField(ctx.Background(), &corev1.PersistentVolumeClaim{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.PersistentVolumeClaim).Name}
	})
	if err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbtypes.FoundationDBCluster{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		// Only react on generation changes or annotation changes
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))
	for _, object := range watchedObjects {
		builder.Owns(object)
	}
	return builder.Complete(r)
}

func (r *FoundationDBClusterReconciler) checkRetryableError(err error) (ctrl.Result, error) {
	notReadyError, canCast := err.(ReconciliationNotReadyError)
	if canCast && notReadyError.retryable {
		log.Info("Retrying reconciliation", "reason", notReadyError.message, "requeueAfter", notReadyError.requeueAfter)
		return ctrl.Result{Requeue: true, RequeueAfter: notReadyError.requeueAfter}, nil
	}

	if k8serrors.IsConflict(err) {
		log.Info("Retrying reconciliation", "reason", "Conflict")
		return ctrl.Result{Requeue: true, RequeueAfter: notReadyError.requeueAfter}, nil
	}

	return ctrl.Result{}, err
}

func (r *FoundationDBClusterReconciler) updatePodDynamicConf(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (bool, error) {
	if cluster.InstanceIsBeingRemoved(instance.GetInstanceID()) {
		return true, nil
	}
	podClient, err := r.getPodClient(cluster, instance)
	if err != nil {
		return false, err
	}

	if instance.Pod == nil {
		return false, MissingPodError(instance, cluster)
	}

	serversPerPod := 1
	if instance.GetProcessClass() == fdbtypes.ProcessClassStorage {
		serversPerPod, err = getStorageServersPerPodForInstance(&instance)
		if err != nil {
			return false, err
		}
	}

	conf, err := GetMonitorConf(cluster, instance.GetProcessClass(), podClient, serversPerPod)
	if err != nil {
		return false, err
	}

	syncedFDBcluster, clusterErr := UpdateDynamicFiles(podClient, "fdb.cluster", cluster.Status.ConnectionString, func(client FdbPodClient) error { return client.CopyFiles() })
	syncedFDBMonitor, err := UpdateDynamicFiles(podClient, "fdbmonitor.conf", conf, func(client FdbPodClient) error { return client.GenerateMonitorConf() })
	if !syncedFDBcluster || !syncedFDBMonitor {
		if clusterErr != nil {
			return false, clusterErr
		}

		return false, err
	}

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return false, err
	}

	if !version.SupportsUsingBinariesFromMainContainer() || cluster.IsBeingUpgraded() {
		return CheckDynamicFilePresent(podClient, fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
	}

	return true, nil
}

func getPodMetadata(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string, specHash string) metav1.ObjectMeta {
	var customMetadata *metav1.ObjectMeta

	processSettings := cluster.GetProcessSettings(processClass)
	if processSettings.PodTemplate != nil {
		customMetadata = &processSettings.PodTemplate.ObjectMeta
	} else {
		customMetadata = nil
	}

	metadata := getObjectMetadata(cluster, customMetadata, processClass, id)

	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[fdbtypes.LastSpecKey] = specHash
	metadata.Annotations[fdbtypes.PublicIPSourceAnnotation] = string(*cluster.Spec.Services.PublicIPSource)

	return metadata
}

func getPvcMetadata(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) metav1.ObjectMeta {
	var customMetadata *metav1.ObjectMeta

	processSettings := cluster.GetProcessSettings(processClass)
	if processSettings.VolumeClaimTemplate != nil {
		customMetadata = &processSettings.VolumeClaimTemplate.ObjectMeta
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

func getObjectMetadata(cluster *fdbtypes.FoundationDBCluster, base *metav1.ObjectMeta, processClass fdbtypes.ProcessClass, id string) metav1.ObjectMeta {
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

	return *metadata
}

func getMinimalPodLabels(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) map[string]string {
	labels := map[string]string{}

	labels[fdbtypes.FDBClusterLabel] = cluster.ObjectMeta.Name

	if processClass != "" {
		labels[fdbtypes.FDBProcessClassLabel] = string(processClass)
	}

	if id != "" {
		labels[fdbtypes.FDBInstanceIDLabel] = id
	}

	return labels
}

func getMinimalSinglePodLabels(cluster *fdbtypes.FoundationDBCluster, id string) map[string]string {
	return getMinimalPodLabels(cluster, "", id)
}

func getPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(getMinimalPodLabels(cluster, processClass, id))}
}

func getSinglePodListOptions(cluster *fdbtypes.FoundationDBCluster, instanceID string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(getMinimalSinglePodLabels(cluster, instanceID))}
}

func buildOwnerReference(ownerType metav1.TypeMeta, ownerMetadata metav1.ObjectMeta) []metav1.OwnerReference {
	var isController = true
	return []metav1.OwnerReference{{
		APIVersion: ownerType.APIVersion,
		Kind:       ownerType.Kind,
		Name:       ownerMetadata.Name,
		UID:        ownerMetadata.UID,
		Controller: &isController,
	}}
}

func setMonitorConfForFilename(cluster *fdbtypes.FoundationDBCluster, data map[string]string, filename string, connectionString string, processClass fdbtypes.ProcessClass, serversPerPod int) error {
	if connectionString == "" {
		data[filename] = ""
	} else {
		conf, err := GetMonitorConf(cluster, processClass, nil, serversPerPod)
		if err != nil {
			return err
		}
		data[filename] = conf
	}

	return nil
}

func getConfigMapMonitorConfEntry(pClass fdbtypes.ProcessClass, serversPerPod int) string {
	if serversPerPod > 1 {
		return fmt.Sprintf("fdbmonitor-conf-%s-density-%d", pClass, serversPerPod)
	}

	return fmt.Sprintf("fdbmonitor-conf-%s", pClass)
}

// GetConfigMap builds a config map for a cluster's dynamic config
func GetConfigMap(cluster *fdbtypes.FoundationDBCluster) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	connectionString := cluster.Status.ConnectionString
	data[clusterFileKey] = connectionString
	data["running-version"] = cluster.Status.RunningVersion

	var caFile strings.Builder
	for _, ca := range cluster.Spec.TrustedCAs {
		if caFile.Len() > 0 {
			caFile.WriteString("\n")
		}
		caFile.WriteString(ca)
	}

	if caFile.Len() > 0 {
		data["ca-file"] = caFile.String()
	}

	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return nil, err
	}
	desiredCounts := desiredCountStruct.Map()

	for processClass, count := range desiredCounts {
		if count > 0 {
			if processClass == fdbtypes.ProcessClassStorage {
				storageServersPerDisk := cluster.Status.StorageServersPerDisk
				// If the status field is not initialized we fallback to only the specified count
				// in the cluster spec. This should only happen in the initial phase of a new cluster.
				if len(cluster.Status.StorageServersPerDisk) == 0 {
					storageServersPerDisk = []int{cluster.GetStorageServersPerPod()}
				}

				for _, serversPerPod := range storageServersPerDisk {
					err := setMonitorConfForFilename(cluster, data, getConfigMapMonitorConfEntry(processClass, serversPerPod), connectionString, processClass, serversPerPod)
					if err != nil {
						return nil, err
					}
				}
				continue
			}

			err := setMonitorConfForFilename(cluster, data, getConfigMapMonitorConfEntry(processClass, 1), connectionString, processClass, 1)
			if err != nil {
				return nil, err
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

	if cluster.Spec.ConfigMap != nil {
		for k, v := range cluster.Spec.ConfigMap.Data {
			data[k] = v
		}
	}

	metadata := getConfigMapMetadata(cluster)
	metadata.OwnerReferences = buildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)

	return &corev1.ConfigMap{
		ObjectMeta: metadata,
		Data:       data,
	}, nil
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, podClient FdbPodClient, serversPerPod int) (string, error) {
	if cluster.Status.ConnectionString == "" {
		return "", nil
	}

	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)

	// Don't instantiate any servers if the `EmptyMonitorConf` buggify option is engaged.
	if !cluster.Spec.Buggify.EmptyMonitorConf {
		for i := 1; i <= serversPerPod; i++ {
			confLines = append(confLines, fmt.Sprintf("[fdbserver.%d]", i))
			commands, err := getStartCommandLines(cluster, processClass, podClient, i, serversPerPod)
			if err != nil {
				return "", err
			}
			confLines = append(confLines, commands...)
		}
	}

	return strings.Join(confLines, "\n"), nil
}

// GetStartCommand builds the expected start command for an instance.
func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, podClient FdbPodClient, processNumber int, processCount int) (string, error) {
	if instance.Pod == nil {
		return "", MissingPodError(instance, cluster)
	}

	lines, err := getStartCommandLines(cluster, instance.GetProcessClass(), podClient, processNumber, processCount)
	if err != nil {
		return "", err
	}

	regex := regexp.MustCompile(`^(\w+)\s*=\s*(.*)`)
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

func getStartCommandLines(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, podClient FdbPodClient, processNumber int, processCount int) ([]string, error) {
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
		fmt.Sprintf("public_address = %s", cluster.GetFullAddressList("$FDB_PUBLIC_IP", false, processNumber)),
		fmt.Sprintf("class = %s", processClass),
		"logdir = /var/log/fdb-trace-logs",
		fmt.Sprintf("loggroup = %s", logGroup))

	if processCount <= 1 {
		confLines = append(confLines, "datadir = /var/fdb/data")
	} else {
		confLines = append(confLines, fmt.Sprintf("datadir = /var/fdb/data/%d", processNumber), fmt.Sprintf("locality_process_id = $FDB_INSTANCE_ID-%d", processNumber))
	}

	confLines = append(confLines,
		"locality_instance_id = $FDB_INSTANCE_ID",
		"locality_machineid = $FDB_MACHINE_ID",
		fmt.Sprintf("locality_zoneid = %s", zoneVariable))

	if cluster.Spec.DataCenter != "" {
		confLines = append(confLines, fmt.Sprintf("locality_dcid = %s", cluster.Spec.DataCenter))
	}

	if cluster.Spec.DataHall != "" {
		confLines = append(confLines, fmt.Sprintf("locality_data_hall = %s", cluster.Spec.DataHall))
	}

	if cluster.Spec.MainContainer.PeerVerificationRules != "" {
		confLines = append(confLines, fmt.Sprintf("tls_verify_peers = %s", cluster.Spec.MainContainer.PeerVerificationRules))
	}

	if cluster.NeedsExplicitListenAddress() {
		confLines = append(confLines, fmt.Sprintf("listen_address = %s", cluster.GetFullAddressList("$FDB_POD_IP", false, processNumber)))
	}

	podSettings := cluster.GetProcessSettings(processClass)

	if podSettings.CustomParameters != nil {
		confLines = append(confLines, *podSettings.CustomParameters...)
	}

	for index := range confLines {
		for key, value := range substitutions {
			confLines[index] = strings.Replace(confLines[index], "$"+key, value, -1)
		}
	}
	return confLines, nil
}

// GetPodSpecHash builds the hash of the expected spec for a pod.
func GetPodSpecHash(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id int, spec *corev1.PodSpec) (string, error) {
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

// getDynamicConfHash gets a hash of the data from the config map holding the
// cluster's dynamic conf.
//
// This will omit keys that we do not expect the Pods to reference e.g. for storage Pods only include the storage config.
func getDynamicConfHash(configMap *corev1.ConfigMap, pClass fdbtypes.ProcessClass, serversPerPod int) (string, error) {
	fields := []string{
		clusterFileKey,
		getConfigMapMonitorConfEntry(pClass, serversPerPod),
		"running-version",
		"ca-file",
		"sidecar-conf",
	}
	var data = make(map[string]string, len(fields))

	for _, field := range fields {
		if val, ok := configMap.Data[field]; ok {
			data[field] = val
		}
	}

	return GetJSONHash(data)
}

func (r *FoundationDBClusterReconciler) getPodClient(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (FdbPodClient, error) {
	if instance.Pod == nil {
		return nil, MissingPodError(instance, cluster)
	}

	pod := instance.Pod
	client, err := r.PodClientProvider(cluster, pod)
	if err == fdbPodClientErrorNoIP {
		return nil, ReconciliationNotReadyError{message: fmt.Sprintf("Waiting for pod %s/%s/%s to be assigned an IP", cluster.Namespace, cluster.Name, pod.Name), retryable: true, requeueAfter: 5 * time.Second}
	} else if err == fdbPodClientErrorNotReady {
		return nil, ReconciliationNotReadyError{message: fmt.Sprintf("Waiting for pod %s/%s/%s to be ready", cluster.Namespace, cluster.Name, pod.Name), retryable: true, requeueAfter: 5 * time.Second}
	} else if err != nil {
		return nil, err
	}

	return client, nil
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBClusterReconciler) getDatabaseClientProvider() DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}
	if r.AdminClientProvider != nil || r.LockClientProvider != nil {
		return legacyDatabaseClientProvider{AdminClientProvider: r.AdminClientProvider, LockClientProvider: r.LockClientProvider}
	}
	panic("Cluster reconciler does not have a DatabaseClientProvider defined")
}

func (r *FoundationDBClusterReconciler) getLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	return r.getDatabaseClientProvider().GetLockClient(cluster)
}

// takeLock attempts to acquire a lock.
func (r *FoundationDBClusterReconciler) takeLock(cluster *fdbtypes.FoundationDBCluster, action string) (bool, error) {
	log.Info("Taking lock on cluster", "namespace", cluster.Namespace, "cluster", cluster.Name, "action", action)
	lockClient, err := r.getLockClient(cluster)
	if err != nil {
		return false, err
	}

	hasLock, err := lockClient.TakeLock()
	if err != nil {
		return false, err
	}

	if !hasLock {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "LockAcquisitionFailed", fmt.Sprintf("Lock required before %s", action))
	}
	return hasLock, nil
}

// clearPendingRemovalsFromSpec removes the pending removals from the cluster spec.
func (r *FoundationDBClusterReconciler) clearPendingRemovalsFromSpec(context ctx.Context, cluster *fdbtypes.FoundationDBCluster) error {
	modifiedCluster := cluster.DeepCopy()
	modifiedCluster.Spec.PendingRemovals = nil
	return r.Update(context, modifiedCluster)
}

func sortPodsByID(pods *corev1.PodList) {
	sort.Slice(pods.Items, func(i, j int) bool {
		return GetInstanceIDFromMeta(pods.Items[i].ObjectMeta) < GetInstanceIDFromMeta(pods.Items[j].ObjectMeta)
	})
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
	UpdatePods(reconciler *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instances []FdbInstance, unsafe bool) error

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
	return metadata.Labels[fdbtypes.FDBInstanceIDLabel]
}

// GetProcessClass fetches the process class from an instance's metadata.
func (instance FdbInstance) GetProcessClass() fdbtypes.ProcessClass {
	return internal.GetProcessClassFromMeta(*instance.Metadata)
}

// GetPublicIPSource determines how an instance has gotten its public IP.
func (instance FdbInstance) GetPublicIPSource() fdbtypes.PublicIPSource {
	source := instance.Metadata.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" {
		return fdbtypes.PublicIPSourcePod
	}
	return fdbtypes.PublicIPSource(source)
}

// GetPublicIPs returns the public IP of an instance.
func (instance FdbInstance) GetPublicIPs() []string {
	if instance.Pod == nil {
		return []string{}
	}

	source := instance.Metadata.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" || source == string(fdbtypes.PublicIPSourcePod) {
		// TODO for dual-stack support return PodIPs
		return []string{instance.Pod.Status.PodIP}
	}

	return []string{instance.Pod.ObjectMeta.Annotations[fdbtypes.PublicIPAnnotation]}
}

// GetProcessID fetches the instance ID from an instance's metadata.
func (instance FdbInstance) GetProcessID(processNumber int) string {
	return fmt.Sprintf("%s-%d", GetInstanceIDFromMeta(*instance.Metadata), processNumber)
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
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
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
func (manager StandardPodLifecycleManager) UpdatePods(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instances []FdbInstance, unsafe bool) error {
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
func ParseInstanceID(id string) (fdbtypes.ProcessClass, int, error) {
	result := instanceIDRegex.FindStringSubmatch(id)
	if result == nil {
		return "", 0, fmt.Errorf("could not parse instance ID %s", id)
	}
	prefix := result[1]
	number, err := strconv.Atoi(result[2])
	if err != nil {
		return "", 0, err
	}
	return fdbtypes.ProcessClass(prefix), number, nil
}

// GetInstanceIDFromProcessID returns the instance ID for the process ID
func GetInstanceIDFromProcessID(id string) string {
	result := processIDRegex.FindStringSubmatch(id)
	if result == nil {
		// In this case we assume that instance ID == process ID
		return id
	}

	return result[1]
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
	message      string
	retryable    bool
	requeueAfter time.Duration
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

	Class fdbtypes.ProcessClass
}

// These indexes are used for sorting and since we sort ascending
func getClassIndex(cls fdbtypes.ProcessClass) int {
	switch cls {
	case fdbtypes.ProcessClassStorage:
		return 0
	case fdbtypes.ProcessClassLog:
		return 1
	case fdbtypes.ProcessClassTransaction:
		return 2
	}

	return math.MaxInt32
}

// This will sort the processes according to the following rules:
// First all storage processes next log processes and then tlog processes.
// Inside each process class the processes will be sorted by the ID (lower IDs come first).
// We have to do this to ensure we get a deterministic result for selecting the candidates
// otherwise we get a (nearly) random result since processes are stored in a map which is by definition
// not sorted and doesn't return values in a stable way.
func sortLocalities(processes []localityInfo) {
	// Sort the processes for ID to ensure we have a stable input
	sort.SliceStable(processes, func(i, j int) bool {
		if processes[i].Class == processes[j].Class {
			return processes[i].ID < processes[j].ID
		}

		return getClassIndex(processes[i].Class) < getClassIndex(processes[j].Class)
	})
}

// localityInfoForProcess converts the process information from the JSON status
// into locality info for selecting processes.
func localityInfoForProcess(process fdbtypes.FoundationDBStatusProcessInfo, mainContainerTLS bool) (localityInfo, error) {
	addresses, err := fdbtypes.ParseProcessAddressesFromCmdline(process.CommandLine)
	if err != nil {
		return localityInfo{}, err
	}

	var addr string
	// Iterate over the addresses and set the expected address as process address
	// e.g. if we want to use TLS set it to the tls address otherwise use the non-TLS.
	for _, address := range addresses {
		if address.Flags["tls"] == mainContainerTLS {
			addr = address.String()
			break
		}
	}

	return localityInfo{
		ID:           process.Locality["instance_id"],
		Address:      addr,
		LocalityData: process.Locality,
		Class:        process.ProcessClass,
	}, nil
}

// localityInfoForProcess converts the process information from the sidecar's
// context into locality info for selecting processes.
func localityInfoFromSidecar(cluster *fdbtypes.FoundationDBCluster, client FdbPodClient) (localityInfo, error) {
	substitutions, err := client.GetVariableSubstitutions()
	if err != nil {
		return localityInfo{}, err
	}

	// This locality information is only used during the initial cluster file generation.
	// So it should be good to only use the first process address here.
	// This has the implication that in the initial cluster file only the first processes will be used.
	address := cluster.GetFullAddress(substitutions["FDB_PUBLIC_IP"], 1)
	return localityInfo{
		ID:      substitutions["FDB_INSTANCE_ID"],
		Address: address,
		LocalityData: map[string]string{
			fdbtypes.FDBLocalityZoneIDKey: substitutions["FDB_ZONE_ID"],
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
		fields = []string{fdbtypes.FDBLocalityZoneIDKey, fdbtypes.FDBLocalityDCIDKey}
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

	// Sort the processes to ensure a deterministic result
	sortLocalities(processes)

	for len(chosen) < count {
		choseAny := false

		for _, process := range processes {
			if chosenIDs[process.ID] {
				continue
			}

			eligible := true
			for _, field := range fields {
				value := process.LocalityData[field]
				if chosenCounts[field][value] >= currentLimits[field] {
					eligible = false
					break
				}
			}

			if !eligible {
				continue
			}

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
				return chosen, notEnoughProcessesError{Desired: count, Chosen: len(chosen), Options: processes}
			}
		}
	}

	return chosen, nil
}

func getHardLimits(cluster *fdbtypes.FoundationDBCluster) map[string]int {
	if cluster.Spec.UsableRegions <= 1 {
		return map[string]int{fdbtypes.FDBLocalityZoneIDKey: 1}
	}

	// TODO (johscheuer): should we calculate that based on the number of DCs?
	maxCoordinatorsPerDC := int(math.Floor(float64(cluster.DesiredCoordinatorCount()) / 2.0))

	return map[string]int{fdbtypes.FDBLocalityZoneIDKey: 1, fdbtypes.FDBLocalityDCIDKey: maxCoordinatorsPerDC}
}

// checkCoordinatorValidity determines if the cluster's current coordinators
// meet the fault tolerance requirements.
//
// The first return value will be whether the coordinators are valid.
// The second return value will be whether the processes have their TLS flags
// matching the cluster spec.
// The third return value will hold any errors encountered when checking the
// coordinators.
func checkCoordinatorValidity(cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBStatus, coordinatorStatus map[string]bool) (bool, bool, error) {
	if len(coordinatorStatus) == 0 {
		return false, false, errors.New("unable to get coordinator status")
	}

	allAddressesValid := true

	coordinatorZones := make(map[string]int, len(coordinatorStatus))
	coordinatorDCs := make(map[string]int, len(coordinatorStatus))
	processGroups := make(map[string]*fdbtypes.ProcessGroupStatus)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroups[processGroup.ProcessGroupID] = processGroup
	}

	for _, process := range status.Cluster.Processes {
		if process.Address == "" {
			continue
		}

		addresses, err := fdbtypes.ParseProcessAddressesFromCmdline(process.CommandLine)
		if err != nil {
			return false, false, err
		}

		var address string
		for _, addr := range addresses {
			if addr.Flags["tls"] == cluster.Spec.MainContainer.EnableTLS {
				address = addr.String()
				break
			}
		}

		_, isCoordinator := coordinatorStatus[address]
		processGroupStatus := processGroups[process.Locality["instance_id"]]
		pendingRemoval := processGroupStatus != nil && processGroupStatus.Remove

		if isCoordinator && !process.Excluded && !pendingRemoval {
			coordinatorStatus[address] = true
		}

		if isCoordinator {
			coordinatorZones[process.Locality[fdbtypes.FDBLocalityZoneIDKey]]++
			coordinatorDCs[process.Locality[fdbtypes.FDBLocalityDCIDKey]]++
		}

		if address == "" {
			log.Info("Process has invalid address", "namespace", cluster.Namespace, "name", cluster.Name, "process", process.Locality[fdbtypes.FDBLocalityInstanceIDKey], "address", address)
			allAddressesValid = false
		}
	}

	desiredCount := cluster.DesiredCoordinatorCount()
	hasEnoughZones := len(coordinatorZones) == desiredCount
	if !hasEnoughZones {
		log.Info("Cluster does not have coordinators in the correct number of zones", "namespace", cluster.Namespace, "name", cluster.Name, "desiredCount", desiredCount, "coordinatorZones", coordinatorZones)
	}

	var maxCoordinatorsPerDC int
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

	return hasEnoughDCs && hasEnoughZones && allHealthy, allAddressesValid, nil
}
