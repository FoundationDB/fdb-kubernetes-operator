/*
 * exclude_instances.go
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

// ExcludeInstances provides a reconciliation step for excluding instances from
// the database.
type ExcludeInstances struct{}

func (e ExcludeInstances) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		if address != "" {
			addresses = append(addresses, cluster.GetFullAddress(address))
		}
	}

	if len(addresses) > 0 {
		err = adminClient.ExcludeInstances(addresses)
		r.Recorder.Event(cluster, "Normal", "ExcludingProcesses", fmt.Sprintf("Excluding %v", addresses))
		if err != nil {
			return false, err
		}
	}

	remaining := addresses
	for len(remaining) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return false, err
		}
		if len(remaining) > 0 {
			log.Info("Waiting for exclusions to complete", "namespace", cluster.Namespace, "cluster", cluster.Name, "remainingServers", remaining)
			time.Sleep(time.Second)
		}
	}

	return true, nil
}

func (e ExcludeInstances) RequeueAfter() time.Duration {
	return 0
}
