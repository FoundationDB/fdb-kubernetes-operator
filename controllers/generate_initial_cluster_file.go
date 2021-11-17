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
	ctx "context"
	"fmt"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// generateInitialClusterFile provides a reconciliation step for generating the
// cluster file for a newly created cluster.
type generateInitialClusterFile struct{}

// reconcile runs the reconciler's work.
func (g generateInitialClusterFile) reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "generateInitialClusterFile")
	if cluster.Status.ConnectionString != "" {
		return nil
	}

	logger.Info("Generating initial cluster file")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing initial coordinators")
	pods, err := r.PodLifecycleManager.GetPods(r, cluster, context, internal.GetPodListOptions(cluster, fdbtypes.ProcessClassStorage, "")...)
	if err != nil {
		return &requeue{curError: err}
	}

	count := cluster.DesiredCoordinatorCount()
	if len(pods) < count {
		return &requeue{
			message: fmt.Sprintf("cannot find enough Pods to recruit coordinators. Require %d, got %d Pods", count, len(pods)),
			delay:   podSchedulingDelayDuration,
		}
	}

	var clusterName string
	if cluster.Spec.PartialConnectionString.DatabaseName != "" {
		clusterName = cluster.Spec.PartialConnectionString.DatabaseName
	} else {
		clusterName = connectionStringNameRegex.ReplaceAllString(cluster.Name, "_")
	}

	connectionString := fdbtypes.ConnectionString{DatabaseName: clusterName}
	if cluster.Spec.PartialConnectionString.GenerationID != "" {
		connectionString.GenerationID = cluster.Spec.PartialConnectionString.GenerationID
	} else {
		err = connectionString.GenerateNewGenerationID()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	processLocality := make([]localityInfo, 0, len(pods))
	for indexOfProcess := range pods {
		client, message := r.getPodClient(cluster, pods[indexOfProcess])
		if client == nil {
			return &requeue{message: message, delay: podSchedulingDelayDuration}
		}
		locality, err := localityInfoFromSidecar(cluster, client)
		if err != nil {
			return &requeue{curError: err}
		}
		if locality.ID == "" {
			logger.Info("Pod is ineligible to be a coordinator due to missing locality information", "podName", pods[indexOfProcess].Name)
			continue
		}
		processLocality = append(processLocality, locality)
	}

	coordinators, err := chooseDistributedProcesses(cluster, processLocality, count, processSelectionConstraint{})
	if err != nil {
		return &requeue{curError: err}
	}

	for _, locality := range coordinators {
		connectionString.Coordinators = append(connectionString.Coordinators, locality.Address.String())
	}

	cluster.Status.ConnectionString = connectionString.String()

	err = r.Status().Update(context, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	return nil
}
