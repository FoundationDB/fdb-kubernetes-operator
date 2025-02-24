/*
 * replace_misconfigured_process_groups.go
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

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/replacements"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// replaceMisconfiguredProcessGroups identifies processes that need to be replaced in
// order to bring up new processes with different configuration.
type replaceMisconfiguredProcessGroups struct{}

// reconcile runs the reconciler's work.
func (c replaceMisconfiguredProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	hasReplacements, err := replacements.ReplaceMisconfiguredProcessGroups(ctx, r.PodLifecycleManager, r, logger, cluster, r.ReplaceOnSecurityContextChange)
	if err != nil {
		return &requeue{curError: err}
	}

	if hasReplacements {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}

		logger.Info("Removals have been updated in the cluster status")
	}

	return nil
}
