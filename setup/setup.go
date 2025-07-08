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
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/fdbclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"gopkg.in/natefinch/lumberjack.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
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
	ReplaceOnSecurityContextChange     bool
	MetricsAddr                        string
	LeaderElectionID                   string
	LogFile                            string
	LogFilePermission                  string
	LabelSelector                      string
	ClusterLabelKeyForNodeTrigger      string
	WatchNamespace                     string
	PodUpdateMethod                    string
	CliTimeout                         int
	MaxCliTimeout                      int
	MaxConcurrentReconciles            int
	LogFileMaxSize                     int
	LogFileMaxAge                      int
	MaxNumberOfOldLogFiles             int
	MinimumRecoveryTimeForExclusion    float64
	MinimumRecoveryTimeForInclusion    float64
	LogFileMinAge                      time.Duration
	GetTimeout                         time.Duration
	PostTimeout                        time.Duration
	MaintenanceListStaleDuration       time.Duration
	MaintenanceListWaitDuration        time.Duration
	// GlobalSynchronizationWaitDuration is the wait time for the operator when the synchronization mode is set to
	// global. The wait time defines the period where no updates for the according action should happen. Increasing the
	// wait time will increase the chances that all updates are part of the list but will also delay the rollout of
	// the change.
	GlobalSynchronizationWaitDuration time.Duration
	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership. This is measured against time of
	// last observed ack. Default is 15 seconds.
	LeaseDuration time.Duration
	// RenewDeadline is the duration that the acting control plane will retry
	// refreshing leadership before giving up. Default is 10 seconds.
	RenewDeadline time.Duration
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions. Default is 2 seconds.
	RetryPeriod                   time.Duration
	DeprecationOptions            internal.DeprecationOptions
	MinimumRequiredUptimeCCBounce time.Duration
}

// BindFlags will parse the given flagset for the operator option flags
func (o *Options) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(
		&o.MetricsAddr,
		"metrics-addr",
		":8080",
		"The address the metric endpoint binds to.",
	)
	fs.BoolVar(
		&o.EnableLeaderElection,
		"enable-leader-election",
		true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.",
	)
	fs.StringVar(
		&o.LeaderElectionID,
		"leader-election-id",
		"fdb-kubernetes-operator",
		"LeaderElectionID determines the name of the resource that leader election will use for holding the leader lock.",
	)
	fs.BoolVar(
		&o.DeprecationOptions.UseFutureDefaults,
		"use-future-defaults",
		false,
		"Apply defaults from the next major version of the operator. This is only intended for use in development.",
	)
	fs.StringVar(&o.LogFile, "log-file", "", "The path to a file to write logs to.")
	fs.IntVar(&o.CliTimeout, "cli-timeout", 10, "The timeout to use for CLI commands in seconds.")
	fs.IntVar(
		&o.MaxCliTimeout,
		"max-cli-timeout",
		40,
		"The maximum timeout to use for CLI commands in seconds. This timeout is used for CLI requests that are known to be potentially slow like get status or exclude.",
	)
	fs.IntVar(
		&o.MaxConcurrentReconciles,
		"max-concurrent-reconciles",
		1,
		"Defines the maximum number of concurrent reconciles for all controllers.",
	)
	fs.BoolVar(
		&o.CleanUpOldLogFile,
		"cleanup-old-cli-logs",
		true,
		"Defines if the operator should delete old fdbcli log files.",
	)
	fs.DurationVar(
		&o.LogFileMinAge,
		"log-file-min-age",
		5*time.Minute,
		"Defines the minimum age of fdbcli log files before removing when \"--cleanup-old-cli-logs\" is set.",
	)
	fs.IntVar(
		&o.LogFileMaxAge,
		"log-file-max-age",
		28,
		"Defines the maximum age to retain old operator log file in number of days.",
	)
	fs.IntVar(
		&o.LogFileMaxSize,
		"log-file-max-size",
		250,
		"Defines the maximum size in megabytes of the operator log file before it gets rotated.",
	)
	fs.StringVar(
		&o.LogFilePermission,
		"log-file-permission",
		"0644",
		"The file permission for the log file. Only used if log-file is set. Only the octal representation is supported.",
	)
	fs.StringVar(&o.ClusterLabelKeyForNodeTrigger, "cluster-label-key-for-node-trigger", "",
		"The label key to use to trigger a reconciliation if a node resources changes.")
	fs.IntVar(
		&o.MaxNumberOfOldLogFiles,
		"max-old-log-files",
		3,
		"Defines the maximum number of old operator log files to retain.",
	)
	fs.BoolVar(
		&o.CompressOldFiles,
		"compress",
		false,
		"Defines whether the rotated log files should be compressed using gzip or not.",
	)
	fs.BoolVar(&o.PrintVersion, "version", false, "Prints the version of the operator and exits.")
	fs.StringVar(
		&o.LabelSelector,
		"label-selector",
		"",
		"Defines a label-selector that will be used to select resources.",
	)
	fs.StringVar(
		&o.WatchNamespace,
		"watch-namespace",
		os.Getenv("WATCH_NAMESPACE"),
		"Defines which namespace the operator should watch.",
	)
	fs.StringVar(
		&o.PodUpdateMethod,
		"pod-update-method",
		string(podmanager.Update),
		"Defines how the Pod manager should update pods, possible values are \"update\" and \"patch\".",
	)
	fs.DurationVar(
		&o.GetTimeout,
		"get-timeout",
		5*time.Second,
		"http timeout for get requests to the FDB sidecar.",
	)
	fs.DurationVar(
		&o.PostTimeout,
		"post-timeout",
		10*time.Second,
		"http timeout for post requests to the FDB sidecar.",
	)
	fs.DurationVar(
		&o.LeaseDuration,
		"leader-election-lease-duration",
		15*time.Second,
		"the duration that non-leader candidates will wait to force acquire leadership.",
	)
	fs.DurationVar(
		&o.RenewDeadline,
		"leader-election-renew-deadline",
		10*time.Second,
		"the duration that the acting controlplane will retry refreshing leadership before giving up.",
	)
	fs.DurationVar(
		&o.RetryPeriod,
		"leader-election-retry-period",
		2*time.Second,
		"the duration the LeaderElector clients should wait between tries of action.",
	)
	fs.DurationVar(
		&o.MaintenanceListStaleDuration,
		"maintenance-list-stale-duration",
		4*time.Hour,
		"the duration after stale entries will be deleted form the maintenance list. Only has an affect if the operator is allowed to reset the maintenance zone.",
	)
	fs.DurationVar(
		&o.MaintenanceListWaitDuration,
		"maintenance-list-wait-duration",
		5*time.Minute,
		"the duration where a process in the maintenance list in a different zone will be assumed to block the maintenance zone reset. Only has an affect if the operator is allowed to reset the maintenance zone.",
	)
	fs.DurationVar(
		&o.MinimumRequiredUptimeCCBounce,
		"minimum-required-uptime-for-cc-bounce",
		1*time.Hour,
		"the minimum required uptime of the cluster before allowing the operator to restart the CC if there is a failed tester process.",
	)
	fs.DurationVar(
		&o.GlobalSynchronizationWaitDuration,
		"global-synchronization-wait-duration",
		30*time.Second,
		"the wait time for the global synchronization mode in multi-region deployments",
	)
	fs.BoolVar(
		&o.EnableRestartIncompatibleProcesses,
		"enable-restart-incompatible-processes",
		true,
		"This flag enables/disables in the operator to restart incompatible fdbserver processes.",
	)
	fs.BoolVar(
		&o.ServerSideApply,
		"server-side-apply",
		false,
		"This flag enables server side apply.",
	)
	fs.BoolVar(
		&o.EnableRecoveryState,
		"enable-recovery-state",
		true,
		"This flag enables the use of the recovery state for the minimum uptime between bounced if the FDB version supports it.",
	)
	fs.BoolVar(
		&o.CacheDatabaseStatus,
		"cache-database-status",
		true,
		"Defines the default value for caching the database status.",
	)
	fs.BoolVar(
		&o.EnableNodeIndex,
		"enable-node-index",
		false,
		"Deprecated, not used anymore. Defines if the operator should add an index for accessing node objects. This requires a ClusterRoleBinding with node access. If the taint feature should be used, this setting should be set to true.",
	)
	fs.BoolVar(
		&o.ReplaceOnSecurityContextChange,
		"replace-on-security-context-change",
		false,
		"This flag enables the operator"+
			" to automatically replace pods whose effective security context has one of the following fields change: "+
			"FSGroup, FSGroupChangePolicy, RunAsGroup, RunAsUser",
	)
	fs.Float64Var(
		&o.MinimumRecoveryTimeForInclusion,
		"minimum-recovery-time-for-inclusion",
		600.0,
		"Defines the minimum uptime of the cluster before inclusions are allowed. For clusters after 7.1 this will use the recovery state. This should reduce the risk of frequent recoveries because of inclusions.",
	)
	fs.Float64Var(
		&o.MinimumRecoveryTimeForExclusion,
		"minimum-recovery-time-for-exclusion",
		120.0,
		"Defines the minimum uptime of the cluster before exclusions are allowed. For clusters after 7.1 this will use the recovery state. This should reduce the risk of frequent recoveries because of exclusions.",
	)
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
		_, _ = fmt.Fprintf(
			os.Stderr,
			"unable to setup logger: %s, got error: %s\n",
			operatorOpts.LogFile,
			err.Error(),
		)
		os.Exit(1)
	}

	logger := zap.New(
		zap.UseFlagOptions(&logOpts),
		zap.WriteTo(logWriter))
	ctrl.SetLogger(logger)
	log.SetLogger(logger)

	// Might be called by controller-runtime in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/1420
	klog.SetLogger(logger)

	setupLog := logger.WithName("setup")
	fdbclient.DefaultCLITimeout = time.Duration(operatorOpts.CliTimeout) * time.Second
	fdbclient.MaxCliTimeout = time.Duration(operatorOpts.MaxCliTimeout) * time.Second

	// Define the cache options for the client cache used by the operator. If no label selector is defined, the
	// default cache configuration will be used.
	cacheOptions := cache.Options{}
	// Only if a label selector is defined we have to update the cache options.
	if operatorOpts.LabelSelector != "" {
		// Parse the label selector, if the label selector is not parsable panic.
		selector, parseErr := labels.Parse(operatorOpts.LabelSelector)
		if parseErr != nil {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"could not parse label selector: %s, got error: %s",
				operatorOpts.LabelSelector,
				parseErr,
			)
			os.Exit(1)
		}

		// Set the label selector for all resources that the operator manages, this should reduce the resources that
		// are cached by the operator if a label selector is provided.
		cacheOptions.DefaultLabelSelector = selector
	}

	if operatorOpts.WatchNamespace != "" {
		setupLog.Info(
			"Operator starting in single namespace mode",
			"namespace",
			operatorOpts.WatchNamespace,
		)
		cacheOptions.DefaultNamespaces = map[string]cache.Config{
			operatorOpts.WatchNamespace: {},
		}
	} else {
		setupLog.Info("Operator starting in Global mode")
	}

	options := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			// TODO (johscheuer): Fix: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1258
			BindAddress: operatorOpts.MetricsAddr,
		},
		LeaderElection:         operatorOpts.EnableLeaderElection,
		LeaderElectionID:       operatorOpts.LeaderElectionID,
		LeaseDuration:          &operatorOpts.LeaseDuration,
		RenewDeadline:          &operatorOpts.RenewDeadline,
		RetryPeriod:            &operatorOpts.RetryPeriod,
		Cache:                  cacheOptions,
		HealthProbeBindAddress: "[::1]:9443",
		ReadinessEndpointName:  "[::1]:9443",
		LivenessEndpointName:   "[::1]:9443",
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

	labelSelector, err := metav1.ParseToLabelSelector(
		strings.Trim(operatorOpts.LabelSelector, "\""),
	)
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
		clusterReconciler.ReplaceOnSecurityContextChange = operatorOpts.ReplaceOnSecurityContextChange
		clusterReconciler.MinimumRequiredUptimeCCBounce = operatorOpts.MinimumRequiredUptimeCCBounce
		clusterReconciler.MaintenanceListStaleDuration = operatorOpts.MaintenanceListStaleDuration
		clusterReconciler.MaintenanceListWaitDuration = operatorOpts.MaintenanceListWaitDuration
		clusterReconciler.MinimumRecoveryTimeForInclusion = operatorOpts.MinimumRecoveryTimeForInclusion
		clusterReconciler.MinimumRecoveryTimeForExclusion = operatorOpts.MinimumRecoveryTimeForExclusion
		clusterReconciler.ClusterLabelKeyForNodeTrigger = strings.Trim(
			operatorOpts.ClusterLabelKeyForNodeTrigger,
			"\"",
		)
		clusterReconciler.Namespace = operatorOpts.WatchNamespace
		clusterReconciler.GlobalSynchronizationWaitDuration = operatorOpts.GlobalSynchronizationWaitDuration

		// If the provided PodLifecycleManager supports the update method, we can set the desired update method, otherwise the
		// update method will be ignored.
		castedPodManager, ok := clusterReconciler.PodLifecycleManager.(podmanager.PodLifecycleManagerWithPodUpdateMethod)
		if ok {
			setupLog.Info(
				"Updating pod update method",
				"podUpdateMethod",
				operatorOpts.PodUpdateMethod,
			)
			castedPodManager.SetUpdateMethod(
				podmanager.PodUpdateMethod(operatorOpts.PodUpdateMethod),
			)
		}
		if err := clusterReconciler.SetupWithManager(mgr, operatorOpts.MaxConcurrentReconciles, *labelSelector, watchedObjects...); err != nil {
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
		setupLog.V(1).
			Info("setup log file cleaner", "LogFileMinAge", operatorOpts.LogFileMinAge.String())
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

	libDir, err := os.Open(os.Getenv(fdbv1beta2.EnvNameFDBExternalClientDir))
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(
				os.Getenv(fdbv1beta2.EnvNameFDBExternalClientDir),
				os.ModeDir|os.ModePerm,
			)
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
		if !binEntry.IsDir() {
			continue
		}

		version, err := fdbv1beta2.ParseFdbVersion(binEntry.Name())
		if err != nil {
			log.Info("Could not parse version from directory name", "name", binEntry.Name())
			continue
		}

		versionBinFile, err := os.Open(
			path.Join(binFile.Name(), binEntry.Name(), "bin", binEntry.Name()),
		)
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

		versionLibFile, err := os.Open(
			path.Join(binFile.Name(), binEntry.Name(), "lib", "libfdb_c.so"),
		)
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
