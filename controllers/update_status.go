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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateStatus provides a reconciliation step for updating the status in the
// CRD.
type UpdateStatus struct {
}

// Reconcile runs the reconciler's work.
func (s UpdateStatus) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.Generations.Reconciled = cluster.Status.Generations.Reconciled
	status.IncorrectProcesses = make(map[string]int64)
	status.MissingProcesses = make(map[string]int64)

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
		adminClient, err := r.AdminClientProvider(cluster, r)
		if err != nil {
			return false, err
		}
		defer adminClient.Close()
		databaseStatus, err = adminClient.GetStatus()
		if err != nil {
			if cluster.Spec.Version != cluster.Status.RunningVersion && cluster.Status.RunningVersion != "" {
				log.Info("Failed to get status; falling back to version from spec", "runningVersion", cluster.Status.RunningVersion, "newVersion", cluster.Spec.Version)
				originalRunningVersion := cluster.Status.RunningVersion
				cluster.Status.RunningVersion = cluster.Spec.Version
				fallbackAdminClient, clientErr := r.AdminClientProvider(cluster, r)
				if clientErr != nil {
					cluster.Status.RunningVersion = originalRunningVersion
					return false, clientErr
				}
				defer fallbackAdminClient.Close()
				databaseStatusFallback, errFallback := adminClient.GetStatus()
				if errFallback != nil {
					cluster.Status.RunningVersion = originalRunningVersion
					return false, err
				}
				databaseStatus = databaseStatusFallback
			} else {
				return false, err
			}
		}
	}

	for _, process := range databaseStatus.Cluster.Processes {
		instanceID := process.Locality["instance_id"]
		processMap[instanceID] = append(processMap[instanceID], process)
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

	status.IncorrectPods = make([]string, 0)
	status.FailingPods = make([]string, 0)

	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return false, err
	}
	configMapHash, err := GetDynamicConfHash(configMap)
	if err != nil {
		return false, err
	}

	for _, instance := range instances {
		processClass := instance.GetProcessClass()
		instanceID := instance.GetInstanceID()

		if cluster.InstanceIsBeingRemoved(instanceID) {
			continue
		}

		status.ProcessCounts.IncreaseCount(processClass, 1)

		processStatus := processMap[instanceID]
		if len(processStatus) == 0 {
			existingTime, exists := cluster.Status.MissingProcesses[instanceID]
			if exists {
				status.MissingProcesses[instanceID] = existingTime
			} else {
				status.MissingProcesses[instanceID] = time.Now().Unix()
			}
		} else {
			podClient, err := r.getPodClient(context, cluster, instance)
			correct := false
			if err != nil {
				log.Error(err, "Error getting pod client", "instance", instance.Metadata.Name)
			} else {
				for _, process := range processStatus {
					commandLine, err := GetStartCommand(cluster, instance, podClient)
					if err != nil {
						return false, err
					}
					correct = commandLine == process.CommandLine && (process.Version == cluster.Spec.Version || process.Version == fmt.Sprintf("%s-PRERELEASE", cluster.Spec.Version))
					break
				}
			}

			if !correct {
				instanceID := instance.GetInstanceID()
				existingTime, exists := cluster.Status.IncorrectProcesses[instanceID]
				if exists {
					status.IncorrectProcesses[instanceID] = existingTime
				} else {
					status.IncorrectProcesses[instanceID] = time.Now().Unix()
				}
			}
		}

		if instance.Pod != nil {
			id := instance.GetInstanceID()
			_, idNum, err := ParseInstanceID(id)
			if err != nil {
				return false, err
			}

			specHash, err := GetPodSpecHash(cluster, instance.GetProcessClass(), idNum, nil)
			if err != nil {
				return false, err
			}

			incorrectPod := !metadataMatches(*instance.Metadata, getPodMetadata(cluster, processClass, id, specHash))
			if !incorrectPod {
				updated, err := r.PodLifecycleManager.InstanceIsUpdated(r, context, cluster, instance)
				if err != nil {
					return false, err
				}
				incorrectPod = !updated
			}

			incorrectPod = incorrectPod || instance.Metadata.Annotations[LastConfigMapKey] != configMapHash

			pvcs := &corev1.PersistentVolumeClaimList{}
			err = r.List(context, pvcs, getPodListOptions(cluster, processClass, id)...)
			desiredPvc, err := GetPvc(cluster, processClass, idNum)
			if err != nil {
				return false, err
			}

			if (len(pvcs.Items) == 1) != (desiredPvc != nil) {
				incorrectPod = true
			}

			if !incorrectPod && desiredPvc != nil {
				incorrectPod = !metadataMatches(pvcs.Items[0].ObjectMeta, desiredPvc.ObjectMeta)
			}

			if incorrectPod {
				status.IncorrectPods = append(status.IncorrectPods, instance.Metadata.Name)
			}

			for _, container := range instance.Pod.Spec.Containers {
				if container.Name == "foundationdb" {
					image := strings.Split(container.Image, ":")
					version, err := fdbtypes.ParseFdbVersion(image[len(image)-1])
					if err != nil {
						return false, err
					}
					if !version.PrefersCommandLineArgumentsInSidecar() {
						status.NeedsSidecarConfInConfigMap = true
					}
				}
			}

			for _, container := range instance.Pod.Status.ContainerStatuses {
				if !container.Ready {
					status.FailingPods = append(status.FailingPods, instance.Metadata.Name)
				}
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

	status.PendingRemovals = cluster.Status.PendingRemovals

	if status.PendingRemovals == nil {
		if existingConfigMap.Data["pending-removals"] != "" {
			removals := map[string]fdbtypes.PendingRemovalState{}
			err = json.Unmarshal([]byte(existingConfigMap.Data["pending-removals"]), &removals)
			if err != nil {
				return false, err
			}
			status.PendingRemovals = removals
		} else if cluster.Spec.PendingRemovals != nil {
			status.PendingRemovals = make(map[string]fdbtypes.PendingRemovalState)
			for podName, address := range cluster.Spec.PendingRemovals {
				pods := &corev1.PodList{}
				err = r.List(context, pods, client.InNamespace(cluster.Namespace), client.MatchingField("metadata.name", podName))
				if err != nil {
					return false, err
				}
				if len(pods.Items) > 0 {
					instanceID := pods.Items[0].ObjectMeta.Labels["fdb-instance-id"]
					status.PendingRemovals[instanceID] = fdbtypes.PendingRemovalState{
						PodName: podName,
						Address: address,
					}
				}
			}
		}
	}

	status.HasIncorrectConfigMap = status.HasIncorrectConfigMap || !reflect.DeepEqual(existingConfigMap.Data, configMap.Data) || !metadataMatches(existingConfigMap.ObjectMeta, configMap.ObjectMeta)

	service, err := GetHeadlessService(cluster)
	if err != nil {
		return false, err
	}
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

	if len(status.IncorrectProcesses) == 0 {
		status.IncorrectProcesses = nil
	}
	if len(status.MissingProcesses) == 0 {
		status.MissingProcesses = nil
	}

	if status.Configured && cluster.Status.ConnectionString != "" {
		coordinatorsValid, _, err := checkCoordinatorValidity(cluster, databaseStatus)
		if err != nil {
			return false, err
		}

		status.NeedsNewCoordinators = !coordinatorsValid
	}

	originalStatus := cluster.Status.DeepCopy()

	cluster.Status = status

	_, err = cluster.CheckReconciliation()
	if err != nil {
		return false, err
	}

	if !reflect.DeepEqual(cluster.Status, *originalStatus) {
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
