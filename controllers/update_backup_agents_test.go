/*
 * update_backup_agents_test.go
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
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("updateBackupAgents", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var backup *fdbv1beta2.FoundationDBBackup

	BeforeEach(func(ctx SpecContext) {
		cluster = internal.CreateDefaultCluster()
		backup = internal.CreateDefaultBackup(cluster)

		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		_, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		_, err = reconcileBackup(backup)
		Expect(err).NotTo(HaveOccurred())
	})

	When("a backup agent pod is in a terminal state but too young to be deleted", func() {
		BeforeEach(func(ctx SpecContext) {
			backupReconciler.MinimumAgeForTerminalPodDeletion = 1 * time.Hour
			setupUnreadyDeploymentWithTerminalPod(ctx, backup, corev1.PodFailed)
		})

		It("should not delete the pod and should return a delayed requeue", func(ctx SpecContext) {
			result := updateBackupAgents{}.reconcile(ctx, backupReconciler, backup)
			Expect(result).NotTo(BeNil())
			Expect(result.delayedRequeue).To(BeTrue())
			Expect(result.delay).To(BeNumerically(">", 0))
			Expect(result.delay).To(BeNumerically("<=", 1*time.Hour))

			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(backup.Namespace),
				client.MatchingLabels(map[string]string{
					fdbv1beta2.BackupDeploymentPodLabel: internal.GetBackupDeploymentName(backup),
				}),
			)).To(Succeed())
			// We have one pod in this list because our test setup only generates one pod.
			Expect(podList.Items).To(HaveLen(1))
		})
	})
})

// setupUnreadyDeploymentWithTerminalPod marks the named deployment as having one unready replica
// and creates a single Pod in the given terminal phase with the deployment's selector labels.
func setupUnreadyDeploymentWithTerminalPod(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
	phase corev1.PodPhase,
) {
	deploymentName := internal.GetBackupDeploymentName(backup)

	deployment := &appsv1.Deployment{}
	Expect(k8sClient.Get(
		ctx,
		types.NamespacedName{Namespace: backup.Namespace, Name: deploymentName},
		deployment,
	)).To(Succeed())

	deployment.Status.Replicas = 3
	deployment.Status.ReadyReplicas = 2
	Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "terminal-backup-agent",
			Namespace: backup.Namespace,
			Labels:    deployment.Spec.Selector.MatchLabels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "backup-agent", Image: fdbv1beta2.FoundationDBKubernetesBaseImage},
			},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())
	pod.Status.Phase = phase
	Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
}
