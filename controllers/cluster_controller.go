/*
 * cluster_controller.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"golang.org/x/net/context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FoundationDBClusterReconciler reconciles a FoundationDBCluster object
type FoundationDBClusterReconciler struct {
	client.Client
	Recorder            record.EventRecorder
	Log                 logr.Logger
	InSimulation        bool
	PodLifecycleManager PodLifecycleManager
	PodClientProvider   func(*fdbtypes.FoundationDBCluster, *corev1.Pod) (internal.FdbPodClient, error)

	DatabaseClientProvider DatabaseClientProvider
	DeprecationOptions     internal.DeprecationOptions
	RequeueOnNotFound      bool

	// Deprecated: Use DatabaseClientProvider instead
	AdminClientProvider func(*fdbtypes.FoundationDBCluster, client.Client) (AdminClient, error)

	// Deprecated: Use DatabaseClientProvider instead
	LockClientProvider LockClientProvider
}

// NewFoundationDBClusterReconciler creates a new FoundationDBClusterReconciler with defaults.
func NewFoundationDBClusterReconciler(podLifecycleManager PodLifecycleManager) *FoundationDBClusterReconciler {
	return &FoundationDBClusterReconciler{
		PodLifecycleManager: podLifecycleManager,
		PodClientProvider:   NewFdbPodClient,
	}
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
			if r.RequeueOnNotFound {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil

		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	clusterLog := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name)

	if cluster.Spec.Skip {
		clusterLog.Info("Skipping cluster with skip value true", "skip", cluster.Spec.Skip)
		// Don't requeue
		return ctrl.Result{}, nil
	}

	err = internal.NormalizeClusterSpec(cluster, r.DeprecationOptions)
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
		RemoveProcessGroups{},
		UpdateStatus{},
	}

	originalGeneration := cluster.ObjectMeta.Generation
	normalizedSpec := cluster.Spec.DeepCopy()
	delayedRequeue := false

	for _, subReconciler := range subReconcilers {
		// We have to set the normalized spec here again otherwise any call to Update() for the status of the cluster
		// will reset all normalized fields...
		cluster.Spec = *(normalizedSpec.DeepCopy())
		clusterLog.Info("Attempting to run sub-reconciler", "subReconciler", fmt.Sprintf("%T", subReconciler))

		requeue := subReconciler.Reconcile(r, ctx, cluster)
		if requeue == nil {
			continue
		}

		if requeue.DelayedRequeue {
			clusterLog.Info("Delaying requeue for sub-reconciler",
				"subReconciler", fmt.Sprintf("%T", subReconciler),
				"message", requeue.Message)
			delayedRequeue = true
			continue
		}

		return processRequeue(requeue, subReconciler, cluster, r.Recorder, clusterLog)
	}

	if cluster.Status.Generations.Reconciled < originalGeneration || delayedRequeue {
		clusterLog.Info("Cluster was not fully reconciled by reconciliation process", "status", cluster.Status.Generations)

		return ctrl.Result{Requeue: true}, nil
	}

	clusterLog.Info("Reconciliation complete", "generation", cluster.Status.Generations.Reconciled)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ReconciliationComplete", fmt.Sprintf("Reconciled generation %d", cluster.Status.Generations.Reconciled))

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

func (r *FoundationDBClusterReconciler) updatePodDynamicConf(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (bool, error) {
	if cluster.InstanceIsBeingRemoved(instance.GetInstanceID()) {
		return true, nil
	}
	podClient, message := r.getPodClient(cluster, instance)
	if podClient == nil {
		log.Info("Unable to generate pod client", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", instance.GetInstanceID(), "message", message)
		return false, nil
	}

	var err error

	serversPerPod := 1
	if instance.GetProcessClass() == fdbtypes.ProcessClassStorage {
		serversPerPod, err = getStorageServersPerPodForInstance(&instance)
		if err != nil {
			return false, err
		}
	}

	conf, err := internal.GetMonitorConf(cluster, instance.GetProcessClass(), podClient, serversPerPod)
	if err != nil {
		return false, err
	}

	syncedFDBcluster, clusterErr := internal.UpdateDynamicFiles(podClient, "fdb.cluster", cluster.Status.ConnectionString, func(client internal.FdbPodClient) error { return client.CopyFiles() })
	syncedFDBMonitor, err := internal.UpdateDynamicFiles(podClient, "fdbmonitor.conf", conf, func(client internal.FdbPodClient) error { return client.GenerateMonitorConf() })
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
		return internal.CheckDynamicFilePresent(podClient, fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
	}

	return true, nil
}

func (r *FoundationDBClusterReconciler) getPodClient(cluster *fdbtypes.FoundationDBCluster, instance FdbInstance) (internal.FdbPodClient, string) {
	if instance.Pod == nil {
		return nil, fmt.Sprintf("Instance %s in cluster %s/%s does not have pod defined", instance.GetInstanceID(), cluster.Namespace, cluster.Name)
	}

	pod := instance.Pod
	client, err := r.PodClientProvider(cluster, pod)
	if err != nil {
		return nil, err.Error()
	}

	return client, ""
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
		return internal.GetInstanceIDFromMeta(pods.Items[i].ObjectMeta) < internal.GetInstanceIDFromMeta(pods.Items[j].ObjectMeta)
	})
}

var connectionStringNameRegex, _ = regexp.Compile("[^A-Za-z0-9_]")

// ClusterSubReconciler describes a class that does part of the work of
// reconciliation for a cluster.
type ClusterSubReconciler interface {
	/**
	Reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a	Requeue object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a Requeue object with
	a `Message` field.
	*/
	Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue
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
	Address fdbtypes.ProcessAddress

	// The locality map.
	LocalityData map[string]string

	Class fdbtypes.ProcessClass
}

// Sort processes by their priority and their ID.
// We have to do this to ensure we get a deterministic result for selecting the candidates
// otherwise we get a (nearly) random result since processes are stored in a map which is by definition
// not sorted and doesn't return values in a stable way.
func sortLocalities(cluster *fdbtypes.FoundationDBCluster, processes []localityInfo) {
	// Sort the processes for ID to ensure we have a stable input
	sort.SliceStable(processes, func(i, j int) bool {
		p1 := cluster.GetClassCandidatePriority(processes[i].Class)
		p2 := cluster.GetClassCandidatePriority(processes[j].Class)

		// If both have the same priority sort them by the process ID
		if p1 == p2 {
			return processes[i].ID < processes[j].ID
		}

		// prefer processes with a higher priority
		return p1 > p2
	})
}

// localityInfoForProcess converts the process information from the JSON status
// into locality info for selecting processes.
func localityInfoForProcess(process fdbtypes.FoundationDBStatusProcessInfo, mainContainerTLS bool) (localityInfo, error) {
	addresses, err := fdbtypes.ParseProcessAddressesFromCmdline(process.CommandLine)
	if err != nil {
		return localityInfo{}, err
	}

	var addr fdbtypes.ProcessAddress
	// Iterate the addresses and set the expected address as process address
	// e.g. if we want to use TLS set it to the tls address otherwise use the non-TLS.
	for _, address := range addresses {
		if address.Flags["tls"] == mainContainerTLS {
			addr = address
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
func localityInfoFromSidecar(cluster *fdbtypes.FoundationDBCluster, client internal.FdbPodClient) (localityInfo, error) {
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
// of processes from a set of potential candidates.
func chooseDistributedProcesses(cluster *fdbtypes.FoundationDBCluster, processes []localityInfo, count int, constraint processSelectionConstraint) ([]localityInfo, error) {
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
	sortLocalities(cluster, processes)

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
	allEligible := true

	coordinatorZones := make(map[string]int, len(coordinatorStatus))
	coordinatorDCs := make(map[string]int, len(coordinatorStatus))
	processGroups := make(map[string]*fdbtypes.ProcessGroupStatus)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroups[processGroup.ProcessGroupID] = processGroup
	}

	for _, process := range status.Cluster.Processes {
		if process.Address.IsEmpty() {
			continue
		}

		addresses, err := fdbtypes.ParseProcessAddressesFromCmdline(process.CommandLine)
		if err != nil {
			// We will end here in the error case when the address
			// is not parsable e.g. no IP address is assigned.
			allAddressesValid = false
			continue
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

			if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
				log.Info("Process class of process is not eligible as coordinator", "namespace", cluster.Namespace, "cluster", cluster.Name, "process", process.Locality[fdbtypes.FDBLocalityInstanceIDKey], "class", process.ProcessClass, "address", address)
				allEligible = false
			}
		}

		if address == "" {
			log.Info("Process has invalid address", "namespace", cluster.Namespace, "cluster", cluster.Name, "process", process.Locality[fdbtypes.FDBLocalityInstanceIDKey], "address", address)
			allAddressesValid = false
		}
	}

	desiredCount := cluster.DesiredCoordinatorCount()
	hasEnoughZones := len(coordinatorZones) == desiredCount
	if !hasEnoughZones {
		log.Info("Cluster does not have coordinators in the correct number of zones", "namespace", cluster.Namespace, "cluster", cluster.Name, "desiredCount", desiredCount, "coordinatorZones", coordinatorZones)
	}

	var maxCoordinatorsPerDC int
	hasEnoughDCs := true
	if cluster.Spec.UsableRegions > 1 {
		maxCoordinatorsPerDC = int(math.Floor(float64(desiredCount) / 2.0))

		for dc, count := range coordinatorDCs {
			if count > maxCoordinatorsPerDC {
				log.Info("Cluster has too many coordinators in a single DC", "namespace", cluster.Namespace, "cluster", cluster.Name, "DC", dc, "count", count, "max", maxCoordinatorsPerDC)
				hasEnoughDCs = false
			}
		}
	}

	allHealthy := true
	for address, healthy := range coordinatorStatus {
		allHealthy = allHealthy && healthy

		if !healthy {
			log.Info("Cluster has an unhealthy coordinator", "namespace", cluster.Namespace, "cluster", cluster.Name, "address", address)
		}
	}

	return hasEnoughDCs && hasEnoughZones && allHealthy && allEligible, allAddressesValid, nil
}

// NewFdbPodClient builds a client for working with an FDB Pod
func NewFdbPodClient(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (internal.FdbPodClient, error) {
	return internal.NewFdbPodClient(cluster, pod)
}
