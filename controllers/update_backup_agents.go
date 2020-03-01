/*
 * update_backup_agents.go
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
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateBackupAgents provides a reconciliation step for updating the
// deployment for the backup agents.
type UpdateBackupAgents struct{}

// Reconcile runs the reconciler's work.
func (u UpdateBackupAgents) Reconcile(r *FoundationDBBackupReconciler, context ctx.Context, backup *fdbtypes.FoundationDBBackup) (bool, error) {
	deploymentName := fmt.Sprintf("%s-backup-agents", backup.ObjectMeta.Name)
	existingDeployments := &appsv1.DeploymentList{}

	err := r.List(context, existingDeployments, client.InNamespace(backup.Namespace), client.MatchingField("metadata.name", deploymentName))
	if err != nil {
		return false, err
	}
	deployment, err := GetBackupDeployment(context, backup, r)
	if err != nil {
		return false, err
	}

	if deployment != nil && deployment.ObjectMeta.Name != deploymentName {
		return false, fmt.Errorf("Inconsistent deployment names: %s != %s", deployment.ObjectMeta.Name, deploymentName)
	}

	if len(existingDeployments.Items) == 0 && deployment != nil {
		err = r.Create(context, deployment)
		if err != nil {
			return false, err
		}
	}
	if len(existingDeployments.Items) != 0 && deployment != nil {
		if existingDeployments.Items[0].ObjectMeta.Annotations[LastSpecKey] != deployment.ObjectMeta.Annotations[LastSpecKey] {
			err = r.Update(context, deployment)
			if err != nil {
				return false, err
			}
		}
	}
	if len(existingDeployments.Items) != 0 && deployment == nil {
		err = r.Delete(context, &existingDeployments.Items[0])
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdateBackupAgents) RequeueAfter() time.Duration {
	return 0
}
