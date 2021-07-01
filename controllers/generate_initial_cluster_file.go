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

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// GenerateInitialClusterFile provides a reconciliation step for generating the
// cluster file for a newly created cluster.
type GenerateInitialClusterFile struct{}

// Reconcile runs the reconciler's work.
func (g GenerateInitialClusterFile) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	if cluster.Status.ConnectionString != "" {
		return nil
	}

	log.Info("Generating initial cluster file", "namespace", cluster.Namespace, "cluster", cluster.Name)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ChangingCoordinators", "Choosing initial coordinators")
	instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getPodListOptions(cluster, fdbtypes.ProcessClassStorage, "")...)
	if err != nil {
		return &Requeue{Error: err}
	}
	err = sortInstancesByID(instances)
	if err != nil {
		return &Requeue{Error: err}
	}

	count := cluster.DesiredCoordinatorCount()
	if len(instances) < count {
		return &Requeue{Message: "cannot find enough pods to recruit coordinators", Delay: podSchedulingDelayDuration}
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
			return &Requeue{Error: err}
		}
	}

	processLocality := make([]localityInfo, len(instances))
	for indexOfProcess := range instances {
		client, message := r.getPodClient(cluster, instances[indexOfProcess])
		if client == nil {
			return &Requeue{Message: message, Delay: podSchedulingDelayDuration}
		}
		locality, err := localityInfoFromSidecar(cluster, client)
		if err != nil {
			return &Requeue{Error: err}
		}
		processLocality[indexOfProcess] = locality
	}

	coordinators, err := chooseDistributedProcesses(processLocality, count, processSelectionConstraint{})
	if err != nil {
		return &Requeue{Error: err}
	}

	for _, locality := range coordinators {
		connectionString.Coordinators = append(connectionString.Coordinators, locality.Address)
	}

	cluster.Status.ConnectionString = connectionString.String()

	err = r.Status().Update(context, cluster)
	return &Requeue{Error: err}
}
