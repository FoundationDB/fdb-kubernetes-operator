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

package foundationdbcluster

import (
	ctx "context"
	"reflect"
	"strings"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateStatus provides a reconciliation step for updating the status in the
// CRD.
type UpdateStatus struct {
	UpdateGenerations bool
}

func (s UpdateStatus) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	status := fdbtypes.FoundationDBClusterStatus{}
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
			address := strings.Split(process.Address, ":")
			processMap[address[0]] = append(processMap[address[0]], process)
		}

		status.DatabaseConfiguration = databaseStatus.Cluster.DatabaseConfiguration.FillInDefaultsFromStatus()
	} else {
		databaseStatus = nil
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}

	hasIncorrectPodSpecs := false

	for _, instance := range instances {
		processClass := instance.Metadata.Labels["fdb-process-class"]

		_, pendingRemoval := cluster.Spec.PendingRemovals[instance.Metadata.Name]
		if !pendingRemoval {
			status.ProcessCounts.IncreaseCount(processClass, 1)
		}

		var processStatus []fdbtypes.FoundationDBStatusProcessInfo
		if instance.Pod == nil {
			processStatus = nil
		} else {
			podClient, err := r.getPodClient(context, cluster, instance)
			if err != nil {
				log.Error(err, "Error getting pod IP", "namespace", cluster.Namespace, "name", cluster.Name, "instance", instance.Metadata.Name)
				processStatus = nil
			} else {
				ip := podClient.GetPodIP()
				processStatus = processMap[ip]
			}
		}
		if len(processStatus) == 0 {
			existingTime, exists := cluster.Status.MissingProcesses[instance.Metadata.Name]
			if exists {
				status.MissingProcesses[instance.Metadata.Name] = existingTime
			} else {
				status.MissingProcesses[instance.Metadata.Name] = time.Now().Unix()
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
				existingTime, exists := cluster.Status.IncorrectProcesses[instance.Metadata.Name]
				if exists {
					status.IncorrectProcesses[instance.Metadata.Name] = existingTime
				} else {
					status.IncorrectProcesses[instance.Metadata.Name] = time.Now().Unix()
				}
			}
		}

		if instance.Pod != nil {
			spec := GetPodSpec(cluster, instance.Metadata.Labels["fdb-process-class"], instance.Metadata.Labels["fdb-instance-id"])
			specHash, err := hashPodSpec(spec)
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

	reconciled := cluster.Spec.Configured &&
		len(cluster.Spec.PendingRemovals) == 0 &&
		cluster.GetProcessCountsWithDefaults().CountsAreSatisfied(status.ProcessCounts) &&
		len(status.IncorrectProcesses) == 0 &&
		!hasIncorrectPodSpecs &&
		databaseStatus != nil &&
		reflect.DeepEqual(status.DatabaseConfiguration, cluster.DesiredDatabaseConfiguration()) &&
		configMapUpdated

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

	cluster.Status = status
	err = r.postStatusUpdate(context, cluster)
	if err != nil {
		log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
		return false, err
	}

	return true, nil
}

func (s UpdateStatus) RequeueAfter() time.Duration {
	return 0
}
