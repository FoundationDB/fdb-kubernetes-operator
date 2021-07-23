/*
 * update_config_map.go
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
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// UpdatePodConfig provides a reconciliation step for updating the dynamic conf
// for a all pods.
type UpdatePodConfig struct{}

// Reconcile runs the reconciler's work.
func (u UpdatePodConfig) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "UpdatePodConfig")
	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &Requeue{Error: err}
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &Requeue{Error: err}
	}

	instanceProcessGroupMap := make(map[string]FdbInstance, len(instances))
	for _, instance := range instances {
		instanceProcessGroupMap[instance.GetInstanceID()] = instance
	}

	allSynced := true
	hasUpdate := false
	var errs []error
	// We try to update all instances and if we observe an error we add it to the error list.
	for _, processGroup := range cluster.Status.ProcessGroups {
		curLogger := logger.WithValues("processGroupID", processGroup.ProcessGroupID)

		if cluster.SkipProcessGroup(processGroup) {
			curLogger.Info("Process group has pending Pod, will be skipped")
			continue
		}

		instance, ok := instanceProcessGroupMap[processGroup.ProcessGroupID]
		if !ok || instance.Pod == nil || instance.Metadata == nil {
			curLogger.Info("Could not find Pod for process group")
			// TODO (johscheuer): we should requeue if that happens.
			continue
		}

		serverPerPod, err := getStorageServersPerPodForInstance(&instance)
		if err != nil {
			curLogger.Info("Error when receiving storage server per Pod", "error", err)
			errs = append(errs, err)
			continue
		}

		configMapHash, err := internal.GetDynamicConfHash(configMap, instance.GetProcessClass(), serverPerPod)
		if err != nil {
			curLogger.Info("Error when receiving dynamic ConfigMap hash", "error", err)
			errs = append(errs, err)
			continue
		}

		if instance.Metadata.Annotations[fdbtypes.LastConfigMapKey] == configMapHash {
			continue
		}

		synced, err := r.updatePodDynamicConf(cluster, instance)
		if !synced {
			allSynced = false
			hasUpdate = true
			curLogger.Info("Update dynamic Pod config", "synced", synced, "error", err)
			if internal.IsNetworkError(err) {
				processGroup.UpdateCondition(fdbtypes.SidecarUnreachable, true, cluster.Status.ProcessGroups, processGroup.ProcessGroupID)
			} else {
				processGroup.UpdateCondition(fdbtypes.IncorrectConfigMap, true, cluster.Status.ProcessGroups, processGroup.ProcessGroupID)
			}

			instance.Metadata.Annotations[fdbtypes.OutdatedConfigMapKey] = time.Now().Format(time.RFC3339)
			err = r.PodLifecycleManager.UpdateMetadata(r, context, cluster, instance)
			if err != nil {
				allSynced = false
				curLogger.Info("Update Pod ConfigMap annotation", "error", err)
			}
			continue
		}

		instance.Metadata.Annotations[fdbtypes.LastConfigMapKey] = configMapHash
		delete(instance.Metadata.Annotations, fdbtypes.OutdatedConfigMapKey)
		err = r.PodLifecycleManager.UpdateMetadata(r, context, cluster, instance)
		if err != nil {
			allSynced = false
			curLogger.Info("Update Pod metadata", "error", err)
			errs = append(errs, err)
		}

		hasUpdate = true
		processGroup.UpdateCondition(fdbtypes.SidecarUnreachable, false, cluster.Status.ProcessGroups, processGroup.ProcessGroupID)
	}

	if hasUpdate {
		err = r.Status().Update(context, cluster)
		if err != nil {
			return &Requeue{Error: err}
		}
	}

	// If any error has happened return the first error
	if len(errs) > 0 {
		return &Requeue{Error: errs[0]}
	}

	// If we return an error we don't requeue
	// So we just return that we can't continue but don't have an error
	if !allSynced {
		return &Requeue{Message: "Waiting for Pod to receive ConfigMap update", Delay: podSchedulingDelayDuration}
	}

	return nil
}
