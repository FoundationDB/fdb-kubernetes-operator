/*
 * modify_backup_test.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("modifyBackup reconciler", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var backup *fdbv1beta2.FoundationDBBackup
	var adminClient *mock.AdminClient

	BeforeEach(func() {
		var err error
		cluster = internal.CreateDefaultCluster()
		backup = internal.CreateDefaultBackup(cluster)
		adminClient, err = mock.NewMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Create(context.TODO(), cluster)).To(Succeed())
		Expect(adminClient.StartBackup(backup)).To(Succeed())
	})

	// buildBackupDetails returns a BackupDetails struct reflecting the state
	// after a normal StartBackup, with a caller-supplied tag override.
	buildBackupDetails := func(tag string) *fdbv1beta2.FoundationDBBackupStatusBackupDetails {
		backupURL, err := backup.BackupURL()
		Expect(err).NotTo(HaveOccurred())
		return &fdbv1beta2.FoundationDBBackupStatusBackupDetails{
			URL:                   backupURL,
			Running:               true,
			Tag:                   tag,
			SnapshotPeriodSeconds: 864000,
		}
	}

	When("the status tag differs from the spec tag", func() {
		It("should requeue with an immutability error and not call ModifyBackup", func() {
			backup.Status.BackupDetails = buildBackupDetails("old-tag")
			backup.Spec.SnapshotPeriodSeconds = ptr.To(100000)

			req := modifyBackup{}.reconcile(context.TODO(), backupReconciler, backup)

			Expect(req).NotTo(BeNil())
			Expect(req.curError).To(MatchError(ContainSubstring("backup tags are immutable")))

			// ModifyBackup must not have been called — snapshot period is unchanged.
			status, err := adminClient.GetBackupStatus(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.SnapshotIntervalSeconds).To(Equal(864000))
		})
	})

	When("the backup mode is one-time", func() {
		It("should skip ModifyBackup even when the snapshot period changed", func() {
			backup.Status.BackupDetails = buildBackupDetails(fdbv1beta2.DefaultBackupTagBackupTag)
			backup.Spec.BackupMode = ptr.To(fdbv1beta2.BackupModeOneTime)
			backup.Spec.SnapshotPeriodSeconds = ptr.To(100000)

			req := modifyBackup{}.reconcile(context.TODO(), backupReconciler, backup)

			Expect(req).To(BeNil())

			// Snapshot period must not have been updated in the mock.
			status, err := adminClient.GetBackupStatus(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.SnapshotIntervalSeconds).To(Equal(864000))
		})
	})
})
