/*
 * restore_controller.go
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
	ctx "context"
	"fmt"
	"time"

	"golang.org/x/net/context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FoundationDBRestoreReconciler reconciles a FoundationDBRestore object
type FoundationDBRestoreReconciler struct {
	client.Client
	Recorder            record.EventRecorder
	Log                 logr.Logger
	scheme              *runtime.Scheme
	InSimulation        bool
	AdminClientProvider func(*fdbtypes.FoundationDBCluster, client.Client) (AdminClient, error)
}

// SetScheme sets the current runtime Scheme
func (r *FoundationDBRestoreReconciler) SetScheme(scheme *runtime.Scheme) {
	r.scheme = scheme
}

// Scheme returns the current runtime Scheme
func (r *FoundationDBRestoreReconciler) Scheme() *runtime.Scheme {
	return r.scheme
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbrestores/status,verbs=get;update;patch

// Reconcile runs the reconciliation logic.
func (r *FoundationDBRestoreReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	restore := &fdbtypes.FoundationDBRestore{}
	err := r.Get(ctx, request.NamespacedName, restore)

	originalGeneration := restore.ObjectMeta.Generation

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	subReconcilers := []RestoreSubReconciler{
		StartRestore{},
	}

	for _, subReconciler := range subReconcilers {
		canContinue, err := subReconciler.Reconcile(r, ctx, restore)
		if !canContinue || err != nil {
			log.Info("Reconciliation terminated early", "namespace", restore.Namespace, "restore", restore.Name, "lastAction", fmt.Sprintf("%T", subReconciler))
		}

		if err != nil {
			log.Error(err, "Error in reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler), "namespace", restore.Namespace, "restore", restore.Name)
			return ctrl.Result{}, err
		} else if restore.ObjectMeta.Generation != originalGeneration {
			log.Info("Ending reconciliation early because restore has been updated")
			return ctrl.Result{}, nil
		} else if !canContinue {
			log.Info("Requeuing reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler), "namespace", restore.Namespace, "restore", restore.Name)
			return ctrl.Result{Requeue: true, RequeueAfter: subReconciler.RequeueAfter()}, nil
		}
	}

	log.Info("Reconciliation complete", "namespace", restore.Namespace, "restore", restore.Name)

	return ctrl.Result{}, nil
}

// AdminClientForRestore provides an admin client for a restore reconciler.
func (r *FoundationDBRestoreReconciler) AdminClientForRestore(context ctx.Context, restore *fdbtypes.FoundationDBRestore) (AdminClient, error) {
	cluster := &fdbtypes.FoundationDBCluster{}
	err := r.Get(context, types.NamespacedName{Namespace: restore.ObjectMeta.Namespace, Name: restore.Spec.DestinationClusterName}, cluster)
	if err != nil {
		return nil, err
	}

	return r.AdminClientProvider(cluster, r)
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fdbtypes.FoundationDBRestore{}).
		Complete(r)
}

// RestoreSubReconciler describes a class that does part of the work of
// reconciliation for a restore.
type RestoreSubReconciler interface {
	/**
	Reconcile runs the reconciler's work.

	If reconciliation can continue, this should return (true, nil).

	If reconciliation encounters an error, this should return (false, err).

	If reconciliation cannot proceed, or if this method has to make a change
	to the restore spec, this should return (false, nil).

	This method will only be called once for a given instance of the reconciler.
	*/
	Reconcile(r *FoundationDBRestoreReconciler, context ctx.Context, restore *fdbtypes.FoundationDBRestore) (bool, error)

	/**
	RequeueAfter returns the delay before we should run the reconciliation
	again.
	*/
	RequeueAfter() time.Duration
}
