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
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

	"github.com/onsi/gomega/gexec"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
var reconciler *FoundationDBClusterReconciler

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

	reconciler = &FoundationDBClusterReconciler{
		Client:              k8sManager.GetClient(),
		Log:                 ctrl.Log.WithName("controllers").WithName("SecretScope"),
		Recorder:            k8sManager.GetEventRecorderFor("secretscope-controller"),
		InSimulation:        true,
		PodLifecycleManager: StandardPodLifecycleManager{},
		PodClientProvider:   NewMockFdbPodClient,
		PodIPProvider:       MockPodIP,
		AdminClientProvider: NewMockAdminClient,
	}

	err = (reconciler).SetupWithManager(k8sManager)
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
			Backup: fdbtypes.BackupSpec{
				AgentCount: 3,
			},
		},
		Status: fdbtypes.FoundationDBClusterStatus{
			RequiredAddresses: fdbtypes.RequiredAddressSet{
				NonTLS: true,
			},
		},
	}
}
