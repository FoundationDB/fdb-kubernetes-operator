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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

func reloadBackup(client client.Client, backup *fdbtypes.FoundationDBBackup) (int64, error) {
	generations, err := reloadBackupGenerations(client, backup)
	return generations.Reconciled, err
}

func reloadBackupGenerations(client client.Client, backup *fdbtypes.FoundationDBBackup) (fdbtypes.BackupGenerationStatus, error) {
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
		ClearMockAdminClients()
		cluster = createDefaultCluster()
		backup = createDefaultBackup(cluster)
		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Reconciliation", func() {
		var originalVersion int64
		var generationGap int64
		var timeout time.Duration

		BeforeEach(func() {
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			timeout = time.Second * 5
			Eventually(func() (int64, error) {
				return reloadCluster(k8sClient, cluster)
			}, timeout).ShouldNot(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), backup)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() (int64, error) {
				return reloadBackup(k8sClient, backup)
			}, timeout).ShouldNot(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			originalVersion = backup.ObjectMeta.Generation

			generationGap = 1
		})

		JustBeforeEach(func() {
			Eventually(func() (int64, error) { return reloadBackup(k8sClient, backup) }, timeout).Should(Equal(originalVersion + generationGap))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanupCluster(cluster)
			cleanupBackup(backup)
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
						URL:     "blobstore://test@test-service/test-backup?bucket=fdb-backups",
						Running: true,
					},
					Generations: fdbtypes.BackupGenerationStatus{
						Reconciled: 1,
					},
				}))
			})

			It("should start a backup", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbtypes.FoundationDBStatusBackupTag{
					"default": {
						CurrentContainer: "blobstore://test@test-service/test-backup?bucket=fdb-backups",
						RunningBackup:    true,
						Restorable:       true,
					},
				}))
				Expect(status.Cluster.Layers.Backup.Paused).To(BeFalse())
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
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), deployments)
					return len(deployments.Items), err
				}, timeout).Should(Equal(1))
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
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), deployments)
					return len(deployments.Items), err
				}, timeout).Should(Equal(0))
			})
		})

		Context("when stopping a new backup", func() {
			BeforeEach(func() {
				backup.Spec.BackupState = "Stopped"
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should stop the backup", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbtypes.FoundationDBStatusBackupTag{
					"default": {
						CurrentContainer: "blobstore://test@test-service/test-backup?bucket=fdb-backups",
						RunningBackup:    false,
						Restorable:       true,
					},
				}))
			})
		})

		Context("when pausing a backup", func() {
			BeforeEach(func() {
				backup.Spec.BackupState = "Paused"
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should pause the backup", func() {
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.Layers.Backup.Paused).To(BeTrue())
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
				status, err := adminClient.GetStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Cluster.Layers.Backup.Paused).To(BeFalse())
			})
		})
	})
})
