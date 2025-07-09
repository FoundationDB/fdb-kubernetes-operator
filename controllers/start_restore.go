/*
 * start_restore.go
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
	"context"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// startRestore provides a reconciliation step for starting a new restore.
type startRestore struct {
}

// reconcile runs the reconciler's work.
func (s startRestore) reconcile(
	ctx context.Context,
	r *FoundationDBRestoreReconciler,
	restore *fdbv1beta2.FoundationDBRestore,
) *requeue {
	adminClient, err := r.adminClientForRestore(ctx, restore)
	if err != nil {
		return &requeue{curError: err}
	}
	defer func() {
		_ = adminClient.Close()
	}()

	status, err := adminClient.GetRestoreStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	// TODO (johscheuer): Make use of the status.state setting to see if the restore was started.
	if len(strings.TrimSpace(status)) == 0 {
		err = adminClient.StartRestore(
			restore.BackupURL(),
			restore.Spec.KeyRanges,
			restore.Spec.EncryptionKeyPath,
		)
		if err != nil {
			return &requeue{curError: err}
		}

		restore.Status.Running = true
		err = r.updateOrApply(ctx, restore)
		if err != nil {
			return &requeue{curError: err}
		}

		return nil
	}

	return nil
}
