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
			ExpireAfterSeconds:   ptr.To(int64(86400)),
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
			Expect(backup.Status.Generations.Reconciled).To(Equal(backup.Generation))
		},
		Entry("running", fdbv1beta2.BackupStateRunning, 2),
		Entry("paused", fdbv1beta2.BackupStatePaused, 2),
		Entry("stopped without agents", fdbv1beta2.BackupStateStopped, 0),
	)

	DescribeTable(
		"uses the current inputs when creating a missing Job",
		func(deleteJob bool) {
			Expect(updateBackupStatus{}.reconcile(ctx, backupReconciler, backup)).To(BeNil())
			Expect(
				expireBackup{}.reconcile(ctx, backupReconciler, backup).curError,
			).NotTo(HaveOccurred())
			if deleteJob {
				Expect(
					expireBackup{}.reconcile(ctx, backupReconciler, backup).curError,
				).NotTo(HaveOccurred())
				job := jobs()[0]
				Expect(k8sClient.Delete(ctx, &job)).To(Succeed())
			}
			Expect(jobs()).To(BeEmpty())
			backup.Spec.ClusterName = "replacement-cluster"
			backup.Spec.Expiration = &fdbv1beta2.BackupExpiration{
				ExpireAfterSeconds:     ptr.To(int64(172800)),
				RestorableAfterVersion: ptr.To(int64(123456789)),
			}
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			backup.Status.BackupDetails.URL = "blobstore://replacement-backup"
			Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
			_, err := reloadBackup(backup)
			Expect(err).NotTo(HaveOccurred())
			before := time.Now().UTC().Truncate(time.Second)
			Expect(
				expireBackup{}.reconcile(ctx, backupReconciler, backup).curError,
			).NotTo(HaveOccurred())
			Expect(jobs()).To(HaveLen(1))
			pod := jobs()[0].Spec.Template
			args := pod.Spec.Containers[0].Args
			Expect(
				args,
			).To(ContainElements("blobstore://replacement-backup", "--restorable-after-version", "123456789"))
			Expect(args).NotTo(ContainElement("--restorable-after-timestamp"))
			cutoff, err := time.Parse("2006/01/02.15:04:05-0700", args[4])
			Expect(err).NotTo(HaveOccurred())
			Expect(cutoff).To(BeTemporally(">=", before.Add(-48*time.Hour)))
			Expect(cutoff).To(BeTemporally("<=", time.Now().Add(-48*time.Hour)))
			Expect(pod.Spec.Volumes).To(ContainElement(corev1.Volume{
				Name: "config-map",
				VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "replacement-cluster-config",
					},
					Items: []corev1.KeyToPath{
						{Key: fdbv1beta2.ClusterFileKey, Path: "fdb.cluster"},
					},
				}},
			}))
		},
		Entry("before initial creation", false),
		Entry("after deletion", true),
	)

	It("records success and preserves its schedule after policy changes or Job deletion", func() {
		reconcile()
		job := jobs()[0]
		backup.Spec.Expiration.IntervalSeconds = ptr.To(int64(7200))
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(backup.Status.Expiration.Phase).To(Equal(fdbv1beta2.BackupExpirationPhaseSucceeded))
		Expect(backup.Status.Expiration.CompletionTime).NotTo(BeNil())
		Expect(
			backup.Status.Expiration.NextRunTime.Sub(backup.Status.Expiration.CompletionTime.Time),
		).To(Equal(2 * time.Hour))
		nextRun := backup.Status.Expiration.NextRunTime.DeepCopy()
		Expect(backup.Status.Generations.Reconciled).To(Equal(backup.Generation))
		Expect(k8sClient.Delete(ctx, &job)).To(Succeed())
		backup.Spec.AgentCount = ptr.To(3)
		backup.Spec.Expiration.IntervalSeconds = ptr.To(int64(60))
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(BeEmpty())
		Expect(backup.Status.Expiration.Phase).To(Equal(fdbv1beta2.BackupExpirationPhaseSucceeded))
		Expect(backup.Status.Expiration.NextRunTime).To(Equal(nextRun))
	})

	DescribeTable(
		"keeps expiration timing separate from pending agent updates",
		func(condition batchv1.JobConditionType, agentsReady, changeGeneration bool, expectedDelay time.Duration) {
			backup.Spec.Expiration.IntervalSeconds = ptr.To(int64(86400))
			Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			reconcile()
			job := jobs()[0]
			if condition != "" {
				finish(&job, condition)
				reconcile()
				backup.Status.Expiration.CompletionTime.Time = backup.Status.Expiration.CompletionTime.Add(
					-time.Minute,
				)
				backup.Status.Expiration.NextRunTime.Time = backup.Status.Expiration.NextRunTime.Add(
					-time.Minute,
				)
				Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
			}
			nextRun := backup.Status.Expiration.NextRunTime.DeepCopy()
			if changeGeneration {
				backup.Spec.AgentCount = ptr.To(3)
				backup.Generation++
				Expect(k8sClient.Update(ctx, backup)).To(Succeed())
			}
			reconciler := *backupReconciler
			reconciler.InSimulation = agentsReady
			reconciler.BackupExpirationPollInterval = 3 * time.Minute

			before := time.Now()
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(backup),
			})
			after := time.Now()
			Expect(err).NotTo(HaveOccurred())
			_, err = reloadBackup(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(backup.Status.Generations.NeedsBackupAgentUpdate != 0).To(Equal(!agentsReady))
			if agentsReady {
				Expect(backup.Status.Generations.Reconciled).To(Equal(backup.Generation))
			} else if changeGeneration {
				Expect(
					backup.Status.Generations.Reconciled,
				).To(BeNumerically("<", backup.Generation))
			}
			if agentsReady && nextRun != nil {
				deadline := backup.Status.Expiration.CompletionTime.Add(expectedDelay)
				Expect(result.RequeueAfter).To(BeNumerically(">=", deadline.Sub(after)))
				Expect(result.RequeueAfter).To(BeNumerically("<=", deadline.Sub(before)))
			} else {
				Expect(result.RequeueAfter).To(Equal(expectedDelay))
			}
			Expect(backup.Status.Expiration.NextRunTime).To(Equal(nextRun))
			Expect(jobs()).To(HaveLen(1))
			Expect(jobs()[0].UID).To(Equal(job.UID))
		},
		Entry(
			"pending agents with active expiration",
			batchv1.JobConditionType(""),
			false,
			true,
			5*time.Second,
		),
		Entry(
			"pending agents after successful expiration",
			batchv1.JobComplete,
			false,
			true,
			5*time.Second,
		),
		Entry(
			"pending agents during expiration backoff",
			batchv1.JobFailed,
			false,
			true,
			5*time.Second,
		),
		Entry(
			"ready agents with active expiration",
			batchv1.JobConditionType(""),
			true,
			true,
			3*time.Minute,
		),
		Entry(
			"ready agents after successful expiration",
			batchv1.JobComplete,
			true,
			true,
			24*time.Hour,
		),
		Entry(
			"ready agents during expiration backoff",
			batchv1.JobFailed,
			true,
			true,
			24*time.Hour,
		),
		Entry(
			"unready agents without a spec change",
			batchv1.JobComplete,
			false,
			false,
			5*time.Second,
		),
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
			backup.Status.Expiration.CompletionTime.Time = backup.Status.Expiration.CompletionTime.Add(
				-time.Minute,
			)
			backup.Status.Expiration.NextRunTime.Time = backup.Status.Expiration.NextRunTime.Add(
				-time.Minute,
			)
			Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
			nextRun := backup.Status.Expiration.NextRunTime.DeepCopy()
			reconciler := *backupReconciler
			reconciler.MinimumAgeForTerminalPodDeletion = podCleanupDelay
			reconciler.InSimulation = false
			setupUnreadyDeploymentWithTerminalPod(ctx, backup, corev1.PodFailed)
			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: backup.Namespace, Name: "terminal-backup-agent",
			}, pod)).To(Succeed())
			deadline := pod.CreationTimestamp.Add(podCleanupDelay)
			if nextRun.Time.Before(deadline) {
				deadline = nextRun.Time
			}

			before := time.Now()
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(backup),
			})
			after := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(result.RequeueAfter).To(BeNumerically(">=", deadline.Sub(after)))
			Expect(result.RequeueAfter).To(BeNumerically("<=", deadline.Sub(before)))
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
			(*backup.Spec.Expiration.ExpireAfterSeconds)++
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
		originalCutoff, err := time.Parse(
			"2006/01/02.15:04:05-0700",
			job.Spec.Template.Spec.Containers[0].Args[4],
		)
		Expect(err).NotTo(HaveOccurred())
		*backup.Spec.Expiration.ExpireAfterSeconds *= 2
		backup.Generation++
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Name).To(Equal(job.Name))
		Expect(jobs()[0].Spec.Template).To(Equal(job.Spec.Template))
		Expect(backup.Status.Generations.Reconciled).To(Equal(backup.Generation))
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(jobs()[0].Name).To(Equal(job.Name))
		backup.Status.Expiration.NextRunTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Minute)))
		Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Name).NotTo(Equal(job.Name))
		cutoff, err := time.Parse(
			"2006/01/02.15:04:05-0700",
			jobs()[0].Spec.Template.Spec.Containers[0].Args[4],
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(cutoff).To(BeTemporally("<", originalCutoff))
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

	It("uses the backup status and computes both cutoffs without another admin call", func() {
		backup.Status.BackupDetails = &fdbv1beta2.FoundationDBBackupStatusBackupDetails{
			URL: "blobstore://backup",
		}
		adminClient.MockError(errors.New("unexpected admin call"))
		before := time.Now().Truncate(time.Second)
		req := expireBackup{}.reconcile(ctx, backupReconciler, backup)
		Expect(req.curError).NotTo(HaveOccurred())
		req = expireBackup{}.reconcile(ctx, backupReconciler, backup)
		Expect(req.curError).NotTo(HaveOccurred())
		args := jobs()[0].Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("blobstore://backup"))
		cutoff, err := time.Parse("2006/01/02.15:04:05-0700", args[4])
		Expect(err).NotTo(HaveOccurred())
		restorableAfter, err := time.Parse("2006/01/02.15:04:05-0700", args[6])
		Expect(err).NotTo(HaveOccurred())
		Expect(cutoff).To(BeTemporally(">=", before.Add(-24*time.Hour)))
		Expect(cutoff).To(BeTemporally("<=", time.Now().Add(-24*time.Hour)))
		Expect(restorableAfter.Sub(cutoff)).To(Equal(23 * time.Hour))
	})

	DescribeTable(
		"waits for backup details instead of querying the admin client",
		func(scheduled bool) {
			if scheduled {
				backup.Status.Expiration = &fdbv1beta2.BackupExpirationStatus{
					JobName: "expiration-job", Phase: fdbv1beta2.BackupExpirationPhaseRunning,
				}
			}
			currentStatus := backup.Status.Expiration.DeepCopy()
			adminClient.MockError(errors.New("unexpected admin call"))
			req := expireBackup{}.reconcile(ctx, backupReconciler, backup)
			Expect(req.curError).NotTo(HaveOccurred())
			Expect(backup.Status.Expiration).To(Equal(currentStatus))
			Expect(jobs()).To(BeEmpty())
		},
		Entry("before scheduling", false),
		Entry("before Job creation", true),
	)

	It("keeps an existing Job unchanged across restarts and configuration changes", func() {
		backup.Spec.Expiration.RestorableForSeconds = nil
		backup.Spec.Expiration.RestorableAfterVersion = ptr.To(int64(123456789))
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		job := jobs()[0]
		*backup.Spec.Expiration.RestorableAfterVersion = 987654321
		*backup.Spec.Expiration.ExpireAfterSeconds *= 2
		backup.Spec.ClusterName = "replacement-cluster"
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		backup.Status.BackupDetails.URL = "blobstore://replacement-backup"
		Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())
		_, err := reloadBackup(backup)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			expireBackup{}.reconcile(ctx, backupReconciler, backup).curError,
		).NotTo(HaveOccurred())
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].UID).To(Equal(job.UID))
		Expect(jobs()[0].Spec).To(Equal(job.Spec))
	})

	It("uses the configured polling interval", func() {
		reconciler := *backupReconciler
		reconciler.BackupExpirationPollInterval = 3 * time.Minute
		req := expireBackup{}.reconcile(ctx, &reconciler, backup)
		Expect(req.delay).To(Equal(3 * time.Minute))
	})

	It("caps repeated failures at a daily retry", func() {
		Expect(backupExpirationNextRunDelay(time.Hour, 32)).To(Equal(24 * time.Hour))
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
		"cancels expiration and waits for its Pods before backup deletion",
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

			request := ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(backup),
			}
			for range 2 {
				result, err := reconciler.Reconcile(ctx, request)
				Expect(err).NotTo(HaveOccurred())
				Expect(jobs()).To(HaveLen(1))
				job = jobs()[0]
				Expect(job.DeletionTimestamp).NotTo(BeNil())
				Expect(result.RequeueAfter).To(Equal(5 * time.Second))
			}
			job.Finalizers = nil
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			Expect(jobs()).To(BeEmpty())
			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).To(MatchError("cleanup reached admin client"))
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
})
