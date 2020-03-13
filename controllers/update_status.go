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
}

// Reconcile runs the reconciler's work.
func (s UpdateStatus) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	status := fdbtypes.FoundationDBClusterStatus{}
	status.Generations.Reconciled = cluster.Status.Generations.Reconciled
	status.IncorrectProcesses = make(map[string]int64)
	status.MissingProcesses = make(map[string]int64)

	var databaseStatus *fdbtypes.FoundationDBStatus
	processMap := make(map[string][]fdbtypes.FoundationDBStatusProcessInfo)

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
					correct = commandLine == process.CommandLine && process.Version == cluster.Spec.Version
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
		}
	}

	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return false, err
	}
	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existingConfigMap)
	if err != nil && k8serrors.IsNotFound(err) {
		status.HasIncorrectConfigMap = true
	} else if err != nil {
		return false, err
	}

	status.HasIncorrectConfigMap = status.HasIncorrectConfigMap || !reflect.DeepEqual(existingConfigMap.Data, configMap.Data) || !metadataMatches(existingConfigMap.ObjectMeta, configMap.ObjectMeta)

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
