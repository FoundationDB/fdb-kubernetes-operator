/*
 * generate_initial_cluster_file.go
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

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/coordinator"

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/locality"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// generateInitialClusterFile provides a reconciliation step for generating the
// cluster file for a newly created cluster.
type generateInitialClusterFile struct{}

// reconcile runs the reconciler's work.
func (g generateInitialClusterFile) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	if cluster.Status.ConnectionString != "" {
		return nil
	}

	logger.Info("Generating initial cluster file")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "GenerateInitialCoordinators", "Choosing initial coordinators")

	processCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &requeue{curError: err}
	}

	var pods = make([]*corev1.Pod, 0, processCounts.Total())
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			logger.V(1).Info("Ignore process group marked for removal",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		// Ignore all process groups that are not eligible as a coordinator.
		if !cluster.IsEligibleAsCandidate(processGroup.ProcessClass) {
			continue
		}

		pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, processGroup.GetPodName(cluster))
		// If a Pod is not found ignore it for now.
		if err != nil {
			logger.V(1).Info("Could not find Pod for process group ID",
				"processGroupID", processGroup.ProcessGroupID)
			continue
		}

		if pod.Status.Phase != corev1.PodRunning {
			logger.V(1).Info("Ignore process group with Pod not in running state",
				"processGroupID", processGroup.ProcessGroupID,
				"phase", pod.Status.Phase)
			continue
		}

		pods = append(pods, pod)
	}

	count := cluster.DesiredCoordinatorCount()
	if len(pods) < count {
		return &requeue{
			message: fmt.Sprintf("cannot find enough running Pods to recruit coordinators. Require %d, got %d Pods", count, len(pods)),
			delay:   podSchedulingDelayDuration,
		}
	}

	var clusterName string
	if cluster.Spec.PartialConnectionString.DatabaseName != "" {
		clusterName = cluster.Spec.PartialConnectionString.DatabaseName
	} else {
		clusterName = fdbv1beta2.SanitizeConnectionStringDescription(cluster.Name)
	}

	connectionString := fdbv1beta2.ConnectionString{DatabaseName: clusterName}
	if cluster.Spec.PartialConnectionString.GenerationID != "" {
		connectionString.GenerationID = cluster.Spec.PartialConnectionString.GenerationID
	} else {
		err := connectionString.GenerateNewGenerationID()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	processLocality := make([]locality.Info, 0, len(pods))
	for _, pod := range pods {
		client, message := r.getPodClient(cluster, pod)
		if client == nil {
			return &requeue{message: message, delay: podSchedulingDelayDuration}
		}
		currentLocality, err := locality.InfoFromSidecar(cluster, client)
		if err != nil {
			return &requeue{curError: err}
		}
		if currentLocality.ID == "" {
			processGroupID := internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta)
			logger.Info("Pod is ineligible to be a coordinator due to missing locality information", "processGroupID", processGroupID)
			continue
		}
		currentLocality.Priority = cluster.GetClassCandidatePriority(currentLocality.Class)
		processLocality = append(processLocality, currentLocality)
	}

	limits := locality.GetHardLimits(cluster)
	// Only for the three data hall mode we allow a less restrictive selection of the initial coordinators.
	// The reason for this is that we don't know the data_hall locality until the fdbserver processes are running
	// as they will report the data_hall locality. So this is a workaround to allow an easy bring up of a three_data_hall
	// cluster with the unified image. Once the processes are reporting and the cluster is configured, the operator
	// will choose 9 coordinators spread across the 3 data halls.
	if cluster.Spec.DatabaseConfiguration.RedundancyMode == fdbv1beta2.RedundancyModeThreeDataHall {
		count = 3
		delete(limits, fdbv1beta2.FDBLocalityDataHallKey)
	}

	coordinators, err := locality.ChooseDistributedProcesses(cluster, processLocality, count, locality.ProcessSelectionConstraint{
		HardLimits: limits,
	})
	if err != nil {
		return &requeue{curError: err}
	}

	for _, currentLocality := range coordinators {
		connectionString.Coordinators = append(connectionString.Coordinators, coordinator.GetCoordinatorAddress(cluster, currentLocality).String())
	}

	// Ensure that the connection string is in a valid format.
	err = connectionString.Validate()
	if err != nil {
		return &requeue{curError: err}
	}

	cluster.Status.ConnectionString = connectionString.String()
	err = r.updateOrApply(ctx, cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}
