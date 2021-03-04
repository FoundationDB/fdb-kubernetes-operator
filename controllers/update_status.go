/*
 * update_status.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/api/equality"
)

// UpdateStatus provides a reconciliation step for updating the status in the
// CRD.
type UpdateStatus struct {
}

// Reconcile runs the reconciler's work.
func (s UpdateStatus) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.Generations.Reconciled = cluster.Status.Generations.Reconciled

	// Initialize with the current desired storage servers per Pod
	status.StorageServersPerDisk = []int{cluster.GetStorageServersPerPod()}

	var databaseStatus *fdbtypes.FoundationDBStatus
	processMap := make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)

	if cluster.Status.ConnectionString == "" {
		databaseStatus = &fdbtypes.FoundationDBStatus{
			Cluster: fdbtypes.FoundationDBStatusClusterInfo{
				Layers: fdbtypes.FoundationDBStatusLayerInfo{
					Error: "configurationMissing",
				},
			},
		}
	} else {
		version, connectionString, err := tryConnectionOptions(cluster, r)
		if err != nil {
			return false, err
		}
		cluster.Status.RunningVersion = version
		cluster.Status.ConnectionString = connectionString

		adminClient, err := r.AdminClientProvider(cluster, r)
		if err != nil {
			return false, err
		}
		defer adminClient.Close()

		databaseStatus, err = adminClient.GetStatus()
		if err != nil {
			return false, err
		}
	}

	for _, process := range databaseStatus.Cluster.Processes {
		processID, ok := process.Locality["process_id"]
		// if the processID is not set we fall back to the instanceID
		if !ok {
			processID = process.Locality["instance_id"]
		}
		processMap[processID] = append(processMap[processID], process)
	}

	status.DatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.NormalizeConfiguration()
	cluster.ClearMissingVersionFlags(&status.DatabaseConfiguration)
	status.Configured = cluster.Status.Configured || (databaseStatus.Client.DatabaseStatus.Available && databaseStatus.Cluster.Layers.Error != "configurationMissing")

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	if cluster.Spec.MainContainer.EnableTLS {
		status.RequiredAddresses.TLS = true
	} else {
		status.RequiredAddresses.NonTLS = true
	}

	if databaseStatus != nil {
		for _, coordinator := range databaseStatus.Client.Coordinators.Coordinators {
			address, err := fdbtypes.ParseProcessAddress(coordinator.Address)
			if err != nil {
				return false, err
			}

			if address.Flags["tls"] {
				status.RequiredAddresses.TLS = true
			} else {
				status.RequiredAddresses.NonTLS = true
			}
		}
	}

	cluster.Status.RequiredAddresses = status.RequiredAddresses

	configMap, err := GetConfigMap(cluster)
	if err != nil {
		return false, err
	}

	status.ProcessGroups = make([]*fdbtypes.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup != nil && processGroup.ProcessGroupID != "" {
			status.ProcessGroups = append(status.ProcessGroups, processGroup)
		}
	}

	status.ProcessGroups, err = validateInstances(r, context, cluster, &status, processMap, instances, configMap)
	if err != nil {
		return false, err
	}
	removeDuplicateConditions(status)

	// Track all PVCs
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	for _, pvc := range pvcs.Items {
		processGroupID := pvc.Labels[FDBInstanceIDLabel]
		if fdbtypes.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbtypes.NewProcessGroupStatus(processGroupID, processClassFromLabels(pvc.Labels), nil))
	}

	// Track all Services
	services := &corev1.ServiceList{}
	err = r.List(context, services, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	for _, service := range services.Items {
		processGroupID := service.Labels[FDBInstanceIDLabel]
		if processGroupID == "" || fdbtypes.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbtypes.NewProcessGroupStatus(processGroupID, processClassFromLabels(service.Labels), nil))
	}

	// Ensure that anything the user has explicitly chosen to remove is marked
	// for removal.
	for _, processGroupID := range cluster.Spec.InstancesToRemove {
		for _, processGroup := range status.ProcessGroups {
			if processGroup.ProcessGroupID == processGroupID {
				processGroup.Remove = true
			}
		}
	}
	for _, processGroupID := range cluster.Spec.InstancesToRemoveWithoutExclusion {
		for _, processGroup := range status.ProcessGroups {
			if processGroup.ProcessGroupID == processGroupID {
				processGroup.ExclusionSkipped = true
			}
		}
	}

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		status.HasIncorrectConfigMap = true
	} else if err != nil {
		return false, err
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
		status.ConnectionString = existingConfigMap.Data["cluster-file"]
	}

	if status.ConnectionString == "" {
		status.ConnectionString = cluster.Spec.SeedConnectionString
	}

	if cluster.Spec.PendingRemovals != nil {
		for podName, address := range cluster.Spec.PendingRemovals {
			pods := &corev1.PodList{}
			err = r.List(context, pods, client.InNamespace(cluster.Namespace), client.MatchingFields{"metadata.name": podName})
			if err != nil {
				return false, err
			}
			if len(pods.Items) > 0 {
				instanceID := pods.Items[0].ObjectMeta.Labels[FDBInstanceIDLabel]
				processClass := processClassFromLabels(pods.Items[0].ObjectMeta.Labels)
				included, newStatus := fdbtypes.MarkProcessGroupForRemoval(status.ProcessGroups, instanceID, processClass, address)
				if !included {
					status.ProcessGroups = append(status.ProcessGroups, newStatus)
				}
			}
		}
	}

	if cluster.Status.PendingRemovals != nil {
		for instanceID, state := range cluster.Status.PendingRemovals {
			pods := &corev1.PodList{}
			err = r.List(context, pods, client.InNamespace(cluster.Namespace), client.MatchingFields{"metadata.name": state.PodName})
			if err != nil {
				return false, err
			}
			var processClass fdbtypes.ProcessClass
			if len(pods.Items) > 0 {
				processClass = processClassFromLabels(pods.Items[0].ObjectMeta.Labels)
			}
			included, newStatus := fdbtypes.MarkProcessGroupForRemoval(status.ProcessGroups, instanceID, processClass, state.Address)
			if !included {
				status.ProcessGroups = append(status.ProcessGroups, newStatus)
			}
		}
		cluster.Status.PendingRemovals = nil
	}

	status.HasIncorrectConfigMap = status.HasIncorrectConfigMap || !reflect.DeepEqual(existingConfigMap.Data, configMap.Data) || !metadataMatches(existingConfigMap.ObjectMeta, configMap.ObjectMeta)

	service := GetHeadlessService(cluster)
	existingService := &corev1.Service{}
	err = r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, existingService)
	if err != nil && k8serrors.IsNotFound(err) {
		existingService = nil
	} else if err != nil {
		return false, err
	}

	status.HasIncorrectServiceConfig = (service == nil) != (existingService == nil)

	if databaseStatus != nil {
		status.Health.Available = databaseStatus.Client.DatabaseStatus.Available
		status.Health.Healthy = databaseStatus.Client.DatabaseStatus.Healthy
		status.Health.FullReplication = databaseStatus.Cluster.FullReplication
		status.Health.DataMovementPriority = databaseStatus.Cluster.Data.MovingData.HighestPriority
	}

	if status.Configured && cluster.Status.ConnectionString != "" {
		coordinatorsValid, _, err := checkCoordinatorValidity(cluster, databaseStatus)
		if err != nil {
			return false, err
		}

		status.NeedsNewCoordinators = !coordinatorsValid
	}

	if len(cluster.Spec.LockOptions.DenyList) > 0 && cluster.ShouldUseLocks() && status.Configured {
		lockClient, err := r.getLockClient(cluster)
		if err != nil {
			return false, err
		}
		denyList, err := lockClient.GetDenyList()
		if err != nil {
			return false, err
		}
		if len(denyList) == 0 {
			denyList = nil
		}
		status.Locks.DenyList = denyList
	}

	// Sort the storage servers per Disk to prevent a reodering to issue a new reconcile loop.
	sort.Ints(status.StorageServersPerDisk)

	originalStatus := cluster.Status.DeepCopy()

	// Sort ProcessGroups by ProcessGroupID otherwise this can result in an endless loop when the
	// order changes.
	sort.SliceStable(status.ProcessGroups, func(i, j int) bool {
		return status.ProcessGroups[i].ProcessGroupID < status.ProcessGroups[j].ProcessGroupID
	})

	cluster.Status = status

	_, err = cluster.CheckReconciliation()
	if err != nil {
		return false, err
	}

	// See: https://github.com/kubernetes-sigs/kubebuilder/issues/592
	// If we use the default reflect.DeepEqual method it will be recreating the
	// status multiple times because the pointers are different.
	if !equality.Semantic.DeepEqual(cluster.Status, *originalStatus) {
		err = r.Status().Update(context, cluster)
		if err != nil {
			log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			return false, err
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (s UpdateStatus) RequeueAfter() time.Duration {
	return 0
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

// tryConnectionOptions attempts to connect with all the combinations of
// versions and connection strings for this cluster and returns the set that
// allow connecting to the cluster.
func tryConnectionOptions(cluster *fdbtypes.FoundationDBCluster, r *FoundationDBClusterReconciler) (string, string, error) {
	versions := optionList(cluster.Status.RunningVersion, cluster.Spec.Version)
	connectionStrings := optionList(cluster.Status.ConnectionString, cluster.Spec.SeedConnectionString)

	originalVersion := cluster.Status.RunningVersion
	originalConnectionString := cluster.Status.ConnectionString

	if len(versions) == 1 && len(connectionStrings) == 1 {
		return originalVersion, originalConnectionString, nil
	}

	log.Info("Trying connection options",
		"namespace", cluster.Namespace, "cluster", cluster.Name,
		"version", versions, "connectionString", connectionStrings)

	var firstError error = nil

	defer func() { cluster.Status.RunningVersion = originalVersion }()
	defer func() { cluster.Status.ConnectionString = originalConnectionString }()

	for _, version := range versions {
		for _, connectionString := range connectionStrings {
			log.Info("Attempting to get status from cluster",
				"namespace", cluster.Namespace, "cluster", cluster.Name,
				"version", version, "connectionString", connectionString)
			cluster.Status.RunningVersion = version
			cluster.Status.ConnectionString = connectionString
			adminClient, clientErr := r.AdminClientProvider(cluster, r)

			if clientErr != nil {
				return originalVersion, originalConnectionString, clientErr
			}
			defer adminClient.Close()

			activeConnectionString, err := adminClient.GetConnectionString()
			if err == nil {
				log.Info("Chose connection option",
					"namespace", cluster.Namespace, "cluster", cluster.Name,
					"version", version, "connectionString", activeConnectionString)
				return version, activeConnectionString, err
			}
			log.Error(err, "Error getting status from cluster",
				"namespace", cluster.Namespace, "cluster", cluster.Name,
				"version", version, "connectionString", connectionString)
			if firstError == nil {
				firstError = err
			}
		}
	}
	return originalVersion, originalConnectionString, firstError
}

// CheckAndSetProcessStatus checks the status of the Process and if missing or incorrect add it to the related status field
func CheckAndSetProcessStatus(r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, processMap map[string][]fdbtypes.FoundationDBStatusProcessInfo, status *fdbtypes.FoundationDBClusterStatus, processNumber int, processCount int, processGroupStatus *fdbtypes.ProcessGroupStatus) error {
	instanceID := instance.GetInstanceID()

	if processCount > 1 {
		instanceID = fmt.Sprintf("%s-%d", instanceID, processNumber)
	}

	processStatus := processMap[instanceID]

	processGroupStatus.UpdateCondition(fdbtypes.MissingProcesses, len(processStatus) == 0, cluster.Status.ProcessGroups, instanceID)
	if len(processStatus) == 0 {
		return nil
	}

	podClient, err := r.getPodClient(cluster, instance)
	correct := false
	if err != nil {
		log.Error(err, "Error getting pod client", "instance", instance.Metadata.Name)
	} else {
		for _, process := range processStatus {
			commandLine, err := GetStartCommand(cluster, instance, podClient, processNumber, processCount)
			if err != nil {
				return err
			}
			correct = commandLine == process.CommandLine && (process.Version == cluster.Spec.Version || process.Version == fmt.Sprintf("%s-PRERELEASE", cluster.Spec.Version))

			if !correct {
				log.Info("IncorrectProcess", "expected", commandLine, "got", process.CommandLine)
			}
		}
	}

	processGroupStatus.UpdateCondition(fdbtypes.IncorrectCommandLine, !correct, cluster.Status.ProcessGroups, instanceID)

	return nil
}

func validateInstances(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBClusterStatus, processMap map[string][]fdbtypes.FoundationDBStatusProcessInfo, instances []FdbInstance, configMap *corev1.ConfigMap) ([]*fdbtypes.ProcessGroupStatus, error) {
	processGroups := status.ProcessGroups
	processGroupMap := make(map[string]*fdbtypes.ProcessGroupStatus, len(processGroups))

	for _, processGroup := range processGroups {
		processGroupMap[processGroup.ProcessGroupID] = processGroup
	}

	// TODO (johscheuer): should be process specific #377
	configMapHash, err := GetDynamicConfHash(configMap)
	if err != nil {
		return processGroups, err
	}

	for _, instance := range instances {
		processClass := instance.GetProcessClass()
		instanceID := instance.GetInstanceID()

		processGroupStatus, found := processGroupMap[instanceID]
		if !found {
			processGroupStatus = fdbtypes.NewProcessGroupStatus(instanceID, processClass, nil)
			processGroups = append(processGroups, processGroupStatus)
			processGroupMap[instanceID] = processGroupStatus
		}

		processGroupStatus.Addresses = append(processGroupStatus.Addresses, instance.GetPublicIPs()...)

		if r.PodIPProvider != nil && instance.Pod != nil {
			processGroupStatus.Addresses = append(processGroupStatus.Addresses, r.PodIPProvider(instance.Pod))
		}

		processGroupStatus.Addresses = cleanAddressList((processGroupStatus.Addresses))

		processCount := 1

		// If the instance is not being removed and the Pod is not set we need to put it into
		// the failing list.
		isBeingRemoved := cluster.InstanceIsBeingRemoved(instanceID)

		missingPod := instance.Pod == nil && !isBeingRemoved
		processGroupStatus.UpdateCondition(fdbtypes.MissingPod, missingPod, processGroups, instanceID)
		if missingPod {
			continue
		}

		// Even the instance will be removed we need to keep the config around.
		// Set the processCount for the instance specific storage servers per pod
		if processClass == fdbtypes.ProcessClassStorage {
			processCount, err = getStorageServersPerPodForInstance(&instance)
			if err != nil {
				return processGroups, err
			}

			status.AddStorageServerPerDisk(processCount)
		}

		if isBeingRemoved {
			processGroupStatus.Remove = true
			continue
		}

		// In theory we could also support multiple processes per pod for different classes
		for i := 1; i <= processCount; i++ {
			err := CheckAndSetProcessStatus(r, cluster, instance, processMap, status, i, processCount, processGroupStatus)
			if err != nil {
				return processGroups, err
			}
		}

		needsSidecarConfInConfigMap, err := validateInstance(r, context, cluster, instance, configMapHash, processGroupStatus)

		if err != nil {
			return processGroups, err
		}

		if needsSidecarConfInConfigMap {
			status.NeedsSidecarConfInConfigMap = needsSidecarConfInConfigMap
		}
	}

	return processGroups, nil
}

// validateInstance runs specific checks for the status of an instance.
// returns failing, incorrect, error
func validateInstance(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, configMapHash string, processGroupStatus *fdbtypes.ProcessGroupStatus) (bool, error) {
	processClass := instance.GetProcessClass()
	instanceID := instance.GetInstanceID()

	processGroupStatus.UpdateCondition(fdbtypes.MissingPod, instance.Pod == nil, cluster.Status.ProcessGroups, instanceID)
	if instance.Pod == nil {
		return false, nil
	}

	_, idNum, err := ParseInstanceID(instanceID)
	if err != nil {
		return false, err
	}

	specHash, err := GetPodSpecHash(cluster, instance.GetProcessClass(), idNum, nil)
	if err != nil {
		return false, err
	}

	incorrectPod := !metadataMatches(*instance.Metadata, getPodMetadata(cluster, processClass, instanceID, specHash))
	if !incorrectPod {
		updated, err := r.PodLifecycleManager.InstanceIsUpdated(r, context, cluster, instance)
		if err != nil {
			return false, err
		}
		incorrectPod = !updated
	}

	processGroupStatus.UpdateCondition(fdbtypes.IncorrectPodSpec, incorrectPod, cluster.Status.ProcessGroups, instanceID)

	incorrectConfigMap := instance.Metadata.Annotations[LastConfigMapKey] != configMapHash

	processGroupStatus.UpdateCondition(fdbtypes.IncorrectConfigMap, incorrectConfigMap, cluster.Status.ProcessGroups, instanceID)

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, getPodListOptions(cluster, processClass, instanceID)...)
	if err != nil {
		return false, err
	}
	desiredPvc, err := GetPvc(cluster, processClass, idNum)
	if err != nil {
		return false, err
	}

	incorrectPVC := (len(pvcs.Items) == 1) != (desiredPvc != nil)
	if !incorrectPVC && desiredPvc != nil {
		incorrectPVC = !metadataMatches(pvcs.Items[0].ObjectMeta, desiredPvc.ObjectMeta)
	}

	processGroupStatus.UpdateCondition(fdbtypes.MissingPVC, incorrectPVC, cluster.Status.ProcessGroups, instanceID)

	var needsSidecarConfInConfigMap bool
	for _, container := range instance.Pod.Spec.Containers {
		if container.Name == "foundationdb" {
			image := strings.Split(container.Image, ":")
			version, err := fdbtypes.ParseFdbVersion(image[len(image)-1])
			if err != nil {
				return false, err
			}
			if !version.PrefersCommandLineArgumentsInSidecar() {
				needsSidecarConfInConfigMap = true
			}
		}
	}

	failing := false
	for _, container := range instance.Pod.Status.ContainerStatuses {
		if !container.Ready {
			failing = true
			break
		}
	}

	processGroupStatus.UpdateCondition(fdbtypes.PodFailing, failing, cluster.Status.ProcessGroups, instanceID)

	return needsSidecarConfInConfigMap, nil
}

func removeDuplicateConditions(status fdbtypes.FoundationDBClusterStatus) {
	for _, processGroupStatus := range status.ProcessGroups {
		conditionTimes := make(map[fdbtypes.ProcessGroupConditionType]int64, len(processGroupStatus.ProcessGroupConditions))
		copiedConditions := make(map[fdbtypes.ProcessGroupConditionType]bool, len(processGroupStatus.ProcessGroupConditions))
		conditions := make([]*fdbtypes.ProcessGroupCondition, 0, len(processGroupStatus.ProcessGroupConditions))
		for _, condition := range processGroupStatus.ProcessGroupConditions {
			existingTime, present := conditionTimes[condition.ProcessGroupConditionType]
			if !present || existingTime > condition.Timestamp {
				conditionTimes[condition.ProcessGroupConditionType] = condition.Timestamp
			}
		}
		for _, condition := range processGroupStatus.ProcessGroupConditions {
			if condition.Timestamp == conditionTimes[condition.ProcessGroupConditionType] && !copiedConditions[condition.ProcessGroupConditionType] {
				conditions = append(conditions, condition)
				copiedConditions[condition.ProcessGroupConditionType] = true
			}
		}
		processGroupStatus.ProcessGroupConditions = conditions
	}
}

// This method removes duplicates and empty strings from a list of addresses.
func cleanAddressList(addresses []string) []string {
	result := make([]string, 0, len(addresses))
	resultMap := make(map[string]bool)
	for _, value := range addresses {
		if value != "" && !resultMap[value] {
			result = append(result, value)
			resultMap[value] = true
		}
	}
	return result
}
