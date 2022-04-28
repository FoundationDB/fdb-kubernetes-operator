/*
 * exclude_processes.go
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
	"math"
	"net"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
)

// The fraction of processes that must be present in order to start a new
// exclusion.
var missingProcessThreshold = 0.8

// excludeProcesses provides a reconciliation step for excluding processes from
// the database.
type excludeProcesses struct{}

// reconcile runs the reconciler's work.
func (e excludeProcesses) reconcile(_ context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	removalCount := 0
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			removalCount++
		}
	}

	fdbProcessesToExclude := make([]fdbv1beta2.ProcessAddress, 0, removalCount)
	processClassesToExclude := make(map[fdbv1beta2.ProcessClass]fdbv1beta2.None)
	if removalCount > 0 {
		exclusions, err := adminClient.GetExclusions()
		if err != nil {
			return &requeue{curError: err}
		}
		log.Info("current exclusions", "ex", exclusions)

		err = getProcessesToExclude(exclusions, cluster, &fdbProcessesToExclude, processClassesToExclude)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	if len(fdbProcessesToExclude) > 0 {
		for processClass := range processClassesToExclude {
			canExclude, missingProcesses := canExcludeNewProcesses(cluster, processClass)
			if !canExclude {
				// We want to delay the requeue so that the operator can do some other tasks
				// before retrying.
				return &requeue{
					message:        fmt.Sprintf("Waiting for missing processes: %v. Addresses to exclude: %v", missingProcesses, fdbProcessesToExclude),
					delayedRequeue: true,
				}
			}
		}

		hasLock, err := r.takeLock(cluster, fmt.Sprintf("excluding processes: %v", fdbProcessesToExclude))
		if !hasLock {
			return &requeue{curError: err}
		}

		r.Recorder.Event(cluster, corev1.EventTypeNormal, "ExcludingProcesses", fmt.Sprintf("Excluding %v", fdbProcessesToExclude))

		err = adminClient.ExcludeProcesses(fdbProcessesToExclude)
		if err != nil {
			// If we run into a timeout error don't requeue directly
			// to allow the operator to take some other tasks.
			// This can be useful with blocking exclusions and exclusions
			// that take a longer time.
			if internal.IsTimeoutError(err) {
				return &requeue{
					message:        err.Error(),
					delayedRequeue: true,
				}
			}

			return &requeue{curError: err}
		}
	}

	return nil
}

func getProcessesToExclude(exclusions []fdbv1beta2.ProcessAddress, cluster *fdbv1beta2.FoundationDBCluster, fdbProcessesToExclude *[]fdbv1beta2.ProcessAddress, processClassesToExclude map[fdbv1beta2.ProcessClass]fdbv1beta2.None) error {
	currentExclusionMap := make(map[string]fdbv1beta2.None, len(exclusions))
	for _, exclusion := range exclusions {
		currentExclusionMap[exclusion.String()] = fdbv1beta2.None{}
	}
	fdbVersion, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return err
	}
	for _, processGroup := range cluster.Status.ProcessGroups {
		// Process already excluded using locality, so we don't have to exclude it again
		if _, ok := currentExclusionMap[processGroup.GetExclusionString()]; ok {
			continue
		}

		if fdbVersion.IsAtLeast(fdbv1beta2.Versions.LatestFdbVersion) && processGroup.IsMarkedForRemoval() && !processGroup.IsExcluded() {
			*fdbProcessesToExclude = append(*fdbProcessesToExclude, fdbv1beta2.ProcessAddress{StringAddress: processGroup.GetExclusionString()})
			processClassesToExclude[processGroup.ProcessClass] = fdbv1beta2.None{}
			continue
		}

		for _, address := range processGroup.Addresses {
			// Already excluded, so we don't have to exclude it again
			if _, ok := currentExclusionMap[address]; ok {
				continue
			}

			if processGroup.IsMarkedForRemoval() && !processGroup.IsExcluded() {
				*fdbProcessesToExclude = append(*fdbProcessesToExclude, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(address)})
				processClassesToExclude[processGroup.ProcessClass] = fdbv1beta2.None{}
			}
		}
	}
	return nil
}

func canExcludeNewProcesses(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass) (bool, []string) {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "excludeProcesses")

	// Block excludes on missing processes not marked for removal
	missingProcesses := make([]string, 0)
	validProcesses := make([]string, 0)

	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.IsMarkedForRemoval() || processGroupStatus.ProcessClass != processClass {
			continue
		}

		if processGroupStatus.GetConditionTime(fdbv1beta2.MissingProcesses) != nil ||
			processGroupStatus.GetConditionTime(fdbv1beta2.MissingPod) != nil {
			missingProcesses = append(missingProcesses, processGroupStatus.ProcessGroupID)
			logger.Info("Missing processes", "processGroupID", processGroupStatus.ProcessGroupID)
			continue
		}

		validProcesses = append(validProcesses, processGroupStatus.ProcessGroupID)
	}

	desiredProcesses, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		logger.Error(err, "Error calculating process counts")
		return false, missingProcesses
	}
	desiredCount := desiredProcesses.Map()[processClass]

	if len(validProcesses) < desiredCount-1 && len(validProcesses) < int(math.Ceil(float64(desiredCount)*missingProcessThreshold)) {
		return false, missingProcesses
	}

	return true, nil
}
