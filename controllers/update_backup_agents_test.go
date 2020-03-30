/*
 * update_backup_agents_test.go
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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("UpdateBackupAgents", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var backup *fdbtypes.FoundationDBBackup
	var shouldContinue bool
	var err error

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster, backup = createReconciledBackup()
		shouldContinue = true
	})

	AfterEach(func() {
		cleanupCluster(cluster)
		cleanupBackup(backup)
	})

	JustBeforeEach(func() {
		err = runBackupReconciler(UpdateBackupAgents{}, backup, shouldContinue)
	})

	Context("with a reconciled backup", func() {
		It("should not return an error", func() {
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with a change to the backup agent spec", func() {
		BeforeEach(func() {
			backup.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "foundationdb",
						Env: []corev1.EnvVar{{
							Name:  "TEST_VAR",
							Value: "TEST_VALUE",
						}},
					}},
				},
			}
		})

		It("should update the backup deployment", func() {
			deployment := &appsv1.Deployment{}

			Eventually(func() (corev1.EnvVar, error) {
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name + "-backup-agents"}, deployment)
				return deployment.Spec.Template.Spec.Containers[0].Env[0], err
			}).Should(Equal(corev1.EnvVar{Name: "TEST_VAR", Value: "TEST_VALUE"}))
		})
	})
})
