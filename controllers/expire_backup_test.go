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
			BeforeTimestamp: metav1.NewTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
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
			Expect(backup.Status.Expiration.Phase).To(Equal("Running"))
			Expect(backup.Status.Generations.NeedsBackupExpiration).To(Equal(backup.Generation))
			Expect(backup.Status.Generations.Reconciled).To(BeNumerically("<", backup.Generation))
		},
		Entry("running", fdbv1beta2.BackupStateRunning, 2),
		Entry("paused", fdbv1beta2.BackupStatePaused, 2),
		Entry("stopped without agents", fdbv1beta2.BackupStateStopped, 0),
	)

	It("persists the target before creating a Job and retains it across restarts", func() {
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
		Expect(backup.Status.Expiration.Phase).To(Equal("Succeeded"))
		Expect(backup.Status.Expiration.CompletionTime).NotTo(BeNil())
		Expect(backup.Status.Generations.Reconciled).To(Equal(backup.Generation))
		Expect(k8sClient.Delete(ctx, &job)).To(Succeed())
		backup.Spec.AgentCount = ptr.To(3)
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(BeEmpty())
		Expect(backup.Status.Expiration.Phase).To(Equal("Succeeded"))
	})

	It("retains a failed Job until the user deletes it to retry", func() {
		reconcile()
		job := jobs()[0]
		finish(&job, batchv1.JobFailed)
		reconcile()
		Expect(backup.Status.Expiration.Phase).To(Equal("Failed"))
		Expect(backup.Status.Expiration.Message).To(ContainSubstring("TestResult"))
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].UID).To(Equal(job.UID))
		Expect(k8sClient.Delete(ctx, &job)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Status.Conditions).To(BeEmpty())
		Expect(backup.Status.Expiration.Phase).To(Equal("Running"))
		Expect(backup.Status.Expiration.Message).To(BeEmpty())
	})

	It("finishes an active request before processing a changed cutoff", func() {
		reconcile()
		job := jobs()[0]
		originalCutoff := backup.Spec.Expiration.BeforeTimestamp
		backup.Spec.Expiration.BeforeTimestamp = metav1.NewTime(originalCutoff.Add(24 * time.Hour))
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Name).To(Equal(job.Name))
		Expect(backup.Status.Expiration.BeforeTimestamp).To(Equal(originalCutoff))
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Name).NotTo(Equal(job.Name))
		Expect(
			backup.Status.Expiration.BeforeTimestamp,
		).To(Equal(backup.Spec.Expiration.BeforeTimestamp))
	})

	It("observes an active Job after expiration is removed without creating another", func() {
		reconcile()
		job := jobs()[0]
		backup.Spec.Expiration = nil
		Expect(k8sClient.Update(ctx, backup)).To(Succeed())
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		finish(&job, batchv1.JobComplete)
		reconcile()
		Expect(backup.Status.Expiration.Phase).To(Equal("Succeeded"))
		Expect(jobs()).To(HaveLen(1))
	})

	It("does not create an unstarted request after expiration is removed", func() {
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
			Expect(backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)).To(Succeed())
			job := jobs()[0]
			// Hold the Job until its dependents have been removed by garbage collection
			job.Finalizers = []string{metav1.FinalizerDeleteDependents}
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			finish(&job, condition)
			Expect(backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)).To(Succeed())
			Expect(jobs()).To(HaveLen(1))
			job = jobs()[0]
			Expect(job.DeletionTimestamp).NotTo(BeNil())
			Expect(backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup)).To(Succeed())
			job.Finalizers = nil
			Expect(k8sClient.Update(ctx, &job)).To(Succeed())
			Expect(jobs()).To(BeEmpty())
			Expect(
				backupReconciler.updateFinalizerIfNeeded(ctx, testLogger, backup),
			).To(MatchError("cleanup reached admin client"))
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
