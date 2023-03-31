/*
 * suite_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2019 Apple Inc. and the FoundationDB project authors
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
	"testing"
	"time"

	mockpodclient "github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/mock-kubernetes-client/client"

	"github.com/onsi/gomega/gexec"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	//"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var k8sClient *mockclient.MockClient
var clusterReconciler *FoundationDBClusterReconciler
var backupReconciler *FoundationDBBackupReconciler
var restoreReconciler *FoundationDBRestoreReconciler
var requeueLimit = 20

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyTimeout(10 * time.Second)
	RunSpecs(t, "FDB Controllers")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	err := scheme.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = fdbv1beta2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme
	k8sClient = mockclient.NewMockClient(scheme.Scheme)

	clusterReconciler = createTestClusterReconciler()

	backupReconciler = &FoundationDBBackupReconciler{
		Client:                 k8sClient,
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBBackup"),
		Recorder:               k8sClient,
		InSimulation:           true,
		DatabaseClientProvider: mock.DatabaseClientProvider{},
	}

	restoreReconciler = &FoundationDBRestoreReconciler{
		Client:                 k8sClient,
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBRestore"),
		Recorder:               k8sClient,
		DatabaseClientProvider: mock.DatabaseClientProvider{},
	}
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)
})

var _ = AfterEach(func() {
	k8sClient.Clear()
	mock.ClearMockAdminClients()
	mock.ClearMockLockClients()
})

func createDefaultRestore(cluster *fdbv1beta2.FoundationDBCluster) *fdbv1beta2.FoundationDBRestore {
	return &fdbv1beta2.FoundationDBRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: fdbv1beta2.FoundationDBRestoreSpec{
			BlobStoreConfiguration: &fdbv1beta2.BlobStoreConfiguration{
				AccountName: "test@test-service",
				BackupName:  "test-backup",
				Bucket:      "fdb-backups",
			},
			DestinationClusterName: cluster.Name,
		},
		Status: fdbv1beta2.FoundationDBRestoreStatus{},
	}
}

func reconcileCluster(cluster *fdbv1beta2.FoundationDBCluster) (reconcile.Result, error) {
	return reconcileObject(clusterReconciler, cluster.ObjectMeta, requeueLimit)
}

func reconcileClusterWithCustomRequeueLimit(cluster *fdbv1beta2.FoundationDBCluster, customRequeueLimit int) (reconcile.Result, error) {
	return reconcileObject(clusterReconciler, cluster.ObjectMeta, customRequeueLimit)
}

func reconcileBackup(backup *fdbv1beta2.FoundationDBBackup) (reconcile.Result, error) {
	return reconcileObject(backupReconciler, backup.ObjectMeta, requeueLimit)
}

func reconcileRestore(restore *fdbv1beta2.FoundationDBRestore) (reconcile.Result, error) {
	return reconcileObject(restoreReconciler, restore.ObjectMeta, requeueLimit)
}

func reconcileObject(reconciler reconcile.Reconciler, metadata metav1.ObjectMeta, requeueLimit int) (reconcile.Result, error) {
	attempts := requeueLimit + 1
	result := reconcile.Result{Requeue: true}
	var err error
	for result.Requeue && attempts > 0 {
		log.Info("Running test reconciliation", "Attemps", attempts)
		attempts--

		result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: metadata.Namespace, Name: metadata.Name}})
		if err != nil {
			log.Error(err, "Error in reconciliation")
			break
		}

		if !result.Requeue {
			log.Info("Reconciliation successful")
		}
	}

	return result, err
}

func setupClusterForTest(cluster *fdbv1beta2.FoundationDBCluster) error {
	// Q: Are all fields in cluster saved in k8s' etcd? why?
	err := k8sClient.Create(context.TODO(), cluster)
	if err != nil {
		return err
	}

	_, err = reconcileCluster(cluster)
	if err != nil {
		return err
	}

	_, err = reloadCluster(cluster)
	if err != nil {
		return err
	}

	return internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
}

func createTestClusterReconciler() *FoundationDBClusterReconciler {
	return &FoundationDBClusterReconciler{
		Client:                 k8sClient,
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		Recorder:               k8sClient,
		InSimulation:           true,
		PodLifecycleManager:    podmanager.StandardPodLifecycleManager{},
		PodClientProvider:      mockpodclient.NewMockFdbPodClient,
		DatabaseClientProvider: mock.DatabaseClientProvider{},
	}
}
