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
	ctx "context"
	"reflect"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updateBackupStatus provides a reconciliation step for updating the status in the
// CRD.
type updateBackupStatus struct{}

// reconcile runs the reconciler's work.
func (s updateBackupStatus) reconcile(r *FoundationDBBackupReconciler, context ctx.Context, backup *fdbtypes.FoundationDBBackup) *requeue {
	status := fdbtypes.FoundationDBBackupStatus{}
	status.Generations.Reconciled = backup.Status.Generations.Reconciled

	backupDeployments := &appsv1.DeploymentList{}
	err := r.List(context, backupDeployments, client.InNamespace(backup.Namespace), client.MatchingLabels(map[string]string{fdbtypes.BackupDeploymentLabel: string(backup.ObjectMeta.UID)}))
	if err != nil {
		return &requeue{curError: err}
	}

	desiredBackupDeployment, err := internal.GetBackupDeployment(backup)
	if err != nil {
		return &requeue{curError: err}
	}

	if len(backupDeployments.Items) == 1 && desiredBackupDeployment != nil {
		backupDeployment := backupDeployments.Items[0]
		status.AgentCount = int(backupDeployment.Status.ReadyReplicas)
		if status.AgentCount > int(backupDeployment.Status.UpdatedReplicas) {
			status.AgentCount = int(backupDeployment.Status.UpdatedReplicas)
		}
		generationsMatch := backupDeployment.Status.ObservedGeneration == backupDeployment.ObjectMeta.Generation

		annotationChange := mergeAnnotations(&backupDeployment.ObjectMeta, desiredBackupDeployment.ObjectMeta)

		metadataMatch := !annotationChange &&
			reflect.DeepEqual(backupDeployment.ObjectMeta.Labels, desiredBackupDeployment.ObjectMeta.Labels)

		status.DeploymentConfigured = generationsMatch && metadataMatch

		if r.InSimulation {
			status.AgentCount = int(*backupDeployment.Spec.Replicas)
			status.DeploymentConfigured = metadataMatch
		}
	} else if len(backupDeployments.Items) == 0 && desiredBackupDeployment == nil {
		status.DeploymentConfigured = true
	} else {
		status.DeploymentConfigured = false
	}

	adminClient, err := r.adminClientForBackup(context, backup)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	liveStatus, err := adminClient.GetBackupStatus()
	if err != nil {
		return &requeue{curError: err}
	}

	status.BackupDetails = &fdbtypes.FoundationDBBackupStatusBackupDetails{
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

	if !reflect.DeepEqual(backup.Status, *originalStatus) {
		err = r.Status().Update(context, backup)
		if err != nil {
			log.Error(err, "Error updating backup status", "namespace", backup.Namespace, "backup", backup.Name)
			return &requeue{curError: err}
		}
	}

	return nil
}
