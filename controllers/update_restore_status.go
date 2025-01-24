/*
 * update_restore_status.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2024 Apple Inc. and the FoundationDB project authors
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
	"regexp"
	"strings"
)

// updateRestoreStatus provides a reconciliation step for updating the restore status.
type updateRestoreStatus struct {
}

// reconcile runs the reconciler's work.
func (updateRestoreStatus) reconcile(ctx context.Context, r *FoundationDBRestoreReconciler, restore *fdbv1beta2.FoundationDBRestore) *requeue {
	adminClient, err := r.adminClientForRestore(ctx, restore)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	status, err := adminClient.GetRestoreStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	parsedState := parseRestoreStatus(status)
	// If the correct status is already present, we can ignore it.
	if restore.Status.State == parsedState {
		return nil
	}

	restore.Status.State = parsedState
	err = r.updateOrApply(ctx, restore)
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}

// parseRestoreStatus returns the restore state based on the restore status output
func parseRestoreStatus(status string) fdbv1beta2.FoundationDBRestoreState {
	if strings.TrimSpace(status) == "" {
		return fdbv1beta2.UnknownFoundationDBRestoreState
	}

	statusRe := regexp.MustCompile(`State:\s([\w]*)`)
	result := statusRe.FindStringSubmatch(status)

	// Expected to find exact one state in the status.
	if len(result) != 2 {
		return fdbv1beta2.UnknownFoundationDBRestoreState
	}

	return fdbv1beta2.FoundationDBRestoreState(result[1])
}
