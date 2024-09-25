/*
 * fdb_backup.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2024 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	"context"
	"encoding/json"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FdbBackup represents a fdbv1beta2.FoundationDBBackup resource for doing backups of a FdbCluster.
type FdbBackup struct {
	backup     *fdbv1beta2.FoundationDBBackup
	fdbCluster *FdbCluster
}

// CreateBackupForCluster will create a FoundationDBBackup for the provided cluster.
func (factory *Factory) CreateBackupForCluster(
	fdbCluster *FdbCluster,
) *FdbBackup {
	// For more information how the backup system with the operator is working please look at
	// the operator documentation: https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/manual/backup.md
	fdbVersion := factory.GetFDBVersion()

	backup := &fdbv1beta2.FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fdbCluster.Name(),
			Namespace: fdbCluster.Namespace(),
		},
		Spec: fdbv1beta2.FoundationDBBackupSpec{
			AllowTagOverride: pointer.BoolPtr(true),
			ClusterName:      fdbCluster.Name(),
			Version:          fdbVersion.String(),
			BlobStoreConfiguration: &fdbv1beta2.BlobStoreConfiguration{
				AccountName: "seaweedfs@seaweedfs:8333",
				URLParameters: []fdbv1beta2.URLParameter{
					"secure_connection=0",
					// The region must be specified since the blobstore URL doesn't have that information.
					"region=us-east-1",
				},
			},
			CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
				// Enable if you want to get http debug logs.
				// "knob_http_verbose_level=10",
			},
			ImageType:        fdbCluster.cluster.Spec.ImageType,
			MainContainer:    fdbCluster.cluster.Spec.MainContainer,
			SidecarContainer: fdbCluster.cluster.Spec.SidecarContainer,
			PodTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: fdbv1beta2.MainContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "FDB_TLS_CERTIFICATE_FILE",
									Value: "/tmp/fdb-certs/tls.crt",
								},
								{
									Name:  "FDB_TLS_CA_FILE",
									Value: "/tmp/fdb-certs/ca.pem",
								},
								{
									Name:  "FDB_TLS_KEY_FILE",
									Value: "/tmp/fdb-certs/tls.key",
								},
								{
									Name:  "FDB_BLOB_CREDENTIALS",
									Value: "/tmp/backup-credentials/credentials",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "fdb-certs",
									ReadOnly:  true,
									MountPath: "/tmp/fdb-certs",
								},
								{
									Name:      "backup-credentials",
									ReadOnly:  true,
									MountPath: "/tmp/backup-credentials",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "fdb-certs",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: factory.GetSecretName(),
								},
							},
						},
						{
							Name: "backup-credentials",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: factory.GetBackupSecretName(),
								},
							},
						},
					},
				},
			},
		},
	}

	gomega.Expect(factory.CreateIfAbsent(backup)).NotTo(gomega.HaveOccurred())

	factory.AddShutdownHook(func() error {
		err := factory.GetControllerRuntimeClient().Delete(context.Background(), backup)
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	})

	curBackup := &FdbBackup{
		backup:     backup,
		fdbCluster: fdbCluster,
	}
	curBackup.WaitForReconciliation()
	return curBackup
}

func (fdbBackup *FdbBackup) setState(state fdbv1beta2.BackupState) {
	objectKey := client.ObjectKeyFromObject(fdbBackup.backup)
	foundationDBBackup := &fdbv1beta2.FoundationDBBackup{}
	gomega.Expect(fdbBackup.fdbCluster.factory.GetControllerRuntimeClient().
		Get(context.Background(), objectKey, foundationDBBackup)).NotTo(gomega.HaveOccurred())

	// Backup is already in desired state
	if foundationDBBackup.Spec.BackupState == state {
		return
	}

	foundationDBBackup.Spec.BackupState = state
	gomega.Expect(fdbBackup.fdbCluster.factory.GetControllerRuntimeClient().
		Update(context.Background(), foundationDBBackup)).NotTo(gomega.HaveOccurred())
	fdbBackup.backup = foundationDBBackup
	fdbBackup.WaitForReconciliation()
}

// Stop will stop the FdbBackup.
func (fdbBackup *FdbBackup) Stop() {
	fdbBackup.setState(fdbv1beta2.BackupStateStopped)
}

// Start will start the FdbBackup.
func (fdbBackup *FdbBackup) Start() {
	fdbBackup.setState(fdbv1beta2.BackupStateRunning)
}

// Pause will pause the FdbBackup.
func (fdbBackup *FdbBackup) Pause() {
	fdbBackup.setState(fdbv1beta2.BackupStatePaused)
}

// WaitForReconciliation waits until the FdbBackup resource is fully reconciled.
func (fdbBackup *FdbBackup) WaitForReconciliation() {
	objectKey := client.ObjectKeyFromObject(fdbBackup.backup)

	gomega.Eventually(func(g gomega.Gomega) bool {
		curBackup := &fdbv1beta2.FoundationDBBackup{}
		g.Expect(fdbBackup.fdbCluster.factory.GetControllerRuntimeClient().
			Get(context.Background(), objectKey, curBackup)).NotTo(gomega.HaveOccurred())

		return curBackup.Status.Generations.Reconciled == curBackup.ObjectMeta.Generation
	}).WithTimeout(15*time.Minute).WithPolling(2*time.Second).Should(gomega.BeTrue(), "error waiting for reconciliation")
}

// WaitForRestorableVersion will wait until the back is restorable.
func (fdbBackup *FdbBackup) WaitForRestorableVersion(version uint64) {
	gomega.Eventually(func(g gomega.Gomega) uint64 {
		backupPod := fdbBackup.GetBackupPod()
		out, _, err := fdbBackup.fdbCluster.ExecuteCmdOnPod(
			*backupPod,
			fdbv1beta2.MainContainerName,
			"fdbbackup status --json",
			false,
		)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		var result map[string]interface{}
		g.Expect(json.Unmarshal([]byte(out), &result)).NotTo(gomega.HaveOccurred())

		restorable, ok := result["Restorable"].(bool)
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(restorable).To(gomega.BeTrue())

		restorablePoint, ok := result["LatestRestorablePoint"].(map[string]interface{})
		g.Expect(ok).To(gomega.BeTrue())

		restorableVersion, ok := restorablePoint["Version"].(float64)
		g.Expect(ok).To(gomega.BeTrue())

		return uint64(restorableVersion)
	}).WithTimeout(10*time.Minute).WithPolling(2*time.Second).Should(gomega.BeNumerically(">", version), "error waiting for restorable version")
}

// GetBackupPod returns a random backup Pod for the provided backup.
func (fdbBackup *FdbBackup) GetBackupPod() *corev1.Pod {
	return fdbBackup.fdbCluster.factory.ChooseRandomPod(fdbBackup.GetBackupPods())
}

// GetBackupPods returns a *corev1.PodList, which contains all pods for the provided backup.
func (fdbBackup *FdbBackup) GetBackupPods() *corev1.PodList {
	podList := &corev1.PodList{}

	gomega.Expect(fdbBackup.fdbCluster.factory.GetControllerRuntimeClient().List(context.Background(), podList,
		client.InNamespace(fdbBackup.fdbCluster.Namespace()),
		client.MatchingLabels(map[string]string{fdbv1beta2.BackupDeploymentPodLabel: fdbBackup.fdbCluster.Name() + "-backup-agents"}),
	)).NotTo(gomega.HaveOccurred())

	return podList
}
