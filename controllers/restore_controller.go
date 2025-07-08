/*
 * restore_controller.go
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
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// FoundationDBRestoreReconciler reconciles a FoundationDBRestore object
type FoundationDBRestoreReconciler struct {
	client.Client
	Recorder               record.EventRecorder
	Log                    logr.Logger
	DatabaseClientProvider fdbadminclient.DatabaseClientProvider
	ServerSideApply        bool
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbrestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs the reconciliation logic.
func (r *FoundationDBRestoreReconciler) Reconcile(
	ctx context.Context,
	request ctrl.Request,
) (ctrl.Result, error) {
	restore := &fdbv1beta2.FoundationDBRestore{}
	err := r.Get(ctx, request.NamespacedName, restore)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	restoreLog := globalControllerLogger.WithValues(
		"namespace",
		restore.Namespace,
		"restore",
		restore.Name,
		"traceID",
		uuid.NewUUID(),
	)

	subReconcilers := []restoreSubReconciler{
		updateRestoreStatus{},
		startRestore{},
		updateRestoreStatus{},
	}

	for _, subReconciler := range subReconcilers {
		req := subReconciler.reconcile(ctx, r, restore)
		if req == nil {
			continue
		}
		return processRequeue(req, subReconciler, restore, r.Recorder, restoreLog)
	}

	if restore.Status.State != fdbv1beta2.CompletedFoundationDBRestoreState {
		restoreLog.Info("Restore has not yet completed",
			"Status", restore.Status.State,
			"Running", restore.Status.Running)

		// Check the status of the restore every 5 minutes, otherwise check the status every minute.
		if restore.Status.State != fdbv1beta2.RunningFoundationDBRestoreState {
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Minute}, nil
		}

		return ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Minute}, nil
	}

	restoreLog.Info("Reconciliation complete")

	return ctrl.Result{}, nil
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBRestoreReconciler) getDatabaseClientProvider() fdbadminclient.DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}
	panic("Restore reconciler does not have a DatabaseClientProvider defined")
}

// adminClientForRestore provides an admin client for a restore reconciler.
func (r *FoundationDBRestoreReconciler) adminClientForRestore(
	ctx context.Context,
	restore *fdbv1beta2.FoundationDBRestore,
) (fdbadminclient.AdminClient, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: restore.ObjectMeta.Namespace,
			Name:      restore.Spec.DestinationClusterName,
		},
		cluster,
	)
	if err != nil {
		return nil, err
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return nil, err
	}

	adminClient.SetKnobs(restore.Spec.CustomParameters.GetKnobsForCLI())

	return adminClient, nil
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBRestoreReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrentReconciles int,
	selector metav1.LabelSelector,
) error {
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbv1beta2.FoundationDBRestore{}).
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

// restoreSubReconciler describes a class that does part of the work of
// reconciliation for a restore.
type restoreSubReconciler interface {
	/**
	reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a `requeue` object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a `requeue` object with
	a `Message` field.
	*/
	reconcile(
		ctx context.Context,
		r *FoundationDBRestoreReconciler,
		restore *fdbv1beta2.FoundationDBRestore,
	) *requeue
}

// updateOrApply updates the status either with server-side apply or if disabled with the normal update call.
func (r *FoundationDBRestoreReconciler) updateOrApply(
	ctx context.Context,
	restore *fdbv1beta2.FoundationDBRestore,
) error {
	if r.ServerSideApply {
		// TODO(johscheuer): We have to set the TypeMeta otherwise the Patch command will fail. This is the rudimentary
		// support for server side apply which should be enough for the status use case. The controller runtime will
		// add some additional support in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/347.
		patch := &fdbv1beta2.FoundationDBRestore{
			TypeMeta: metav1.TypeMeta{
				Kind:       restore.Kind,
				APIVersion: restore.APIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      restore.Name,
				Namespace: restore.Namespace,
			},
			Status: restore.Status,
		}

		return r.Status().
			Patch(ctx, patch, client.Apply, client.FieldOwner("fdb-operator"))
		//, client.ForceOwnership)
	}

	return r.Status().Update(ctx, restore)
}
