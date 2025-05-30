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

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/setup"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fdbv1beta2.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	fdb.MustAPIVersion(710)
	operatorOpts := setup.Options{}
	operatorOpts.BindFlags(flag.CommandLine)

	logOpts := zap.Options{}
	logOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	mgr, file := setup.StartManager(
		scheme,
		operatorOpts,
		logOpts,
		controllers.NewFoundationDBClusterReconciler(
			&podmanager.StandardPodLifecycleManager{},
		),
		&controllers.FoundationDBBackupReconciler{},
		&controllers.FoundationDBRestoreReconciler{},
		ctrl.Log)

	if file != nil {
		defer func() {
			_ = file.Close()
		}()
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "problem starting manager")
		os.Exit(1)
	}
}
