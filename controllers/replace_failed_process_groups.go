/*
 * replace_failed_process_groups.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors.
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
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal/replacements"
)

// replaceFailedProcessGroups identifies processes groups that have failed and need to be
// replaced.
type replaceFailedProcessGroups struct{}

// return non-nil requeue if a process has been replaced
func (c replaceFailedProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	// If the EmptyMonitorConf setting is set we expect that all fdb processes in this part of the cluster are missing. In order
	// to prevent the operator from replacing any process groups we skip this reconciliation here.
	if cluster.Spec.Buggify.EmptyMonitorConf {
		logger.V(1).Info("Skipping because EmptyMonitorConf is set to true")
		return nil
	}

	// If the status is not cached, we have to fetch it.
	if status == nil {
		adminClient, err := r.getAdminClient(logger, cluster)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
		defer func() {
			_ = adminClient.Close()
		}()

		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	// If the database is unavailable skip further steps as the operator is not able to make any good decision about the automatic
	// replacements.
	if !status.Client.DatabaseStatus.Available {
		logger.Info("Skipping replaceFailedProcessGroups reconciler as the database is not available.")
		return &requeue{message: "cluster is not available", delayedRequeue: true, delay: 5 * time.Second}
	}

	// Only replace process groups without an address, if the cluster has the desired fault tolerance and is available.
	hasDesiredFaultTolerance := fdbstatus.HasDesiredFaultToleranceFromStatus(logger, status, cluster)
	hasReplacement, hasMoreFailedProcesses := replacements.ReplaceFailedProcessGroups(logger, cluster, status, hasDesiredFaultTolerance)
	// If the reconciler replaced at least one process group we want to update the status and requeue.
	if hasReplacement {
		err := r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}

		return &requeue{message: "Removals have been updated in the cluster status"}
	}

	// If there are more failed processes that are not yet automatically replaced, we want the controller to requeue this
	// request to make sure it takes another attempt to replace the faulty process group(s).
	if hasMoreFailedProcesses {
		return &requeue{message: "More failed process groups are detected", delayedRequeue: true, delay: 5 * time.Minute}
	}

	return nil
}
