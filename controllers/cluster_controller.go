/*
 * cluster_controller.go
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
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FoundationDBClusterReconciler reconciles a FoundationDBCluster object
type FoundationDBClusterReconciler struct {
	client.Client
	Recorder                                    record.EventRecorder
	Log                                         logr.Logger
	InSimulation                                bool
	EnableRestartIncompatibleProcesses          bool
	ServerSideApply                             bool
	EnableRecoveryState                         bool
	CacheDatabaseStatusForReconciliationDefault bool
	PodLifecycleManager                         podmanager.PodLifecycleManager
	PodClientProvider                           func(*fdbv1beta2.FoundationDBCluster, *corev1.Pod) (podclient.FdbPodClient, error)
	DatabaseClientProvider                      fdbadminclient.DatabaseClientProvider
	DeprecationOptions                          internal.DeprecationOptions
	GetTimeout                                  time.Duration
	PostTimeout                                 time.Duration
	MinimumRequiredUptimeCCBounce               time.Duration
	MaintenanceListStaleDuration                time.Duration
	MaintenanceListWaitDuration                 time.Duration
}

// NewFoundationDBClusterReconciler creates a new FoundationDBClusterReconciler with defaults.
func NewFoundationDBClusterReconciler(podLifecycleManager podmanager.PodLifecycleManager) *FoundationDBClusterReconciler {
	r := &FoundationDBClusterReconciler{
		PodLifecycleManager: podLifecycleManager,
	}
	r.PodClientProvider = r.newFdbPodClient

	return r
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods;configmaps;persistentvolumeclaims;events;secrets;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs the reconciliation logic.
func (r *FoundationDBClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}

	err := r.Get(ctx, request.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	clusterLog := globalControllerLogger.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name)
	cacheStatus := cluster.CacheDatabaseStatusForReconciliation(r.CacheDatabaseStatusForReconciliationDefault)
	// Printout the duration of the reconciliation, independent if the reconciliation was successful or had an error.
	startTime := time.Now()
	defer func() {
		clusterLog.Info("Reconciliation run finished", "duration_seconds", time.Since(startTime).Seconds(), "cacheStatus", cacheStatus)
	}()

	if cluster.Spec.Skip {
		clusterLog.Info("Skipping cluster with skip value true", "skip", cluster.Spec.Skip)
		// Don't requeue
		return ctrl.Result{}, nil
	}

	err = internal.NormalizeClusterSpec(cluster, r.DeprecationOptions)
	if err != nil {
		return ctrl.Result{}, err
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer adminClient.Close()

	err = cluster.Validate()
	if err != nil {
		r.Recorder.Event(cluster, corev1.EventTypeWarning, "ClusterSpec not valid", err.Error())
		return ctrl.Result{}, fmt.Errorf("ClusterSpec is not valid: %w", err)
	}

	supportedVersion, err := adminClient.VersionSupported(cluster.Spec.Version)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supportedVersion {
		return ctrl.Result{}, fmt.Errorf("version %s is not supported", cluster.Spec.Version)
	}

	var status *fdbv1beta2.FoundationDBStatus
	if cacheStatus {
		clusterLog.Info("Fetch machine-readable status for reconcilitation loop", "cacheStatus", cacheStatus)
		status, err = r.getStatusFromClusterOrDummyStatus(clusterLog, cluster)
		if err != nil {
			clusterLog.Info("could not fetch machine-readable status and therefore didn't cache the it")
		}
	}

	subReconcilers := []clusterSubReconciler{
		updateStatus{},
		updateLockConfiguration{},
		updateConfigMap{},
		checkClientCompatibility{},
		deletePodsForBuggification{},
		replaceMisconfiguredProcessGroups{},
		replaceFailedProcessGroups{},
		addProcessGroups{},
		addServices{},
		addPVCs{},
		addPods{},
		generateInitialClusterFile{},
		removeIncompatibleProcesses{},
		updateSidecarVersions{},
		updatePodConfig{},
		updateMetadata{},
		updateDatabaseConfiguration{},
		chooseRemovals{},
		excludeProcesses{},
		changeCoordinators{},
		bounceProcesses{},
		maintenanceModeChecker{},
		updatePods{},
		removeProcessGroups{},
		removeServices{},
		updateStatus{},
	}

	originalGeneration := cluster.ObjectMeta.Generation
	normalizedSpec := cluster.Spec.DeepCopy()
	delayedRequeue := false

	for _, subReconciler := range subReconcilers {
		// We have to set the normalized spec here again otherwise any call to Update() for the status of the cluster
		// will reset all normalized fields...
		cluster.Spec = *(normalizedSpec.DeepCopy())

		requeue := runClusterSubReconciler(ctx, clusterLog, subReconciler, r, cluster, status)
		if requeue == nil {
			continue
		}

		if requeue.delayedRequeue {
			clusterLog.Info("Delaying requeue for sub-reconciler",
				"reconciler", fmt.Sprintf("%T", subReconciler),
				"message", requeue.message,
				"error", requeue.curError)
			delayedRequeue = true
			continue
		}

		return processRequeue(requeue, subReconciler, cluster, r.Recorder, clusterLog)
	}

	if cluster.Status.Generations.Reconciled < originalGeneration || delayedRequeue {
		clusterLog.Info("Cluster was not fully reconciled by reconciliation process", "status", cluster.Status.Generations,
			"CurrentGeneration", cluster.Status.Generations.Reconciled,
			"OriginalGeneration", originalGeneration, "DelayedRequeue", delayedRequeue)

		return ctrl.Result{Requeue: true}, nil
	}

	clusterLog.Info("Reconciliation complete", "generation", cluster.Status.Generations.Reconciled)
	r.Recorder.Event(cluster, corev1.EventTypeNormal, "ReconciliationComplete", fmt.Sprintf("Reconciled generation %d", cluster.Status.Generations.Reconciled))

	return ctrl.Result{}, nil
}

// runClusterSubReconciler will start the subReconciler and will log the duration of the subReconciler.
func runClusterSubReconciler(ctx context.Context, logger logr.Logger, subReconciler clusterSubReconciler, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus) *requeue {
	subReconcileLogger := logger.WithValues("reconciler", fmt.Sprintf("%T", subReconciler))
	startTime := time.Now()
	subReconcileLogger.Info("Attempting to run sub-reconciler")
	defer func() {
		subReconcileLogger.Info("Subreconciler finished run", "duration_seconds", time.Since(startTime).Seconds())
	}()

	return subReconciler.reconcile(ctx, r, cluster, status, subReconcileLogger)
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBClusterReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int, enableNodeIndex bool, selector metav1.LabelSelector, watchedObjects ...client.Object) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.Pod).Name}
	})
	if err != nil {
		return err
	}

	if enableNodeIndex {
		err = mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Node{}, "metadata.name", func(o client.Object) []string {
			return []string{o.(*corev1.Node).Name}
		})
		if err != nil {
			return err
		}
	}

	err = mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Service{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.Service).Name}
	})
	if err != nil {
		return err
	}

	err = mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.PersistentVolumeClaim{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*corev1.PersistentVolumeClaim).Name}
	})
	if err != nil {
		return err
	}

	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbv1beta2.FoundationDBCluster{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		// Only react on generation changes or annotation changes and only watch
		// resources with the provided label selector.
		WithEventFilter(
			predicate.And(
				labelSelectorPredicate,
				predicate.Or(
					predicate.LabelChangedPredicate{},
					predicate.GenerationChangedPredicate{},
					predicate.AnnotationChangedPredicate{},
				),
			))

	for _, object := range watchedObjects {
		builder.Owns(object)
	}
	return builder.Complete(r)
}

func (r *FoundationDBClusterReconciler) updatePodDynamicConf(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (bool, error) {
	if cluster.ProcessGroupIsBeingRemoved(podmanager.GetProcessGroupID(cluster, pod)) {
		return true, nil
	}

	podClient, message := r.getPodClient(cluster, pod)
	if podClient == nil {
		logger.Info("Unable to generate pod client", "message", message)
		return false, nil
	}

	processClass, err := podmanager.GetProcessClass(cluster, pod)
	if err != nil {
		return false, err
	}

	serversPerPod, err := internal.GetServersPerPodForPod(pod, processClass)
	if err != nil {
		return false, err
	}

	var expectedConf string

	imageType := internal.GetImageType(pod)
	if imageType == internal.FDBImageTypeUnified {
		config, err := internal.GetMonitorProcessConfiguration(cluster, processClass, serversPerPod, imageType, nil)
		if err != nil {
			return false, err
		}
		configData, err := json.Marshal(config)
		if err != nil {
			return false, err
		}
		expectedConf = string(configData)
	} else {
		expectedConf, err = internal.GetMonitorConf(cluster, processClass, podClient, serversPerPod)
		if err != nil {
			return false, err
		}
	}

	syncedFDBcluster, clusterErr := podClient.UpdateFile("fdb.cluster", cluster.Status.ConnectionString)
	syncedFDBMonitor, err := podClient.UpdateFile("fdbmonitor.conf", expectedConf)
	if !syncedFDBcluster || !syncedFDBMonitor {
		if clusterErr != nil {
			return false, clusterErr
		}

		return false, err
	}

	if cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		return podClient.IsPresent(fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
	}

	return true, nil
}

func (r *FoundationDBClusterReconciler) getPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (podclient.FdbPodClient, string) {
	if pod == nil {
		return nil, fmt.Sprintf("Process group in cluster %s/%s does not have pod defined", cluster.Namespace, cluster.Name)
	}

	podClient, err := r.PodClientProvider(cluster, pod)
	if err != nil {
		return nil, err.Error()
	}

	return podClient, ""
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBClusterReconciler) getDatabaseClientProvider() fdbadminclient.DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}

	panic("Cluster reconciler does not have a DatabaseClientProvider defined")
}

func (r *FoundationDBClusterReconciler) getLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return r.getDatabaseClientProvider().GetLockClient(cluster)
}

// takeLock attempts to acquire a lock.
func (r *FoundationDBClusterReconciler) takeLock(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, action string) (bool, error) {
	logger.Info("Taking lock on cluster", "namespace", cluster.Namespace, "cluster", cluster.Name, "action", action)
	lockClient, err := r.getLockClient(cluster)
	if err != nil {
		return false, err
	}

	hasLock, err := lockClient.TakeLock()
	if err != nil {
		return false, err
	}

	if !hasLock {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "LockAcquisitionFailed", fmt.Sprintf("Lock required before %s", action))
	}
	return hasLock, nil
}

// releaseLock attempts to release a lock.
func (r *FoundationDBClusterReconciler) releaseLock(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) error {
	logger.Info("Release lock on cluster", "namespace", cluster.Namespace, "cluster", cluster.Name)
	lockClient, err := r.getLockClient(cluster)
	if err != nil {
		return err
	}

	return lockClient.ReleaseLock()
}

var connectionStringNameRegex, _ = regexp.Compile("[^A-Za-z0-9_]")

// clusterSubReconciler describes a class that does part of the work of
// reconciliation for a cluster.
type clusterSubReconciler interface {
	/**
	reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a	requeue object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a requeue object with
	a `Message` field.
	*/
	reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue
}

// newFdbPodClient builds a client for working with an FDB Pod
func (r *FoundationDBClusterReconciler) newFdbPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (podclient.FdbPodClient, error) {
	return internal.NewFdbPodClient(cluster, pod, globalControllerLogger.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "pod", pod.Name), r.GetTimeout, r.PostTimeout)
}

// updateOrApply updates the status either with server-side apply or if disabled with the normal update call.
func (r *FoundationDBClusterReconciler) updateOrApply(ctx context.Context, cluster *fdbv1beta2.FoundationDBCluster) error {
	if r.ServerSideApply {
		// TODO(johscheuer): We have to set the TypeMeta otherwise the Patch command will fail. This is the rudimentary
		// support for server side apply which should be enough for the status use case. The controller runtime will
		// add some additional support in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/347.
		patch := &fdbv1beta2.FoundationDBCluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       cluster.Kind,
				APIVersion: cluster.APIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
			Status: cluster.Status,
		}

		return r.Status().Patch(ctx, patch, client.Apply, client.FieldOwner("fdb-operator")) //, client.ForceOwnership)
	}

	return r.Status().Update(ctx, cluster)
}

// getStatusFromClusterOrDummyStatus will fetch the machine-readable status from the FoundationDBCluster if the cluster is configured. If not a default status is returned indicating, that
// some configuration is missing.
func (r *FoundationDBClusterReconciler) getStatusFromClusterOrDummyStatus(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) (*fdbv1beta2.FoundationDBStatus, error) {
	if cluster.Status.ConnectionString == "" {
		return &fdbv1beta2.FoundationDBStatus{
			Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
				Layers: fdbv1beta2.FoundationDBStatusLayerInfo{
					Error: "configurationMissing",
				},
			},
		}, nil
	}

	connectionString, err := tryConnectionOptions(logger, cluster, r)
	if err != nil {
		return nil, err
	}

	// Update the connection string if the newly fetched connection string is different from the current one and if the
	// newly fetched connection string is not empty.
	if cluster.Status.ConnectionString != connectionString && connectionString != "" {
		logger.Info("Updating out-of-date connection string", "previousConnectionString", cluster.Status.ConnectionString, "newConnectionString", connectionString)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConnectionString", fmt.Sprintf("Setting connection string to %s", connectionString))
		cluster.Status.ConnectionString = connectionString
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return nil, err
	}
	defer adminClient.Close()

	// If the cluster is not yet configured, we can reduce the timeout to make sure the initial reconcile steps
	// are faster.
	if !cluster.Status.Configured {
		adminClient.SetTimeout(10 * time.Second)
	}

	status, err := adminClient.GetStatus()
	if err == nil {
		return status, nil
	}

	if cluster.Status.Configured {
		return nil, err
	}

	return &fdbv1beta2.FoundationDBStatus{
		Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
			Layers: fdbv1beta2.FoundationDBStatusLayerInfo{
				Error: "configurationMissing",
			},
		},
	}, nil
}
