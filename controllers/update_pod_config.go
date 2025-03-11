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
	"github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	"strconv"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// updatePodConfig provides a reconciliation step for updating the dynamic conf
// for all Pods.
type updatePodConfig struct{}

// reconcile runs the reconciler's work.
func (updatePodConfig) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	originalStatus := cluster.Status.DeepCopy()
	allSynced := true
	delayedRequeue := true
	var errs []error
	// We try to update all process groups and if we observe an error we add it to the error list.
	for _, processGroup := range cluster.Status.ProcessGroups {
		curLogger := logger.WithValues("processGroupID", processGroup.ProcessGroupID)
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			curLogger.V(1).Info("Ignore process group marked that is stuck terminating")
			continue
		}

		if cluster.SkipProcessGroup(processGroup) {
			curLogger.Info("Process group has pending Pod, will be skipped")
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		originalPod := pod.DeepCopy()
		// If a Pod is not found ignore it for now.
		if err != nil {
			logger.V(1).Info("Could not find Pod for process group ID")
			continue
		}

		if !pod.DeletionTimestamp.IsZero() {
			logger.V(1).Info("ignore Pod that has a deletion timestamp for config updates")
			continue
		}

		serverPerPod, err := internal.GetServersPerPodForPod(pod, processGroup.ProcessClass)
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

		configMapHash, err := internal.GetDynamicConfHash(configMap, processClass, internal.GetImageType(pod), serverPerPod)
		if err != nil {
			curLogger.Error(err, "Error when receiving dynamic ConfigMap hash")
			errs = append(errs, err)
			continue
		}

		// If we do a cluster version incompatible upgrade we use the fdbv1beta2.IncorrectConfigMap to signal when the operator
		// can restart fdbserver processes. Since the ConfigMap itself won't change during the upgrade we have to run the updatePodDynamicConf
		// to make sure all process groups have the required files ready. In the future we will use a different condition to indicate that a
		// process group si ready to be restarted.
		if pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey] == configMapHash && !cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
			continue
		}

		synced, err := r.updatePodDynamicConf(curLogger, cluster, pod)
		if !synced {
			allSynced = false
			if err != nil {
				curLogger.Error(err, "Update Pod ConfigMap annotation")
			}

			if internal.IsNetworkError(err) && processGroup.GetConditionTime(fdbv1beta2.SidecarUnreachable) == nil {
				curLogger.Info("process group sidecar is not reachable")
				processGroup.UpdateCondition(fdbv1beta2.SidecarUnreachable, true)
			} else if processGroup.GetConditionTime(fdbv1beta2.IncorrectConfigMap) == nil {
				processGroup.UpdateCondition(fdbv1beta2.IncorrectConfigMap, true)
				// If we are still waiting for a ConfigMap update we should not delay the requeue to ensure all processes are bounced
				// at the same time. If the process is unreachable e.g. has the SidecarUnreachable status we can delay the requeue.
				delayedRequeue = false
			}

			pod.ObjectMeta.Annotations[api.OutdatedConfigMapAnnotation] = strconv.FormatInt(time.Now().Unix(), 10)
			err = r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod, originalPod)
			if err != nil {
				allSynced = false
				curLogger.Error(err, "Update Pod ConfigMap annotation")
				errs = append(errs, err)
			}
			continue
		}

		// Update the LastConfigMapKey annotation once the Pod was updated.
		if pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey] != configMapHash {
			pod.ObjectMeta.Annotations[fdbv1beta2.LastConfigMapKey] = configMapHash
			delete(pod.ObjectMeta.Annotations, api.OutdatedConfigMapAnnotation)
			err = r.PodLifecycleManager.UpdateMetadata(ctx, r, cluster, pod, originalPod)
			if err != nil {
				allSynced = false
				curLogger.Error(err, "Update Pod metadata")
				errs = append(errs, err)
			}
		}

		processGroup.UpdateCondition(fdbv1beta2.SidecarUnreachable, false)
	}

	if !equality.Semantic.DeepEqual(cluster.Status, *originalStatus) {
		err = r.updateOrApply(ctx, cluster)
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
		return &requeue{message: "Waiting for Pod to receive ConfigMap update", delay: podSchedulingDelayDuration, delayedRequeue: delayedRequeue}
	}

	return nil
}
