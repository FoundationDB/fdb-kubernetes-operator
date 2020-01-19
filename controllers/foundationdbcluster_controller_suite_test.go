/*
 * foundationdbcluster_controller_suite_test.go
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

package foundationdbcluster

import (
	stdlog "log"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/foundationdb/fdb-kubernetes-operator/pkg/apis"
	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var cfg *rest.Config

// TestMain runs the test suite.
func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crds")},
	}
	apis.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}

	code := m.Run()
	t.Stop()
	os.Exit(code)
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(t *testing.T, inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(req)
		if err != nil {
			switch err.(type) {
			case ReconciliationNotReadyError:
				requests <- req
				return reconcile.Result{}, nil
			default:
				t.Errorf("Reconcile function returned %T error %s", err, err.Error())
				return reconcile.Result{Requeue: true}, nil
			}
		}
		if !result.Requeue && result.RequeueAfter == 0 {
			requests <- req
		}
		return result, err
	})
	return fn, requests
}

// StartTestManager adds recFn
func StartTestManager(mgr manager.Manager, g *gomega.GomegaWithT) (chan struct{}, *sync.WaitGroup) {
	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		g.Expect(mgr.Start(stop)).NotTo(gomega.HaveOccurred())
	}()
	return stop, wg
}

// newReconciler returns a new reconcile.Reconciler
func newTestReconciler(mgr manager.Manager) *ReconcileFoundationDBCluster {
	return &ReconcileFoundationDBCluster{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetRecorder("foundationdbcluster_controller"),
		Scheme:              mgr.GetScheme(),
		InSimulation:        true,
		PodLifecycleManager: StandardPodLifecycleManager{},
		PodClientProvider:   NewMockFdbPodClient,
		AdminClientProvider: NewMockAdminClient,
	}
}

var Versions = struct {
	Default, WithSidecarInstanceIdSubstitution, WithoutSidecarInstanceIdSubstitution,
	WithCommandLineVariablesForSidecar, WithEnvironmentVariablesForSidecar,
	WithBinariesFromMainContainer, WithBinariesFromMainContainerNext,
	WithoutBinariesFromMainContainer fdbtypes.FdbVersion
}{
	Default:                              fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithSidecarInstanceIdSubstitution:    fdbtypes.FdbVersion{Major: 7, Minor: 0, Patch: 0},
	WithoutSidecarInstanceIdSubstitution: fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithCommandLineVariablesForSidecar:   fdbtypes.FdbVersion{Major: 7, Minor: 0, Patch: 0},
	WithEnvironmentVariablesForSidecar:   fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithBinariesFromMainContainer:        fdbtypes.FdbVersion{Major: 7, Minor: 0, Patch: 0},
	WithBinariesFromMainContainerNext:    fdbtypes.FdbVersion{Major: 7, Minor: 0, Patch: 1},
	WithoutBinariesFromMainContainer:     fdbtypes.FdbVersion{Major: 6, Minor: 2, Patch: 11},
}
