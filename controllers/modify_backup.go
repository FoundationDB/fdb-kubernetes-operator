/*
 * modify_backup.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// ModifyBackup provides a reconciliation step for modifying a backup's
// configuration.
type ModifyBackup struct {
}

// Reconcile runs the reconciler's work.
func (s ModifyBackup) Reconcile(r *FoundationDBBackupReconciler, context ctx.Context, backup *fdbtypes.FoundationDBBackup) *Requeue {
	if backup.Status.BackupDetails == nil || !backup.ShouldRun() {
		return nil
	}

	snapshotPeriod := backup.SnapshotPeriodSeconds()
	if backup.Status.BackupDetails.SnapshotPeriodSeconds != snapshotPeriod {
		adminClient, err := r.AdminClientForBackup(context, backup)
		if err != nil {
			return &Requeue{Error: err}
		}
		defer adminClient.Close()

		err = adminClient.ModifyBackup(snapshotPeriod)
		if err != nil {
			return &Requeue{Error: err}
		}
	}

	return nil
}
