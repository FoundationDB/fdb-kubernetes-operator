/*
 * backup_expiration.go
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
	"errors"
	"maps"
	"strconv"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const backupTimestampLayout = "2006/01/02.15:04:05-0700"

// GetBackupExpirationJob builds a Job using the current expiration policy and backup destination
func GetBackupExpirationJob(
	backup *fdbv1beta2.FoundationDBBackup,
	now time.Time,
) (*batchv1.Job, error) {
	currentStatus := backup.Status.Expiration
	if currentStatus == nil {
		return nil, errors.New("backup has no expiration status")
	}

	// Build the same runtime configuration even when the backup has no agent deployment
	configuration := backup.DeepCopy()
	configuration.Spec.AgentCount = ptr.To(1)
	deployment, err := GetBackupDeployment(configuration)
	if err != nil {
		return nil, err
	}
	template := deployment.Spec.Template
	delete(template.Labels, fdbv1beta2.BackupDeploymentPodLabel)
	template.Spec.RestartPolicy = corev1.RestartPolicyNever
	template.Spec.EphemeralContainers = nil
	policy := backup.Spec.Expiration
	now = now.UTC()
	var cutoff time.Time
	if policy.BeforeTimestamp != nil {
		cutoff = policy.BeforeTimestamp.UTC()
	} else {
		cutoff = now.Add(-time.Duration(*policy.ExpireAfterSeconds) * time.Second)
	}

	for _, container := range template.Spec.Containers {
		if container.Name != fdbv1beta2.MainContainerName {
			continue
		}
		container.Command = []string{"fdbbackup"}
		container.Args = []string{
			"expire", "-d", backup.Status.BackupDetails.URL,
			"--expire-before-timestamp", cutoff.Format(backupTimestampLayout),
		}
		if policy.RestorableAfterTimestamp != nil {
			container.Args = append(container.Args, "--restorable-after-timestamp",
				policy.RestorableAfterTimestamp.UTC().Format(backupTimestampLayout))
		}
		if policy.RestorableForSeconds != nil {
			restorableAfter := now.Add(-time.Duration(*policy.RestorableForSeconds) * time.Second)
			container.Args = append(
				container.Args,
				"--restorable-after-timestamp",
				restorableAfter.Format(backupTimestampLayout),
			)
		}
		if policy.RestorableAfterVersion != nil {
			container.Args = append(container.Args, "--restorable-after-version",
				strconv.FormatInt(*policy.RestorableAfterVersion, 10))
		}
		// Only knobs can be inherited: other CLI options could override the destination or disable safety checks
		for _, parameter := range backup.Spec.CustomParameters.GetKnobsForBackupRestoreCLI() {
			if strings.HasPrefix(parameter, "--knob_") {
				container.Args = append(container.Args, parameter)
			}
		}
		container.LivenessProbe = nil
		container.ReadinessProbe = nil
		container.StartupProbe = nil
		container.Lifecycle = nil
		template.Spec.Containers = []corev1.Container{container}
		break
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            currentStatus.JobName,
			Namespace:       backup.Namespace,
			Labels:          maps.Clone(backup.Labels),
			OwnerReferences: BuildOwnerReference(backup.TypeMeta, backup.ObjectMeta),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To(int32(3)),
			ActiveDeadlineSeconds: ptr.To(int64(3600)),
			Template:              template,
		},
	}, nil
}
