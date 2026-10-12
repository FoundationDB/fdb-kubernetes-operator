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
	pending := &requeue{
		delay:          r.backupExpirationPollInterval(),
		delayedRequeue: true,
		message:        "waiting for backup expiration",
	}
	// Spec changes and deletion of a terminal Job must not bypass the persisted schedule
	if backup.Spec.Expiration != nil {
		if req := requeueUntilBackupExpiration(backup.Status.Expiration); req != nil {
			return req
		}
	}

	job, err := r.getBackupExpirationJob(ctx, backup)
	if err != nil {
		return &requeue{curError: err}
	}
	if job != nil {
		if err = r.updateBackupExpirationStatus(ctx, backup, job); err != nil {
			return &requeue{curError: err}
		}
		if backup.Status.Expiration.Phase == fdbv1beta2.BackupExpirationPhaseRunning {
			return pending
		}
	}

	if backup.Spec.Expiration != nil {
		if req := requeueUntilBackupExpiration(backup.Status.Expiration); req != nil {
			return req
		}
	}
	// Remove the previous Job and its Pods before accepting another run or clearing status
	if job != nil {
		if err = r.deleteBackupExpirationJob(ctx, job); err != nil {
			return &requeue{curError: err}
		}
		return pending
	}
	if backup.Spec.Expiration == nil {
		if backup.Status.Expiration == nil {
			return nil
		}
		backup.Status.Expiration = nil
		if err = r.updateOrApply(ctx, backup); err != nil {
			return &requeue{curError: err}
		}
		return nil
	}
	currentStatus := backup.Status.Expiration
	if currentStatus != nil && currentStatus.Phase == fdbv1beta2.BackupExpirationPhaseRunning {
		job, err = internal.GetBackupExpirationJob(backup)
		if err != nil {
			return &requeue{curError: err}
		}
		if err = r.Create(ctx, job); err != nil {
			return &requeue{curError: err}
		}
		return pending
	}
	if backup.Status.BackupDetails == nil || backup.Status.BackupDetails.URL == "" {
		pending.message = "waiting for backup destination before expiration"
		return pending
	}
	if err = r.scheduleBackupExpiration(ctx, backup); err != nil {
		return &requeue{curError: err}
	}
	return pending
}

func requeueUntilBackupExpiration(currentStatus *fdbv1beta2.BackupExpirationStatus) *requeue {
	if currentStatus == nil || currentStatus.NextRunTime == nil {
		return nil
	}
	delay := time.Until(currentStatus.NextRunTime.Time)
	if delay <= 0 {
		return nil
	}
	return &requeue{
		delay:          delay,
		delayedRequeue: true,
		message:        "waiting for scheduled backup expiration",
	}
}

func (r *FoundationDBBackupReconciler) backupExpirationPollInterval() time.Duration {
	if r.BackupExpirationPollInterval <= 0 {
		return time.Minute
	}
	return r.BackupExpirationPollInterval
}

// scheduleBackupExpiration persists the cutoffs and target before Job creation so restarts cannot redirect deletion
func (r *FoundationDBBackupReconciler) scheduleBackupExpiration(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
) error {
	now := time.Now().UTC().Truncate(time.Second)
	policy := backup.Spec.Expiration.DeepCopy()
	cutoff := metav1.NewTime(now.Add(-time.Duration(policy.ExpireAfterSeconds) * time.Second))
	hash := sha256.Sum256([]byte(string(backup.UID) + "/" + cutoff.Format(time.RFC3339Nano)))
	failureCount := 0
	if backup.Status.Expiration != nil {
		failureCount = backup.Status.Expiration.FailureCount
	}
	backup.Status.Expiration = &fdbv1beta2.BackupExpirationStatus{
		Policy:          *policy,
		BeforeTimestamp: cutoff,
		DestinationURL:  backup.Status.BackupDetails.URL,
		ClusterName:     backup.Spec.ClusterName,
		JobName:         fmt.Sprintf("fdbbackup-expire-%x", hash[:16]),
		Phase:           fdbv1beta2.BackupExpirationPhaseRunning,
		FailureCount:    failureCount,
	}
	if policy.RestorableForSeconds != nil {
		restorableAfter := metav1.NewTime(
			now.Add(-time.Duration(*policy.RestorableForSeconds) * time.Second),
		)
		backup.Status.Expiration.RestorableAfterTimestamp = &restorableAfter
	}
	return r.updateOrApply(ctx, backup)
}

func (r *FoundationDBBackupReconciler) updateBackupExpirationStatus(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
	job *batchv1.Job,
) error {
	phase, message := backupExpirationJobResult(job)
	if backup.Status.Expiration.Phase == phase {
		return nil
	}
	backup.Status.Expiration.Phase = phase
	backup.Status.Expiration.Message = message
	backup.Status.Expiration.CompletionTime = nil
	backup.Status.Expiration.NextRunTime = nil
	if phase != fdbv1beta2.BackupExpirationPhaseRunning {
		now := metav1.Now()
		backup.Status.Expiration.CompletionTime = &now
		if phase == fdbv1beta2.BackupExpirationPhaseFailed {
			backup.Status.Expiration.FailureCount = min(backup.Status.Expiration.FailureCount+1, 32)
		} else {
			backup.Status.Expiration.FailureCount = 0
		}
		nextRun := metav1.NewTime(now.Add(backupExpirationNextRunDelay(backup.Status.Expiration)))
		backup.Status.Expiration.NextRunTime = &nextRun
	}
	if err := r.updateOrApply(ctx, backup); err != nil {
		return err
	}
	if phase != fdbv1beta2.BackupExpirationPhaseRunning {
		eventType := corev1.EventTypeNormal
		if phase == fdbv1beta2.BackupExpirationPhaseFailed {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Event(
			backup,
			eventType,
			"BackupExpiration"+string(phase),
			"Expiration Job "+job.Name+": "+string(phase),
		)
	}
	return nil
}

func backupExpirationNextRunDelay(currentStatus *fdbv1beta2.BackupExpirationStatus) time.Duration {
	interval := currentStatus.Policy.Interval()
	for failures := 1; failures < currentStatus.FailureCount && interval < 24*time.Hour; failures++ {
		interval *= 2
	}
	return min(interval, 24*time.Hour)
}

// deleteBackupExpirationJob starts foreground deletion; callers must wait for the Job to disappear
func (r *FoundationDBBackupReconciler) deleteBackupExpirationJob(
	ctx context.Context,
	job *batchv1.Job,
) error {
	if job.DeletionTimestamp != nil {
		return nil
	}
	err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground))
	return client.IgnoreNotFound(err)
}

// getBackupExpirationJob fetches the recorded Job and verifies that this backup controls it
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

func backupExpirationJobResult(job *batchv1.Job) (fdbv1beta2.BackupExpirationPhase, string) {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return fdbv1beta2.BackupExpirationPhaseSucceeded, ""
		case batchv1.JobFailed:
			return fdbv1beta2.BackupExpirationPhaseFailed, condition.Reason + ": " + condition.Message
		}
	}
	return fdbv1beta2.BackupExpirationPhaseRunning, ""
}
