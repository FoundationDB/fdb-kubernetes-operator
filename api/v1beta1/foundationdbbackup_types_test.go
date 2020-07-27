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
	"testing"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCheckingReconciliationForBackup(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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

	backup := createBackup()

	result, err := backup.CheckReconciliation()
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Status.AgentCount = 5
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupAgentUpdate: 2,
	}))

	backup = createBackup()
	agentCount = 0
	backup.Status.AgentCount = 0
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	agentCount = 3
	backup.Status.BackupDetails = nil
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:       1,
		NeedsBackupStart: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Stopped"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:      1,
		NeedsBackupStop: 2,
	}))

	backup = createBackup()
	backup.Status.BackupDetails = nil
	backup.Spec.BackupState = "Stopped"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Status.BackupDetails.Running = false
	backup.Spec.BackupState = "Stopped"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Paused"
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupPauseToggle: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Paused"
	backup.Status.BackupDetails = nil
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupStart:       2,
		NeedsBackupPauseToggle: 2,
	}))

	backup = createBackup()
	backup.Spec.BackupState = "Paused"
	backup.Status.BackupDetails.Paused = true
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeTrue())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled: 2,
	}))

	backup = createBackup()
	backup.Status.BackupDetails.Paused = true
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:             1,
		NeedsBackupPauseToggle: 2,
	}))

	backup = createBackup()
	var time = 100000
	backup.Spec.SnapshotPeriodSeconds = &time
	result, err = backup.CheckReconciliation()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result).To(gomega.BeFalse())
	g.Expect(backup.Status.Generations).To(gomega.Equal(BackupGenerationStatus{
		Reconciled:                 1,
		NeedsBackupReconfiguration: 2,
	}))
	backup.Spec.SnapshotPeriodSeconds = nil
}

func TestCheckingBackupStates(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{},
	}

	g.Expect(backup.ShouldRun()).To(gomega.BeTrue())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

	backup.Spec.BackupState = "Running"
	g.Expect(backup.ShouldRun()).To(gomega.BeTrue())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

	backup.Spec.BackupState = "Stopped"
	g.Expect(backup.ShouldRun()).To(gomega.BeFalse())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeFalse())

	backup.Spec.BackupState = "Paused"
	g.Expect(backup.ShouldRun()).To(gomega.BeTrue())
	g.Expect(backup.ShouldBePaused()).To(gomega.BeTrue())
}

func TestGettingBucketName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{},
	}

	g.Expect(backup.Bucket()).To(gomega.Equal("fdb-backups"))
	backup.Spec.Bucket = "fdb-backup-v2"
	g.Expect(backup.Bucket()).To(gomega.Equal("fdb-backup-v2"))
}

func TestGettingBackupName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{},
	}

	g.Expect(backup.BackupName()).To(gomega.Equal("sample-cluster"))
	backup.Spec.BackupName = "sample_cluster_2020_03_22"
	g.Expect(backup.BackupName()).To(gomega.Equal("sample_cluster_2020_03_22"))
}

func TestBuildingBackupURL(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{
			AccountName: "test@test-service",
		},
	}

	g.Expect(backup.BackupURL()).To(gomega.Equal("blobstore://test@test-service/sample-cluster?bucket=fdb-backups"))
}

func TestGettingSnapshotTime(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	backup := FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-cluster",
			Namespace: "default",
		},
		Spec: FoundationDBBackupSpec{
			AccountName: "test@test-service",
		},
	}

	g.Expect(backup.SnapshotPeriodSeconds()).To(gomega.Equal(864000))

	period := 60
	backup.Spec.SnapshotPeriodSeconds = &period
	g.Expect(backup.SnapshotPeriodSeconds()).To(gomega.Equal(60))
}
