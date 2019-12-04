/*
 * change_coordinators.go
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
	"errors"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

// ChangeCoordinators provides a reconciliation step for choosing new
// coordinators.
type ChangeCoordinators struct{}

func (c ChangeCoordinators) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if !cluster.Spec.Configured {
		return true, nil
	}

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}
	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range status.Client.Coordinators.Coordinators {
		coordinatorStatus[coordinator.Address] = false
	}

	if len(coordinatorStatus) == 0 {
		return false, errors.New("Unable to get coordinator status")
	}

	for _, process := range status.Cluster.Processes {
		_, isCoordinator := coordinatorStatus[process.Address]
		if isCoordinator && !process.Excluded {
			coordinatorStatus[process.Address] = true
		}
	}

	needsChange := len(coordinatorStatus) != cluster.DesiredCoordinatorCount()
	for _, healthy := range coordinatorStatus {
		needsChange = needsChange || !healthy
	}

	if needsChange {
		log.Info("Changing coordinators", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing new coordinators")
		coordinatorCount := cluster.DesiredCoordinatorCount()
		coordinators := make([]string, 0, coordinatorCount)
		for _, process := range status.Cluster.Processes {
			eligible := !process.Excluded && isStateful(process.ProcessClass)
			if eligible {
				coordinators = append(coordinators, process.Address)
			}
			if len(coordinators) >= coordinatorCount {
				break
			}
		}
		if len(coordinators) < coordinatorCount {
			return false, errors.New("Unable to recruit new coordinators")
		}
		connectionString, err := adminClient.ChangeCoordinators(coordinators)
		if err != nil {
			return false, err
		}
		cluster.Spec.ConnectionString = connectionString
		err = r.Update(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return !needsChange, nil
}

func (c ChangeCoordinators) RequeueAfter() time.Duration {
	return 0
}
