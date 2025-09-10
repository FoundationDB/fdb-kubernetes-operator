/*
 * backup_controller.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// FoundationDBBackupReconciler reconciles a FoundationDBCluster object
type FoundationDBBackupReconciler struct {
	client.Client
	Recorder               record.EventRecorder
	Log                    logr.Logger
	InSimulation           bool
	DatabaseClientProvider fdbadminclient.DatabaseClientProvider
	ServerSideApply        bool
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

var backupSubReconcilers = []backupSubReconciler{
	updateBackupStatus{},
	updateBackupAgents{},
	startBackup{},
	stopBackup{},
	toggleBackupPaused{},
	modifyBackup{},
	updateBackupStatus{},
}

// Reconcile runs the reconciliation logic.
func (r *FoundationDBBackupReconciler) Reconcile(
	ctx context.Context,
	request ctrl.Request,
) (ctrl.Result, error) {
	backup := &fdbv1beta2.FoundationDBBackup{}
	err := r.Get(ctx, request.NamespacedName, backup)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	originalGeneration := backup.ObjectMeta.Generation
	backupLog := globalControllerLogger.WithValues(
		"namespace",
		backup.Namespace,
		"backup",
		backup.Name,
	)

	// Check if the finalizer is added or should be removed. If the FoundationDBBackup resource is being deleted (has
	// a deletion timestamp set) perform the required actions to allow the finalizer to be removed.
	err = r.updateFinalizerIfNeeded(ctx, backupLog, backup)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If the resource is being deleted, we don't want to perform any work, besides the work to remove the
	// finalizer.
	if backup.DeletionTimestamp != nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	for _, subReconciler := range backupSubReconcilers {
		req := subReconciler.reconcile(ctx, r, backup)
		if req == nil {
			continue
		}

		return processRequeue(req, subReconciler, backup, r.Recorder, backupLog)
	}

	if backup.Status.Generations.Reconciled < originalGeneration {
		backupLog.Info("Backup was not fully reconciled by reconciliation process")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	backupLog.Info("Reconciliation complete")

	return ctrl.Result{}, nil
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBBackupReconciler) getDatabaseClientProvider() fdbadminclient.DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}
	panic("Backup reconciler does not have a DatabaseClientProvider defined")
}

// adminClientForBackup provides an admin client for a backup reconciler.
func (r *FoundationDBBackupReconciler) adminClientForBackup(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
) (fdbadminclient.AdminClient, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := r.Get(
		ctx,
		types.NamespacedName{Namespace: backup.ObjectMeta.Namespace, Name: backup.Spec.ClusterName},
		cluster,
	)
	if err != nil {
		return nil, err
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return nil, err
	}

	adminClient.SetKnobs(backup.Spec.CustomParameters.GetKnobsForBackupRestoreCLI())

	return adminClient, nil
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBBackupReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrentReconciles int,
	selector metav1.LabelSelector,
) error {
	err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &appsv1.Deployment{}, "metadata.name", func(o client.Object) []string {
			return []string{o.(*appsv1.Deployment).Name}
		})
	if err != nil {
		return err
	}
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbv1beta2.FoundationDBBackup{}).
		Owns(&appsv1.Deployment{}).
		// Only react on generation changes or annotation changes and only watch
		// resources with the provided label selector.
		WithEventFilter(
			predicate.And(
				labelSelectorPredicate,
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicate.AnnotationChangedPredicate{},
				),
			)).
		Complete(r)
}

// backupSubReconciler describes a class that does part of the work of
// reconciliation for a backup.
type backupSubReconciler interface {
	/**
	reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a requeue object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a requeue object with a
	`Message` field.
	*/
	reconcile(
		ctx context.Context,
		r *FoundationDBBackupReconciler,
		backup *fdbv1beta2.FoundationDBBackup,
	) *requeue
}

// updateOrApply updates the status either with server-side apply or if disabled with the normal update call.
func (r *FoundationDBBackupReconciler) updateOrApply(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
) error {
	if r.ServerSideApply {
		// TODO(johscheuer): We have to set the TypeMeta otherwise the Patch command will fail. This is the rudimentary
		// support for server side apply which should be enough for the status use case. The controller runtime will
		// add some additional support in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/347.
		patch := &fdbv1beta2.FoundationDBBackup{
			TypeMeta: metav1.TypeMeta{
				Kind:       backup.Kind,
				APIVersion: backup.APIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      backup.Name,
				Namespace: backup.Namespace,
			},
			Status: backup.Status,
		}

		return r.Status().
			Patch(ctx, patch, client.Apply, client.FieldOwner("fdb-operator"))
		//, client.ForceOwnership)
	}

	return r.Status().Update(ctx, backup)
}

// updateOrApplySpec updates the spec either with server-side apply or if disabled with the normal update call.
func (r *FoundationDBBackupReconciler) updateOrApplySpec(
	ctx context.Context,
	backup *fdbv1beta2.FoundationDBBackup,
) error {
	// TODO(johscheuer): Implement the server-side apply feature, during testing we observed issues with the server-side
	// apply.
	return r.Update(ctx, backup)
}

// updateFinalizerIfNeeded will check if the finalizes must be updated. In case that they must be updated, they will
// be either added or removed. In case that the resource has a deletion timestamp, the operator will perform the
// according cleanup operation based on the fdbv1beta2.BackupDeletionPolicy.
func (r *FoundationDBBackupReconciler) updateFinalizerIfNeeded(ctx context.Context,
	logger logr.Logger,
	backup *fdbv1beta2.FoundationDBBackup,
) error {
	deletionPolicy := backup.GetDeletionPolicy()
	logger.V(1).Info("checking finalizer state", "deletionPolicy", deletionPolicy)

	// If the deletion policy is noop we can remove the finalizer if one is present.
	if deletionPolicy == fdbv1beta2.BackupDeletionPolicyNoop {
		if controllerutil.RemoveFinalizer(backup, fdbv1beta2.FoundationDBBackupFinalizerName) {
			logger.V(1).Info("removing the finalizer for backup resource")
			return r.updateOrApplySpec(ctx, backup)
		}

		return nil
	}

	if deletionPolicy == fdbv1beta2.BackupDeletionPolicyCleanup ||
		deletionPolicy == fdbv1beta2.BackupDeletionPolicyStop {
		// If the deletion timestamp is nil, the resource is not yet deleted. So we have to check if the resource
		// has the finalizer applied, if not the operator has to add it.
		if backup.DeletionTimestamp == nil {
			if controllerutil.AddFinalizer(backup, fdbv1beta2.FoundationDBBackupFinalizerName) {
				logger.V(1).Info("adding the finalizer for backup resource")
				return r.updateOrApplySpec(ctx, backup)
			}

			return nil
		}

		// This part of the code will be executed when the backup resource has a deletion timestamp and is waiting
		// for the cleanup of external resources.
		adminClient, err := r.adminClientForBackup(ctx, backup)
		if err != nil {
			return err
		}

		backupStatus, err := adminClient.GetBackupStatus()
		if err != nil {
			return err
		}

		if backupStatus.Status.Running {
			err = adminClient.AbortBackup(backup)
			if err != nil {
				return err
			}
		}

		// If the deletion policy is stop and the backup is stopped, we can remove the finalizer. This will allow
		// Kubernetes to remove the resource.
		if deletionPolicy == fdbv1beta2.BackupDeletionPolicyStop {
			if !backupStatus.Status.Running && controllerutil.RemoveFinalizer(
				backup,
				fdbv1beta2.FoundationDBBackupFinalizerName,
			) {
				return r.updateOrApplySpec(ctx, backup)
			}

			return nil
		}

		logger.V(1).Info("deleting backup data")
		// Delete the backup data from the blobstore, this will be a blocking operation. We eventually want to create
		// a dedicated go routine for this to not block everything. Adding a go routine will make things a bit more
		// complicated unless we get a good signal from the fdbbackup command if the data is deleted.
		err = adminClient.DeleteBackup(backup)
		if err != nil {
			if internal.IsBackupNotfound(err) {
				if controllerutil.RemoveFinalizer(
					backup,
					fdbv1beta2.FoundationDBBackupFinalizerName,
				) {
					return r.updateOrApplySpec(ctx, backup)
				}

				return nil
			}

			return err
		}

		return nil
	}

	return fmt.Errorf("unknown backup deletion policy: %s", deletionPolicy)
}
