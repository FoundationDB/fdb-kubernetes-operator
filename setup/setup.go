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
	"io/fs"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/fdbclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"gopkg.in/natefinch/lumberjack.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var operatorVersion = "latest"

// Options provides all configuration Options for the operator
type Options struct {
	EnableLeaderElection               bool
	CleanUpOldLogFile                  bool
	CompressOldFiles                   bool
	PrintVersion                       bool
	EnableRestartIncompatibleProcesses bool
	ServerSideApply                    bool
	EnableRecoveryState                bool
	CacheDatabaseStatus                bool
	EnableNodeIndex                    bool
	MetricsAddr                        string
	LeaderElectionID                   string
	LogFile                            string
	LogFilePermission                  string
	LabelSelector                      string
	WatchNamespace                     string
	CliTimeout                         int
	MaxCliTimeout                      int
	MaxConcurrentReconciles            int
	LogFileMaxSize                     int
	LogFileMaxAge                      int
	MaxNumberOfOldLogFiles             int
	LogFileMinAge                      time.Duration
	GetTimeout                         time.Duration
	PostTimeout                        time.Duration
	DeprecationOptions                 internal.DeprecationOptions
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
	fs.IntVar(&o.CliTimeout, "cli-timeout", 10, "The timeout to use for CLI commands in seconds.")
	fs.IntVar(&o.MaxCliTimeout, "max-cli-timeout", 40, "The maximum timeout to use for CLI commands in seconds. This timeout is used for CLI requests that are known to be potentially slow like get status or exclude.")
	fs.IntVar(&o.MaxConcurrentReconciles, "max-concurrent-reconciles", 1, "Defines the maximum number of concurrent reconciles for all controllers.")
	fs.BoolVar(&o.CleanUpOldLogFile, "cleanup-old-cli-logs", true, "Defines if the operator should delete old fdbcli log files.")
	fs.DurationVar(&o.LogFileMinAge, "log-file-min-age", 5*time.Minute, "Defines the minimum age of fdbcli log files before removing when \"--cleanup-old-cli-logs\" is set.")
	fs.IntVar(&o.LogFileMaxAge, "log-file-max-age", 28, "Defines the maximum age to retain old operator log file in number of days.")
	fs.IntVar(&o.LogFileMaxSize, "log-file-max-size", 250, "Defines the maximum size in megabytes of the operator log file before it gets rotated.")
	fs.StringVar(&o.LogFilePermission, "log-file-permission", "0644",
		"The file permission for the log file. Only used if log-file is set. Only the octal representation is supported.")
	fs.IntVar(&o.MaxNumberOfOldLogFiles, "max-old-log-files", 3, "Defines the maximum number of old operator log files to retain.")
	fs.BoolVar(&o.CompressOldFiles, "compress", false, "Defines whether the rotated log files should be compressed using gzip or not.")
	fs.BoolVar(&o.PrintVersion, "version", false, "Prints the version of the operator and exits.")
	fs.StringVar(&o.LabelSelector, "label-selector", "", "Defines a label-selector that will be used to select resources.")
	fs.StringVar(&o.WatchNamespace, "watch-namespace", os.Getenv("WATCH_NAMESPACE"), "Defines which namespace the operator should watch.")
	fs.DurationVar(&o.GetTimeout, "get-timeout", 5*time.Second, "http timeout for get requests to the FDB sidecar.")
	fs.DurationVar(&o.PostTimeout, "post-timeout", 10*time.Second, "http timeout for post requests to the FDB sidecar.")
	fs.BoolVar(&o.EnableRestartIncompatibleProcesses, "enable-restart-incompatible-processes", true, "This flag enables/disables in the operator to restart incompatible fdbserver processes.")
	fs.BoolVar(&o.ServerSideApply, "server-side-apply", false, "This flag enables server side apply.")
	fs.BoolVar(&o.EnableRecoveryState, "enable-recovery-state", true, "This flag enables the use of the recovery state for the minimum uptime between bounced if the FDB version supports it.")
	fs.BoolVar(&o.CacheDatabaseStatus, "cache-database-status", true, "Defines the default value for caching the database status.")
	fs.BoolVar(&o.EnableNodeIndex, "enable-node-index", false, "Defines if the operator should add an index for accessing node objects. This requires a ClusterRoleBinding with node access. If the taint feature should be used, this setting should be set to true.")
}

// StartManager will start the FoundationDB operator manager.
// Each reconciler that is not nil will be added to the list of reconcilers
// For all reconcilers the Client, Recorder and if appropriate the namespace will be set.
func StartManager(
	scheme *runtime.Scheme,
	operatorOpts Options,
	logOpts zap.Options,
	clusterReconciler *controllers.FoundationDBClusterReconciler,
	backupReconciler *controllers.FoundationDBBackupReconciler,
	restoreReconciler *controllers.FoundationDBRestoreReconciler,
	logr logr.Logger,
	watchedObjects ...client.Object) (manager.Manager, *os.File) {
	if operatorOpts.PrintVersion {
		fmt.Printf("version: %s\n", operatorVersion)
		os.Exit(0)
	}

	logWriter, err := setupLogger(operatorOpts)
	if err != nil {
		log.Fatalf("unable to setup logger: %s, got error: %s\n", operatorOpts.LogFile, err.Error())
	}

	logger := zap.New(
		zap.UseFlagOptions(&logOpts),
		zap.WriteTo(logWriter))
	ctrl.SetLogger(logger)

	// Might be called by controller-runtime in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/1420
	klog.SetLogger(logger)

	setupLog := logger.WithName("setup")
	fdbclient.DefaultCLITimeout = time.Duration(operatorOpts.CliTimeout) * time.Second
	fdbclient.MaxCliTimeout = time.Duration(operatorOpts.MaxCliTimeout) * time.Second

	options := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: operatorOpts.MetricsAddr,
		LeaderElection:     operatorOpts.EnableLeaderElection,
		LeaderElectionID:   operatorOpts.LeaderElectionID,
		Port:               9443,
	}

	if operatorOpts.WatchNamespace != "" {
		options.Namespace = operatorOpts.WatchNamespace
		setupLog.Info("Operator starting in single namespace mode", "namespace", options.Namespace)
	} else {
		setupLog.Info("Operator starting in Global mode")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := moveFDBBinaries(setupLog); err != nil {
		setupLog.Error(err, "unable to move FDB binaries")
		os.Exit(1)
	}

	labelSelector, err := metav1.ParseToLabelSelector(strings.Trim(operatorOpts.LabelSelector, "\""))
	if err != nil {
		setupLog.Error(err, "unable to parse provided label selector")
		os.Exit(1)
	}

	if clusterReconciler != nil {
		clusterReconciler.Client = mgr.GetClient()
		clusterReconciler.Recorder = mgr.GetEventRecorderFor("foundationdbcluster-controller")
		clusterReconciler.DeprecationOptions = operatorOpts.DeprecationOptions
		clusterReconciler.DatabaseClientProvider = fdbclient.NewDatabaseClientProvider(logger)
		clusterReconciler.GetTimeout = operatorOpts.GetTimeout
		clusterReconciler.PostTimeout = operatorOpts.PostTimeout
		clusterReconciler.Log = logr.WithName("controllers").WithName("FoundationDBCluster")
		clusterReconciler.EnableRestartIncompatibleProcesses = operatorOpts.EnableRestartIncompatibleProcesses
		clusterReconciler.ServerSideApply = operatorOpts.ServerSideApply
		clusterReconciler.EnableRecoveryState = operatorOpts.EnableRecoveryState
		clusterReconciler.CacheDatabaseStatusForReconciliationDefault = operatorOpts.CacheDatabaseStatus

		if err := clusterReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles, operatorOpts.EnableNodeIndex, *labelSelector, watchedObjects...); err != nil {
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
		backupReconciler.DatabaseClientProvider = fdbclient.NewDatabaseClientProvider(logger)
		backupReconciler.Log = logr.WithName("controllers").WithName("FoundationDBBackup")
		backupReconciler.ServerSideApply = operatorOpts.ServerSideApply

		if err := backupReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles, *labelSelector); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FoundationDBBackup")
			os.Exit(1)
		}
	}

	if restoreReconciler != nil {
		restoreReconciler.Client = mgr.GetClient()
		restoreReconciler.Recorder = mgr.GetEventRecorderFor("foundationdbrestore-controller")
		restoreReconciler.DatabaseClientProvider = fdbclient.NewDatabaseClientProvider(logger)
		restoreReconciler.Log = logr.WithName("controllers").WithName("FoundationDBRestore")
		restoreReconciler.ServerSideApply = operatorOpts.ServerSideApply

		if err := restoreReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles, *labelSelector); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FoundationDBRestore")
			os.Exit(1)
		}
	}

	if operatorOpts.CleanUpOldLogFile {
		setupLog.V(1).Info("setup log file cleaner", "LogFileMinAge", operatorOpts.LogFileMinAge.String())
		cleaner := internal.NewCliLogFileCleaner(logger, operatorOpts.LogFileMinAge)
		ticker := time.NewTicker(operatorOpts.LogFileMinAge)
		go func() {
			for {
				<-ticker.C
				cleaner.CleanupOldCliLogs()
			}
		}()
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("setup manager")
	return mgr, nil
}

// MoveFDBBinaries moves FDB binaries that are pulled from setup containers into
// the correct locations.
func moveFDBBinaries(log logr.Logger) error {
	binFile, err := os.Open(os.Getenv("FDB_BINARY_DIR"))
	if err != nil {
		return err
	}
	defer binFile.Close()

	libDir, err := os.Open(os.Getenv("FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY"))
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(os.Getenv("FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY"), os.ModeDir|os.ModePerm)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	binDir, err := binFile.Readdir(0)
	if err != nil {
		return err
	}

	for _, binEntry := range binDir {
		if binEntry.IsDir() && v1beta2.VersionRegex.Match([]byte(binEntry.Name())) {
			version, err := v1beta2.ParseFdbVersion(binEntry.Name())
			if err != nil {
				return err
			}

			versionBinFile, err := os.Open(path.Join(binFile.Name(), binEntry.Name(), "bin", binEntry.Name()))
			if err != nil && !os.IsNotExist(err) {
				return err
			}

			if err == nil {
				minorVersionPath := path.Join(binFile.Name(), version.GetBinaryVersion())
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
					log.Info("Moving FDB binary file", "currentPath", currentPath, "newPath", newPath)
					err = os.Rename(currentPath, newPath)
					if err != nil {
						return err
					}
				}
			}
			_ = versionBinFile.Close()

			versionLibFile, err := os.Open(path.Join(binFile.Name(), binEntry.Name(), "lib", "libfdb_c.so"))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				currentPath := path.Join(versionLibFile.Name())
				newPath := path.Join(libDir.Name(), fmt.Sprintf("libfdb_c_%s.so", version))
				log.Info("Moving FDB library file", "currentPath", currentPath, "newPath", newPath)
				err = os.Rename(currentPath, newPath)
				if err != nil {
					return err
				}
			}
			_ = versionLibFile.Close()
		}
	}

	return nil
}

// setupLogger will return a MultiWriter if the operator should log to a file and stdout otherwise only the stdout
// io.Writer is returned. If the operator should log to a file the operator will make sure to create the file with
// the expected permissions.
func setupLogger(operatorOpts Options) (io.Writer, error) {
	if operatorOpts.LogFile != "" {
		expectedPermission := fs.FileMode(0644)

		if operatorOpts.LogFilePermission != "" {
			expectedPermissionUnit, err := strconv.ParseUint(operatorOpts.LogFilePermission, 8, 32)
			if err != nil {
				return nil, err
			}

			expectedPermission = fs.FileMode(expectedPermissionUnit)
		}

		// We have to create the original file by ourself since lumberjack doesn't support to pass down the expected permissions
		// see: https://github.com/natefinch/lumberjack/issues/82#issuecomment-482143273.
		stat, err := os.Stat(operatorOpts.LogFile)
		// File doesn't exist and must be created with the expected permission.
		if os.IsNotExist(err) {
			err := os.WriteFile(operatorOpts.LogFile, nil, expectedPermission)
			if err != nil {
				return nil, err
			}
		}

		if err == nil && stat.Mode() != expectedPermission {
			err = os.Chmod(operatorOpts.LogFile, expectedPermission)
			if err != nil {
				return nil, err
			}
		}

		lumberjackLogger := &lumberjack.Logger{
			Filename:   operatorOpts.LogFile,
			MaxSize:    operatorOpts.LogFileMaxSize,
			MaxAge:     operatorOpts.LogFileMaxAge,
			MaxBackups: operatorOpts.MaxNumberOfOldLogFiles,
			Compress:   operatorOpts.CompressOldFiles,
		}

		return io.MultiWriter(os.Stdout, lumberjackLogger), nil
	}

	return os.Stdout, nil
}
