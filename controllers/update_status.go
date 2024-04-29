/*
 * update_status.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal/locality"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

// updateStatus provides a reconciliation step for updating the status in the
// CRD.
type updateStatus struct{}

// reconcile runs the reconciler's work.
func (updateStatus) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, databaseStatus *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	originalStatus := cluster.Status.DeepCopy()
	clusterStatus := fdbv1beta2.FoundationDBClusterStatus{}
	clusterStatus.Generations.Reconciled = cluster.Status.Generations.Reconciled
	clusterStatus.ProcessGroups = cluster.Status.ProcessGroups
	// Initialize with the current desired storage servers per Pod
	clusterStatus.StorageServersPerDisk = []int{cluster.GetStorageServersPerPod()}
	clusterStatus.LogServersPerDisk = []int{cluster.GetLogServersPerPod()}
	clusterStatus.ImageTypes = []fdbv1beta2.ImageType{fdbv1beta2.ImageType(internal.GetDesiredImageType(cluster))}
	processMap := make(map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo)

	if databaseStatus == nil {
		var err error
		databaseStatus, err = r.getStatusFromClusterOrDummyStatus(logger, cluster)
		if err != nil {
			return &requeue{curError: fmt.Errorf("update_status error fetching status: %w", err), delayedRequeue: true}
		}
	}

	versionMap := map[string]int{}
	for _, process := range databaseStatus.Cluster.Processes {
		versionMap[process.Version]++
		// Ignore all processes for the process map that are for a different data center
		if !cluster.ProcessSharesDC(process) {
			continue
		}

		processID, ok := process.Locality[fdbv1beta2.FDBLocalityProcessIDKey]
		// if the processID is not set we fall back to the instanceID
		if !ok {
			processID = process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
		}
		processMap[fdbv1beta2.ProcessGroupID(processID)] = append(processMap[fdbv1beta2.ProcessGroupID(processID)], process)
	}

	// Update the running version based on the reported version of the FDB processes
	version, err := getRunningVersion(logger, versionMap, cluster.Status.RunningVersion)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in getRunningVersion: %w", err)}
	}
	cluster.Status.RunningVersion = version

	clusterStatus.HasListenIPsForAllPods = cluster.NeedsExplicitListenAddress()
	// Update the configuration if the database is available, otherwise the machine-readable status will contain no information
	// about the current database configuration, leading to a wrong signal that the database configuration must be changed as
	// the configuration will be overwritten with the default values.
	if databaseStatus.Client.DatabaseStatus.Available {
		clusterStatus.DatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.NormalizeConfigurationWithSeparatedProxies(cluster.Spec.Version, cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured())
		// Removing excluded servers as we don't want them during comparison.
		clusterStatus.DatabaseConfiguration.ExcludedServers = nil
		cluster.ClearMissingVersionFlags(&clusterStatus.DatabaseConfiguration)
	}

	// If we saw at least once that the cluster was configured, we assume that the cluster is always configured.
	clusterStatus.Configured = cluster.Status.Configured || (databaseStatus.Client.DatabaseStatus.Available && databaseStatus.Cluster.Layers.Error != "configurationMissing")

	if cluster.Spec.MainContainer.EnableTLS {
		clusterStatus.RequiredAddresses.TLS = true
	} else {
		clusterStatus.RequiredAddresses.NonTLS = true
	}

	if databaseStatus != nil {
		for _, coordinator := range databaseStatus.Client.Coordinators.Coordinators {
			address, err := fdbv1beta2.ParseProcessAddress(coordinator.Address.String())
			if err != nil {
				return &requeue{curError: fmt.Errorf("update_status skipped due to error in ParseProcessAddress: %w", err)}
			}

			if address.Flags["tls"] {
				clusterStatus.RequiredAddresses.TLS = true
			} else {
				clusterStatus.RequiredAddresses.NonTLS = true
			}
		}

		clusterStatus.Health.Available = databaseStatus.Client.DatabaseStatus.Available
		clusterStatus.Health.Healthy = databaseStatus.Client.DatabaseStatus.Healthy
		clusterStatus.Health.FullReplication = databaseStatus.Cluster.FullReplication
		clusterStatus.Health.DataMovementPriority = databaseStatus.Cluster.Data.MovingData.HighestPriority
	}

	cluster.Status.RequiredAddresses = clusterStatus.RequiredAddresses

	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in GetConfigMap: %w", err)}
	}

	updateFaultDomains(logger, processMap, &clusterStatus)
	pvcs, err := refreshProcessGroupStatus(ctx, r, cluster, &clusterStatus)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in refreshProcessGroupStatus: %w", err)}
	}

	err = validateProcessGroups(ctx, r, cluster, &clusterStatus, processMap, configMap, pvcs, logger)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in validateProcessGroups: %w", err)}
	}

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		clusterStatus.HasIncorrectConfigMap = true
	} else if err != nil {
		return &requeue{curError: err}
	}

	clusterStatus.RunningVersion = cluster.Status.RunningVersion

	if clusterStatus.RunningVersion == "" {
		version, present := existingConfigMap.Data["running-version"]
		if present {
			clusterStatus.RunningVersion = version
		}
	}

	if clusterStatus.RunningVersion == "" {
		clusterStatus.RunningVersion = cluster.Spec.Version
	}

	clusterStatus.ConnectionString = cluster.Status.ConnectionString
	if clusterStatus.ConnectionString == "" {
		clusterStatus.ConnectionString = existingConfigMap.Data[internal.ClusterFileKey]
	}

	if clusterStatus.ConnectionString == "" {
		clusterStatus.ConnectionString = cluster.Spec.SeedConnectionString
	}

	clusterStatus.HasIncorrectConfigMap = clusterStatus.HasIncorrectConfigMap || !equality.Semantic.DeepEqual(existingConfigMap.Data, configMap.Data) || !metadataMatches(existingConfigMap.ObjectMeta, configMap.ObjectMeta)

	service := internal.GetHeadlessService(cluster)
	existingService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, existingService)
	if err != nil && k8serrors.IsNotFound(err) {
		existingService = nil
	} else if err != nil {
		return &requeue{curError: err}
	}

	clusterStatus.HasIncorrectServiceConfig = (service == nil) != (existingService == nil)

	if clusterStatus.Configured && cluster.Status.ConnectionString != "" {
		coordinatorStatus := make(map[string]bool, len(databaseStatus.Client.Coordinators.Coordinators))
		for _, coordinator := range databaseStatus.Client.Coordinators.Coordinators {
			coordinatorStatus[coordinator.Address.String()] = false
		}

		coordinatorsValid, _, err := locality.CheckCoordinatorValidity(logger, cluster, databaseStatus, coordinatorStatus)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		clusterStatus.NeedsNewCoordinators = !coordinatorsValid
	}

	if len(cluster.Spec.LockOptions.DenyList) > 0 && cluster.ShouldUseLocks() && clusterStatus.Configured {
		lockClient, err := r.getLockClient(cluster)
		if err != nil {
			return &requeue{curError: err}
		}
		denyList, err := lockClient.GetDenyList()
		if err != nil {
			return &requeue{curError: err}
		}
		if len(denyList) == 0 {
			denyList = nil
		}
		clusterStatus.Locks.DenyList = denyList
	}

	// Sort slices that are assembled based on pods to prevent a reordering from
	// issuing a new reconcile loop.
	sort.Ints(clusterStatus.StorageServersPerDisk)
	sort.Ints(clusterStatus.LogServersPerDisk)
	sort.Slice(clusterStatus.ImageTypes, func(i int, j int) bool {
		return string(clusterStatus.ImageTypes[i]) < string(clusterStatus.ImageTypes[j])
	})

	// Sort ProcessGroups by ProcessGroupID otherwise this can result in an endless loop when the
	// order changes.
	sort.SliceStable(clusterStatus.ProcessGroups, func(i, j int) bool {
		return clusterStatus.ProcessGroups[i].ProcessGroupID < clusterStatus.ProcessGroups[j].ProcessGroupID
	})

	cluster.Status = clusterStatus

	reconciled, err := cluster.CheckReconciliation(logger)
	if err != nil {
		return &requeue{curError: err}
	}

	if reconciled {
		// Once the cluster is reconciled the operator will release any pending locks for this cluster.
		lockErr := r.releaseLock(logger, cluster)
		if lockErr != nil {
			return &requeue{curError: lockErr}
		}
	}

	// See: https://github.com/kubernetes-sigs/kubebuilder/issues/592
	// If we use the default reflect.DeepEqual method it will be recreating the
	// clusterStatus multiple times because the pointers are different.
	if !equality.Semantic.DeepEqual(cluster.Status, *originalStatus) {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			logger.Error(err, "Error updating cluster clusterStatus")
			return &requeue{curError: err}
		}
	}

	return nil
}

// containsAll determines if one map contains all the keys and matching values
// from another map.
func containsAll(current map[string]string, desired map[string]string) bool {
	for key, value := range desired {
		if current[key] != value {
			return false
		}
	}

	return true
}

// optionList creates an order-preserved unique list
func optionList(options ...string) []string {
	valueMap := make(map[string]bool, len(options))
	values := make([]string, 0, len(options))
	for _, option := range options {
		if option != "" && !valueMap[option] {
			values = append(values, option)
			valueMap[option] = true
		}
	}
	return values
}

// tryConnectionOptions attempts to connect with all the connection strings for this cluster and
// returns the connection string that allows connecting to the cluster.
func tryConnectionOptions(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, r *FoundationDBClusterReconciler) (string, error) {
	connectionStrings := optionList(cluster.Status.ConnectionString, cluster.Spec.SeedConnectionString)
	logger.Info("Trying connection options", "connectionString", connectionStrings)

	originalConnectionString := cluster.Status.ConnectionString
	defer func() { cluster.Status.ConnectionString = originalConnectionString }()

	var err error
	for _, connectionString := range connectionStrings {
		logger.Info("Attempting to get connection string from cluster", "connectionString", connectionString)
		cluster.Status.ConnectionString = connectionString
		adminClient, clientErr := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
		if clientErr != nil {
			return originalConnectionString, clientErr
		}

		// If the cluster is not yet configured, we can reduce the timeout to make sure the initial reconcile steps
		// are faster.
		if !cluster.Status.Configured {
			adminClient.SetTimeout(10 * time.Second)
		}

		var activeConnectionString string
		activeConnectionString, err = adminClient.GetConnectionString()

		closeErr := adminClient.Close()
		if closeErr != nil {
			logger.V(1).Info("Could not close admin client", "error", closeErr)
		}

		if err == nil {
			logger.Info("Chose connection option", "connectionString", activeConnectionString)
			return activeConnectionString, nil
		}
		logger.Error(err, "Error getting connection string from cluster", "connectionString", connectionString)
	}

	return originalConnectionString, nil
}

// checkAndSetProcessStatus checks the status of the Process and if missing or incorrect add it to the related status field
func checkAndSetProcessStatus(logger logr.Logger, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo, processCount int, processGroupStatus *fdbv1beta2.ProcessGroupStatus) error {
	// Only perform any process specific validation if the machine-readable status has at least one process. We can improve this check
	// later by validating additional messages in the machine-readable status.
	if len(processMap) == 0 {
		return nil
	}

	var excluded, hasIncorrectCommandLine, hasMissingProcesses, sidecarUnreachable bool
	var substitutions map[string]string
	var err error

	// Fetch the pod client and variables once per Pod.
	podClient, message := r.getPodClient(cluster, pod)
	if podClient == nil {
		logger.Info("Unable to build pod client", "processGroupID", processGroupStatus.ProcessGroupID, "message", message)
		// As the Pod is not ready we can assume that the sidecar is not reachable.
		sidecarUnreachable = true
	} else {
		substitutions, err = podClient.GetVariableSubstitutions()
		if err != nil {
			if internal.IsNetworkError(err) {
				sidecarUnreachable = true
			}
		}
	}

	versionCompatibleUpgrade := cluster.VersionCompatibleUpgradeInProgress()
	for processNumber := 1; processNumber <= processCount; processNumber++ {
		var processID fdbv1beta2.ProcessGroupID
		if processCount > 1 {
			processID = fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%d", processGroupStatus.ProcessGroupID, processNumber))
		} else {
			processID = processGroupStatus.ProcessGroupID
		}

		processStatus := processMap[processID]
		if !hasMissingProcesses {
			hasMissingProcesses = len(processStatus) == 0
		}

		if len(processStatus) == 0 {
			continue
		}

		for _, process := range processStatus {
			// Check if the process is reporting any messages, those will normally include error messages.
			if len(process.Messages) > 0 {
				logger.Info("found error message(s) for the process", "processGroupID", processGroupStatus.ProcessGroupID, "messages", process.Messages)
			}

			if !excluded {
				excluded = process.Excluded
			}

			if len(substitutions) == 0 {
				continue
			}

			commandLine, err := internal.GetStartCommandWithSubstitutions(cluster, processGroupStatus.ProcessClass, substitutions, processNumber, processCount)
			if err != nil {
				return err
			}

			// If a version compatible upgrade is in progress, skip the version check since we will run a mixed set of versions
			// until the cluster is fully reconciled.
			versionMatch := true
			if !versionCompatibleUpgrade {
				versionMatch = process.Version == cluster.Spec.Version || process.Version == fmt.Sprintf("%s-PRERELEASE", cluster.Spec.Version)
			}

			// If the `EmptyMonitorConf` is set, the commandline is by definition wrong since there should be no running processes.
			if !(commandLine == process.CommandLine && versionMatch && !cluster.Spec.Buggify.EmptyMonitorConf) {
				logger.Info("IncorrectProcess",
					"expected", commandLine, "got", process.CommandLine,
					"expectedVersion", cluster.Spec.Version,
					"version", process.Version,
					"processGroupID", processGroupStatus.ProcessGroupID,
					"emptyMonitorConf", cluster.Spec.Buggify.EmptyMonitorConf)
				hasIncorrectCommandLine = true
			}
		}
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.MissingProcesses, hasMissingProcesses)
	processGroupStatus.UpdateCondition(fdbv1beta2.SidecarUnreachable, sidecarUnreachable)
	// If the processes are absent, we are not able to determine the state of the processes and therefore we won't change it.
	if hasMissingProcesses {
		return nil
	}
	processGroupStatus.UpdateCondition(fdbv1beta2.ProcessIsMarkedAsExcluded, excluded)
	// If the sidecar is unreachable we are not able to compute the desired commandline.
	if sidecarUnreachable {
		return nil
	}
	processGroupStatus.UpdateCondition(fdbv1beta2.IncorrectCommandLine, hasIncorrectCommandLine)

	return nil
}

// Validate and set progressGroup's status
func validateProcessGroups(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBClusterStatus, processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo, configMap *corev1.ConfigMap, pvcs *corev1.PersistentVolumeClaimList, logger logr.Logger) error {
	processGroupsWithoutExclusion := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion))
	for _, processGroupID := range cluster.Spec.ProcessGroupsToRemoveWithoutExclusion {
		processGroupsWithoutExclusion[processGroupID] = fdbv1beta2.None{}
	}

	pvcMap := internal.CreatePVCMap(cluster, pvcs)

	disableTaintFeature := cluster.IsTaintFeatureDisabled()
	if disableTaintFeature {
		logger.Info("Disable taint feature", "Disabled", disableTaintFeature)
	}

	for _, processGroup := range status.ProcessGroups {
		// If the process group should be removed mark it for removal.
		if cluster.ProcessGroupIsBeingRemoved(processGroup.ProcessGroupID) {
			processGroup.MarkForRemoval()
			// Check if we should skip exclusion for the process group
			_, ok := processGroupsWithoutExclusion[processGroup.ProcessGroupID]
			// If the process group should be removed without exclusion or the process class is test, remove it without
			// further checks. For the test processes there is no reason to try to exclude them as they are not maintaining
			// any data.
			if ok || processGroup.ProcessClass == fdbv1beta2.ProcessClassTest {
				processGroup.ExclusionSkipped = ok
				processGroup.SetExclude()
			}
		}

		pod, podError := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		if podError != nil {
			// If the process group is not being removed and the Pod is not set we need to put it into
			// the failing list.
			if k8serrors.IsNotFound(podError) {
				// Mark process groups as terminating if the pod has been deleted but other
				// resources are stuck in terminating.
				if processGroup.IsMarkedForRemoval() && processGroup.IsExcluded() {
					processGroup.UpdateCondition(fdbv1beta2.ResourcesTerminating, true)
				} else {
					processGroup.UpdateCondition(fdbv1beta2.MissingPod, true)
				}

				processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, false)
				continue
			}

			logger.Info("could not fetch Pod information")
			continue
		}
		processGroup.UpdateCondition(fdbv1beta2.MissingPod, false)
		processGroup.AddAddresses(podmanager.GetPublicIPs(pod, logger), processGroup.IsMarkedForRemoval() || !status.Health.Available)

		// This handles the case where the Pod has a DeletionTimestamp and should be deleted.
		if !pod.ObjectMeta.DeletionTimestamp.IsZero() {
			// If the ProcessGroup is marked for removal and is excluded, we can put the status into ResourcesTerminating.
			if processGroup.IsMarkedForRemoval() && processGroup.IsExcluded() {
				processGroup.UpdateCondition(fdbv1beta2.ResourcesTerminating, true)
				continue
			}
			// Otherwise we set PodFailing to ensure that the operator will trigger a replacement. This case can happen
			// if a Pod is marked for terminating (e.g. node failure) but the process itself is still reporting to the
			// cluster. We only set this condition if the Pod is in this state for GetFailedPodDuration(), the default
			// here is 5 minutes.
			if pod.ObjectMeta.DeletionTimestamp.Add(cluster.GetFailedPodDuration()).Before(time.Now()) {
				processGroup.UpdateCondition(fdbv1beta2.PodFailing, true)
				continue
			}

			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, false)
		}

		// Even if the process group will be removed we need to keep the config around.
		// Set the processCount for the process group specific storage servers per pod
		processCount, err := internal.GetServersPerPodForPod(pod, processGroup.ProcessClass)
		if err != nil {
			return err
		}
		status.AddServersPerDisk(processCount, processGroup.ProcessClass)

		imageType := internal.GetImageType(pod)
		imageTypeString := fdbv1beta2.ImageType(imageType)
		imageTypeFound := false
		for _, currentImageType := range status.ImageTypes {
			if imageTypeString == currentImageType {
				imageTypeFound = true
				break
			}
		}
		if !imageTypeFound {
			status.ImageTypes = append(status.ImageTypes, imageTypeString)
		}

		if pod.ObjectMeta.DeletionTimestamp.IsZero() && status.HasListenIPsForAllPods {
			hasPodIP := false
			for _, container := range pod.Spec.Containers {
				if container.Name == fdbv1beta2.SidecarContainerName || container.Name == fdbv1beta2.MainContainerName {
					for _, env := range container.Env {
						if env.Name == "FDB_POD_IP" {
							hasPodIP = true
						}
					}
				}
			}
			if !hasPodIP {
				status.HasListenIPsForAllPods = false
			}
		}

		err = checkAndSetProcessStatus(logger, r, cluster, pod, processMap, processCount, processGroup)
		if err != nil {
			return err
		}

		configMapHash, err := internal.GetDynamicConfHash(configMap, processGroup.ProcessClass, imageType, processCount)
		if err != nil {
			return err
		}

		var pvc *corev1.PersistentVolumeClaim
		pvcValue, pvcExists := pvcMap[processGroup.ProcessGroupID]
		if pvcExists {
			pvc = &pvcValue
		}

		err = validateProcessGroup(ctx, r, cluster, pod, pvc, configMapHash, processGroup, disableTaintFeature, logger)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateProcessGroup runs specific checks for the status of an process group.
// returns failing, incorrect, error
func validateProcessGroup(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod, currentPVC *corev1.PersistentVolumeClaim, configMapHash string, processGroupStatus *fdbv1beta2.ProcessGroupStatus,
	disableTaintFeature bool, logger logr.Logger) error {
	if pod == nil {
		processGroupStatus.UpdateCondition(fdbv1beta2.MissingPod, true)
		return nil
	}

	specHash, err := internal.GetPodSpecHash(cluster, processGroupStatus, nil)
	if err != nil {
		return err
	}

	incorrectPod := !metadataMatches(pod.ObjectMeta, internal.GetPodMetadata(cluster, processGroupStatus.ProcessClass, processGroupStatus.ProcessGroupID, specHash))
	if !incorrectPod {
		updated, err := r.PodLifecycleManager.PodIsUpdated(ctx, r, cluster, pod)
		if err != nil {
			return err
		}
		incorrectPod = !updated
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.IncorrectPodSpec, incorrectPod)

	// If we do a cluster version incompatible upgrade we use the fdbv1beta2.IncorrectConfigMap to signal when the operator
	// can restart fdbserver processes. Since the ConfigMap itself won't change during the upgrade we have to run the updatePodDynamicConf
	// to make sure all process groups have the required files ready. In the future we will use a different condition to indicate that a
	// process group is ready to be restarted.
	var synced bool
	if cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		synced, err = r.updatePodDynamicConf(logger, cluster, pod)
		if err != nil {
			logger.Info("error when checking if Pod has the correct files")
			synced = false
		}
	} else {
		synced = pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey] == configMapHash
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.IncorrectConfigMap, !synced)

	desiredPvc, err := internal.GetPvc(cluster, processGroupStatus)
	if err != nil {
		return err
	}

	incorrectPVC := (currentPVC != nil) != (desiredPvc != nil)
	if !incorrectPVC && desiredPvc != nil {
		incorrectPVC = !metadataMatches(currentPVC.ObjectMeta, desiredPvc.ObjectMeta)
	}
	if incorrectPVC {
		logger.Info("ValidateProcessGroup found incorrectPVC", "CurrentPVC", currentPVC, "DesiredPVC", desiredPvc)
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.MissingPVC, incorrectPVC)

	if pod.Status.Phase == corev1.PodPending {
		processGroupStatus.UpdateCondition(fdbv1beta2.PodPending, true)
		return nil
	}

	failing := false
	for _, container := range pod.Status.ContainerStatuses {
		if !container.Ready {
			failing = true
			break
		}
	}

	// Fix for https://github.com/kubernetes/kubernetes/issues/92067
	// This will delete the Pod that is stuck in the "NodeAffinity"
	// at a later stage the Pod will be recreated by the operator.
	if pod.Status.Phase == corev1.PodFailed {
		failing = true

		// Only recreate the Pod if it is already 5 minutes up (just to prevent to recreate the Pod multiple times
		// and give the cluster some time to get the kubelet up
		if pod.Status.Reason == "NodeAffinity" && pod.CreationTimestamp.Add(5*time.Minute).Before(time.Now()) {
			logger.Info("Delete Pod that is stuck in NodeAffinity",
				"processGroupID", processGroupStatus.ProcessGroupID)

			err = r.PodLifecycleManager.DeletePod(logr.NewContext(ctx, logger), r, pod)
			if err != nil {
				return err
			}
		}
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.PodFailing, failing)
	processGroupStatus.UpdateCondition(fdbv1beta2.PodPending, false)

	if !disableTaintFeature {
		err = updateTaintCondition(ctx, r, cluster, pod, processGroupStatus, logger.WithValues("Pod", pod.Name, "nodeName", pod.Spec.NodeName, "processGroupID", processGroupStatus.ProcessGroupID))
		if err != nil {
			return err
		}
	} else {
		// If the taint feature is disabled we should make sure we reset the taint related conditions.
		processGroupStatus.UpdateCondition(fdbv1beta2.NodeTaintDetected, false)
		processGroupStatus.UpdateCondition(fdbv1beta2.NodeTaintReplacing, false)
	}

	return nil
}

// updateTaintCondition checks pod's node taint label and update pod's taint-related condition accordingly
func updateTaintCondition(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod, processGroup *fdbv1beta2.ProcessGroupStatus, logger logr.Logger) error {
	node := &corev1.Node{}
	err := r.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node)
	if err != nil {
		return fmt.Errorf("get pod %s node %s fails with error :%w", pod.Name, pod.Spec.NodeName, err)
	}

	// Check the tainted duration and only mark the process group tainted after the configured tainted duration
	if !checkIfNodeHasTaintsAndUpdateConditions(logger, node.Spec.Taints, cluster, processGroup) {
		// Remove NodeTaintDetected condition if the pod is no longer on a tainted node.
		processGroup.UpdateCondition(fdbv1beta2.NodeTaintDetected, false)
		processGroup.UpdateCondition(fdbv1beta2.NodeTaintReplacing, false)
	}

	return nil
}

func checkIfNodeHasTaintsAndUpdateConditions(logger logr.Logger, taints []corev1.Taint, cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus) bool {
	hasMatchingTaint := false

	for _, taint := range taints {
		hasExactMatch := hasExactMatchedTaintKey(cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions, taint.Key)
		for _, taintConfiguredKey := range cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions {
			taintConfiguredKeyString := pointer.StringDeref(taintConfiguredKey.Key, "")
			if taintConfiguredKeyString == "" || pointer.Int64Deref(taintConfiguredKey.DurationInSeconds, math.MinInt64) < 0 {
				logger.Info("Cluster's TaintReplacementOption is disabled",
					"Key", taintConfiguredKey.Key,
					"DurationInSeconds", taintConfiguredKey.DurationInSeconds)
				continue
			}
			if taint.Key == "" {
				// Node's taint is not properly set, skip the node's taint
				logger.Info("Taint is not properly set", "Taint", taint)
				continue
			}
			// If a wildcard is specified as key, it will be used as default value, except there is an exact match defined for the taint key.
			if (hasExactMatch && taint.Key != taintConfiguredKeyString) ||
				(!hasExactMatch && taintConfiguredKeyString != "*") {
				continue
			}

			hasMatchingTaint = true
			nodeTaintDetectedTime := processGroup.GetConditionTime(fdbv1beta2.NodeTaintDetected)
			if nodeTaintDetectedTime == nil {
				processGroup.UpdateCondition(fdbv1beta2.NodeTaintDetected, true)
				logger.Info("Add NodeTaintDetected condition",
					"TaintKey", taint.Key,
					"TaintValue", taint.Value,
					"TaintEffect", taint.Effect)
				// Update nodeTaintDetectedTime here to make sure we are not hitting a nil pointer below.
				nodeTaintDetectedTime = pointer.Int64(time.Now().Unix())
			}

			// Use node taint's timestamp as the NodeTaintDetected condition's starting time.
			if !taint.TimeAdded.IsZero() && taint.TimeAdded.Time.Unix() < *nodeTaintDetectedTime {
				logger.Info("Update NodeTaintDetected condition",
					"TaintKey", taint.Key,
					"TaintValue", taint.Value,
					"TaintEffect", taint.Effect,
					"oldNodeTaintDetectedTime", time.Unix(*nodeTaintDetectedTime, 0).String(),
					"newNodeTaintDetectedTime", taint.TimeAdded.String())
				processGroup.UpdateConditionTime(fdbv1beta2.NodeTaintDetected, taint.TimeAdded.Unix())
				// Update the current timestamp with the new value.
				nodeTaintDetectedTime = pointer.Int64(taint.TimeAdded.Unix())
			}

			// If the process group already has the NodeTaintReplacing condition we can skip any further work.
			taintReplaceTime := processGroup.GetConditionTime(fdbv1beta2.NodeTaintReplacing)
			if taintReplaceTime != nil {
				return hasMatchingTaint
			}

			taintDetectedTimestamp := pointer.Int64Deref(nodeTaintDetectedTime, math.MaxInt64)
			if time.Now().Unix()-taintDetectedTimestamp > pointer.Int64Deref(taintConfiguredKey.DurationInSeconds, math.MaxInt64) {
				taintDetectedTime := time.Unix(taintDetectedTimestamp, 0)
				processGroup.UpdateCondition(fdbv1beta2.NodeTaintReplacing, true)
				logger.Info("Add NodeTaintReplacing condition",
					"TaintKey", taint.Key,
					"TaintDetectedTime", taintDetectedTime.String(),
					"TaintDuration", time.Since(taintDetectedTime).String(),
					"TaintValue", taint.Value,
					"TaintEffect", taint.Effect,
					"ClusterTaintDetectionDuration", (time.Duration(pointer.Int64Deref(taintConfiguredKey.DurationInSeconds, math.MaxInt64)) * time.Second).String())
			}
		}
	}

	return hasMatchingTaint
}

func refreshProcessGroupStatus(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBClusterStatus) (*corev1.PersistentVolumeClaimList, error) {
	knownProcessGroups := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None{}

	for _, processGroup := range status.ProcessGroups {
		knownProcessGroups[processGroup.ProcessGroupID] = fdbv1beta2.None{}
	}

	// Track all created resources this will ensure that we catch all resources that are created by the operator
	// even if the process group is currently missing for some reasons.
	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		processGroupID := fdbv1beta2.ProcessGroupID(pod.Labels[cluster.GetProcessGroupIDLabel()])
		if _, ok := knownProcessGroups[processGroupID]; ok {
			continue
		}

		// Since we found a new process group we have to add it to our map.
		knownProcessGroups[processGroupID] = fdbv1beta2.None{}
		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(cluster, pod.Labels), nil))
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, pvcs, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return nil, err
	}

	for _, pvc := range pvcs.Items {
		processGroupID := fdbv1beta2.ProcessGroupID(pvc.Labels[cluster.GetProcessGroupIDLabel()])
		if _, ok := knownProcessGroups[processGroupID]; ok {
			continue
		}

		// Since we found a new process group we have to add it to our map.
		knownProcessGroups[processGroupID] = fdbv1beta2.None{}
		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(cluster, pvc.Labels), nil))
	}

	services := &corev1.ServiceList{}
	err = r.List(ctx, services, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return nil, err
	}

	for _, service := range services.Items {
		processGroupID := fdbv1beta2.ProcessGroupID(service.Labels[cluster.GetProcessGroupIDLabel()])
		if processGroupID == "" {
			continue
		}

		if _, ok := knownProcessGroups[processGroupID]; ok {
			continue
		}

		// Since we found a new process group we have to add it to our map.
		knownProcessGroups[processGroupID] = fdbv1beta2.None{}
		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(cluster, service.Labels), nil))
	}

	return pvcs, nil
}

func getRunningVersion(logger logr.Logger, versionMap map[string]int, currentRunningVersion string) (string, error) {
	if len(versionMap) == 0 {
		return currentRunningVersion, nil
	}

	var currentCandidate fdbv1beta2.Version
	var currentMaxCount int

	for version, count := range versionMap {
		if count < currentMaxCount {
			continue
		}

		parsedVersion, err := fdbv1beta2.ParseFdbVersion(version)
		if err != nil {
			return currentRunningVersion, err
		}
		// In this case we want to ensure we always pick the newer version to have a stable return value. Otherwise,
		// it could happen that the version will be flapping between two versions.
		if count == currentMaxCount {
			if currentCandidate.IsAtLeast(parsedVersion) {
				continue
			}
		}

		currentCandidate = parsedVersion
		currentMaxCount = count
	}

	candidateString := currentCandidate.String()
	// Only in the case of a new version detected we should log the new version and more information why the operator
	// made this decision.
	if candidateString != currentRunningVersion {
		logger.Info("getting the running version from status",
			"detectedCandidate", candidateString,
			"previousVersion", currentCandidate,
			"versionMap", versionMap)
	}

	return candidateString, nil
}

func hasExactMatchedTaintKey(taintReplacementOptions []fdbv1beta2.TaintReplacementOption, nodeTaintKey string) bool {
	for _, configuredTaintKey := range taintReplacementOptions {
		if *configuredTaintKey.Key == nodeTaintKey {
			return true
		}
	}
	return false
}

// getFaultDomainFromProcesses returns the fault domain from the process information slice.
func getFaultDomainFromProcesses(processes []fdbv1beta2.FoundationDBStatusProcessInfo) string {
	if len(processes) == 1 {
		return processes[0].Locality[fdbv1beta2.FDBLocalityZoneIDKey]
	}

	// If we find more than one process with the same process group ID, we might have a case were one process was restarted
	if len(processes) > 1 {
		// If we have more than one process we will take the information from the process that was most recently started and
		// has the locality information present.
		latestProcess := fdbv1beta2.FoundationDBStatusProcessInfo{
			UptimeSeconds: math.MaxFloat64,
		}

		for _, process := range processes {
			_, hasZone := process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
			if !hasZone {
				continue
			}

			if latestProcess.UptimeSeconds < process.UptimeSeconds {
				continue
			}

			latestProcess = process
		}

		return latestProcess.Locality[fdbv1beta2.FDBLocalityZoneIDKey]
	}

	return ""
}

// updateFaultDomains will update the process groups fault domain, based on the last seen zone id in the cluster status.
func updateFaultDomains(logger logr.Logger, processes map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo, status *fdbv1beta2.FoundationDBClusterStatus) {
	// If the process map is empty we can skip any further steps.
	if len(processes) == 0 {
		return
	}

	for idx, processGroup := range status.ProcessGroups {
		process, ok := processes[processGroup.ProcessGroupID]
		if !ok || len(processes) == 0 {
			// Fallback for multiple storage or log servers, those will contain the process information with the process number as a suffix.
			process, ok = processes[processGroup.ProcessGroupID+"-1"]
			if !ok || len(processes) == 0 {
				logger.Info("skip updating fault domain for process group with missing process in FoundationDB cluster status", "processGroupID", processGroup.ProcessGroupID)
				continue
			}
		}

		faultDomain := getFaultDomainFromProcesses(process)
		if faultDomain == "" {
			logger.Info("skip updating fault domain for process group with missing zoneid", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		status.ProcessGroups[idx].FaultDomain = fdbv1beta2.FaultDomain(faultDomain)
	}
}
