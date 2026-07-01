/*
 * update_backup_agents.go
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
	"fmt"
	"reflect"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updateBackupAgents provides a reconciliation step for updating the
// deployment for the backup agents.
type updateBackupAgents struct{}

// reconcile runs the reconciler's work.
func (u updateBackupAgents) reconcile(
	ctx context.Context,
	r *FoundationDBBackupReconciler,
	backup *fdbv1beta2.FoundationDBBackup,
) *requeue {
	logger := globalControllerLogger.WithValues(
		"namespace",
		backup.Namespace,
		"cluster",
		backup.Name,
		"reconciler",
		"updateBackupAgents",
	)
	deploymentName := internal.GetBackupDeploymentName(backup)
	existingDeployment := &appsv1.Deployment{}
	needCreation := false

	err := r.Get(
		ctx,
		client.ObjectKey{Name: deploymentName, Namespace: backup.Namespace},
		existingDeployment,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needCreation = true
		} else {
			return &requeue{curError: err}
		}
	}

	deployment, err := internal.GetBackupDeployment(backup)
	if err != nil {
		r.Recorder.Event(backup, corev1.EventTypeWarning, "GetBackupDeployment", err.Error())
		return &requeue{curError: err}
	}

	if deployment != nil && deployment.ObjectMeta.Name != deploymentName {
		return &requeue{
			curError: fmt.Errorf(
				"inconsistent deployment names: %s != %s",
				deployment.ObjectMeta.Name,
				deploymentName,
			),
		}
	}

	if needCreation && deployment != nil {
		logger.V(1).Info("Creating deployment", "name", deployment.Name)
		err = r.Create(ctx, deployment)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	if !needCreation && deployment != nil {
		annotationChange := internal.MergeAnnotations(
			&existingDeployment.ObjectMeta,
			deployment.ObjectMeta,
		)
		deployment.ObjectMeta.Annotations = existingDeployment.ObjectMeta.Annotations

		if annotationChange ||
			!reflect.DeepEqual(existingDeployment.ObjectMeta.Labels, deployment.ObjectMeta.Labels) {
			err = r.Update(ctx, deployment)
			if err != nil {
				return &requeue{curError: err}
			}
		}

		if existingDeployment.Status.ReadyReplicas < existingDeployment.Status.Replicas {
			backupAgentPods := &corev1.PodList{}
			err = r.List(
				ctx,
				backupAgentPods,
				client.InNamespace(backup.Namespace),
				client.MatchingLabels(existingDeployment.Spec.Selector.MatchLabels),
			)
			if err != nil {
				return &requeue{curError: err}
			}

			// minTimeTillNextDeletion keeps track of the minimum time the operator has to wait before it can delete a new pod that
			// is stuck in a terminal state.
			var minTimeTillNextDeletion time.Duration
			for _, pod := range backupAgentPods.Items {
				phase := pod.Status.Phase
				reason := pod.Status.Reason
				if phase != corev1.PodFailed && phase != corev1.PodSucceeded {
					continue
				}

				remaining := time.Until(
					pod.CreationTimestamp.Add(r.MinimumAgeForTerminalPodDeletion),
				)
				if remaining > 0 {
					logger.Info("Pod in terminal state is too young to be deleted",
						"pod", pod.Name,
						"phase", phase,
						"reason", reason,
						"minimumAge", r.MinimumAgeForTerminalPodDeletion)
					if minTimeTillNextDeletion == 0 || remaining < minTimeTillNextDeletion {
						minTimeTillNextDeletion = remaining
					}
					continue
				}

				logger.Info("Deleting pod that is stuck in a terminal state",
					"pod", pod.Name,
					"phase", phase,
					"reason", reason)
				err = r.Delete(ctx, ptr.To(pod))
				if err != nil {
					return &requeue{curError: err}
				}
			}

			if minTimeTillNextDeletion > 0 {
				return &requeue{
					message:        "pod in terminal state is too young to be deleted",
					delay:          minTimeTillNextDeletion,
					delayedRequeue: true,
				}
			}
		}
	}

	if !needCreation && deployment == nil {
		logger.V(1).Info("Deleting deployment", "name", existingDeployment.Name)
		err = r.Delete(ctx, existingDeployment)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
