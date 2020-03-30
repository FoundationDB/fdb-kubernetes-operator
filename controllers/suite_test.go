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
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

	"github.com/onsi/gomega/gexec"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var clusterReconciler *FoundationDBClusterReconciler
var backupReconciler *FoundationDBBackupReconciler

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{envtest.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = scheme.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = fdbtypes.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	clusterReconciler = &FoundationDBClusterReconciler{
		Client:              k8sManager.GetClient(),
		Log:                 ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		Recorder:            k8sManager.GetEventRecorderFor("foundationdbcluster-controller"),
		InSimulation:        true,
		PodLifecycleManager: StandardPodLifecycleManager{},
		PodClientProvider:   NewMockFdbPodClient,
		PodIPProvider:       MockPodIP,
		AdminClientProvider: NewMockAdminClient,
	}

	err = (clusterReconciler).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	backupReconciler = &FoundationDBBackupReconciler{
		Client:       k8sManager.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("FoundationDBBackup"),
		Recorder:     k8sManager.GetEventRecorderFor("foundationdbbackup-controller"),
		InSimulation: true,
	}
	err = backupReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var Versions = struct {
	NextMajorVersion,
	WithSidecarInstanceIDSubstitution, WithoutSidecarInstanceIDSubstitution,
	WithCommandLineVariablesForSidecar, WithEnvironmentVariablesForSidecar,
	WithBinariesFromMainContainer, WithoutBinariesFromMainContainer,
	WithRatekeeperRole, WithoutRatekeeperRole,
	Default fdbtypes.FdbVersion
}{
	Default:                              fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 15},
	NextMajorVersion:                     fdbtypes.FdbVersion{Major: 7, Minor: 0, Patch: 0},
	WithSidecarInstanceIDSubstitution:    fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithoutSidecarInstanceIDSubstitution: fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithCommandLineVariablesForSidecar:   fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithEnvironmentVariablesForSidecar:   fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithBinariesFromMainContainer:        fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithoutBinariesFromMainContainer:     fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithRatekeeperRole:                   fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithoutRatekeeperRole:                fdbtypes.FdbVersion{Major: 6, Minor: 1, Patch: 12},
}

func createDefaultCluster() *fdbtypes.FoundationDBCluster {
	return &fdbtypes.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-test-1",
			Namespace: "my-ns",
		},
		Spec: fdbtypes.FoundationDBClusterSpec{
			Version:          Versions.Default.String(),
			ConnectionString: "operator-test:asdfasf@127.0.0.1:4501",
			ProcessCounts: fdbtypes.ProcessCounts{
				Storage:           4,
				ClusterController: 1,
			},
			FaultDomain: fdbtypes.FoundationDBClusterFaultDomain{
				Key: "foundationdb.org/none",
			},
		},
		Status: fdbtypes.FoundationDBClusterStatus{
			RequiredAddresses: fdbtypes.RequiredAddressSet{
				NonTLS: true,
			},
		},
	}
}

func createReconciledCluster() *fdbtypes.FoundationDBCluster {
	cluster := createDefaultCluster()
	cluster.Spec.ConnectionString = ""
	err := k8sClient.Create(context.TODO(), cluster)
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() (bool, error) { return checkClusterReconciled(k8sClient, cluster) }, 10*time.Second).Should(BeTrue())
	err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	Expect(err).NotTo(HaveOccurred())

	return cluster
}

func createDefaultBackup(cluster *fdbtypes.FoundationDBCluster) *fdbtypes.FoundationDBBackup {
	agentCount := 3
	return &fdbtypes.FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: fdbtypes.FoundationDBBackupSpec{
			Version:     cluster.Spec.Version,
			ClusterName: cluster.Name,
			AgentCount:  &agentCount,
		},
		Status: fdbtypes.FoundationDBBackupStatus{},
	}
}

func cleanupCluster(cluster *fdbtypes.FoundationDBCluster) {
	err := k8sClient.Delete(context.TODO(), cluster)
	Expect(err).NotTo(HaveOccurred())

	pods := &corev1.PodList{}
	err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range pods.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}

	configMaps := &corev1.ConfigMapList{}
	err = k8sClient.List(context.TODO(), configMaps, getListOptions(cluster)...)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range configMaps.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = k8sClient.List(context.TODO(), pvcs, getListOptions(cluster)...)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range pvcs.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}
}

func cleanupBackup(backup *fdbtypes.FoundationDBBackup) {
	err := k8sClient.Delete(context.TODO(), backup)
	Expect(err).NotTo(HaveOccurred())

	deployments := &appsv1.DeploymentList{}
	err = k8sClient.List(context.TODO(), deployments)
	Expect(err).NotTo(HaveOccurred())

	for _, item := range deployments.Items {
		err = k8sClient.Delete(context.TODO(), &item)
		Expect(err).NotTo(HaveOccurred())
	}
}

func runClusterReconciler(reconciler ClusterSubReconciler, cluster *fdbtypes.FoundationDBCluster, shouldContinue bool) error {
	canContinue, err := reconciler.Reconcile(clusterReconciler, context.TODO(), cluster)
	if shouldContinue {
		Expect(err).To(BeNil())
	}
	Expect(canContinue).To(Equal(shouldContinue))
	return err
}
