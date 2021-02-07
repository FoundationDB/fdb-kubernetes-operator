/*
 * backup_controller_test.go
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

package controllers

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func reloadBackup(backup *fdbtypes.FoundationDBBackup) (int64, error) {
	generations, err := reloadBackupGenerations(backup)
	if err != nil {
		return 0, err
	}
	return generations.Reconciled, err
}

func reloadBackupGenerations(backup *fdbtypes.FoundationDBBackup) (fdbtypes.BackupGenerationStatus, error) {
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, backup)
	if err != nil {
		return fdbtypes.BackupGenerationStatus{}, err
	}
	return backup.Status.Generations, err
}

var _ = Describe("backup_controller", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var backup *fdbtypes.FoundationDBBackup
	var adminClient *MockAdminClient
	var err error

	BeforeEach(func() {
		cluster = createDefaultCluster()
		backup = createDefaultBackup(cluster)
		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Reconciliation", func() {
		var originalVersion int64
		var generationGap int64

		BeforeEach(func() {
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			result, err := reconcileCluster(cluster)
			Expect(err).NotTo((HaveOccurred()))
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).NotTo(Equal(int64(0)))

			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), backup)
			Expect(err).NotTo(HaveOccurred())

			result, err = reconcileBackup(backup)
			Expect(err).NotTo((HaveOccurred()))
			Expect(result.Requeue).To(BeFalse())

			generation, err = reloadBackup(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).NotTo(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			originalVersion = backup.ObjectMeta.Generation

			generationGap = 1
		})

		JustBeforeEach(func() {
			result, err := reconcileBackup(backup)
			Expect(err).NotTo((HaveOccurred()))
			Expect(result.Requeue).To(BeFalse())

			generation, err := reloadBackup(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(generation).To(Equal(originalVersion + generationGap))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when reconciling a new backup", func() {
			BeforeEach(func() {
				generationGap = 0
			})

			It("should create the backup deployment", func() {
				deployment := &appsv1.Deployment{}
				deploymentName := fmt.Sprintf("%s-backup-agents", cluster.Name)

				err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: deploymentName}, deployment)
				Expect(err).NotTo(HaveOccurred())
				Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))
			})

			It("should update the status on the resource", func() {
				Expect(backup.Status).To(Equal(fdbtypes.FoundationDBBackupStatus{
					AgentCount:           3,
					DeploymentConfigured: true,
					BackupDetails: &fdbtypes.FoundationDBBackupStatusBackupDetails{
						URL:                   "blobstore://test@test-service/test-backup?bucket=fdb-backups",
						Running:               true,
						SnapshotPeriodSeconds: 864000,
					},
					Generations: fdbtypes.BackupGenerationStatus{
						Reconciled: 1,
					},
				}))
			})

			It("should start a backup", func() {
				status, err := adminClient.GetBackupStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.DestinationURL).To(Equal("blobstore://test@test-service/test-backup?bucket=fdb-backups"))
				Expect(status.Status.Running).To(BeTrue())
				Expect(status.BackupAgentsPaused).To(BeFalse())
			})
		})

		Context("with a nil backup agent count", func() {
			BeforeEach(func() {
				backup.Spec.AgentCount = nil
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the default replica count", func() {
				deployments := &appsv1.DeploymentList{}
				err = k8sClient.List(context.TODO(), deployments)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(deployments.Items)).To(Equal(1))
				Expect(*deployments.Items[0].Spec.Replicas).To(Equal(int32(2)))
			})
		})

		Context("with backup agent count of zero", func() {
			BeforeEach(func() {
				agentCount := 0
				backup.Spec.AgentCount = &agentCount
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove the deployment", func() {
				deployments := &appsv1.DeploymentList{}
				err = k8sClient.List(context.TODO(), deployments)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(deployments.Items)).To(Equal(0))
			})
		})

		Context("when stopping a new backup", func() {
			BeforeEach(func() {
				backup.Spec.BackupState = "Stopped"
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should stop the backup", func() {
				status, err := adminClient.GetBackupStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Status.Running).To(BeFalse())
			})
		})

		Context("when pausing a backup", func() {
			BeforeEach(func() {
				backup.Spec.BackupState = "Paused"
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should pause the backup", func() {
				status, err := adminClient.GetBackupStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.BackupAgentsPaused).To(BeTrue())
			})
		})

		Context("when resuming a backup", func() {
			BeforeEach(func() {
				err = adminClient.PauseBackups()
				Expect(err).NotTo(HaveOccurred())

				backup.Spec.BackupState = ""
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should resume the backup", func() {
				status, err := adminClient.GetBackupStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.BackupAgentsPaused).To(BeFalse())
			})
		})

		Context("when changing a backup snapshot time", func() {
			BeforeEach(func() {
				period := 100000
				backup.Spec.SnapshotPeriodSeconds = &period
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should modify the backup", func() {
				status, err := adminClient.GetBackupStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.SnapshotIntervalSeconds).To(Equal(100000))
			})
		})

		Context("when changing labels", func() {
			BeforeEach(func() {
				backup.Spec.BackupDeploymentMetadata = &metav1.ObjectMeta{
					Labels: map[string]string{"fdb-test": "test-value"},
				}
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should modify the deployment", func() {
				deployments := &appsv1.DeploymentList{}
				err = k8sClient.List(context.TODO(), deployments)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(deployments.Items)).To(Equal(1))
				Expect(deployments.Items[0].ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-test":                    "test-value",
					"foundationdb.org/backup-for": string(backup.ObjectMeta.UID),
				}))
			})
		})

		Context("when changing annotations", func() {
			BeforeEach(func() {
				deployments := &appsv1.DeploymentList{}
				err = k8sClient.List(context.TODO(), deployments)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(deployments.Items)).To(Equal(1))
				deployment := deployments.Items[0]
				deployment.ObjectMeta.Annotations["fdb-test-1"] = "test-value-1"
				err = k8sClient.Update(context.TODO(), &deployment)
				Expect(err).NotTo(HaveOccurred())

				backup.Spec.BackupDeploymentMetadata = &metav1.ObjectMeta{
					Annotations: map[string]string{"fdb-test-2": "test-value-2"},
				}
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should modify the deployment", func() {
				deployments := &appsv1.DeploymentList{}
				err = k8sClient.List(context.TODO(), deployments)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(deployments.Items)).To(Equal(1))
				Expect(deployments.Items[0].ObjectMeta.Annotations).To(Equal(map[string]string{
					"fdb-test-1":                         "test-value-1",
					"fdb-test-2":                         "test-value-2",
					"foundationdb.org/last-applied-spec": "53bf93c896578af51723c0db12e884751be4ee702c7487a1a57108fa111a23d6",
				}))
			})
		})
	})
})
