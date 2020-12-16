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
	"reflect"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

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
	existingDeployment := &appsv1.Deployment{}
	needCreation := false

	err := r.Get(context, client.ObjectKey{Name: deploymentName, Namespace: backup.Namespace}, existingDeployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needCreation = true
		} else {
			return false, err
		}
	}

	deployment, err := GetBackupDeployment(backup)
	if err != nil {
		r.Recorder.Event(backup, "Error", "GetBackupDeployment", err.Error())
		return false, err
	}

	if deployment != nil && deployment.ObjectMeta.Name != deploymentName {
		return false, fmt.Errorf("inconsistent deployment names: %s != %s", deployment.ObjectMeta.Name, deploymentName)
	}

	if needCreation && deployment != nil {
		err = r.Create(context, deployment)
		if err != nil {
			return false, err
		}
	}
	if !needCreation && deployment != nil {
		annotationChange := mergeAnnotations(&existingDeployment.ObjectMeta, deployment.ObjectMeta)
		deployment.ObjectMeta.Annotations = existingDeployment.ObjectMeta.Annotations

		if annotationChange || !reflect.DeepEqual(existingDeployment.ObjectMeta.Labels, deployment.ObjectMeta.Labels) {
			err = r.Update(context, deployment)
			if err != nil {
				return false, err
			}
		}
	}

	if !needCreation && deployment == nil {
		err = r.Delete(context, existingDeployment)
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
