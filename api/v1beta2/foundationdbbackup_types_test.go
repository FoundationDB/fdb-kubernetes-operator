/*
 * foundationdbbackup_types.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2022 Apple Inc. and the FoundationDB project authors
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

package v1beta2

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			Expect(result).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Status.AgentCount = 5
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupAgentUpdate: 2,
			}))

			backup = createBackup()
			agentCount = 0
			backup.Status.AgentCount = 0
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			agentCount = 3
			backup.Status.BackupDetails = nil
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:       1,
				NeedsBackupStart: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = BackupStateStopped
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:      1,
				NeedsBackupStop: 2,
			}))

			backup = createBackup()
			backup.Status.BackupDetails = nil
			backup.Spec.BackupState = BackupStateStopped
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Status.BackupDetails.Running = false
			backup.Spec.BackupState = BackupStateStopped
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = BackupStatePaused
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupPauseToggle: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = BackupStatePaused
			backup.Status.BackupDetails = nil
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupStart:       2,
				NeedsBackupPauseToggle: 2,
			}))

			backup = createBackup()
			backup.Spec.BackupState = BackupStatePaused
			backup.Status.BackupDetails.Paused = true
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled: 2,
			}))

			backup = createBackup()
			backup.Status.BackupDetails.Paused = true
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:             1,
				NeedsBackupPauseToggle: 2,
			}))

			backup = createBackup()
			var time = 100000
			backup.Spec.SnapshotPeriodSeconds = &time
			result, err = backup.CheckReconciliation()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
			Expect(backup.Status.Generations).To(Equal(BackupGenerationStatus{
				Reconciled:                 1,
				NeedsBackupReconfiguration: 2,
			}))
			backup.Spec.SnapshotPeriodSeconds = nil
		})

	})

	When("checking the backup state", func() {
		It("should show the correct state", func() {
			Expect(backup.ShouldRun()).To(BeTrue())
			Expect(backup.ShouldBePaused()).To(BeFalse())

			backup.Spec.BackupState = BackupStateRunning
			Expect(backup.ShouldRun()).To(BeTrue())
			Expect(backup.ShouldBePaused()).To(BeFalse())

			backup.Spec.BackupState = BackupStateStopped
			Expect(backup.ShouldRun()).To(BeFalse())
			Expect(backup.ShouldBePaused()).To(BeFalse())

			backup.Spec.BackupState = BackupStatePaused
			Expect(backup.ShouldRun()).To(BeTrue())
			Expect(backup.ShouldBePaused()).To(BeTrue())
		})
	})

	When("getting the snapshot time", func() {
		It("should return the snapshot time", func() {
			Expect(backup.SnapshotPeriodSeconds()).To(Equal(864000))

			period := 60
			backup.Spec.SnapshotPeriodSeconds = &period
			Expect(backup.SnapshotPeriodSeconds()).To(Equal(60))
		})
	})

	When("getting the backup URL", func() {
		DescribeTable("should generate the correct backup URL",
			func(backup FoundationDBBackup, expected string) {
				Expect(backup.BackupURL()).To(Equal(expected))
			},
			Entry("A Backup with a blobstore config with backup name",
				FoundationDBBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBBackupSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							BackupName:  "test",
						},
					},
				},
				"blobstore://account@account/test?bucket=fdb-backups"),
			Entry("A Backup with a blobstore config with a bucket name",
				FoundationDBBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBBackupSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							Bucket:      "my-bucket",
						},
					},
				},
				"blobstore://account@account/mybackup?bucket=my-bucket"),
			Entry("A Backup with a blobstore config with a bucket and backup name",
				FoundationDBBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBBackupSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							BackupName:  "test",
							Bucket:      "my-bucket",
						},
					},
				},
				"blobstore://account@account/test?bucket=my-bucket"),
			Entry("A Backup with a blobstore config with HTTP parameters and backup and bucket name",
				FoundationDBBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBBackupSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							BackupName:  "test",
							Bucket:      "my-bucket",
							URLParameters: []URLParameter{
								"secure_connection=0",
							},
						},
					},
				},
				"blobstore://account@account/test?bucket=my-bucket&secure_connection=0"),
			Entry("A Backup with a blobstore config with HTTP parameters",
				FoundationDBBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBBackupSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							URLParameters: []URLParameter{
								"secure_connection=0",
							},
						},
					},
				},
				"blobstore://account@account/mybackup?bucket=fdb-backups&secure_connection=0"),
		)
	})
})
