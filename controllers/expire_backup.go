/*
 * expire_backup.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"crypto/sha256"
	"fmt"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type expireBackup struct{}

func (s expireBackup) reconcile(
	ctx context.Context,
	r *FoundationDBBackupReconciler,
	backup *fdbv1beta2.FoundationDBBackup,
) *requeue {
	if backup.GetBackupType() == fdbv1beta2.BackupTypeUnmanaged {
		return nil
	}
	pending := &requeue{
		delay:          10 * time.Second,
		delayedRequeue: true,
		message:        "waiting for backup expiration",
	}
	request := backup.Status.Expiration
	desired := backup.Spec.Expiration
	sameRequest := desired != nil && request != nil &&
		desired.BeforeTimestamp.Equal(&request.BeforeTimestamp)

	job, err := r.getBackupExpirationJob(ctx, backup)
	if err != nil {
		return &requeue{curError: err}
	}
	if job != nil {
		phase, message := backupExpirationJobResult(job)
		if phase == "Running" {
			if request.Phase != phase {
				request.Phase = phase
				request.Message = ""
				request.CompletionTime = nil
				if err = r.updateOrApply(ctx, backup); err != nil {
					return &requeue{curError: err}
				}
			}
			return pending
		}
		if request.Phase != phase {
			request.Phase = phase
			request.Message = message
			now := metav1.Now()
			request.CompletionTime = &now
			if err = r.updateOrApply(ctx, backup); err != nil {
				return &requeue{curError: err}
			}
			eventType := corev1.EventTypeNormal
			if phase == "Failed" {
				eventType = corev1.EventTypeWarning
			}
			r.Recorder.Event(
				backup,
				eventType,
				"BackupExpiration"+phase,
				"Expiration Job "+job.Name+": "+phase,
			)
		}
	}

	if desired == nil {
		if job == nil && request != nil && request.Phase == "Running" {
			backup.Status.Expiration = nil
			if err = r.updateOrApply(ctx, backup); err != nil {
				return &requeue{curError: err}
			}
		}
		return nil
	}
	if sameRequest {
		if request.Phase == "Succeeded" {
			return nil
		}
		if job != nil {
			// Retain a failed Job for diagnosis; deleting it explicitly retries this request
			return &requeue{
				delay:          time.Minute,
				delayedRequeue: true,
				message:        "backup expiration failed; inspect Job " + job.Name,
			}
		}
		job, err = internal.GetBackupExpirationJob(backup)
		if err != nil {
			return &requeue{curError: err}
		}
		if err = r.Create(ctx, job); err != nil {
			return &requeue{curError: err}
		}
		request.Phase = "Running"
		request.Message = ""
		request.CompletionTime = nil
		if err = r.updateOrApply(ctx, backup); err != nil {
			return &requeue{curError: err}
		}
		return pending
	}

	if job != nil {
		if job.DeletionTimestamp == nil {
			err = r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground))
			if err != nil && !k8serrors.IsNotFound(err) {
				return &requeue{curError: err}
			}
		}
		return pending
	}

	adminClient, err := r.adminClientForBackup(ctx, backup)
	if err != nil {
		return &requeue{curError: err}
	}
	defer func() { _ = adminClient.Close() }()
	liveStatus, err := adminClient.GetBackupStatus(backup)
	if err != nil {
		return &requeue{curError: err}
	}
	if liveStatus.DestinationURL == "" {
		return &requeue{
			delay:          10 * time.Second,
			delayedRequeue: true,
			message:        "waiting for backup destination before expiration",
		}
	}

	hash := sha256.Sum256(
		[]byte(string(backup.UID) + "/" + desired.BeforeTimestamp.UTC().Format(time.RFC3339)),
	)
	backup.Status.Expiration = &fdbv1beta2.BackupExpirationStatus{
		BeforeTimestamp: desired.BeforeTimestamp,
		DestinationURL:  liveStatus.DestinationURL,
		ClusterName:     backup.Spec.ClusterName,
		JobName:         fmt.Sprintf("fdbbackup-expire-%x", hash[:16]),
		Phase:           "Running",
	}
	// Persist the target before creating the Job so restarts cannot redirect a pending request
	if err = r.updateOrApply(ctx, backup); err != nil {
		return &requeue{curError: err}
	}
	return pending
}

func (r *FoundationDBBackupReconciler) getBackupExpirationJob(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
) (*batchv1.Job, error) {
	if backup.Status.Expiration == nil {
		return nil, nil
	}
	job := &batchv1.Job{}
	err := r.Get(
		ctx,
		client.ObjectKey{Namespace: backup.Namespace, Name: backup.Status.Expiration.JobName},
		job,
	)
	if k8serrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(job, backup) {
		return nil, fmt.Errorf("expiration Job %s is not owned by this backup", job.Name)
	}
	return job, nil
}

func backupExpirationJobResult(job *batchv1.Job) (string, string) {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return "Succeeded", ""
		case batchv1.JobFailed:
			return "Failed", condition.Reason + ": " + condition.Message
		}
	}
	return "Running", ""
}
