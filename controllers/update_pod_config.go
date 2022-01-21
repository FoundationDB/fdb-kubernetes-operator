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
	"context"
	"fmt"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// updatePodConfig provides a reconciliation step for updating the dynamic conf
// for a all pods.
type updatePodConfig struct{}

// reconcile runs the reconciler's work.
func (updatePodConfig) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "updatePodConfig")
	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
	if err != nil {
		return &requeue{curError: err}
	}

	podMap := internal.CreatePodMap(cluster, pods)

	allSynced := true
	hasUpdate := false
	var errs []error
	// We try to update all process groups and if we observe an error we add it to the error list.
	for _, processGroup := range cluster.Status.ProcessGroups {
		curLogger := logger.WithValues("processGroupID", processGroup.ProcessGroupID)

		if processGroup.IsMarkedForRemoval() {
			curLogger.V(1).Info("Ignore process group marked for removal")
			continue
		}

		if cluster.SkipProcessGroup(processGroup) {
			curLogger.Info("Process group has pending Pod, will be skipped")
			continue
		}

		pod, ok := podMap[processGroup.ProcessGroupID]
		if !ok || pod == nil {
			curLogger.Info("Could not find Pod for process group")
			continue
		}

		serverPerPod, err := internal.GetStorageServersPerPodForPod(pod)
		if err != nil {
			curLogger.Error(err, "Error when receiving storage server per Pod")
			errs = append(errs, err)
			continue
		}

		processClass, err := podmanager.GetProcessClass(cluster, pod)
		if err != nil {
			curLogger.Error(err, "Error when fetching process class from Pod")
			errs = append(errs, err)
			continue
		}

		imageType := internal.GetImageType(pod)

		configMapHash, err := internal.GetDynamicConfHash(configMap, processClass, imageType, serverPerPod)
		if err != nil {
			curLogger.Error(err, "Error when receiving dynamic ConfigMap hash")
			errs = append(errs, err)
			continue
		}

		if pod.ObjectMeta.Annotations[fdbtypes.LastConfigMapKey] == configMapHash {
			continue
		}

		synced, err := r.updatePodDynamicConf(cluster, pod)
		if !synced {
			allSynced = false
			hasUpdate = true
			if err != nil {
				curLogger.Error(err, "Update Pod ConfigMap annotation")
			}
			if internal.IsNetworkError(err) {
				processGroup.UpdateCondition(fdbtypes.SidecarUnreachable, true, cluster.Status.ProcessGroups, processGroup.ProcessGroupID)
			} else {
				processGroup.UpdateCondition(fdbtypes.IncorrectConfigMap, true, cluster.Status.ProcessGroups, processGroup.ProcessGroupID)
			}

			pod.ObjectMeta.Annotations[fdbtypes.OutdatedConfigMapKey] = fmt.Sprintf("%d", time.Now().Unix())
			err = r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod)
			if err != nil {
				allSynced = false
				curLogger.Error(err, "Update Pod ConfigMap annotation")
				errs = append(errs, err)
			}
			continue
		}

		pod.ObjectMeta.Annotations[fdbtypes.LastConfigMapKey] = configMapHash
		delete(pod.ObjectMeta.Annotations, fdbtypes.OutdatedConfigMapKey)
		err = r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod)
		if err != nil {
			allSynced = false
			curLogger.Error(err, "Update Pod metadata")
			errs = append(errs, err)
		}

		hasUpdate = true
		processGroup.UpdateCondition(fdbtypes.SidecarUnreachable, false, cluster.Status.ProcessGroups, processGroup.ProcessGroupID)
	}

	if hasUpdate {
		err = r.Status().Update(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	// If any error has happened requeue.
	// We don't provide an error here since we log all errors above.
	if len(errs) > 0 {
		return &requeue{message: "errors occurred during update pod config reconcile"}
	}

	// If we return an error we don't requeue
	// So we just return that we can't continue but don't have an error
	if !allSynced {
		return &requeue{message: "Waiting for Pod to receive ConfigMap update", delay: podSchedulingDelayDuration}
	}

	return nil
}
