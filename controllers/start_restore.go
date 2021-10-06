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
	ctx "context"
	"strings"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// startRestore provides a reconciliation step for starting a new restore.
type startRestore struct {
}

// reconcile runs the reconciler's work.
func (s startRestore) reconcile(r *FoundationDBRestoreReconciler, context ctx.Context, restore *fdbtypes.FoundationDBRestore) *requeue {
	adminClient, err := r.adminClientForRestore(context, restore)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	status, err := adminClient.GetRestoreStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	if len(strings.TrimSpace(status)) == 0 {
		err = adminClient.StartRestore(restore.Spec.BackupURL, restore.Spec.KeyRanges)
		if err != nil {
			return &requeue{curError: err}
		}

		restore.Status.Running = true
		err = r.Status().Update(context, restore)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
