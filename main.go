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
	"io"
	"os"

	appsv1beta1 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = appsv1beta1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var logFile string

	fdb.MustAPIVersion(610)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&logFile, "log-file", "", "The path to a file to write logs to.")
	flag.Parse()

	var logWriter io.Writer
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		defer file.Close()
		logWriter = io.MultiWriter(os.Stdout, file)
	} else {
		logWriter = os.Stdout
	}

	ctrl.SetLogger(zap.New(func(o *zap.Options) {
		o.Development = true
		o.DestWritter = logWriter
	}))

	options := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		Port:               9443,
	}

	namespace := os.Getenv("WATCH_NAMESPACE")
	if namespace != "" {
		options.Namespace = namespace
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	clusterReconciler := &controllers.FoundationDBClusterReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor("foundationdbcluster-controller"),
		Log:                 ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		Scheme:              mgr.GetScheme(),
		PodLifecycleManager: controllers.StandardPodLifecycleManager{},
		PodClientProvider:   controllers.NewFdbPodClient,
		AdminClientProvider: controllers.NewCliAdminClient,
		LockClientProvider:  controllers.NewRealLockClient,
	}

	if err = clusterReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FoundationDBCluster")
		os.Exit(1)
	}

	backupReconciler := &controllers.FoundationDBBackupReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor("foundationdbcluster-controller"),
		Log:                 ctrl.Log.WithName("controllers").WithName("FoundationDBCluster"),
		Scheme:              mgr.GetScheme(),
		AdminClientProvider: controllers.NewCliAdminClient,
	}

	if err = backupReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FoundationDBBackup")
		os.Exit(1)
	}

	restoreReconciler := &controllers.FoundationDBRestoreReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor("foundationdbrestore-controller"),
		Log:                 ctrl.Log.WithName("controllers").WithName("FoundationDBRestore"),
		Scheme:              mgr.GetScheme(),
		AdminClientProvider: controllers.NewCliAdminClient,
	}

	if err = restoreReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FoundationDBRestore")
		os.Exit(1)
	}

	if metricsAddr != "0" {
		controllers.InitCustomMetrics(clusterReconciler)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
