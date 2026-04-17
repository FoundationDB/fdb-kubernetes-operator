/*
 * delete_failed_pods.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2026 Apple Inc. and the FoundationDB project authors
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
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// deleteTerminalPods provides a reconciliation step for deleting Pods stuck in a
// terminal state (Failed, or Succeeded) that will not recover on their own. These
// pods will later be recreated by the operator.
type deleteTerminalPods struct{}

// reconcile runs the reconciler's work.
func (d deleteTerminalPods) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	_ *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) != nil {
			logger.Info("Ignore process group stuck in terminating", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		if err != nil {
			logger.Info("Could not find Pod for process group ID", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		phase := pod.Status.Phase
		reason := pod.Status.Reason
		if phase != corev1.PodFailed && phase != corev1.PodSucceeded {
			continue
		}

		var minimumAge time.Duration
		if phase == corev1.PodFailed && reason == "NodeAffinity" {
			// The desired node is not available. Give it some time to be brought
			// rather than recreating the pod multiple times.
			// See https://github.com/kubernetes/kubernetes/issues/92067
			minimumAge = 5 * time.Minute
		} else {
			// In case of a "crash loop", this wait serves to slow down pod recreation.
			minimumAge = 30 * time.Second
		}

		if pod.CreationTimestamp.Add(minimumAge).After(time.Now()) {
			logger.Info("Pod in terminal state is too young to be deleted",
				"processGroupID", processGroup.ProcessGroupID,
				"phase", phase,
				"reason", reason,
				"minimumAge", minimumAge)
			continue
		}

		err = r.PodLifecycleManager.DeletePod(logr.NewContext(ctx, logger), r, pod)
		if err != nil {
			logger.Info("Deleting pod that is stuck in a terminal state",
				"processGroupID", processGroup.ProcessGroupID,
				"phase", phase,
				"reason", reason)
			return &requeue{curError: err}
		}
	}
	return nil
}
