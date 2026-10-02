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
	"fmt"
	"maps"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// GetBackupExpirationJob builds a Job for the request pinned in backup status
func GetBackupExpirationJob(backup *fdbv1beta2.FoundationDBBackup) (*batchv1.Job, error) {
	request := backup.Status.Expiration
	if request == nil {
		return nil, fmt.Errorf("backup has no expiration request")
	}

	// Build the same runtime configuration even when the backup has no agent deployment
	configuration := backup.DeepCopy()
	configuration.Spec.ClusterName = request.ClusterName
	configuration.Spec.AgentCount = ptr.To(1)
	deployment, err := GetBackupDeployment(configuration)
	if err != nil {
		return nil, err
	}
	template := deployment.Spec.Template
	delete(template.Labels, fdbv1beta2.BackupDeploymentPodLabel)
	template.Spec.RestartPolicy = corev1.RestartPolicyNever
	template.Spec.EphemeralContainers = nil

	for _, container := range template.Spec.Containers {
		if container.Name != fdbv1beta2.MainContainerName {
			continue
		}
		container.Command = []string{"fdbbackup"}
		container.Args = []string{
			"expire", "-d", request.DestinationURL,
			"--expire-before-timestamp", request.BeforeTimestamp.UTC().Format("2006/01/02.15:04:05-0700"),
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
			Name:            request.JobName,
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
