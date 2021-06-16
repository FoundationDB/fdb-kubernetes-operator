/*
 * setup.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package setup

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/fdbclient"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var setupLog = ctrl.Log.WithName("setup")

// Options provides all configuration Options for the operator
type Options struct {
	MetricsAddr             string
	EnableLeaderElection    bool
	LeaderElectionID        string
	LogFile                 string
	CliTimeout              int
	DeprecationOptions      internal.DeprecationOptions
	MaxConcurrentReconciles int
}

// BindFlags will parse the given flagset for the operator option flags
func (o *Options) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.MetricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	fs.BoolVar(&o.EnableLeaderElection, "enable-leader-election", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fs.StringVar(&o.LeaderElectionID, "leader-election-id", "fdb-kubernetes-operator",
		"LeaderElectionID determines the name of the resource that leader election will use for holding the leader lock.")
	fs.BoolVar(&o.DeprecationOptions.UseFutureDefaults, "use-future-defaults", false,
		"Apply defaults from the next major version of the operator. This is only intended for use in development.",
	)
	fs.StringVar(&o.LogFile, "log-file", "", "The path to a file to write logs to.")
	fs.IntVar(&o.CliTimeout, "cli-timeout", 10, "The timeout to use for CLI commands.")
	fs.IntVar(&o.MaxConcurrentReconciles, "max-concurrent-reconciles", 1, "Defines the maximum number of concurrent reconciles for all controllers.")
}

// StartManager will start the FoundtionDB operator manager.
// Each reconciler that is not nil will be added to the list of reconcilers
// For all reconcilers the Client, Recorder and if appropriate the namespace will be set.
func StartManager(
	scheme *runtime.Scheme,
	operatorOpts Options,
	logOpts zap.Options,
	clusterReconciler *controllers.FoundationDBClusterReconciler,
	backupReconciler *controllers.FoundationDBBackupReconciler,
	restoreReconciler *controllers.FoundationDBRestoreReconciler,
	watchedObjects ...client.Object) (manager.Manager, *os.File) {
	var logWriter io.Writer
	var file *os.File

	if operatorOpts.LogFile != "" {
		var err error
		file, err = os.OpenFile(operatorOpts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		logWriter = io.MultiWriter(os.Stdout, file)
	} else {
		logWriter = os.Stdout
	}

	logger := zap.New(
		zap.UseFlagOptions(&logOpts),
		zap.WriteTo(logWriter))
	ctrl.SetLogger(logger)

	// Might be called by controller-runtime in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/1420
	klog.SetLogger(logger)

	fdbclient.DefaultCLITimeout = operatorOpts.CliTimeout

	options := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: operatorOpts.MetricsAddr,
		LeaderElection:     operatorOpts.EnableLeaderElection,
		LeaderElectionID:   operatorOpts.LeaderElectionID,
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

	if err := moveFDBBinaries(); err != nil {
		setupLog.Error(err, "unable to move FDB binaries")
		os.Exit(1)
	}

	if clusterReconciler != nil {
		clusterReconciler.Client = mgr.GetClient()
		clusterReconciler.Recorder = mgr.GetEventRecorderFor("foundationdbcluster-controller")
		clusterReconciler.Namespace = namespace

		if err := clusterReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles, watchedObjects...); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FoundationDBCluster")
			os.Exit(1)
		}

		if operatorOpts.MetricsAddr != "0" {
			controllers.InitCustomMetrics(clusterReconciler)
		}
	}

	if backupReconciler != nil {
		backupReconciler.Client = mgr.GetClient()
		backupReconciler.Recorder = mgr.GetEventRecorderFor("foundationdbbackup-controller")

		if err := backupReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FoundationDBBackup")
			os.Exit(1)
		}
	}

	if restoreReconciler != nil {
		restoreReconciler.Client = mgr.GetClient()
		restoreReconciler.Recorder = mgr.GetEventRecorderFor("foundationdbrestore-controller")

		if err := restoreReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FoundationDBRestore")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("setup manager")
	return mgr, file
}

// MoveFDBBinaries moves FDB binaries that are pulled from setup containers into
// the correct locations.
func moveFDBBinaries() error {
	binFile, err := os.Open(os.Getenv("FDB_BINARY_DIR"))
	if err != nil {
		return err
	}
	binDir, err := binFile.Readdir(0)
	if err != nil {
		return err
	}
	for _, binEntry := range binDir {
		if binEntry.IsDir() && v1beta1.FDBVersionRegex.Match([]byte(binEntry.Name())) {
			version, err := v1beta1.ParseFdbVersion(binEntry.Name())
			if err != nil {
				return err
			}

			versionBinFile, err := os.Open(path.Join(binFile.Name(), binEntry.Name(), "bin", binEntry.Name()))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				minorVersionPath := path.Join(binFile.Name(), fmt.Sprintf("%d.%d", version.Major, version.Minor))
				err = os.MkdirAll(minorVersionPath, os.ModeDir|os.ModePerm)
				if err != nil {
					return err
				}

				versionBinDir, err := versionBinFile.Readdir(0)
				if err != nil {
					return err
				}
				for _, versionBinEntry := range versionBinDir {
					currentPath := path.Join(versionBinFile.Name(), versionBinEntry.Name())
					newPath := path.Join(minorVersionPath, versionBinEntry.Name())
					setupLog.Info("Moving FDB binary file", "currentPath", currentPath, "newPath", newPath)
					err = os.Rename(currentPath, newPath)
					if err != nil {
						return err
					}
				}
			}

			versionLibFile, err := os.Open(path.Join(binFile.Name(), binEntry.Name(), "lib", "libfdb_c.so"))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				currentPath := path.Join(versionLibFile.Name())
				newPath := path.Join(binFile.Name(), fmt.Sprintf("libfdb_c_%s.so", version))
				setupLog.Info("Moving FDB library file", "currentPath", currentPath, "newPath", newPath)
				err = os.Rename(currentPath, newPath)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
