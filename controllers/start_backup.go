/*
 * start_backup.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// StartBackup provides a reconciliation step for starting a new backup.
type StartBackup struct {
}

// Reconcile runs the reconciler's work.
func (s StartBackup) Reconcile(r *FoundationDBBackupReconciler, context ctx.Context, backup *fdbtypes.FoundationDBBackup) (bool, error) {
	if !backup.ShouldRun() || (backup.Status.BackupDetails != nil && backup.Status.BackupDetails.Running) {
		return true, nil
	}

	adminClient, err := r.AdminClientForBackup(context, backup)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	err = adminClient.StartBackup(backup.BackupURL(), backup.SnapshotPeriodSeconds())
	if err != nil {
		return false, err
	}

	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (s StartBackup) RequeueAfter() time.Duration {
	return 0
}
