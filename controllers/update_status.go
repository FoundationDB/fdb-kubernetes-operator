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
	"reflect"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateStatus provides a reconciliation step for updating the status in the
// CRD.
type UpdateStatus struct {
	UpdateGenerations bool
}

func (s UpdateStatus) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.IncorrectProcesses = make(map[string]int64)
	status.MissingProcesses = make(map[string]int64)

	var databaseStatus *fdbtypes.FoundationDBStatus
	processMap := make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)

	desiredAddressSet := fdbtypes.RequiredAddressSet{}
	if cluster.Spec.MainContainer.EnableTLS {
		desiredAddressSet.TLS = true
	} else {
		desiredAddressSet.NonTLS = true
	}

	status.RequiredAddresses = desiredAddressSet

	if cluster.Spec.Configured {
		adminClient, err := r.AdminClientProvider(cluster, r)
		if err != nil {
			return false, err
		}
		defer adminClient.Close()
		databaseStatus, err = adminClient.GetStatus()
		if err != nil {
			return false, err
		}
		for _, process := range databaseStatus.Cluster.Processes {
			instanceID := process.Locality["instance_id"]
			processMap[instanceID] = append(processMap[instanceID], process)
		}

		status.DatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.NormalizeConfiguration()
	} else {
		databaseStatus = nil
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
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

	hasIncorrectPodSpecs := false

	for _, instance := range instances {
		processClass := instance.Metadata.Labels["fdb-process-class"]
		instanceID := instance.Metadata.Labels["fdb-instance-id"]

		_, pendingRemoval := cluster.Spec.PendingRemovals[instance.Metadata.Name]
		if !pendingRemoval {
			status.ProcessCounts.IncreaseCount(processClass, 1)
		}

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
					correct = commandLine == process.CommandLine
					break
				}
			}

			if !correct {
				instanceID := instance.Metadata.Labels["fdb-instance-id"]
				existingTime, exists := cluster.Status.IncorrectProcesses[instanceID]
				if exists {
					status.IncorrectProcesses[instanceID] = existingTime
				} else {
					status.IncorrectProcesses[instanceID] = time.Now().Unix()
				}
			}
		}

		if instance.Pod != nil {
			_, idNum, err := ParseInstanceID(instance.Metadata.Labels["fdb-instance-id"])
			if err != nil {
				return false, err
			}

			specHash, err := GetPodSpecHash(cluster, instance.Metadata.Labels["fdb-process-class"], idNum, nil)
			if err != nil {
				return false, err
			}

			if instance.Metadata.Annotations[LastPodHashKey] != specHash {
				hasIncorrectPodSpecs = true
			}
		}
	}

	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return false, err
	}
	existingConfigMap := &corev1.ConfigMap{}
	configMapUpdated := true
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		configMapUpdated = false
	} else if err != nil {
		return false, err
	}

	configMapUpdated = reflect.DeepEqual(existingConfigMap.Data, configMap.Data) && reflect.DeepEqual(existingConfigMap.Labels, configMap.Labels)

	desiredCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return false, err
	}

	reconciled := cluster.Spec.Configured &&
		len(cluster.Spec.PendingRemovals) == 0 &&
		desiredCounts.CountsAreSatisfied(status.ProcessCounts) &&
		len(status.IncorrectProcesses) == 0 &&
		!hasIncorrectPodSpecs &&
		databaseStatus != nil &&
		reflect.DeepEqual(status.DatabaseConfiguration, cluster.DesiredDatabaseConfiguration()) &&
		configMapUpdated &&
		status.RequiredAddresses == desiredAddressSet

	if reconciled && s.UpdateGenerations {
		status.Generations = fdbtypes.GenerationStatus{Reconciled: cluster.ObjectMeta.Generation}
	} else {
		status.Generations = cluster.Status.Generations
	}

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

	if !reflect.DeepEqual(cluster.Status, status) {
		cluster.Status = status
		err = r.postStatusUpdate(context, cluster)
		if err != nil {
			log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			return false, err
		}
	}

	return true, nil
}

func (s UpdateStatus) RequeueAfter() time.Duration {
	return 0
}
