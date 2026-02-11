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
	"fmt"
	"log"
	"strconv"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FdbBackup represents a fdbv1beta2.FoundationDBBackup resource for doing backups of a FdbCluster.
type FdbBackup struct {
	backup     *fdbv1beta2.FoundationDBBackup
	fdbCluster *FdbCluster
}

// FdbBackupConfiguration can be used to configure the created fdbv1beta2.FoundationDBBackup with different options.
type FdbBackupConfiguration struct {
	// BackupType defines the backup type that should be used for this backup.
	BackupType *fdbv1beta2.BackupType
	// BackupState defines the state the backup should be started with.
	BackupState *fdbv1beta2.BackupState
	// EncryptionEnabled determines whether backup encryption should be used.
	EncryptionEnabled bool
	// BackupMode defines the backup mode that should be used.
	BackupMode *fdbv1beta2.BackupMode
	// CustomParameters defines the custom parameters to add to the backup deployment.
	CustomParameters fdbv1beta2.FoundationDBCustomParameters
}

// CreateBackupForCluster will create a FoundationDBBackup for the provided cluster.
func (factory *Factory) CreateBackupForCluster(
	fdbCluster *FdbCluster,
	config *FdbBackupConfiguration,
) *FdbBackup {
	return factory.CreateBackupForClusterFromSpec(
		factory.GenerateBackupSpecForCluster(fdbCluster, config),
		fdbCluster,
	)
}

// GenerateBackupSpecForCluster will generate the fdbv1beta2.FoundationDBBackup spec for the provided configuration
// and FdbCluster.
func (factory *Factory) GenerateBackupSpecForCluster(
	fdbCluster *FdbCluster,
	config *FdbBackupConfiguration,
) *fdbv1beta2.FoundationDBBackup {
	// For more information how the backup system with the operator is working please look at
	// the operator documentation: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/blob/master/docs/manual/backup.md
	fdbVersion := factory.GetFDBVersion()

	// If the config is nil, create a default config.
	if config == nil {
		config = &FdbBackupConfiguration{}
	}

	backup := &fdbv1beta2.FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fdbCluster.Name(),
			Namespace: fdbCluster.Namespace(),
		},
		Spec: fdbv1beta2.FoundationDBBackupSpec{
			AllowTagOverride: ptr.To(true),
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
			DeletionPolicy:   ptr.To(fdbv1beta2.BackupDeletionPolicyCleanup),
			BackupType:       config.BackupType,
			BackupState:      ptr.Deref(config.BackupState, fdbv1beta2.BackupStateRunning),
			CustomParameters: config.CustomParameters,
			BackupMode:       config.BackupMode,
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
								{
									Name: fdbv1beta2.EnvNameMachineID,
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
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
								{
									Name:      "encryption-key",
									ReadOnly:  true,
									MountPath: "/tmp/encryption-key",
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
						{
							Name: "encryption-key",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: factory.GetEncryptionKeySecretName(),
								},
							},
						},
					},
				},
			},
		},
	}

	// Set encryption key path only if encryption is enabled
	if config.EncryptionEnabled {
		backup.Spec.EncryptionKeyPath = "/tmp/encryption-key/key.bin"
	}

	return backup
}

// CreateBackupForClusterFromSpec creates a FdbBackup. This method can be used in combination with the GenerateBackupSpecForCluster method.
// In general this should only be used for special cases that are not covered by changing the FdbBackupConfiguration.
func (factory *Factory) CreateBackupForClusterFromSpec(
	spec *fdbv1beta2.FoundationDBBackup,
	fdbCluster *FdbCluster,
) *FdbBackup {
	startTime := time.Now()
	defer func() {
		log.Println(
			"FoundationDB backup created in",
			time.Since(startTime).String(),
		)
	}()

	gomega.Expect(factory.CreateIfAbsent(spec)).NotTo(gomega.HaveOccurred())
	curBackup := &FdbBackup{
		backup:     spec,
		fdbCluster: fdbCluster,
	}

	factory.AddShutdownHook(func() error {
		curBackup.Destroy()

		return nil
	})

	curBackup.WaitForReconciliation()

	return curBackup
}

// GetBackup fetch the current state of the fdbv1beta2.FoundationDBBackup and return it.
func (fdbBackup *FdbBackup) GetBackup() *fdbv1beta2.FoundationDBBackup {
	objectKey := client.ObjectKeyFromObject(fdbBackup.backup)
	foundationDBBackup := &fdbv1beta2.FoundationDBBackup{}
	gomega.Expect(fdbBackup.fdbCluster.factory.GetControllerRuntimeClient().
		Get(context.Background(), objectKey, foundationDBBackup)).NotTo(gomega.HaveOccurred())

	fdbBackup.backup = foundationDBBackup
	return fdbBackup.backup
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

// RunDescribeCommand run the describe command on the backup pod.
func (fdbBackup *FdbBackup) RunDescribeCommand() string {
	backupPod := fdbBackup.GetBackupPod()
	command := fmt.Sprintf(
		"fdbbackup describe -d \"%s\" --json",
		fdbBackup.backup.BackupURL())
	out, _, err := fdbBackup.fdbCluster.ExecuteCmdOnPod(
		*backupPod,
		fdbv1beta2.MainContainerName,
		command,
		false)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return out
}

// WaitForReconciliation waits until the FdbBackup resource is fully reconciled.
func (fdbBackup *FdbBackup) WaitForReconciliation() {
	objectKey := client.ObjectKeyFromObject(fdbBackup.backup)

	startTime := time.Now()
	waitDuration := 5 * time.Minute
	gomega.Eventually(func(g gomega.Gomega) bool {
		curBackup := &fdbv1beta2.FoundationDBBackup{}
		g.Expect(fdbBackup.fdbCluster.factory.GetControllerRuntimeClient().
			Get(context.Background(), objectKey, curBackup)).NotTo(gomega.HaveOccurred())

		// Dump the operator and cluster status after 5 minutes
		if time.Since(startTime) > waitDuration {
			fdbBackup.fdbCluster.factory.DumpState(fdbBackup.fdbCluster)
			startTime = time.Now()
		}

		return curBackup.Status.Generations.Reconciled == curBackup.ObjectMeta.Generation
	}).WithTimeout(15*time.Minute).WithPolling(2*time.Second).Should(gomega.BeTrue(), "error waiting for reconciliation of FDB backup")
}

// WaitForRestorableVersion will wait until the back is restorable.
func (fdbBackup *FdbBackup) WaitForRestorableVersion(version uint64) uint64 {
	var restorableVersion uint64
	gomega.Eventually(func(g gomega.Gomega) uint64 {
		backupPod := fdbBackup.GetBackupPod()
		out, _, err := fdbBackup.fdbCluster.ExecuteCmdOnPod(
			*backupPod,
			fdbv1beta2.MainContainerName,
			"fdbbackup status --json",
			false,
		)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		status := &fdbv1beta2.FoundationDBLiveBackupStatus{}
		g.Expect(json.Unmarshal([]byte(out), status)).NotTo(gomega.HaveOccurred())

		var latestRestorableVersion uint64
		if status.LatestRestorablePoint != nil {
			latestRestorableVersion = ptr.Deref(status.LatestRestorablePoint.Version, 0)
		}

		log.Println(
			"Backup status running:",
			status.Status.Running,
			"restorable:",
			ptr.Deref(status.Restorable, false),
			"latestRestorablePoint:",
			latestRestorableVersion,
		)
		g.Expect(ptr.Deref(status.Restorable, false)).To(gomega.BeTrue())
		g.Expect(status.LatestRestorablePoint).NotTo(gomega.BeNil())

		return ptr.Deref(status.LatestRestorablePoint.Version, 0)
	}).WithTimeout(10*time.Minute).WithPolling(2*time.Second).Should(gomega.BeNumerically(">=", version), "error waiting for restorable version")
	return restorableVersion
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
	),
	).
		NotTo(gomega.HaveOccurred())

	return podList
}

// Destroy will remove the underlying backup resources.
func (fdbBackup *FdbBackup) Destroy() {
	gomega.Eventually(func(g gomega.Gomega) {
		err := fdbBackup.fdbCluster.getClient().
			Delete(context.Background(), fdbBackup.backup)
		if k8serrors.IsNotFound(err) {
			return
		}

		g.Expect(err).NotTo(gomega.HaveOccurred())
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).To(gomega.Succeed())

	// Ensure that the resource is removed.
	gomega.Eventually(func(g gomega.Gomega) {
		backup := &fdbv1beta2.FoundationDBBackup{}
		err := fdbBackup.fdbCluster.getClient().
			Get(context.Background(), client.ObjectKeyFromObject(fdbBackup.backup), backup)
		g.Expect(k8serrors.IsNotFound(err)).To(gomega.BeTrue())
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).To(gomega.Succeed())
}

// ForceReconcile will add an annotation with the current timestamp on the FoundationDBBackup resource to make sure
// the operator reconciliation loop is triggered. This is used to speed up some test cases.
func (fdbBackup *FdbBackup) ForceReconcile() {
	log.Printf("ForceReconcile: Status Generations=%s, Metadata Generation=%d",
		ToJSON(fdbBackup.backup.Status.Generations),
		fdbBackup.backup.ObjectMeta.Generation)

	patch := client.MergeFrom(fdbBackup.backup.DeepCopy())
	if fdbBackup.backup.Annotations == nil {
		fdbBackup.backup.Annotations = make(map[string]string)
	}
	fdbBackup.backup.Annotations["foundationdb.org/reconcile"] = strconv.FormatInt(
		time.Now().UnixNano(),
		10,
	)

	// This will apply an Annotation to the object which will trigger the reconcile loop.
	// This should speed up the reconcile phase.
	err := fdbBackup.fdbCluster.getClient().Patch(
		context.Background(),
		fdbBackup.backup,
		patch)
	if err != nil {
		log.Println("error patching annotation to force reconcile, error:", err.Error())
	}
}
