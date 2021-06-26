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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/mock-kubernetes-client/client"

	"github.com/onsi/gomega/gexec"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
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

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyTimeout(10 * time.Second)
	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	err := scheme.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = fdbtypes.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient = &mockclient.MockClient{}

	clusterReconciler = createTestClusterReconciler()

	backupReconciler = &FoundationDBBackupReconciler{
		Client:                 k8sClient,
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBBackup"),
		Recorder:               k8sClient,
		InSimulation:           true,
		DatabaseClientProvider: mockDatabaseClientProvider{},
	}

	restoreReconciler = &FoundationDBRestoreReconciler{
		Client:                 k8sClient,
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBRestore"),
		Recorder:               k8sClient,
		InSimulation:           true,
		DatabaseClientProvider: mockDatabaseClientProvider{},
	}

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)
})

var _ = AfterEach(func() {
	k8sClient.Clear()
	ClearMockAdminClients()
	ClearMockLockClients()
})

func createDefaultCluster() *fdbtypes.FoundationDBCluster {
	trueValue := true
	failureDetectionWindow := 1

	return &fdbtypes.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-test-1",
			Namespace: "my-ns",
		},
		Spec: fdbtypes.FoundationDBClusterSpec{
			Version: fdbtypes.Versions.Default.String(),
			ProcessCounts: fdbtypes.ProcessCounts{
				Storage:           4,
				ClusterController: 1,
			},
			FaultDomain: fdbtypes.FoundationDBClusterFaultDomain{
				Key: "foundationdb.org/none",
			},
			AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
				Replacements: fdbtypes.AutomaticReplacementOptions{
					Enabled:                     &trueValue,
					FailureDetectionTimeSeconds: &failureDetectionWindow,
				},
			},
			MinimumUptimeSecondsForBounce: 1,
		},
		Status: fdbtypes.FoundationDBClusterStatus{
			RequiredAddresses: fdbtypes.RequiredAddressSet{
				NonTLS: true,
			},
			ProcessGroups: make([]*fdbtypes.ProcessGroupStatus, 0),
		},
	}
}

func createDefaultBackup(cluster *fdbtypes.FoundationDBCluster) *fdbtypes.FoundationDBBackup {
	agentCount := 3
	return &fdbtypes.FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: fdbtypes.FoundationDBBackupSpec{
			AccountName: "test@test-service",
			BackupName:  "test-backup",
			BackupState: "Running",
			Version:     cluster.Spec.Version,
			ClusterName: cluster.Name,
			AgentCount:  &agentCount,
		},
		Status: fdbtypes.FoundationDBBackupStatus{},
	}
}

func createDefaultRestore(cluster *fdbtypes.FoundationDBCluster) *fdbtypes.FoundationDBRestore {
	return &fdbtypes.FoundationDBRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: fdbtypes.FoundationDBRestoreSpec{
			BackupURL:              "blobstore://test@test-service/test-backup?bucket=fdb-backups",
			DestinationClusterName: cluster.Name,
		},
		Status: fdbtypes.FoundationDBRestoreStatus{},
	}
}

func getEnvVars(container corev1.Container) map[string]*corev1.EnvVar {
	results := make(map[string]*corev1.EnvVar)
	for index, env := range container.Env {
		results[env.Name] = &container.Env[index]
	}
	return results
}

func reconcileCluster(cluster *fdbtypes.FoundationDBCluster) (reconcile.Result, error) {
	return reconcileObject(clusterReconciler, cluster.ObjectMeta, 20)
}

func reconcileBackup(backup *fdbtypes.FoundationDBBackup) (reconcile.Result, error) {
	return reconcileObject(backupReconciler, backup.ObjectMeta, 20)
}

func reconcileRestore(restore *fdbtypes.FoundationDBRestore) (reconcile.Result, error) {
	return reconcileObject(restoreReconciler, restore.ObjectMeta, 20)
}

func reconcileObject(reconciler reconcile.Reconciler, metadata metav1.ObjectMeta, requeueLimit int) (reconcile.Result, error) {
	attempts := requeueLimit + 1
	result := reconcile.Result{Requeue: true}
	var err error = nil
	for result.Requeue && attempts > 0 {
		log.Info("Running test reconciliation")
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

func setupClusterForTest(cluster *fdbtypes.FoundationDBCluster) error {
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

	err = internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{})
	if err != nil {
		return err
	}

	return nil
}

func createTestClusterReconciler() *FoundationDBClusterReconciler {
	return &FoundationDBClusterReconciler{
		Client:                 k8sClient,
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		Recorder:               k8sClient,
		InSimulation:           true,
		PodLifecycleManager:    StandardPodLifecycleManager{},
		PodClientProvider:      NewMockFdbPodClient,
		DatabaseClientProvider: mockDatabaseClientProvider{},
	}
}
