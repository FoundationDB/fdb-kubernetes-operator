/*
 * expire_backup_test.go
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
	"errors"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("backup expiration", func() {
	var backup *fdbv1beta2.FoundationDBBackup
	var adminClient *mock.AdminClient
	ctx := context.Background()

	BeforeEach(func() {
		cluster := internal.CreateDefaultCluster()
		backup = internal.CreateDefaultBackup(cluster)
		backup.Spec.Expiration = &fdbv1beta2.BackupExpiration{
			ExpireAfterSeconds:   86400,
			RestorableForSeconds: ptr.To(int64(3600)),
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		var err error
		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(adminClient.StartBackup(backup)).To(Succeed())
	})

	reconcile := func() {
		_, err := reconcileBackup(backup)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadBackup(backup)
		Expect(err).NotTo(HaveOccurred())
	}
	jobs := func() []batchv1.Job {
		list := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, list, client.InNamespace(backup.Namespace))).To(Succeed())
		return list.Items
	}
	finish := func(job *batchv1.Job, condition batchv1.JobConditionType) {
		job.Status.Conditions = []batchv1.JobCondition{{
			Type: condition, Status: corev1.ConditionTrue, Reason: "TestResult", Message: "test completion",
		}}
		Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
	}

	It("does nothing when expiration is omitted", func() {
		backup.Spec.Expiration = nil
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(BeEmpty())
		Expect(backup.Status.Expiration).To(BeNil())
	})

	DescribeTable(
		"expires managed backups independently of their running state",
		func(state fdbv1beta2.BackupState, agents int) {
			backup.Spec.BackupState = state
			backup.Spec.AgentCount = ptr.To(agents)
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconcile()
			Expect(jobs()).To(HaveLen(1))
			Expect(
				backup.Status.Expiration.Phase,
			).To(Equal(fdbv1beta2.BackupExpirationPhaseRunning))
			Expect(backup.Status.Generations.NeedsBackupExpiration).To(Equal(backup.Generation))
			Expect(backup.Status.Generations.Reconciled).To(BeNumerically("<", backup.Generation))
		},
		Entry("running", fdbv1beta2.BackupStateRunning, 2),
		Entry("paused", fdbv1beta2.BackupStatePaused, 2),
		Entry("stopped without agents", fdbv1beta2.BackupStateStopped, 0),
	)

	It("persists the target before creating a Job and retains it across restarts", func() {
		Expect(updateBackupStatus{}.reconcile(ctx, backupReconciler, backup)).To(BeNil())
		req := expireBackup{}.reconcile(ctx, backupReconciler, backup)
		Expect(req.curError).NotTo(HaveOccurred())
		Expect(jobs()).To(BeEmpty())
		_, err := reloadBackup(backup)
		Expect(err).NotTo(HaveOccurred())
		target := backup.Status.Expiration.DestinationURL
		backup.Spec.BlobStoreConfiguration.BackupName = "replacement-backup"
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Spec.Template.Spec.Containers[0].Args).To(ContainElement(target))
		Expect(backup.Status.Expiration.DestinationURL).To(Equal(target))
	})

	It("records success without repeating it after unrelated changes or Job deletion", func() {
		reconcile()
		job := jobs()[0]
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(backup.Status.Expiration.Phase).To(Equal(fdbv1beta2.BackupExpirationPhaseSucceeded))
		Expect(backup.Status.Expiration.CompletionTime).NotTo(BeNil())
		Expect(backup.Status.Generations.Reconciled).To(Equal(backup.Generation))
		Expect(k8sClient.Delete(ctx, &job)).To(Succeed())
		backup.Spec.AgentCount = ptr.To(3)
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(BeEmpty())
		Expect(backup.Status.Expiration.Phase).To(Equal(fdbv1beta2.BackupExpirationPhaseSucceeded))
	})

	DescribeTable(
		"keeps expiration timing separate from pending agent updates",
		func(condition batchv1.JobConditionType, agentsReady bool, expectedDelay time.Duration) {
			backup.Spec.Expiration.IntervalSeconds = ptr.To(int64(86400))
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconcile()
			job := jobs()[0]
			if condition != "" {
				finish(&job, condition)
				reconcile()
			}
			nextRun := backup.Status.Expiration.NextRunTime.DeepCopy()
			backup.Spec.AgentCount = ptr.To(3)
			backup.Generation++
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconciler := *backupReconciler
			reconciler.InSimulation = agentsReady
			reconciler.BackupExpirationPollInterval = 3 * time.Minute

			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(backup),
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = reloadBackup(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(backup.Status.Generations.NeedsBackupAgentUpdate != 0).To(Equal(!agentsReady))
			Expect(result.RequeueAfter).To(BeNumerically("~", expectedDelay, time.Second))
			Expect(backup.Status.Expiration.NextRunTime).To(Equal(nextRun))
			Expect(jobs()).To(HaveLen(1))
			Expect(jobs()[0].UID).To(Equal(job.UID))
		},
		Entry(
			"pending agents with active expiration",
			batchv1.JobConditionType(""),
			false,
			5*time.Second,
		),
		Entry(
			"pending agents after successful expiration",
			batchv1.JobComplete,
			false,
			5*time.Second,
		),
		Entry("pending agents during expiration backoff", batchv1.JobFailed, false, 5*time.Second),
		Entry(
			"ready agents with active expiration",
			batchv1.JobConditionType(""),
			true,
			3*time.Minute,
		),
		Entry("ready agents after successful expiration", batchv1.JobComplete, true, 24*time.Hour),
		Entry("ready agents during expiration backoff", batchv1.JobFailed, true, 24*time.Hour),
	)

	DescribeTable(
		"requeues at the earlier backup maintenance deadline",
		func(podCleanupDelay, expirationInterval time.Duration) {
			backup.Spec.Expiration.IntervalSeconds = ptr.To(int64(expirationInterval.Seconds()))
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconcile()
			job := jobs()[0]
			finish(&job, batchv1.JobComplete)
			reconcile()
			nextRun := backup.Status.Expiration.NextRunTime.DeepCopy()
			reconciler := *backupReconciler
			reconciler.MinimumAgeForTerminalPodDeletion = podCleanupDelay
			reconciler.InSimulation = false
			setupUnreadyDeploymentWithTerminalPod(ctx, backup, corev1.PodFailed)

			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(backup),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(
				result.RequeueAfter,
			).To(BeNumerically("~", min(podCleanupDelay, expirationInterval), time.Second))
			_, err = reloadBackup(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(backup.Status.Generations.NeedsBackupAgentUpdate).To(Equal(backup.Generation))
			Expect(backup.Status.Expiration.NextRunTime).To(Equal(nextRun))
			Expect(jobs()).To(HaveLen(1))
			Expect(jobs()[0].UID).To(Equal(job.UID))
		},
		Entry("agent cleanup is due first", time.Minute, 24*time.Hour),
		Entry("expiration is due first", 2*time.Hour, time.Hour),
	)

	It("backs off failed runs even after policy changes or Job deletion", func() {
		reconcile()
		for attempt := 1; attempt <= 3; attempt++ {
			job := jobs()[0]
			finish(&job, batchv1.JobFailed)
			reconcile()
			currentStatus := backup.Status.Expiration
			Expect(currentStatus.Phase).To(Equal(fdbv1beta2.BackupExpirationPhaseFailed))
			Expect(currentStatus.Message).To(ContainSubstring("TestResult"))
			Expect(currentStatus.FailureCount).To(Equal(attempt))
			Expect(
				currentStatus.NextRunTime.Sub(currentStatus.CompletionTime.Time),
			).To(Equal(time.Hour * time.Duration(1<<(attempt-1))))
			Expect(jobs()).To(HaveLen(1))
			backup.Spec.Expiration.ExpireAfterSeconds++
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &job)).To(Succeed())
			reconcile()
			Expect(jobs()).To(BeEmpty())
			backup.Status.Expiration.NextRunTime = ptr.To(
				metav1.NewTime(time.Now().Add(-time.Minute)),
			)
			Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
			reconcile()
			Expect(jobs()).To(HaveLen(1))
			Expect(jobs()[0].UID).NotTo(Equal(job.UID))
			Expect(jobs()[0].Status.Conditions).To(BeEmpty())
			Expect(
				backup.Status.Expiration.Phase,
			).To(Equal(fdbv1beta2.BackupExpirationPhaseRunning))
		}
		job := jobs()[0]
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(backup.Status.Expiration.FailureCount).To(BeZero())
		Expect(
			backup.Status.Expiration.NextRunTime.Sub(backup.Status.Expiration.CompletionTime.Time),
		).To(Equal(time.Hour))
	})

	It("finishes an active Job before applying a changed policy at the next scheduled run", func() {
		reconcile()
		job := jobs()[0]
		originalCutoff := backup.Status.Expiration.BeforeTimestamp
		backup.Spec.Expiration.ExpireAfterSeconds *= 2
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Name).To(Equal(job.Name))
		Expect(backup.Status.Expiration.BeforeTimestamp).To(Equal(originalCutoff))
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(jobs()[0].Name).To(Equal(job.Name))
		backup.Status.Expiration.NextRunTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Minute)))
		Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Name).NotTo(Equal(job.Name))
		Expect(
			backup.Status.Expiration.Policy.ExpireAfterSeconds,
		).To(Equal(backup.Spec.Expiration.ExpireAfterSeconds))
		Expect(backup.Status.Expiration.BeforeTimestamp.Before(&originalCutoff)).To(BeTrue())
	})

	It("expires again without changing the spec and waits for the previous Job's Pods", func() {
		reconcile()
		job := jobs()[0]
		job.Finalizers = []string{metav1.FinalizerDeleteDependents}
		Expect(k8sClient.Update(ctx, &job)).To(Succeed())
		finish(&job, batchv1.JobComplete)
		reconcile()
		generation := backup.Generation
		backup.Status.Expiration.NextRunTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Minute)))
		Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		job = jobs()[0]
		Expect(job.DeletionTimestamp).NotTo(BeNil())
		job.Finalizers = nil
		Expect(k8sClient.Update(ctx, &job)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].UID).NotTo(Equal(job.UID))
		Expect(jobs()[0].Status.Conditions).To(BeEmpty())
		Expect(backup.Generation).To(Equal(generation))
	})

	It("pins both relative cutoffs using the backup status without another admin call", func() {
		backup.Status.BackupDetails = &fdbv1beta2.FoundationDBBackupStatusBackupDetails{
			URL: "blobstore://pinned",
		}
		adminClient.MockError(errors.New("unexpected admin call"))
		before := time.Now().Truncate(time.Second)
		req := expireBackup{}.reconcile(ctx, backupReconciler, backup)
		Expect(req.curError).NotTo(HaveOccurred())
		Expect(backup.Status.Expiration.DestinationURL).To(Equal("blobstore://pinned"))
		Expect(
			backup.Status.Expiration.BeforeTimestamp.Time,
		).To(BeTemporally(">=", before.Add(-24*time.Hour)))
		Expect(
			backup.Status.Expiration.BeforeTimestamp.Time,
		).To(BeTemporally("<=", time.Now().Add(-24*time.Hour)))
		Expect(
			backup.Status.Expiration.RestorableAfterTimestamp.Sub(
				backup.Status.Expiration.BeforeTimestamp.Time,
			),
		).To(Equal(23 * time.Hour))
	})

	It("waits for backup details instead of querying the admin client", func() {
		adminClient.MockError(errors.New("unexpected admin call"))
		req := expireBackup{}.reconcile(ctx, backupReconciler, backup)
		Expect(req.curError).NotTo(HaveOccurred())
		Expect(backup.Status.Expiration).To(BeNil())
		Expect(jobs()).To(BeEmpty())
	})

	It("pins the protected checkpoint version across restarts and policy changes", func() {
		backup.Spec.Expiration.RestorableForSeconds = nil
		backup.Spec.Expiration.RestorableAfterVersion = ptr.To(int64(123456789))
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		Expect(updateBackupStatus{}.reconcile(ctx, backupReconciler, backup)).To(BeNil())
		Expect(
			expireBackup{}.reconcile(ctx, backupReconciler, backup).curError,
		).NotTo(HaveOccurred())
		Expect(jobs()).To(BeEmpty())
		_, err := reloadBackup(backup)
		Expect(err).NotTo(HaveOccurred())
		*backup.Spec.Expiration.RestorableAfterVersion = 987654321
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(
			jobs()[0].Spec.Template.Spec.Containers[0].Args,
		).To(ContainElements("--restorable-after-version", "123456789"))
		Expect(
			backup.Status.Expiration.Policy.RestorableAfterVersion,
		).To(HaveValue(Equal(int64(123456789))))
		job := jobs()[0]
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(backup.NeedsBackupExpiration()).To(BeTrue())
	})

	It("uses the configured polling interval", func() {
		reconciler := *backupReconciler
		reconciler.BackupExpirationPollInterval = 3 * time.Minute
		req := expireBackup{}.reconcile(ctx, &reconciler, backup)
		Expect(req.delay).To(Equal(3 * time.Minute))
	})

	It("caps repeated failures at a daily retry", func() {
		Expect(backupExpirationNextRunDelay(&fdbv1beta2.BackupExpirationStatus{
			Policy: *backup.Spec.Expiration, FailureCount: 32,
		})).To(Equal(24 * time.Hour))
	})

	DescribeTable(
		"cleans up an active Job after expiration is removed",
		func(backupType fdbv1beta2.BackupType) {
			reconcile()
			job := jobs()[0]
			backup.Spec.Expiration = nil
			backup.Spec.BackupType = ptr.To(backupType)
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconcile()
			Expect(jobs()).To(HaveLen(1))
			Expect(
				backup.Status.Expiration.Phase,
			).To(Equal(fdbv1beta2.BackupExpirationPhaseRunning))
			job.Finalizers = []string{metav1.FinalizerDeleteDependents}
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			finish(&job, batchv1.JobComplete)
			reconcile()
			Expect(jobs()).To(HaveLen(1))
			job = jobs()[0]
			Expect(job.DeletionTimestamp).NotTo(BeNil())
			Expect(backup.Status.Expiration).NotTo(BeNil())
			job.Finalizers = nil
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			reconcile()
			Expect(backup.Status.Expiration).To(BeNil())
			Expect(jobs()).To(BeEmpty())
		},
		Entry("remaining managed", fdbv1beta2.BackupTypeDefault),
		Entry("switching to unmanaged", fdbv1beta2.BackupTypeUnmanaged),
	)

	DescribeTable(
		"uses expiration polling while backup deletion waits for a Job",
		func(condition batchv1.JobConditionType, policy fdbv1beta2.BackupDeletionPolicy) {
			backup.Spec.DeletionPolicy = ptr.To(policy)
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconcile()
			job := jobs()[0]
			job.Finalizers = []string{metav1.FinalizerDeleteDependents}
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			if condition != "" {
				finish(&job, condition)
			}
			Expect(k8sClient.Delete(ctx, backup)).To(Succeed())
			reconciler := *backupReconciler
			reconciler.BackupExpirationPollInterval = 3 * time.Minute
			adminClient.MockError(errors.New("cleanup reached admin client"))

			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(backup),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(3 * time.Minute))
		},
		Entry(
			"active Job with cleanup",
			batchv1.JobConditionType(""),
			fdbv1beta2.BackupDeletionPolicyCleanup,
		),
		Entry(
			"completed Job with cleanup",
			batchv1.JobComplete,
			fdbv1beta2.BackupDeletionPolicyCleanup,
		),
		Entry("failed Job with cleanup", batchv1.JobFailed, fdbv1beta2.BackupDeletionPolicyCleanup),
		Entry(
			"active Job with stop",
			batchv1.JobConditionType(""),
			fdbv1beta2.BackupDeletionPolicyStop,
		),
		Entry("completed Job with stop", batchv1.JobComplete, fdbv1beta2.BackupDeletionPolicyStop),
		Entry("failed Job with stop", batchv1.JobFailed, fdbv1beta2.BackupDeletionPolicyStop),
	)

	It("does not create an unstarted request after expiration is removed", func() {
		Expect(updateBackupStatus{}.reconcile(ctx, backupReconciler, backup)).To(BeNil())
		req := expireBackup{}.reconcile(ctx, backupReconciler, backup)
		Expect(req.curError).NotTo(HaveOccurred())
		backup.Spec.Expiration = nil
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(BeEmpty())
	})

	DescribeTable(
		"defers deletion cleanup until the expiration Job is gone",
		func(condition batchv1.JobConditionType, policy fdbv1beta2.BackupDeletionPolicy) {
			reconcile()
			backup.Spec.DeletionPolicy = ptr.To(policy)
			backup.DeletionTimestamp = ptr.To(metav1.Now())
			adminClient.MockError(errors.New("cleanup reached admin client"))
			_, err := backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)
			Expect(err).NotTo(HaveOccurred())
			job := jobs()[0]
			// Hold the Job until its dependents have been removed by garbage collection
			job.Finalizers = []string{metav1.FinalizerDeleteDependents}
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			finish(&job, condition)
			_, err = backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(jobs()).To(HaveLen(1))
			job = jobs()[0]
			Expect(job.DeletionTimestamp).NotTo(BeNil())
			_, err = backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)
			Expect(err).NotTo(HaveOccurred())
			job.Finalizers = nil
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			Expect(jobs()).To(BeEmpty())
			_, err = backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)
			Expect(err).To(MatchError("cleanup reached admin client"))
		},
		Entry(
			"completed Job with cleanup",
			batchv1.JobComplete,
			fdbv1beta2.BackupDeletionPolicyCleanup,
		),
		Entry("failed Job with cleanup", batchv1.JobFailed, fdbv1beta2.BackupDeletionPolicyCleanup),
		Entry("completed Job with stop", batchv1.JobComplete, fdbv1beta2.BackupDeletionPolicyStop),
		Entry("failed Job with stop", batchv1.JobFailed, fdbv1beta2.BackupDeletionPolicyStop),
	)
})
