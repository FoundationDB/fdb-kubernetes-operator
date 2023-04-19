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
func (updateStatus) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updateStatus")
	originalStatus := cluster.Status.DeepCopy()
	status := fdbv1beta2.FoundationDBClusterStatus{}
	// Pass through Maintenance Mode Info as the maintenance_mode_checker reconciler takes care of updating it
	originalStatus.MaintenanceModeInfo.DeepCopyInto(&status.MaintenanceModeInfo)
	status.Generations.Reconciled = cluster.Status.Generations.Reconciled

	// Initialize with the current desired storage servers per Pod
	status.StorageServersPerDisk = []int{cluster.GetStorageServersPerPod()}
	status.ImageTypes = []fdbv1beta2.ImageType{fdbv1beta2.ImageType(internal.GetDesiredImageType(cluster))}

	var databaseStatus *fdbv1beta2.FoundationDBStatus
	processMap := make(map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo)

	if cluster.Status.ConnectionString == "" {
		databaseStatus = &fdbv1beta2.FoundationDBStatus{
			Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
				Layers: fdbv1beta2.FoundationDBStatusLayerInfo{
					Error: "configurationMissing",
				},
			},
		}
	} else {
		connectionString, err := tryConnectionOptions(logger, cluster, r)
		if err != nil {
			return &requeue{curError: err}
		}
		cluster.Status.ConnectionString = connectionString

		adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
		if err != nil {
			return &requeue{curError: err}
		}

		databaseStatus, err = adminClient.GetStatus()
		_ = adminClient.Close()

		if err != nil {
			if cluster.Status.Configured {
				return &requeue{curError: err, delayedRequeue: true}
			}
			databaseStatus = &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Layers: fdbv1beta2.FoundationDBStatusLayerInfo{
						Error: "configurationMissing",
					},
				},
			}
		}
	}

	versionMap := map[string]int{}
	for _, process := range databaseStatus.Cluster.Processes {
		processID, ok := process.Locality["process_id"]
		// if the processID is not set we fall back to the instanceID
		if !ok {
			processID = process.Locality["instance_id"]
		}
		processMap[fdbv1beta2.ProcessGroupID(processID)] = append(processMap[fdbv1beta2.ProcessGroupID(processID)], process)
		versionMap[process.Version]++
	}

	// Update the running version based on the reported version of the FDB processes
	version, err := getRunningVersion(versionMap, cluster.Status.RunningVersion)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in getRunningVersion: %w", err)}
	}
	cluster.Status.RunningVersion = version

	status.HasListenIPsForAllPods = cluster.NeedsExplicitListenAddress()
	status.DatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.NormalizeConfigurationWithSeparatedProxies(cluster.Spec.Version, cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured())
	// Removing excluded servers as we don't want them during comparison.
	status.DatabaseConfiguration.ExcludedServers = nil
	cluster.ClearMissingVersionFlags(&status.DatabaseConfiguration)
	status.Configured = cluster.Status.Configured || (databaseStatus.Client.DatabaseStatus.Available && databaseStatus.Cluster.Layers.Error != "configurationMissing")

	if cluster.Spec.MainContainer.EnableTLS {
		status.RequiredAddresses.TLS = true
	} else {
		status.RequiredAddresses.NonTLS = true
	}

	if databaseStatus != nil {
		for _, coordinator := range databaseStatus.Client.Coordinators.Coordinators {
			address, err := fdbv1beta2.ParseProcessAddress(coordinator.Address.String())
			if err != nil {
				return &requeue{curError: fmt.Errorf("update_status skipped due to error in ParseProcessAddress: %w", err)}
			}

			if address.Flags["tls"] {
				status.RequiredAddresses.TLS = true
			} else {
				status.RequiredAddresses.NonTLS = true
			}
		}

		status.Health.Available = databaseStatus.Client.DatabaseStatus.Available
		status.Health.Healthy = databaseStatus.Client.DatabaseStatus.Healthy
		status.Health.FullReplication = databaseStatus.Cluster.FullReplication
		status.Health.DataMovementPriority = databaseStatus.Cluster.Data.MovingData.HighestPriority
	}

	cluster.Status.RequiredAddresses = status.RequiredAddresses

	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in GetConfigMap: %w", err)}
	}

	status.ProcessGroups = make([]*fdbv1beta2.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup != nil && processGroup.ProcessGroupID != "" {
			status.ProcessGroups = append(status.ProcessGroups, processGroup)
		}
	}

	pods, pvcs, err := refreshProcessGroupStatus(ctx, r, cluster, &status)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in refreshProcessGroupStatus: %w", err)}
	}

	status.ProcessGroups, err = validateProcessGroups(ctx, r, cluster, &status, processMap, configMap, pods, pvcs)
	if err != nil {
		return &requeue{curError: fmt.Errorf("update_status skipped due to error in validateProcessGroups: %w", err)}
	}
	removeDuplicateConditions(status)

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		status.HasIncorrectConfigMap = true
	} else if err != nil {
		return &requeue{curError: err}
	}

	status.RunningVersion = cluster.Status.RunningVersion

	if status.RunningVersion == "" {
		version, present := existingConfigMap.Data["running-version"]
		if present {
			status.RunningVersion = version
		}
	}

	if status.RunningVersion == "" {
		status.RunningVersion = cluster.Spec.Version
	}

	status.ConnectionString = cluster.Status.ConnectionString
	if status.ConnectionString == "" {
		status.ConnectionString = existingConfigMap.Data[internal.ClusterFileKey]
	}

	if status.ConnectionString == "" {
		status.ConnectionString = cluster.Spec.SeedConnectionString
	}

	status.HasIncorrectConfigMap = status.HasIncorrectConfigMap || !equality.Semantic.DeepEqual(existingConfigMap.Data, configMap.Data) || !metadataMatches(existingConfigMap.ObjectMeta, configMap.ObjectMeta)

	service := internal.GetHeadlessService(cluster)
	existingService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, existingService)
	if err != nil && k8serrors.IsNotFound(err) {
		existingService = nil
	} else if err != nil {
		return &requeue{curError: err}
	}

	status.HasIncorrectServiceConfig = (service == nil) != (existingService == nil)

	if status.Configured && cluster.Status.ConnectionString != "" {
		coordinatorStatus := make(map[string]bool, len(databaseStatus.Client.Coordinators.Coordinators))
		for _, coordinator := range databaseStatus.Client.Coordinators.Coordinators {
			coordinatorStatus[coordinator.Address.String()] = false
		}

		coordinatorsValid, _, err := locality.CheckCoordinatorValidity(logger, cluster, databaseStatus, coordinatorStatus)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		status.NeedsNewCoordinators = !coordinatorsValid
	}

	if len(cluster.Spec.LockOptions.DenyList) > 0 && cluster.ShouldUseLocks() && status.Configured {
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
		status.Locks.DenyList = denyList
	}

	// Sort slices that are assembled based on pods to prevent a reordering from
	// issuing a new reconcile loop.
	sort.Ints(status.StorageServersPerDisk)
	sort.Slice(status.ImageTypes, func(i int, j int) bool {
		return string(status.ImageTypes[i]) < string(status.ImageTypes[j])
	})

	// Sort ProcessGroups by ProcessGroupID otherwise this can result in an endless loop when the
	// order changes.
	sort.SliceStable(status.ProcessGroups, func(i, j int) bool {
		return status.ProcessGroups[i].ProcessGroupID < status.ProcessGroups[j].ProcessGroupID
	})

	cluster.Status = status

	_, err = cluster.CheckReconciliation(log)
	if err != nil {
		return &requeue{curError: err}
	}

	// See: https://github.com/kubernetes-sigs/kubebuilder/issues/592
	// If we use the default reflect.DeepEqual method it will be recreating the
	// status multiple times because the pointers are different.
	if !equality.Semantic.DeepEqual(cluster.Status, *originalStatus) {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			logger.Error(err, "Error updating cluster status")
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

	if len(connectionStrings) == 1 {
		return cluster.Status.ConnectionString, nil
	}

	logger.Info("Trying connection options", "connectionString", connectionStrings)

	originalConnectionString := cluster.Status.ConnectionString
	defer func() { cluster.Status.ConnectionString = originalConnectionString }()

	for _, connectionString := range connectionStrings {
		logger.Info("Attempting to get connection string from cluster", "connectionString", connectionString)
		cluster.Status.ConnectionString = connectionString
		adminClient, clientErr := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
		if clientErr != nil {
			return originalConnectionString, clientErr
		}

		activeConnectionString, err := adminClient.GetConnectionString()

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
func checkAndSetProcessStatus(r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo, processNumber int, processCount int, processGroupStatus *fdbv1beta2.ProcessGroupStatus) error {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updateStatus")
	processID := processGroupStatus.ProcessGroupID

	if processCount > 1 {
		processID = fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%d", processID, processNumber))
	}

	processStatus := processMap[processID]

	processGroupStatus.UpdateCondition(fdbv1beta2.MissingProcesses, len(processStatus) == 0, cluster.Status.ProcessGroups, processID)
	if len(processStatus) == 0 {
		return nil
	}

	podClient, message := r.getPodClient(cluster, pod)
	if podClient == nil {
		logger.Info("Unable to build pod client", "processGroupID", processGroupStatus.ProcessGroupID, "message", message)
		return nil
	}

	correct := false
	versionCompatibleUpgrade := cluster.VersionCompatibleUpgradeInProgress()
	for _, process := range processStatus {
		commandLine, err := internal.GetStartCommand(cluster, processGroupStatus.ProcessClass, podClient, processNumber, processCount)
		if err != nil {
			if internal.IsNetworkError(err) {
				processGroupStatus.UpdateCondition(fdbv1beta2.SidecarUnreachable, true, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
				return nil
			}

			return err
		}

		// If a version compatible upgrade is in progress, skip the version check since we will run a mixed set of versions
		// until the cluster is fully reconciled.
		versionMatch := true
		if !versionCompatibleUpgrade {
			versionMatch = process.Version == cluster.Spec.Version || process.Version == fmt.Sprintf("%s-PRERELEASE", cluster.Spec.Version)
		}

		// If the `EmptyMonitorConf` is set, the commandline is by definition wrong since there should be no running processes.
		correct = commandLine == process.CommandLine && versionMatch && !cluster.Spec.Buggify.EmptyMonitorConf

		if !correct {
			log.Info("IncorrectProcess", "expected", commandLine, "got", process.CommandLine,
				"expectedVersion", cluster.Spec.Version,
				"version", process.Version, "processGroupID", processGroupStatus.ProcessGroupID,
				"emptyMonitorConf is ", cluster.Spec.Buggify.EmptyMonitorConf)
		}
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.IncorrectCommandLine, !correct, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
	// Reset status for sidecar unreachable, since we are here at this point we were able to reach the sidecar for the substitute variables.
	processGroupStatus.UpdateCondition(fdbv1beta2.SidecarUnreachable, false, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)

	return nil
}

// Validate and set progressGroup's status
func validateProcessGroups(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBClusterStatus, processMap map[fdbv1beta2.ProcessGroupID][]fdbv1beta2.FoundationDBStatusProcessInfo, configMap *corev1.ConfigMap, pods []*corev1.Pod, pvcs *corev1.PersistentVolumeClaimList) ([]*fdbv1beta2.ProcessGroupStatus, error) {
	var err error
	processGroups := status.ProcessGroups
	processGroupsWithoutExclusion := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion))
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updateStatus")

	for _, processGroupID := range cluster.Spec.ProcessGroupsToRemoveWithoutExclusion {
		processGroupsWithoutExclusion[processGroupID] = fdbv1beta2.None{}
	}

	podMap := internal.CreatePodMap(cluster, pods)
	pvcMap := internal.CreatePVCMap(cluster, pvcs)

	disableTaintFeature := cluster.IsTaintFeatureDisabled()
	if disableTaintFeature {
		logger.Info("Disable taint feature", "Disabled", disableTaintFeature)
	}

	for _, processGroup := range processGroups {
		pod, podExists := podMap[processGroup.ProcessGroupID]
		// If the process group is not being removed and the Pod is not set we need to put it into
		// the failing list.
		isBeingRemoved := cluster.ProcessGroupIsBeingRemoved(processGroup.ProcessGroupID)
		if !podExists {
			// Mark process groups as terminating if the pod has been deleted but other
			// resources are stuck in terminating.
			if isBeingRemoved {
				processGroup.UpdateCondition(fdbv1beta2.ResourcesTerminating, true, processGroups, processGroup.ProcessGroupID)
			} else {
				processGroup.UpdateCondition(fdbv1beta2.MissingPod, true, processGroups, processGroup.ProcessGroupID)
			}

			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, false, nil, "")
			continue
		}

		processGroup.AddAddresses(podmanager.GetPublicIPs(pod, log), processGroup.IsMarkedForRemoval() || !status.Health.Available)
		processCount := 1

		// In this case the Pod has a DeletionTimestamp and should be deleted.
		if !pod.ObjectMeta.DeletionTimestamp.IsZero() {
			// If the ProcessGroup is marked for removal we can put the status into ResourcesTerminating
			if processGroup.IsMarkedForRemoval() {
				processGroup.UpdateCondition(fdbv1beta2.ResourcesTerminating, true, processGroups, processGroup.ProcessGroupID)
				continue
			}
			// Otherwise we set PodFailing to ensure that the operator will trigger a replacement. This case can happen
			// if a Pod is marked for terminating (e.g. node failure) but the process itself is still reporting to the
			// cluster. We only set this condition if the Pod is in this state for GetFailedPodDuration(), the default
			// here is 5 minutes.
			if pod.ObjectMeta.DeletionTimestamp.Add(cluster.GetFailedPodDuration()).Before(time.Now()) {
				processGroup.UpdateCondition(fdbv1beta2.PodFailing, true, processGroups, processGroup.ProcessGroupID)
				continue
			}

			processGroup.UpdateCondition(fdbv1beta2.IncorrectCommandLine, false, nil, "")
		}

		// Even the process group will be removed we need to keep the config around.
		// Set the processCount for the process group specific storage servers per pod
		if processGroup.ProcessClass == fdbv1beta2.ProcessClassStorage {
			processCount, err = internal.GetStorageServersPerPodForPod(pod)
			if err != nil {
				return processGroups, err
			}

			status.AddStorageServerPerDisk(processCount)
		}

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

		if isBeingRemoved {
			if processGroup.RemovalTimestamp.IsZero() {
				processGroup.MarkForRemoval()
			}
			// Check if we should skip exclusion for the process group
			_, ok := processGroupsWithoutExclusion[processGroup.ProcessGroupID]
			processGroup.ExclusionSkipped = ok
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

		// In theory we could also support multiple processes per pod for different classes
		for i := 1; i <= processCount; i++ {
			err = checkAndSetProcessStatus(r, cluster, pod, processMap, i, processCount, processGroup)
			if err != nil {
				return processGroups, err
			}
		}

		configMapHash, err := internal.GetDynamicConfHash(configMap, processGroup.ProcessClass, imageType, processCount)
		if err != nil {
			return processGroups, err
		}

		var pvc *corev1.PersistentVolumeClaim
		pvcValue, pvcExists := pvcMap[processGroup.ProcessGroupID]
		if pvcExists {
			pvc = &pvcValue
		}

		err = validateProcessGroup(ctx, r, cluster, pod, pvc, configMapHash, processGroup, disableTaintFeature, logger)
		if err != nil {
			return processGroups, err
		}
	}

	return processGroups, nil
}

// validateProcessGroup runs specific checks for the status of an process group.
// returns failing, incorrect, error
func validateProcessGroup(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod, currentPVC *corev1.PersistentVolumeClaim, configMapHash string, processGroupStatus *fdbv1beta2.ProcessGroupStatus,
	disableTaintFeature bool, logger logr.Logger) error {
	processGroupStatus.UpdateCondition(fdbv1beta2.MissingPod, pod == nil, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
	if pod == nil {
		return nil
	}

	_, idNum, err := podmanager.ParseProcessGroupID(processGroupStatus.ProcessGroupID)
	if err != nil {
		return err
	}

	specHash, err := internal.GetPodSpecHash(cluster, processGroupStatus.ProcessClass, idNum, nil)
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

	processGroupStatus.UpdateCondition(fdbv1beta2.IncorrectPodSpec, incorrectPod, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)

	// If we do a cluster version incompatible upgrade we use the fdbv1beta2.IncorrectConfigMap to signal when the operator
	// can restart fdbserver processes. Since the ConfigMap itself won't change during the upgrade we have to run the updatePodDynamicConf
	// to make sure all process groups have the required files ready. In the future we will use a different condition to indicate that a
	// process group si ready to be restarted.
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

	processGroupStatus.UpdateCondition(fdbv1beta2.IncorrectConfigMap, !synced, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)

	desiredPvc, err := internal.GetPvc(cluster, processGroupStatus.ProcessClass, idNum)
	if err != nil {
		return err
	}

	incorrectPVC := (currentPVC != nil) != (desiredPvc != nil)
	if !incorrectPVC && desiredPvc != nil {
		incorrectPVC = !metadataMatches(currentPVC.ObjectMeta, desiredPvc.ObjectMeta)
	}
	if incorrectPVC {
		log.Info("ValidateProcessGroup found incorrectPVC", "CurrentPVC", currentPVC, "DesiredPVC", desiredPvc)
	}

	processGroupStatus.UpdateCondition(fdbv1beta2.MissingPVC, incorrectPVC, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)

	if pod.Status.Phase == corev1.PodPending {
		processGroupStatus.UpdateCondition(fdbv1beta2.PodPending, true, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
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

	processGroupStatus.UpdateCondition(fdbv1beta2.PodFailing, failing, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
	processGroupStatus.UpdateCondition(fdbv1beta2.PodPending, false, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)

	if !disableTaintFeature {
		// Update taint status
		err = updateTaintCondition(ctx, r, cluster, pod, processGroupStatus, logger)
		if err != nil {
			return err
		}
	}

	return nil
}

// updateTaintCondition checks pod's node taint label and update pod's taint-related condition accordingly
func updateTaintCondition(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod, processGroupStatus *fdbv1beta2.ProcessGroupStatus, logger logr.Logger) error {
	node := &corev1.Node{}
	err := r.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node)
	if err != nil {
		log.Info("Get pod's node fails", "Pod", pod.Name, "Pod's node name", pod.Spec.NodeName, "err", err)
	} else {
		// Check the tainted duration and only mark the process group tainted after the configured tainted duration
		// TODO: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1583
		hasValidTaint := false
		for _, taint := range node.Spec.Taints {
			for _, taintConfiguredKey := range cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions {
				if taintConfiguredKey.Key == nil || pointer.Int64Deref(taintConfiguredKey.DurationInSeconds, math.MinInt64) < 0 {
					logger.Info("Cluster's TaintReplacementOption is disabled", "Key",
						taintConfiguredKey.Key, "DurationInSeconds", taintConfiguredKey.DurationInSeconds,
						"Pod", pod.Name)
					continue
				}
				if len(taint.Key) == 0 || taint.TimeAdded == nil {
					// Node's taint is not properly set, skip the node's taint
					logger.Info("Taint is not properly set", "Node", node.Name, "Taint", taint)
					continue
				}

				if taint.Key == pointer.StringDeref(taintConfiguredKey.Key, "") || pointer.StringDeref(taintConfiguredKey.Key, "") == "*" {
					hasValidTaint = true
					processGroupStatus.UpdateCondition(fdbv1beta2.NodeTaintDetected, true, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
					// Use node taint's timestamp as the NodeTaintDetected condition's starting time
					if taint.TimeAdded != nil && taint.TimeAdded.Time.Unix() < *processGroupStatus.GetConditionTime(fdbv1beta2.NodeTaintDetected) {
						processGroupStatus.UpdateConditionTime(fdbv1beta2.NodeTaintDetected, taint.TimeAdded.Unix())
					}
					taintDetectedTime := pointer.Int64Deref(processGroupStatus.GetConditionTime(fdbv1beta2.NodeTaintDetected), math.MaxInt64)
					if time.Now().Unix()-taintDetectedTime > pointer.Int64Deref(taintConfiguredKey.DurationInSeconds, math.MaxInt64) {
						processGroupStatus.UpdateCondition(fdbv1beta2.NodeTaintReplacing, true, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
						logger.Info("Add NodeTaintReplacing condition", "Pod", pod.Name, "Node", node.Name,
							"TaintKey", taint.Key, "TaintDetectedTime", taintDetectedTime,
							"TaintDuration", int64(time.Since(time.Unix(taintDetectedTime, 0))),
							"TaintValue", taint.Value, "TaintEffect", taint.Effect,
							"ClusterTaintDetectionDuration", time.Duration(pointer.Int64Deref(taintConfiguredKey.DurationInSeconds, math.MaxInt64)))
					}
				}
			}
		}
		if !hasValidTaint {
			// Remove NodeTaintDetected condition if the pod is no longer on a tainted node;
			// This is needed especially when a node's status is flapping
			// We do not remove NodeTaintDetected because the NodeTaintDetected condition is only added when taint is there for configured long time
			processGroupStatus.UpdateCondition(fdbv1beta2.NodeTaintDetected, false, cluster.Status.ProcessGroups, processGroupStatus.ProcessGroupID)
		}
	}

	return err
}

// removeDuplicateConditions will remove all duplicated conditions from the status and if a process group has the ResourcesTerminating
// condition it will remove all other conditions on that process group.
func removeDuplicateConditions(status fdbv1beta2.FoundationDBClusterStatus) {
	for _, processGroupStatus := range status.ProcessGroups {
		conditionTimes := make(map[fdbv1beta2.ProcessGroupConditionType]int64, len(processGroupStatus.ProcessGroupConditions))
		copiedConditions := make(map[fdbv1beta2.ProcessGroupConditionType]bool, len(processGroupStatus.ProcessGroupConditions))
		conditions := make([]*fdbv1beta2.ProcessGroupCondition, 0, len(processGroupStatus.ProcessGroupConditions))
		isTerminating := false

		for _, condition := range processGroupStatus.ProcessGroupConditions {
			existingTime, present := conditionTimes[condition.ProcessGroupConditionType]
			if !present || existingTime > condition.Timestamp {
				conditionTimes[condition.ProcessGroupConditionType] = condition.Timestamp
			}

			if condition.ProcessGroupConditionType == fdbv1beta2.ResourcesTerminating {
				isTerminating = true
			}
		}

		for _, condition := range processGroupStatus.ProcessGroupConditions {
			if condition.Timestamp == conditionTimes[condition.ProcessGroupConditionType] && !copiedConditions[condition.ProcessGroupConditionType] {
				if isTerminating && condition.ProcessGroupConditionType != fdbv1beta2.ResourcesTerminating {
					continue
				}

				conditions = append(conditions, condition)
				copiedConditions[condition.ProcessGroupConditionType] = true
			}
		}

		processGroupStatus.ProcessGroupConditions = conditions
	}
}

func refreshProcessGroupStatus(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBClusterStatus) ([]*corev1.Pod, *corev1.PersistentVolumeClaimList, error) {
	status.ProcessGroups = make([]*fdbv1beta2.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup != nil && processGroup.ProcessGroupID != "" {
			status.ProcessGroups = append(status.ProcessGroups, processGroup)
		}
	}

	// Track all created resources this will ensure that we catch all resources that are created by the operator
	// even if the process group is currently missing for some reasons.
	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return nil, nil, err
	}

	for _, pod := range pods {
		processGroupID := fdbv1beta2.ProcessGroupID(pod.Labels[cluster.GetProcessGroupIDLabel()])
		if fdbv1beta2.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(cluster, pod.Labels), nil))
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, pvcs, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return nil, nil, err
	}

	for _, pvc := range pvcs.Items {
		processGroupID := fdbv1beta2.ProcessGroupID(pvc.Labels[cluster.GetProcessGroupIDLabel()])
		if fdbv1beta2.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(cluster, pvc.Labels), nil))
	}

	services := &corev1.ServiceList{}
	err = r.List(ctx, services, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return nil, nil, err
	}

	for _, service := range services.Items {
		processGroupID := fdbv1beta2.ProcessGroupID(service.Labels[cluster.GetProcessGroupIDLabel()])
		if processGroupID == "" || fdbv1beta2.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(cluster, service.Labels), nil))
	}

	return pods, pvcs, nil
}

func getRunningVersion(versionMap map[string]int, fallback string) (string, error) {
	if len(versionMap) == 0 {
		return fallback, nil
	}

	var currentCandidate fdbv1beta2.Version
	var currentMaxCount int

	for version, count := range versionMap {
		if count < currentMaxCount {
			continue
		}

		parsedVersion, err := fdbv1beta2.ParseFdbVersion(version)
		if err != nil {
			return fallback, err
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

	return currentCandidate.String(), nil
}
