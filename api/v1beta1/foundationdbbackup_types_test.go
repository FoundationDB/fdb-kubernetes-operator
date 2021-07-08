/*
 * foundationdbbackup_types.go
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

package v1beta1

import (
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] FoundationDBBackup", func() {
	var backup *FoundationDBBackup

	BeforeEach(func() {
		backup = &FoundationDBBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample-cluster",
				Namespace: "default",
			},
			Spec: FoundationDBBackupSpec{},
		}
	})

	When("checking reconciliation for backup", func() {
		It("should reconcile successfully", func() {
			var agentCount = 3

			createBackup := func() *FoundationDBBackup {
				return &FoundationDBBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "sample-cluster",
						Namespace:  "default",
						Generation: 2,
					},
					Spec: FoundationDBBackupSpec{
						AgentCount: &agentCount,
					},
					Status: FoundationDBBackupStatus{
						Generations: BackupGenerationStatus{
							Reconciled: 1,
						},
						AgentCount:           3,
						DeploymentConfigured: true,
						BackupDetails: &FoundationDBBackupStatusBackupDetails{
							URL:                   "blobstore://test@test-service/sample-cluster?bucket=fdb-backups",
							Running:               true,
							SnapshotPeriodSeconds: 864000,
						},
					},
				}
			}

			backup = createBackup()

			result, err := backup.CheckReconciliation()
			Expect(result).To(gomega.BeTrue())
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Status.AgentCount = 5
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupAgentUpdate: 2,
			}))

			backup = createBackup()
			agentCount = 0
			backup.Status.AgentCount = 0
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeTrue())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			agentCount = 3
			backup.Status.BackupDetails = nil
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:       1,
				NeedsBackupStart: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = "Stopped"
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:      1,
				NeedsBackupStop: 2,
			}))

			backup = createBackup()
			backup.Status.BackupDetails = nil
			backup.Spec.BackupState = "Stopped"
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeTrue())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Status.BackupDetails.Running = false
			backup.Spec.BackupState = "Stopped"
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeTrue())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = "Paused"
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupPauseToggle: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = "Paused"
			backup.Status.BackupDetails = nil
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupStart:       2,
				NeedsBackupPauseToggle: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = "Paused"
			backup.Status.BackupDetails.Paused = true
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeTrue())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Status.BackupDetails.Paused = true
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupPauseToggle: 2,
			}))

			backup = createBackup()
			var time = 100000
			backup.Spec.SnapshotPeriodSeconds = &time
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(gomega.HaveOccurred())
			Expect(result).To(gomega.BeFalse())
			Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
				Reconciled:                 1,
				NeedsBackupReconfiguration: 2,
			}))
			backup.Spec.SnapshotPeriodSeconds = nil
		})

	})

	When("checking the backup state", func() {
		It("should show the correct state", func() {
			Expect(backup.ShouldRun()).To(gomega.BeTrue())
			Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

			backup.Spec.BackupState = "Running"
			Expect(backup.ShouldRun()).To(gomega.BeTrue())
			Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

			backup.Spec.BackupState = "Stopped"
			Expect(backup.ShouldRun()).To(gomega.BeFalse())
			Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

			backup.Spec.BackupState = "Paused"
			Expect(backup.ShouldRun()).To(gomega.BeTrue())
			Expect(backup.ShouldBePaused()).To(gomega.BeTrue())
		})
	})

	When("getting the bucket name", func() {
		It("should return the bucket name", func() {
			Expect(backup.Bucket()).To(gomega.Equal("fdb-backups"))
			backup.Spec.Bucket = "fdb-backup-v2"
			Expect(backup.Bucket()).To(gomega.Equal("fdb-backup-v2"))
		})
	})

	When("getting the backup name", func() {
		It("should return the backup name", func() {
			Expect(backup.BackupName()).To(gomega.Equal("sample-cluster"))
			backup.Spec.BackupName = "sample_cluster_2020_03_22"
			Expect(backup.BackupName()).To(gomega.Equal("sample_cluster_2020_03_22"))
		})
	})

	When("building the backup url", func() {
		BeforeEach(func() {
			backup.Spec.AccountName = "test@test-service"
		})

		It("should create the correct url", func() {
			Expect(backup.BackupURL()).To(gomega.Equal("blobstore://test@test-service/sample-cluster?bucket=fdb-backups"))
		})
	})

	When("getting the snapshot time", func() {
		BeforeEach(func() {
			backup.Spec.AccountName = "test@test-service"
		})

		It("should return the snapshot time", func() {
			Expect(backup.SnapshotPeriodSeconds()).To(gomega.Equal(864000))

			period := 60
			backup.Spec.SnapshotPeriodSeconds = &period
			Expect(backup.SnapshotPeriodSeconds()).To(gomega.Equal(60))
		})
	})
})
