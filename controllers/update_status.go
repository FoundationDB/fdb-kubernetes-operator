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
	ctx "context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

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
func (s UpdateStatus) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	originalStatus := cluster.Status.DeepCopy()
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
			return &Requeue{Error: err}
		}
		cluster.Status.RunningVersion = version
		cluster.Status.ConnectionString = connectionString

		adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
		if err != nil {
			return &Requeue{Error: err}
		}
		defer adminClient.Close()

		databaseStatus, err = adminClient.GetStatus()
		if err != nil {
			return &Requeue{Error: err}
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
		return &Requeue{Error: err}
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
				return &Requeue{Error: err}
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
		return &Requeue{Error: err}
	}

	status.ProcessGroups = make([]*fdbtypes.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup != nil && processGroup.ProcessGroupID != "" {
			status.ProcessGroups = append(status.ProcessGroups, processGroup)
		}
	}

	status.ProcessGroups, err = validateInstances(r, context, cluster, &status, processMap, instances, configMap)
	if err != nil {
		return &Requeue{Error: err}
	}
	removeDuplicateConditions(status)

	// Track all PVCs
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcs, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, pvc := range pvcs.Items {
		processGroupID := pvc.Labels[fdbtypes.FDBInstanceIDLabel]
		if fdbtypes.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbtypes.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(pvc.Labels), nil))
	}

	// Track all Services
	services := &corev1.ServiceList{}
	err = r.List(context, services, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, service := range services.Items {
		processGroupID := service.Labels[fdbtypes.FDBInstanceIDLabel]
		if processGroupID == "" || fdbtypes.ContainsProcessGroupID(status.ProcessGroups, processGroupID) {
			continue
		}

		status.ProcessGroups = append(status.ProcessGroups, fdbtypes.NewProcessGroupStatus(processGroupID, internal.ProcessClassFromLabels(service.Labels), nil))
	}

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		status.HasIncorrectConfigMap = true
	} else if err != nil {
		return &Requeue{Error: err}
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
		status.ConnectionString = existingConfigMap.Data[clusterFileKey]
	}

	if status.ConnectionString == "" {
		status.ConnectionString = cluster.Spec.SeedConnectionString
	}

	if cluster.Spec.PendingRemovals != nil {
		for podName, address := range cluster.Spec.PendingRemovals {
			pods := &corev1.PodList{}
			err = r.List(context, pods, client.InNamespace(cluster.Namespace), client.MatchingFields{"metadata.name": podName})
			if err != nil {
				return &Requeue{Error: err}
			}
			if len(pods.Items) > 0 {
				instanceID := pods.Items[0].ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel]
				processClass := internal.ProcessClassFromLabels(pods.Items[0].ObjectMeta.Labels)
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
				return &Requeue{Error: err}
			}
			var processClass fdbtypes.ProcessClass
			if len(pods.Items) > 0 {
				processClass = internal.ProcessClassFromLabels(pods.Items[0].ObjectMeta.Labels)
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
		return &Requeue{Error: err}
	}

	status.HasIncorrectServiceConfig = (service == nil) != (existingService == nil)

	if databaseStatus != nil {
		status.Health.Available = databaseStatus.Client.DatabaseStatus.Available
		status.Health.Healthy = databaseStatus.Client.DatabaseStatus.Healthy
		status.Health.FullReplication = databaseStatus.Cluster.FullReplication
		status.Health.DataMovementPriority = databaseStatus.Cluster.Data.MovingData.HighestPriority
	}

	if status.Configured && cluster.Status.ConnectionString != "" {
		coordinatorStatus := make(map[string]bool, len(databaseStatus.Client.Coordinators.Coordinators))
		for _, coordinator := range databaseStatus.Client.Coordinators.Coordinators {
			coordinatorStatus[coordinator.Address] = false
		}

		coordinatorsValid, _, err := checkCoordinatorValidity(cluster, databaseStatus, coordinatorStatus)
		if err != nil {
			return &Requeue{Error: err}
		}

		status.NeedsNewCoordinators = !coordinatorsValid
	}

	if len(cluster.Spec.LockOptions.DenyList) > 0 && cluster.ShouldUseLocks() && status.Configured {
		lockClient, err := r.getLockClient(cluster)
		if err != nil {
			return &Requeue{Error: err}
		}
		denyList, err := lockClient.GetDenyList()
		if err != nil {
			return &Requeue{Error: err}
		}
		if len(denyList) == 0 {
			denyList = nil
		}
		status.Locks.DenyList = denyList
	}

	// Sort the storage servers per Disk to prevent a reodering to issue a new reconcile loop.
	sort.Ints(status.StorageServersPerDisk)
	// Sort ProcessGroups by ProcessGroupID otherwise this can result in an endless loop when the
	// order changes.
	sort.SliceStable(status.ProcessGroups, func(i, j int) bool {
		return status.ProcessGroups[i].ProcessGroupID < status.ProcessGroups[j].ProcessGroupID
	})

	cluster.Status = status

	_, err = cluster.CheckReconciliation()
	if err != nil {
		return &Requeue{Error: err}
	}

	// See: https://github.com/kubernetes-sigs/kubebuilder/issues/592
	// If we use the default reflect.DeepEqual method it will be recreating the
	// status multiple times because the pointers are different.
	if !equality.Semantic.DeepEqual(cluster.Status, *originalStatus) {
		err = r.Status().Update(context, cluster)
		if err != nil {
			log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			return &Requeue{Error: err}
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

	defer func() { cluster.Status.RunningVersion = originalVersion }()
	defer func() { cluster.Status.ConnectionString = originalConnectionString }()

	for _, version := range versions {
		for _, connectionString := range connectionStrings {
			log.Info("Attempting to get connection string from cluster",
				"namespace", cluster.Namespace, "cluster", cluster.Name,
				"version", version, "connectionString", connectionString)
			cluster.Status.RunningVersion = version
			cluster.Status.ConnectionString = connectionString
			adminClient, clientErr := r.getDatabaseClientProvider().GetAdminClient(cluster, r)

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
			log.Error(err, "Error getting connection string from cluster",
				"namespace", cluster.Namespace, "cluster", cluster.Name,
				"version", version, "connectionString", connectionString)
		}
	}
	return originalVersion, originalConnectionString, nil
}

// CheckAndSetProcessStatus checks the status of the Process and if missing or incorrect add it to the related status field
func CheckAndSetProcessStatus(r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster, instance FdbInstance, processMap map[string][]fdbtypes.FoundationDBStatusProcessInfo, processNumber int, processCount int, processGroupStatus *fdbtypes.ProcessGroupStatus) error {
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
	if err != nil {
		log.Error(err, "Error getting pod client", "instance", instance.Metadata.Name)
		return nil
	}

	correct := false
	for _, process := range processStatus {
		commandLine, err := GetStartCommand(cluster, instance, podClient, processNumber, processCount)
		if err != nil {
			return err
		}

		correct = commandLine == process.CommandLine

		settings := cluster.GetProcessSettings(instance.GetProcessClass())
		versionMatch := true
		// if we allow to override the tag we can't compare the versions here
		if !settings.GetAllowTagOverride() {
			versionMatch = process.Version == cluster.Spec.Version || process.Version == fmt.Sprintf("%s-PRERELEASE", cluster.Spec.Version)
		}

		// If the `EmptyMonitorConf` is set, the commandline is by definition wrong since there should be no running processes.
		correct = correct && versionMatch && !cluster.Spec.Buggify.EmptyMonitorConf

		if !correct {
			log.Info("IncorrectProcess", "namespace", cluster.Namespace, "cluster", cluster.Name, "expected", commandLine, "got", process.CommandLine, "expectedVersion", cluster.Spec.Version, "version", process.Version, "processGroupID", instanceID)
		}
	}

	processGroupStatus.UpdateCondition(fdbtypes.IncorrectCommandLine, !correct, cluster.Status.ProcessGroups, instanceID)

	return nil
}

func validateInstances(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster, status *fdbtypes.FoundationDBClusterStatus, processMap map[string][]fdbtypes.FoundationDBStatusProcessInfo, instances []FdbInstance, configMap *corev1.ConfigMap) ([]*fdbtypes.ProcessGroupStatus, error) {
	processGroups := status.ProcessGroups
	processGroupMap := make(map[string]*fdbtypes.ProcessGroupStatus, len(processGroups))
	// TODO (johscheuer): make use of internal.None once the other PR is merged
	processGroupsWithoutExclusion := make(map[string]struct{}, len(cluster.Spec.InstancesToRemoveWithoutExclusion))

	for _, processGroupID := range cluster.Spec.InstancesToRemoveWithoutExclusion {
		processGroupsWithoutExclusion[processGroupID] = struct{}{}
	}

	for _, processGroup := range processGroups {
		processGroupMap[processGroup.ProcessGroupID] = processGroup
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

		processGroupStatus.AddAddresses(instance.GetPublicIPs())

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
		var err error
		if processClass == fdbtypes.ProcessClassStorage {
			processCount, err = getStorageServersPerPodForInstance(&instance)
			if err != nil {
				return processGroups, err
			}

			status.AddStorageServerPerDisk(processCount)
		}

		if isBeingRemoved {
			processGroupStatus.Remove = true
			// Check if we should skip exclusion for the process group
			_, ok := processGroupsWithoutExclusion[processGroupStatus.ProcessGroupID]
			processGroupStatus.ExclusionSkipped = ok
			continue
		}

		// In theory we could also support multiple processes per pod for different classes
		for i := 1; i <= processCount; i++ {
			err := CheckAndSetProcessStatus(r, cluster, instance, processMap, i, processCount, processGroupStatus)
			if err != nil {
				return processGroups, err
			}
		}

		serverPerPod, err := getStorageServersPerPodForInstance(&instance)
		if err != nil {
			return processGroups, err
		}

		configMapHash, err := getDynamicConfHash(configMap, instance.GetProcessClass(), serverPerPod)
		if err != nil {
			return processGroups, err
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

	incorrectConfigMap := instance.Metadata.Annotations[fdbtypes.LastConfigMapKey] != configMapHash
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
			version, err := fdbtypes.ParseFdbVersion(cluster.Status.RunningVersion)
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

	// Fix for https://github.com/kubernetes/kubernetes/issues/92067
	// This will delete the Pod that is stuck in the "NodeAffinity"
	// at a later stage the Pod will be recreated by the operator.
	if instance.Pod.Status.Phase == corev1.PodFailed {
		failing = true

		// Only recreate the Pod if it is already 5 minutes up (just to prevent to recreate the Pod multiple times
		// and give the cluster some time to get the kubelet up
		if instance.Pod.Status.Reason == "NodeAffinity" && instance.Pod.CreationTimestamp.Add(5*time.Minute).Before(time.Now()) {
			log.Info("Delete Pod that is stuck in NodeAffinity",
				"namespace", cluster.Namespace,
				"cluster", cluster.Name,
				"processGroupID", instanceID)

			err = r.PodLifecycleManager.DeleteInstance(r, context, instance)
			if err != nil {
				return needsSidecarConfInConfigMap, err
			}
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
