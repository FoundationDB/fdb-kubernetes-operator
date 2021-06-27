/*
 * toggle_backup_paused.go
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

// ToggleBackupPaused provides a reconciliation step for pausing an unpausing
// backups.
type ToggleBackupPaused struct {
}

// Reconcile runs the reconciler's work.
func (s ToggleBackupPaused) Reconcile(r *FoundationDBBackupReconciler, context ctx.Context, backup *fdbtypes.FoundationDBBackup) *Requeue {
	if backup.Status.BackupDetails == nil {
		if backup.ShouldRun() {
			return &Requeue{Message: "Cannot toggle backup state because backup is not running"}
		}
		return nil
	}

	if backup.ShouldBePaused() && !backup.Status.BackupDetails.Paused {
		adminClient, err := r.AdminClientForBackup(context, backup)
		if err != nil {
			return &Requeue{Error: err}
		}
		defer adminClient.Close()

		err = adminClient.PauseBackups()
		if err != nil {
			return &Requeue{Error: err}
		}
		return nil
	} else if !backup.ShouldBePaused() && backup.Status.BackupDetails.Paused {
		adminClient, err := r.AdminClientForBackup(context, backup)
		if err != nil {
			return &Requeue{Error: err}
		}
		defer adminClient.Close()

		err = adminClient.ResumeBackups()
		if err != nil {
			return &Requeue{Error: err}
		}
		return nil
	}

	return nil
}
