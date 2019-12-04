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

package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

// BounceProcesses provides a reconciliation step for bouncing fdbserver
// processes.
type BounceProcesses struct{}

func (b BounceProcesses) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Status.IncorrectProcesses))
	for instanceName := range cluster.Status.IncorrectProcesses {
		instances, err := r.PodLifecycleManager.GetInstances(r, context, getSinglePodListOptions(cluster, instanceName))
		if err != nil {
			return false, err
		}
		if len(instances) == 0 {
			return false, MissingPodErrorByName(instanceName, cluster)
		}

		podClient, err := r.getPodClient(context, cluster, instances[0])
		if err != nil {
			return false, err
		}
		addresses = append(addresses, cluster.GetFullAddress(podClient.GetPodIP()))

		synced, err := r.updatePodDynamicConf(context, cluster, instances[0])
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
				fmt.Sprintf("Spec require a bounce of some processes, but killing processes is disabled"))
			cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
			err = r.postStatusUpdate(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Kills are disabled"}
		}

		log.Info("Bouncing instances", "namespace", cluster.Namespace, "cluster", cluster.Name, "addresses", addresses)
		r.Recorder.Event(cluster, "Normal", "BouncingInstances", fmt.Sprintf("Bouncing processes: %v", addresses))
		err = adminClient.KillInstances(addresses)
		if err != nil {
			return false, err
		}
	}

	if cluster.Spec.RunningVersion != cluster.Spec.Version {
		cluster.Spec.RunningVersion = cluster.Spec.Version
		err = r.Update(context, cluster)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

func (b BounceProcesses) RequeueAfter() time.Duration {
	return 0
}
