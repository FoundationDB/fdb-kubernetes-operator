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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal/replacements"
)

// replaceFailedProcessGroups identifies processes groups that have failed and need to be
// replaced.
type replaceFailedProcessGroups struct{}

// reconcile runs the reconciler's work.
// return non-nil requeue if a process has been replaced
func (c replaceFailedProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster) *requeue {
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "reconciler", "replaceFailedProcessGroups")
	// If the EmptyMonitorConf setting is set we expect that all fdb processes in this part of the cluster are missing. In order
	// to prevent the operator from replacing any process groups we skip this reconciliation here.
	if cluster.Spec.Buggify.EmptyMonitorConf {
		logger.V(1).Info("Skipping because EmptyMonitorConf is set to true")
		return nil
	}

	adminClient, err := r.DatabaseClientProvider.GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	if replacements.ReplaceFailedProcessGroups(logger, cluster, adminClient) {
		err := r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}

		return &requeue{message: "Removals have been updated in the cluster status"}
	}

	return nil
}
