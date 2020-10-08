/*
 * bounce_processes.go
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
	"math"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

// BounceProcesses provides a reconciliation step for bouncing fdbserver
// processes.
type BounceProcesses struct{}

// Reconcile runs the reconciler's work.
func (b BounceProcesses) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}

	minimumUptime := math.Inf(1)
	addressMap := make(map[string]string, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		addressMap[process.Locality["instance_id"]] = process.Address
		if process.UptimeSeconds < minimumUptime {
			minimumUptime = process.UptimeSeconds
		}
	}

	addresses := make([]string, 0, len(cluster.Status.IncorrectProcesses))

	for instanceID := range cluster.Status.IncorrectProcesses {

		if addressMap[instanceID] == "" {
			return false, fmt.Errorf("Could not find address for instance %s", instanceID)
		}

		addresses = append(addresses, addressMap[instanceID])

		instances, err := r.PodLifecycleManager.GetInstances(r, cluster, context, getSinglePodListOptions(cluster, instanceID)...)
		if err != nil {
			return false, err
		}
		if len(instances) == 0 {
			return false, MissingPodErrorByName(instanceID, cluster)
		}

		synced, err := r.updatePodDynamicConf(cluster, instances[0])
		if !synced {
			return synced, err
		}
	}

	if len(addresses) > 0 {
		var enabled = cluster.Spec.AutomationOptions.KillProcesses
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return false, err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsBounce",
				"Spec require a bounce of some processes, but killing processes is disabled")
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Kills are disabled"}
		}

		if minimumUptime < MinimumUptimeSecondsForBounce {
			r.Recorder.Event(cluster, "Normal", "NeedsBounce",
				fmt.Sprintf("Spec require a bounce of some processes, but the cluster has only been up for %f seconds", minimumUptime))
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.Status().Update(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Cluster needs to stabilize before bouncing"}
		}

		log.Info("Bouncing instances", "namespace", cluster.Namespace, "cluster", cluster.Name, "addresses", addresses)
		r.Recorder.Event(cluster, "Normal", "BouncingInstances", fmt.Sprintf("Bouncing processes: %v", addresses))
		err = adminClient.KillInstances(addresses)
		if err != nil {
			return false, err
		}
	}

	if cluster.Status.RunningVersion != cluster.Spec.Version {
		cluster.Status.RunningVersion = cluster.Spec.Version
		err = r.Status().Update(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (b BounceProcesses) RequeueAfter() time.Duration {
	return 0
}
