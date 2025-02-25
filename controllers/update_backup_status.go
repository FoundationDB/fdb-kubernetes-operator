/*
 * update_backup_status.go
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
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updateBackupStatus provides a reconciliation step for updating the status in the
// CRD.
type updateBackupStatus struct{}

// reconcile runs the reconciler's work.
func (s updateBackupStatus) reconcile(ctx context.Context, r *FoundationDBBackupReconciler, backup *fdbv1beta2.FoundationDBBackup) *requeue {
	status := fdbv1beta2.FoundationDBBackupStatus{}
	status.Generations.Reconciled = backup.Status.Generations.Reconciled

	desiredBackupDeployment, err := internal.GetBackupDeployment(backup)
	if err != nil {
		return &requeue{curError: err}
	}

	currentBackupDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{Namespace: backup.Namespace, Name: internal.GetBackupDeploymentName(backup)}, currentBackupDeployment)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return &requeue{curError: err}
		}

		currentBackupDeployment = nil
	}

	if currentBackupDeployment != nil && desiredBackupDeployment != nil {
		status.AgentCount = int(currentBackupDeployment.Status.ReadyReplicas)
		if status.AgentCount > int(currentBackupDeployment.Status.UpdatedReplicas) {
			status.AgentCount = int(currentBackupDeployment.Status.UpdatedReplicas)
		}
		generationsMatch := currentBackupDeployment.Status.ObservedGeneration == currentBackupDeployment.ObjectMeta.Generation

		annotationChange := mergeAnnotations(&currentBackupDeployment.ObjectMeta, desiredBackupDeployment.ObjectMeta)

		metadataMatch := !annotationChange &&
			equality.Semantic.DeepEqual(currentBackupDeployment.ObjectMeta.Labels, desiredBackupDeployment.ObjectMeta.Labels)

		status.DeploymentConfigured = generationsMatch && metadataMatch

		if r.InSimulation {
			status.AgentCount = int(*currentBackupDeployment.Spec.Replicas)
			status.DeploymentConfigured = metadataMatch
		}
	} else if currentBackupDeployment == nil && desiredBackupDeployment == nil {
		status.DeploymentConfigured = true
	} else {
		status.DeploymentConfigured = false
	}

	adminClient, err := r.adminClientForBackup(ctx, backup)
	if err != nil {
		return &requeue{curError: err}
	}

	liveStatus, err := adminClient.GetBackupStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	status.BackupDetails = &fdbv1beta2.FoundationDBBackupStatusBackupDetails{
		URL:                   liveStatus.DestinationURL,
		Running:               liveStatus.Status.Running,
		Paused:                liveStatus.BackupAgentsPaused,
		SnapshotPeriodSeconds: liveStatus.SnapshotIntervalSeconds,
	}

	originalStatus := backup.Status.DeepCopy()

	backup.Status = status

	_, err = backup.CheckReconciliation()
	if err != nil {
		return &requeue{curError: err}
	}

	if !equality.Semantic.DeepEqual(backup.Status, *originalStatus) {
		err = r.updateOrApply(ctx, backup)
		if err != nil {
			globalControllerLogger.Error(err, "Error updating backup status", "namespace", backup.Namespace, "backup", backup.Name)
			return &requeue{curError: err}
		}
	}

	return nil
}
