/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/fdbclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/setup"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = fdbtypes.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	fdb.MustAPIVersion(610)
	operatorOpts := setup.Options{}
	operatorOpts.BindFlags(flag.CommandLine)

	logOpts := zap.Options{}
	logOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	clusterReconciler := &controllers.FoundationDBClusterReconciler{
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		PodLifecycleManager:    controllers.StandardPodLifecycleManager{},
		PodClientProvider:      controllers.NewFdbPodClient,
		DatabaseClientProvider: fdbclient.NewDatabaseClientProvider(),
		DeprecationOptions:     operatorOpts.DeprecationOptions,
	}

	backupReconciler := &controllers.FoundationDBBackupReconciler{
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		DatabaseClientProvider: fdbclient.NewDatabaseClientProvider(),
	}

	restoreReconciler := &controllers.FoundationDBRestoreReconciler{
		Log:                    ctrl.Log.WithName("controllers").WithName("FoundationDBRestore"),
		DatabaseClientProvider: fdbclient.NewDatabaseClientProvider(),
	}

	mgr, file := setup.StartManager(
		scheme,
		operatorOpts,
		logOpts,
		clusterReconciler,
		backupReconciler,
		restoreReconciler)

	if file != nil {
		defer file.Close()
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "problem starting manager")
		os.Exit(1)
	}
}
