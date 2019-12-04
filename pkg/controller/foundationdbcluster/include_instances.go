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

package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

// IncludeInstances provides a reconciliation step for re-including instances
// after removing them.
type IncludeInstances struct{}

func (i IncludeInstances) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, cluster.GetFullAddress(address))
	}

	if len(addresses) > 0 {
		r.Recorder.Event(cluster, "Normal", "IncludingInstances", fmt.Sprintf("Including removed processes: %v", addresses))
	}

	err = adminClient.IncludeInstances(addresses)
	if err != nil {
		return false, err
	}

	if cluster.Spec.PendingRemovals != nil {
		cluster.Spec.PendingRemovals = nil
		r.Update(context, cluster)
		return false, nil
	}

	return true, nil
}

func (i IncludeInstances) RequeueAfter() time.Duration {
	return 0
}
