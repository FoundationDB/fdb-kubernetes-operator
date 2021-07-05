/*
 * exclude_instances.go
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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ExcludeInstances provides a reconciliation step for excluding instances from
// the database.
type ExcludeInstances struct{}

// Reconcile runs the reconciler's work.
func (e ExcludeInstances) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &Requeue{Error: err}
	}
	defer adminClient.Close()

	removalCount := 0
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.Remove {
			removalCount++
		}
	}

	addresses := make([]string, 0, removalCount)

	if removalCount > 0 {
		exclusions, err := adminClient.GetExclusions()
		if err != nil {
			return &Requeue{Error: err}
		}

		currentExclusionMap := make(map[string]bool, len(exclusions))
		for _, address := range exclusions {
			currentExclusionMap[address] = true
		}

		for _, processGroup := range cluster.Status.ProcessGroups {
			for _, address := range processGroup.Addresses {
				if processGroup.Remove && !processGroup.ExclusionSkipped && !currentExclusionMap[address] {
					addresses = append(addresses, address)
				}
			}
		}
	}

	if len(addresses) > 0 {
		// Block excludes on missing processes not marked for removal
		missingProcesses := make([]string, 0)
		for _, processGroupStatus := range cluster.Status.ProcessGroups {
			processMissingTime := processGroupStatus.GetConditionTime(fdbtypes.MissingProcesses)
			podMissingTime := processGroupStatus.GetConditionTime(fdbtypes.MissingPod)
			if (processMissingTime != nil || podMissingTime != nil) && !processGroupStatus.Remove {
				missingProcesses = append(missingProcesses, processGroupStatus.ProcessGroupID)
			}
		}
		if len(missingProcesses) > 0 {
			log.Info("Waiting for missing processes", "namespace", cluster.Namespace, "cluster", cluster.Name, "missingProcesses", missingProcesses)
			return &Requeue{Message: fmt.Sprintf("Waiting for missing processes: %v", missingProcesses)}
		}

		hasLock, err := r.takeLock(cluster, fmt.Sprintf("excluding instances: %v", addresses))
		if !hasLock {
			return &Requeue{Error: err}
		}

		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ExcludingProcesses", fmt.Sprintf("Excluding %v", addresses))

		err = adminClient.ExcludeInstances(addresses)

		if err != nil {
			return &Requeue{Error: err}
		}
	}

	return nil
}
