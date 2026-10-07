/*
 * backup_expiration_test.go
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

package internal

import (
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("backup expiration Job", func() {
	DescribeTable(
		"inherits backup runtime configuration without running backup agents or sidecars",
		func(imageType fdbv1beta2.ImageType) {
			backup := CreateDefaultBackup(CreateDefaultCluster())
			backup.Labels = map[string]string{"operator-instance": "test"}
			backup.Spec.ImageType = ptr.To(imageType)
			backup.Spec.AgentCount = ptr.To(0)
			backup.Spec.Version = "7.4.6"
			backup.Spec.EncryptionKeyPath = "/keys/backup.key"
			backup.Spec.CustomParameters = fdbv1beta2.FoundationDBCustomParameters{
				"knob_backup_concurrent_deletes=4", "force", "expire-before-version=123", "locality_custom=test",
				"encryption-key-file=/keys/custom.key", "legacy-encryption-format",
			}
			backup.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"custom": "label"}},
				Spec: corev1.PodSpec{
					ServiceAccountName: "backup-account",
					NodeSelector:       map[string]string{"pool": "backup"},
					Containers: []corev1.Container{
						{
							Name: fdbv1beta2.MainContainerName,
							Env: []corev1.EnvVar{
								{Name: "FDB_BLOB_CREDENTIALS", Value: "/credentials/blob.json"},
								{Name: "FDB_TLS_CA_FILE", Value: "/credentials/ca.pem"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "credentials", MountPath: "/credentials"},
							},
							ReadinessProbe: &corev1.Probe{},
							LivenessProbe:  &corev1.Probe{},
							StartupProbe:   &corev1.Probe{},
							Lifecycle:      &corev1.Lifecycle{},
						},
						{Name: "logging-sidecar", Image: "logger"},
					},
					Volumes: []corev1.Volume{
						{Name: "credentials", VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: "backup-credentials"},
						}},
					},
				},
			}
			backup.Status.Expiration = &fdbv1beta2.BackupExpirationStatus{
				BeforeTimestamp: metav1.NewTime(
					time.Date(2026, 9, 1, 8, 0, 0, 0, time.FixedZone("SGT", 8*3600)),
				),
				DestinationURL: "blobstore://test@store:443/pinned-backup?bucket=backups",
				ClusterName:    "original-cluster",
				JobName:        "expiration-job",
				Phase:          "Running",
			}
			original := backup.DeepCopy()
			job, err := GetBackupExpirationJob(backup)
			Expect(err).NotTo(HaveOccurred())
			Expect(backup).To(Equal(original))
			Expect(job.Name).To(Equal("expiration-job"))
			Expect(job.Labels).To(Equal(backup.Labels))
			Expect(metav1.IsControlledBy(job, backup)).To(BeTrue())
			Expect(ptr.Deref(job.Spec.ActiveDeadlineSeconds, 0)).To(Equal(int64(3600)))
			Expect(ptr.Deref(job.Spec.BackoffLimit, 0)).To(Equal(int32(3)))
			pod := job.Spec.Template
			Expect(pod.Labels).To(HaveKeyWithValue("custom", "label"))
			Expect(pod.Labels).NotTo(HaveKey(fdbv1beta2.BackupDeploymentPodLabel))
			Expect(pod.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
			Expect(pod.Spec.ServiceAccountName).To(Equal("backup-account"))
			Expect(pod.Spec.NodeSelector).To(HaveKeyWithValue("pool", "backup"))
			Expect(pod.Spec.InitContainers).To(HaveLen(1))
			Expect(pod.Spec.Volumes).To(ContainElement(corev1.Volume{
				Name: "config-map",
				VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "original-cluster-config",
					},
					Items: []corev1.KeyToPath{
						{Key: fdbv1beta2.ClusterFileKey, Path: "fdb.cluster"},
					},
				}},
			}))
			Expect(
				pod.Spec.Volumes,
			).To(ContainElement(original.Spec.PodTemplateSpec.Spec.Volumes[0]))
			Expect(pod.Spec.Containers).To(HaveLen(1))
			container := pod.Spec.Containers[0]
			Expect(container.Image).To(HaveSuffix(":7.4.6"))
			Expect(container.Command).To(Equal([]string{"fdbbackup"}))
			Expect(container.Args).To(Equal([]string{
				"expire", "-d", backup.Status.Expiration.DestinationURL,
				"--expire-before-timestamp", "2026/09/01.00:00:00+0000",
				"--knob_backup_concurrent_deletes=4",
			}))
			Expect(
				container.Env,
			).To(ContainElements(original.Spec.PodTemplateSpec.Spec.Containers[0].Env))
			Expect(
				container.Env,
			).To(ContainElement(corev1.EnvVar{Name: fdbv1beta2.EnvNameClusterFile, Value: "/var/dynamic-conf/fdb.cluster"}))
			Expect(
				container.VolumeMounts,
			).To(ContainElement(corev1.VolumeMount{Name: "credentials", MountPath: "/credentials"}))
			Expect(container.ReadinessProbe).To(BeNil())
			Expect(container.LivenessProbe).To(BeNil())
			Expect(container.StartupProbe).To(BeNil())
			Expect(container.Lifecycle).To(BeNil())
		},
		Entry("split image", fdbv1beta2.ImageTypeSplit),
		Entry("unified image", fdbv1beta2.ImageTypeUnified),
	)
})
