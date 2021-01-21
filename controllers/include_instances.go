/*
 * include_instances.go
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
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// IncludeInstances provides a reconciliation step for re-including instances
// after removing them.
type IncludeInstances struct{}

// Reconcile runs the reconciler's work.
func (i IncludeInstances) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0)

	needsUpdate := false

	processGroups := make([]*fdbtypes.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			addresses = append(addresses, processGroup.Addresses...)
			needsUpdate = true
		} else {
			processGroups = append(processGroups, processGroup)
		}
	}

	if len(addresses) > 0 {
		r.Recorder.Event(cluster, "Normal", "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))
	}

	err = adminClient.IncludeInstances(addresses)
	if err != nil {
		return false, err
	}

	if cluster.Status.PendingRemovals != nil {
		cluster.Status.PendingRemovals = nil
		needsUpdate = true
	}

	needsSpecUpdate := cluster.Spec.PendingRemovals != nil
	if needsSpecUpdate {
		err := r.clearPendingRemovalsFromSpec(context, cluster)
		if err != nil {
			return false, err
		}
	}

	if needsUpdate {
		cluster.Status.ProcessGroups = processGroups
		err := r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return !needsSpecUpdate, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (i IncludeInstances) RequeueAfter() time.Duration {
	return 0
}
