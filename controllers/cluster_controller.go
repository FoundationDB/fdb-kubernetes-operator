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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient"
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
	Recorder               record.EventRecorder
	Log                    logr.Logger
	InSimulation           bool
	PodLifecycleManager    podmanager.PodLifecycleManager
	PodClientProvider      func(*fdbv1beta2.FoundationDBCluster, *corev1.Pod) (podclient.FdbPodClient, error)
	DatabaseClientProvider DatabaseClientProvider
	DeprecationOptions     internal.DeprecationOptions
	GetTimeout             time.Duration
	PostTimeout            time.Duration
}

// NewFoundationDBClusterReconciler creates a new FoundationDBClusterReconciler with defaults.
func NewFoundationDBClusterReconciler(podLifecycleManager podmanager.PodLifecycleManager) *FoundationDBClusterReconciler {
	r := &FoundationDBClusterReconciler{
		PodLifecycleManager: podLifecycleManager,
	}
	r.PodClientProvider = r.newFdbPodClient

	return r
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods;configmaps;persistentvolumeclaims;events;secrets;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs the reconciliation logic.
func (r *FoundationDBClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}

	err := r.Get(ctx, request.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
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

	storageEngineSupported, err := isStorageEngineSupported(cluster.Spec.Version, cluster.Spec.DatabaseConfiguration.StorageEngine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !storageEngineSupported {
		r.Recorder.Event(cluster, corev1.EventTypeWarning, "Storage engine not supported", fmt.Sprintf("storage engine %s is not supported on version %s", cluster.Spec.DatabaseConfiguration.StorageEngine, cluster.Spec.Version))
		return ctrl.Result{}, fmt.Errorf("storage engine %s is not supported on version %s", cluster.Spec.DatabaseConfiguration.StorageEngine, cluster.Spec.Version)
	}

	subReconcilers := []clusterSubReconciler{
		updateStatus{},
		updateLockConfiguration{},
		updateConfigMap{},
		checkClientCompatibility{},
		replaceMisconfiguredProcessGroups{},
		replaceFailedProcessGroups{},
		deletePodsForBuggification{},
		addProcessGroups{},
		addServices{},
		addPVCs{},
		addPods{},
		generateInitialClusterFile{},
		updateSidecarVersions{},
		updatePodConfig{},
		updateLabels{},
		updateDatabaseConfiguration{},
		chooseRemovals{},
		excludeProcesses{},
		changeCoordinators{},
		bounceProcesses{},
		updatePods{},
		removeServices{},
		removeProcessGroups{},
		updateStatus{},
	}

	originalGeneration := cluster.ObjectMeta.Generation
	normalizedSpec := cluster.Spec.DeepCopy()
	delayedRequeue := false

	for _, subReconciler := range subReconcilers {
		// We have to set the normalized spec here again otherwise any call to Update() for the status of the cluster
		// will reset all normalized fields...
		cluster.Spec = *(normalizedSpec.DeepCopy())
		clusterLog.Info("Attempting to run sub-reconciler", "subReconciler", fmt.Sprintf("%T", subReconciler))

		requeue := subReconciler.reconcile(ctx, r, cluster)
		if requeue == nil {
			continue
		}

		if requeue.delayedRequeue {
			clusterLog.Info("Delaying requeue for sub-reconciler",
				"subReconciler", fmt.Sprintf("%T", subReconciler),
				"message", requeue.message,
				"error", requeue.curError)
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
func (r *FoundationDBClusterReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int, selector metav1.LabelSelector, watchedObjects ...client.Object) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.Pod).Name}
	})
	if err != nil {
		return err
	}

	err = mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Service{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.Service).Name}
	})
	if err != nil {
		return err
	}

	err = mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.PersistentVolumeClaim{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.PersistentVolumeClaim).Name}
	})
	if err != nil {
		return err
	}
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbv1beta2.FoundationDBCluster{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		// Only react on generation changes or annotation changes and only watch
		// resources with the provided label selector.
		WithEventFilter(
			predicate.And(
				labelSelectorPredicate,
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicate.AnnotationChangedPredicate{},
				),
			))

	for _, object := range watchedObjects {
		builder.Owns(object)
	}
	return builder.Complete(r)
}

func (r *FoundationDBClusterReconciler) updatePodDynamicConf(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (bool, error) {
	if cluster.ProcessGroupIsBeingRemoved(podmanager.GetProcessGroupID(cluster, pod)) {
		return true, nil
	}
	podClient, message := r.getPodClient(cluster, pod)
	if podClient == nil {
		log.Info("Unable to generate pod client", "namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", podmanager.GetProcessGroupID(cluster, pod), "message", message)
		return false, nil
	}

	serversPerPod := 1

	processClass, err := podmanager.GetProcessClass(cluster, pod)
	if err != nil {
		return false, err
	}

	if processClass == fdbv1beta2.ProcessClassStorage {
		serversPerPod, err = internal.GetStorageServersPerPodForPod(pod)
		if err != nil {
			return false, err
		}
	}

	var expectedConf string

	imageType := internal.GetImageType(pod)
	if imageType == internal.FDBImageTypeUnified {
		config, err := internal.GetMonitorProcessConfiguration(cluster, processClass, serversPerPod, imageType, nil)
		if err != nil {
			return false, err
		}
		configData, err := json.Marshal(config)
		if err != nil {
			return false, err
		}
		expectedConf = string(configData)
	} else {
		expectedConf, err = internal.GetMonitorConf(cluster, processClass, podClient, serversPerPod)
		if err != nil {
			return false, err
		}
	}

	syncedFDBcluster, clusterErr := podClient.UpdateFile("fdb.cluster", cluster.Status.ConnectionString)
	syncedFDBMonitor, err := podClient.UpdateFile("fdbmonitor.conf", expectedConf)
	if !syncedFDBcluster || !syncedFDBMonitor {
		if clusterErr != nil {
			return false, clusterErr
		}

		return false, err
	}

	if cluster.IsBeingUpgraded() {
		return podClient.IsPresent(fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
	}

	return true, nil
}

func (r *FoundationDBClusterReconciler) getPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (podclient.FdbPodClient, string) {
	if pod == nil {
		return nil, fmt.Sprintf("Process group in cluster %s/%s does not have pod defined", cluster.Namespace, cluster.Name)
	}

	// TODO how to pass this down?
	podClient, err := r.PodClientProvider(cluster, pod)
	if err != nil {
		return nil, err.Error()
	}

	return podClient, ""
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBClusterReconciler) getDatabaseClientProvider() DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}

	panic("Cluster reconciler does not have a DatabaseClientProvider defined")
}

func (r *FoundationDBClusterReconciler) getLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return r.getDatabaseClientProvider().GetLockClient(cluster)
}

// takeLock attempts to acquire a lock.
func (r *FoundationDBClusterReconciler) takeLock(cluster *fdbv1beta2.FoundationDBCluster, action string) (bool, error) {
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

var connectionStringNameRegex, _ = regexp.Compile("[^A-Za-z0-9_]")

// clusterSubReconciler describes a class that does part of the work of
// reconciliation for a cluster.
type clusterSubReconciler interface {
	/**
	reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a	requeue object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a requeue object with
	a `Message` field.
	*/
	reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue
}

// localityInfo captures information about a process for the purposes of
// choosing diverse locality.
type localityInfo struct {
	// The process group ID
	ID string

	// The process's public address.
	Address fdbv1beta2.ProcessAddress

	// The locality map.
	LocalityData map[string]string

	Class fdbv1beta2.ProcessClass
}

// Sort processes by their priority and their ID.
// We have to do this to ensure we get a deterministic result for selecting the candidates
// otherwise we get a (nearly) random result since processes are stored in a map which is by definition
// not sorted and doesn't return values in a stable way.
func sortLocalities(cluster *fdbv1beta2.FoundationDBCluster, processes []localityInfo) {
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
func localityInfoForProcess(process fdbv1beta2.FoundationDBStatusProcessInfo, mainContainerTLS bool) (localityInfo, error) {
	addresses, err := fdbv1beta2.ParseProcessAddressesFromCmdline(process.CommandLine)
	if err != nil {
		return localityInfo{}, err
	}

	var addr fdbv1beta2.ProcessAddress
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
func localityInfoFromSidecar(cluster *fdbv1beta2.FoundationDBCluster, client podclient.FdbPodClient) (localityInfo, error) {
	substitutions, err := client.GetVariableSubstitutions()
	if err != nil {
		return localityInfo{}, err
	}

	if substitutions == nil {
		return localityInfo{}, nil
	}

	// This locality information is only used during the initial cluster file generation.
	// So it should be good to only use the first process address here.
	// This has the implication that in the initial cluster file only the first processes will be used.
	address := cluster.GetFullAddress(substitutions["FDB_PUBLIC_IP"], 1)
	return localityInfo{
		ID:      substitutions["FDB_INSTANCE_ID"],
		Address: address,
		LocalityData: map[string]string{
			fdbv1beta2.FDBLocalityZoneIDKey:  substitutions["FDB_ZONE_ID"],
			fdbv1beta2.FDBLocalityDNSNameKey: substitutions["FDB_DNS_NAME"],
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
func chooseDistributedProcesses(cluster *fdbv1beta2.FoundationDBCluster, processes []localityInfo, count int, constraint processSelectionConstraint) ([]localityInfo, error) {
	chosen := make([]localityInfo, 0, count)
	chosenIDs := make(map[string]bool, count)

	fields := constraint.Fields
	if len(fields) == 0 {
		fields = []string{fdbv1beta2.FDBLocalityZoneIDKey, fdbv1beta2.FDBLocalityDCIDKey}
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

func getHardLimits(cluster *fdbv1beta2.FoundationDBCluster) map[string]int {
	if cluster.Spec.DatabaseConfiguration.UsableRegions <= 1 {
		return map[string]int{fdbv1beta2.FDBLocalityZoneIDKey: 1}
	}

	// TODO (johscheuer): should we calculate that based on the number of DCs?
	maxCoordinatorsPerDC := int(math.Floor(float64(cluster.DesiredCoordinatorCount()) / 2.0))

	return map[string]int{fdbv1beta2.FDBLocalityZoneIDKey: 1, fdbv1beta2.FDBLocalityDCIDKey: maxCoordinatorsPerDC}
}

// checkCoordinatorValidity determines if the cluster's current coordinators
// meet the fault tolerance requirements.
//
// The first return value will be whether the coordinators are valid.
// The second return value will be whether the processes have their TLS flags
// matching the cluster spec.
// The third return value will hold any errors encountered when checking the
// coordinators.
func checkCoordinatorValidity(cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, coordinatorStatus map[string]bool) (bool, bool, error) {
	if len(coordinatorStatus) == 0 {
		return false, false, errors.New("unable to get coordinator status")
	}

	curLog := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name)

	allAddressesValid := true
	allValid := true

	coordinatorZones := make(map[string]int, len(coordinatorStatus))
	coordinatorDCs := make(map[string]int, len(coordinatorStatus))
	processGroups := make(map[string]*fdbv1beta2.ProcessGroupStatus)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroups[processGroup.ProcessGroupID] = processGroup
	}

	runningVersion := cluster.GetRunningVersion()
	for _, process := range status.Cluster.Processes {
		pLogger := curLog.WithValues("process", process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])
		if process.Address.IsEmpty() {
			pLogger.Info("Skip process with empty address")
			continue
		}

		if process.ProcessClass == fdbv1beta2.ProcessClassTest {
			pLogger.Info("Ignoring tester process")
			continue
		}

		processGroupStatus := processGroups[process.Locality["instance_id"]]
		pendingRemoval := processGroupStatus != nil && processGroupStatus.IsMarkedForRemoval()
		if processGroupStatus != nil && cluster.SkipProcessGroup(processGroupStatus) {
			log.Info("Skipping process group with pending Pod",
				"namespace", cluster.Namespace,
				"cluster", cluster.Name,
				"processGroupID", process.Locality["instance_id"],
				"class", process.ProcessClass)
			continue
		}

		addresses, err := fdbv1beta2.ParseProcessAddressesFromCmdline(process.CommandLine)
		if err != nil {
			// We will end here in the error case when the address
			// is not parsable e.g. no IP address is assigned.
			allAddressesValid = false
			// add: command_line
			pLogger.Info("Could not parse address from command_line", "command_line", process.CommandLine)
			continue
		}

		var ipAddress fdbv1beta2.ProcessAddress
		for _, addr := range addresses {
			if addr.Flags["tls"] == cluster.Spec.MainContainer.EnableTLS {
				ipAddress = addr
				break
			}
		}

		coordinatorAddress := ""
		_, isCoordinatorWithIP := coordinatorStatus[ipAddress.String()]
		if isCoordinatorWithIP {
			coordinatorAddress = ipAddress.String()
		}

		dnsName := process.Locality[fdbv1beta2.FDBLocalityDNSNameKey]
		dnsAddress := fdbv1beta2.ProcessAddress{
			StringAddress: dnsName,
			Port:          ipAddress.Port,
			Flags:         ipAddress.Flags,
		}
		_, isCoordinatorWithDNS := coordinatorStatus[dnsAddress.String()]

		if !isCoordinatorWithDNS {
			dnsAddress = ipAddress
			dnsAddress.FromHostname = true
			_, isCoordinatorWithDNS = coordinatorStatus[dnsAddress.String()]
		}

		if isCoordinatorWithDNS {
			coordinatorAddress = dnsAddress.String()
		}

		if coordinatorAddress != "" && !process.Excluded && !pendingRemoval {
			coordinatorStatus[coordinatorAddress] = true
		}

		// Ensure that the coordinator is running in the required version otherwise we have a coordinator that
		// might not be able to talk to other coordinators.
		if process.Version != runningVersion {
			pLogger.Info("Coordinator has wrong version to be eligible", "version", process.Version, "expectedVersion", runningVersion, "address", coordinatorAddress)
			allValid = false
		}

		if coordinatorAddress != "" {
			coordinatorZones[process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]]++
			coordinatorDCs[process.Locality[fdbv1beta2.FDBLocalityDCIDKey]]++

			if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
				pLogger.Info("Process class of process is not eligible as coordinator", "class", process.ProcessClass, "address", coordinatorAddress)
				allValid = false
			}

			useDNS := cluster.UseDNSInClusterFile() && dnsName != ""
			if (isCoordinatorWithIP && useDNS) || (isCoordinatorWithDNS && !useDNS) {
				pLogger.Info("Coordinator is not using the correct address type", "coordinatorList", coordinatorStatus, "address", coordinatorAddress, "expectingDNS", useDNS, "usingDNS", isCoordinatorWithDNS)
				allValid = false
			}
		}

		if ipAddress.IPAddress == nil {
			pLogger.Info("Process has invalid IP address", "addresses", addresses)
			allAddressesValid = false
		}
	}

	desiredCount := cluster.DesiredCoordinatorCount()
	hasEnoughZones := len(coordinatorZones) == desiredCount
	if !hasEnoughZones {
		curLog.Info("Cluster does not have coordinators in the correct number of zones", "desiredCount", desiredCount, "coordinatorZones", coordinatorZones)
	}

	var maxCoordinatorsPerDC int
	hasEnoughDCs := true
	if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 {
		maxCoordinatorsPerDC = int(math.Floor(float64(desiredCount) / 2.0))

		for dc, count := range coordinatorDCs {
			if count > maxCoordinatorsPerDC {
				curLog.Info("Cluster has too many coordinators in a single DC", "DC", dc, "count", count, "max", maxCoordinatorsPerDC)
				hasEnoughDCs = false
			}
		}
	}

	for address, healthy := range coordinatorStatus {
		allValid = allValid && healthy

		if !healthy {
			curLog.Info("Cluster has an unhealthy coordinator", "address", address)
		}
	}

	return hasEnoughDCs && hasEnoughZones && allValid, allAddressesValid, nil
}

// newFdbPodClient builds a client for working with an FDB Pod
func (r *FoundationDBClusterReconciler) newFdbPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (podclient.FdbPodClient, error) {
	return internal.NewFdbPodClient(cluster, pod, log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "pod", pod.Name), r.GetTimeout, r.PostTimeout)
}

func (r *FoundationDBClusterReconciler) getCoordinatorSet(cluster *fdbv1beta2.FoundationDBCluster) (map[string]struct{}, error) {
	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		return map[string]struct{}{}, err
	}
	defer adminClient.Close()

	return adminClient.GetCoordinatorSet()
}

func isStorageEngineSupported(versionString string, storageEngine fdbv1beta2.StorageEngine) (bool, error) {
	version, err := fdbv1beta2.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}
	return version.IsStorageEngineSupported(storageEngine), nil
}
