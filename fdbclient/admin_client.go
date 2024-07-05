/*
 * fdbadminclient.go
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

package fdbclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	fdbcliStr     = "fdbcli"
	fdbbackupStr  = "fdbbackup"
	fdbrestoreStr = "fdbrestore"
)

var maxCommandOutput = parseMaxCommandOutput()

func parseMaxCommandOutput() int {
	flag := os.Getenv("MAX_FDB_CLI_OUTPUT_LENGTH")
	if flag == "" {
		return 20
	}
	result, err := strconv.Atoi(flag)
	if err != nil {
		panic(err)
	}
	return result
}

var protocolVersionRegex = regexp.MustCompile(`(?m)^protocol (\w+)$`)

// cliAdminClient provides an implementation of the admin interface using the FDB CLI.
type cliAdminClient struct {
	// Cluster is the reference to the cluster model.
	Cluster *fdbv1beta2.FoundationDBCluster

	// clusterFilePath is the path to the temp file containing the cluster file
	// for this session.
	clusterFilePath string

	// custom parameters that should be set.
	knobs []string

	// log implementation for logging output
	log logr.Logger

	// cmdRunner is an interface to run commands. In the real runner we use the exec package to execute binaries. In
	// the mock runner we can define mocked output for better integration tests,.
	cmdRunner commandRunner

	// fdbLibClient is an interface to interact with a FDB cluster over the FDB client libraries. In the real fdb lib client
	// we will issue the actual requests against FDB. In the mock runner we will return predefined output.
	fdbLibClient fdbLibClient

	// timeout defines the timeout that should be used for interacting with FDB.
	timeout time.Duration
}

// NewCliAdminClient generates an Admin client for a cluster
func NewCliAdminClient(cluster *fdbv1beta2.FoundationDBCluster, _ client.Client, log logr.Logger) (fdbadminclient.AdminClient, error) {
	clusterFile, err := createClusterFile(cluster)
	if err != nil {
		return nil, err
	}

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name)
	return &cliAdminClient{
		Cluster:         cluster,
		clusterFilePath: clusterFile,
		log:             logger,
		cmdRunner:       &realCommandRunner{log: logger},
		fdbLibClient: &realFdbLibClient{
			cluster: cluster,
			logger:  logger,
		},
	}, nil
}

// cliCommand describes a command that we are running against FDB.
type cliCommand struct {
	// binary is the binary to run.
	binary string

	// command is the command to execute.
	command string

	// version is the version of FoundationDB we should run.
	version string

	// args provides alternative arguments in place of the exec command.
	args []string

	// timeout provides a way to overwrite the default cli timeout.
	timeout time.Duration
}

// hasTimeoutArg determines whether a command accepts a timeout argument.
func (command cliCommand) hasTimeoutArg() bool {
	return command.isFdbCli()
}

// getLogDirParameter returns the log dir parameter for a command, depending on the binary the log dir parameter
// has a dash or not.
func (command cliCommand) getLogDirParameter() string {
	if command.isFdbCli() {
		return "--log-dir"
	}

	return "--logdir"
}

// getClusterFileFlag gets the flag this command uses for its cluster file
// argument.
func (command cliCommand) getClusterFileFlag() string {
	if command.binary == fdbrestoreStr {
		return "--dest_cluster_file"
	}

	return "-C"
}

// getTimeout returns the timeout for the command
func (command cliCommand) getTimeout() time.Duration {
	if command.timeout != 0 {
		return command.timeout
	}

	return DefaultCLITimeout
}

// getVersion returns the versions defined in the command or if not present returns the running version of the
// cluster.
func (command cliCommand) getVersion(cluster *fdbv1beta2.FoundationDBCluster) string {
	if command.version != "" {
		return command.version
	}

	return cluster.GetRunningVersion()
}

// getBinary returns the binary of the command, if unset this will default to fdbcli.
func (command cliCommand) getBinary() string {
	if command.binary != "" {
		return command.binary
	}

	return fdbcliStr
}

// isFdbCli returns true if the used binary is fdbcli.
func (command cliCommand) isFdbCli() bool {
	return command.getBinary() == fdbcliStr
}

// getBinaryPath generates the path to an FDB binary.
func getBinaryPath(binaryName string, version string) string {
	parsed, _ := fdbv1beta2.ParseFdbVersion(version)
	return path.Join(os.Getenv("FDB_BINARY_DIR"), parsed.GetBinaryVersion(), binaryName)
}

func (client *cliAdminClient) getArgsAndTimeout(command cliCommand) ([]string, time.Duration) {
	args := make([]string, len(command.args))
	copy(args, command.args)
	if len(args) == 0 {
		args = append(args, "--exec", command.command)
	}

	// If we want to print out the version we don't have to pass the cluster file path
	if len(args) == 0 || args[0] != "--version" {
		args = append(args, command.getClusterFileFlag(), client.clusterFilePath)
	}

	// We only want to pass the knobs to fdbbackup and fdbrestore
	if !command.isFdbCli() {
		args = append(args, client.knobs...)
	}

	traceDir := os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE")
	if traceDir != "" {
		args = append(args, "--log")

		if command.isFdbCli() {
			format := os.Getenv("FDB_NETWORK_OPTION_TRACE_FORMAT")
			if format == "" {
				format = "xml"
			}

			args = append(args, "--trace_format", format)
		}

		args = append(args, command.getLogDirParameter(), traceDir)
	}

	hardTimeout := command.getTimeout()
	if command.hasTimeoutArg() {
		args = append(args, "--timeout", strconv.Itoa(int(command.getTimeout().Seconds())))
		hardTimeout += command.getTimeout()
	}

	return args, hardTimeout
}

// runCommand executes a command in the CLI.
func (client *cliAdminClient) runCommand(command cliCommand) (string, error) {
	args, hardTimeout := client.getArgsAndTimeout(command)
	timeoutContext, cancelFunction := context.WithTimeout(context.Background(), hardTimeout)
	defer cancelFunction()

	output, err := client.cmdRunner.runCommand(timeoutContext, getBinaryPath(command.getBinary(), command.getVersion(client.Cluster)), args...)
	if err != nil {
		// If we hit a timeout report it as a timeout error and don't log it, but let the caller take care of logging.
		if strings.Contains(string(output), "Specified timeout reached") {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out
			return "", fdbv1beta2.TimeoutError{Err: err}
		}

		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			client.log.Error(exitError, "Error from FDB command", "code", exitError.ProcessState.ExitCode(), "stdout", string(output), "stderr", string(exitError.Stderr))
		}

		return "", err
	}

	outputString := string(output)

	var debugOutput string
	if len(outputString) > maxCommandOutput && maxCommandOutput > 0 {
		debugOutput = outputString[0:maxCommandOutput] + "..."
	} else {
		debugOutput = outputString
	}
	client.log.Info("Command completed", "output", debugOutput)

	return outputString, nil
}

// getStatusFromCli uses the fdbcli to connect to the FDB cluster
func (client *cliAdminClient) getStatusFromCli() (*fdbv1beta2.FoundationDBStatus, error) {
	// Always use the max timeout here. Otherwise we will retry multiple times with an increasing timeout. As the
	// timeout is only the upper bound using directly the max timeout reduces the calls to a single call.
	output, err := client.runCommand(cliCommand{command: "status json", timeout: client.getTimeout()})
	if err != nil {
		return nil, err
	}

	contents, err := fdbstatus.RemoveWarningsInJSON(output)
	if err != nil {
		return nil, err
	}

	return parseMachineReadableStatus(client.log, contents)
}

// getStatus uses fdbcli to connect to the FDB cluster, if the cluster is upgraded and the initial version returns no processes
// the new version for fdbcli will be tried.
func (client *cliAdminClient) getStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	status, err := client.getStatusFromCli()

	// If the cluster is under an upgrade and the getStatus call returns an error, we have to retry it with the new version,
	// as it could be that the wrong version was selected.
	if client.Cluster.IsBeingUpgradedWithVersionIncompatibleVersion() && internal.IsTimeoutError(err) {
		client.log.V(1).Info("retry fetching status with version specified in spec.Version", "error", err, "status", status)
		// Create a copy of the cluster and make use of the desired version instead of the last observed running version.
		clusterCopy := client.Cluster.DeepCopy()
		clusterCopy.Status.RunningVersion = clusterCopy.Spec.Version
		client.Cluster = clusterCopy

		return client.getStatusFromCli()
	}

	return status, err
}

// GetStatus gets the database's status
func (client *cliAdminClient) GetStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	startTime := time.Now()
	// This will call directly the database and fetch the status information from the system key space.
	status, err := getStatusFromDB(client.fdbLibClient, client.log, client.getTimeout())
	// There is a limitation in the multi version client if the cluster is only partially upgraded e.g. because not
	// all fdbserver processes are restarted, then the multi version client sometimes picks the wrong version
	// to connect to the cluster. This will result in an empty status only reporting the unreachable coordinators.
	// In this case we want to fall back to use fdbcli which is version specific and will (hopefully) work.
	// If we hit a timeout we will also use fdbcli to retry the get status command.
	client.log.V(1).Info("Result from multi version client (bindings)", "error", err, "status", status)
	if client.Cluster.Status.Configured && internal.IsTimeoutError(err) {
		client.log.Info("retry fetching status with fdbcli instead of using the client library")
		status, err = client.getStatus()
	}

	client.log.V(1).Info("Completed GetStatus() call", "error", err, "status", status, "duration", time.Since(startTime).String())

	return status, err
}

// ConfigureDatabase sets the database configuration
func (client *cliAdminClient) ConfigureDatabase(configuration fdbv1beta2.DatabaseConfiguration, newDatabase bool, version string) error {
	configurationString, err := configuration.GetConfigurationString(version)
	if err != nil {
		return err
	}

	if newDatabase {
		configurationString = "new " + configurationString
	}

	_, err = client.runCommand(cliCommand{command: fmt.Sprintf("configure %s", configurationString)})
	return err
}

// GetMaintenanceZone gets current maintenance zone, if any. Returns empty string if maintenance mode is off
func (client *cliAdminClient) GetMaintenanceZone() (string, error) {
	mode, err := client.fdbLibClient.getValueFromDBUsingKey("\xff/maintenance", client.getTimeout())
	if err != nil {
		return "", err
	}

	return string(mode), nil
}

// SetMaintenanceZone places zone into maintenance mode
func (client *cliAdminClient) SetMaintenanceZone(zone string, timeoutSeconds int) error {
	_, err := client.runCommand(cliCommand{
		command: fmt.Sprintf(
			"maintenance on %s %s",
			zone,
			strconv.Itoa(timeoutSeconds)),
	})
	return err
}

// ResetMaintenanceMode switches of maintenance mode
func (client *cliAdminClient) ResetMaintenanceMode() error {
	_, err := client.runCommand(cliCommand{
		command: "maintenance off",
	})
	return err
}

// ExcludeProcesses starts evacuating processes so that they can be removed from the database.
func (client *cliAdminClient) ExcludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	version, err := fdbv1beta2.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return err
	}

	var excludeCommand strings.Builder
	excludeCommand.WriteString("exclude ")
	if version.HasNonBlockingExcludes(client.Cluster.GetUseNonBlockingExcludes()) {
		excludeCommand.WriteString("no_wait ")
	}

	excludeCommand.WriteString(fdbv1beta2.ProcessAddressesString(addresses, " "))

	_, err = client.runCommand(cliCommand{command: excludeCommand.String(), timeout: client.getTimeout()})

	return err
}

// IncludeProcesses removes processes from the exclusion list and allows them to take on roles again.
func (client *cliAdminClient) IncludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"include %s",
		fdbv1beta2.ProcessAddressesString(addresses, " "),
	)})
	return err
}

// GetExclusions gets a list of the addresses currently excluded from the
// database.
func (client *cliAdminClient) GetExclusions() ([]fdbv1beta2.ProcessAddress, error) {
	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	return fdbstatus.GetExclusions(status)
}

// CanSafelyRemove checks whether it is safe to remove processes from the cluster
//
// The list returned by this method will be the addresses that are *not* safe to remove.
func (client *cliAdminClient) CanSafelyRemove(addresses []fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, error) {
	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	return fdbstatus.CanSafelyRemoveFromStatus(client.log, client, addresses, status)
}

// KillProcesses restarts processes
func (client *cliAdminClient) KillProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	killCommand := fmt.Sprintf(
		"kill; kill %[1]s; sleep 1; kill %[1]s; sleep 5",
		fdbv1beta2.ProcessAddressesStringWithoutFlags(addresses, " "),
	)
	// Run the kill command once with the max timeout to reduce the risk of multiple recoveries happening.
	_, err := client.runCommand(cliCommand{command: killCommand, timeout: client.getTimeout()})

	return err
}

// ChangeCoordinators changes the coordinator set
func (client *cliAdminClient) ChangeCoordinators(addresses []fdbv1beta2.ProcessAddress) (string, error) {
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"coordinators %s",
		fdbv1beta2.ProcessAddressesString(addresses, " "),
	)})
	if err != nil {
		return "", err
	}

	connectionStringBytes, err := os.ReadFile(client.clusterFilePath)
	if err != nil {
		return "", err
	}

	connectionString, err := fdbv1beta2.ParseConnectionString(string(connectionStringBytes))
	if err != nil {
		return "", err
	}
	return connectionString.String(), nil
}

// cleanConnectionStringOutput is a helper method to remove unrelated output from the get command in the connection string
// output.
func cleanConnectionStringOutput(input string) string {
	startIdx := strings.LastIndex(input, "`")
	endIdx := strings.LastIndex(input, "'")
	if startIdx == -1 && endIdx == -1 {
		return input
	}

	return input[startIdx+1 : endIdx]
}

// GetConnectionString fetches the latest connection string.
func (client *cliAdminClient) GetConnectionString() (string, error) {
	// This will call directly the database and fetch the connection string
	// from the system key space.
	outputBytes, err := getConnectionStringFromDB(client.fdbLibClient, client.getTimeout())

	if err != nil {
		return "", err
	}
	var connectionString fdbv1beta2.ConnectionString
	connectionString, err = fdbv1beta2.ParseConnectionString(cleanConnectionStringOutput(string(outputBytes)))
	if err != nil {
		return "", err
	}

	return connectionString.String(), nil
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *cliAdminClient) VersionSupported(versionString string) (bool, error) {
	_, err := os.Stat(getBinaryPath(fdbcliStr, versionString))
	if err != nil {
		return false, err
	}

	return true, nil
}

// GetProtocolVersion determines the protocol version that is used by a
// version of FDB.
func (client *cliAdminClient) GetProtocolVersion(version string) (string, error) {
	output, err := client.runCommand(cliCommand{args: []string{"--version"}, version: version})
	if err != nil {
		return "", err
	}

	protocolVersionMatch := protocolVersionRegex.FindStringSubmatch(output)

	if protocolVersionMatch == nil || len(protocolVersionMatch) < 2 {
		return "", fmt.Errorf("failed to parse protocol version for %s. Version output:\n%s", version, output)
	}

	return protocolVersionMatch[1], nil
}

func (client *cliAdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
	_, err := client.runCommand(cliCommand{
		binary: fdbbackupStr,
		args: []string{
			"start",
			"-d",
			url,
			"-s",
			fmt.Sprintf("%d", snapshotPeriodSeconds),
			"-z",
		},
	})
	return err
}

// StopBackup stops a backup.
func (client *cliAdminClient) StopBackup(_ string) error {
	_, err := client.runCommand(cliCommand{
		binary: fdbbackupStr,
		args: []string{
			"discontinue",
		},
	})
	return err
}

// PauseBackups pauses the backups.
func (client *cliAdminClient) PauseBackups() error {
	_, err := client.runCommand(cliCommand{
		binary: fdbbackupStr,
		args: []string{
			"pause",
		},
	})
	return err
}

// ResumeBackups resumes the backups.
func (client *cliAdminClient) ResumeBackups() error {
	_, err := client.runCommand(cliCommand{
		binary: fdbbackupStr,
		args: []string{
			"resume",
		},
	})
	return err
}

// ModifyBackup updates the backup parameters.
func (client *cliAdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
	_, err := client.runCommand(cliCommand{
		binary: fdbbackupStr,
		args: []string{
			"modify",
			"-s",
			fmt.Sprintf("%d", snapshotPeriodSeconds),
		},
	})
	return err
}

// GetBackupStatus gets the status of the current backup.
func (client *cliAdminClient) GetBackupStatus() (*fdbv1beta2.FoundationDBLiveBackupStatus, error) {
	statusString, err := client.runCommand(cliCommand{
		binary: fdbbackupStr,
		args: []string{
			"status",
			"--json",
		},
	})

	if err != nil {
		return nil, err
	}

	status := &fdbv1beta2.FoundationDBLiveBackupStatus{}
	statusBytes, err := fdbstatus.RemoveWarningsInJSON(statusString)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(statusBytes, &status)
	if err != nil {
		return nil, err
	}

	return status, nil
}

// StartRestore starts a new restore.
func (client *cliAdminClient) StartRestore(url string, keyRanges []fdbv1beta2.FoundationDBKeyRange) error {
	args := []string{
		"start",
		"-r",
		url,
	}

	if keyRanges != nil {
		keyRangeString := ""
		for _, keyRange := range keyRanges {
			if keyRangeString != "" {
				keyRangeString += ";"
			}
			keyRangeString += keyRange.Start + " " + keyRange.End
		}
		args = append(args, "-k", keyRangeString)
	}
	_, err := client.runCommand(cliCommand{
		binary: fdbrestoreStr,
		args:   args,
	})
	return err
}

// GetRestoreStatus gets the status of the current restore.
func (client *cliAdminClient) GetRestoreStatus() (string, error) {
	return client.runCommand(cliCommand{
		binary: fdbrestoreStr,
		args: []string{
			"status",
		},
	})
}

// Close cleans up any pending resources.
func (client *cliAdminClient) Close() error {
	// Allow to reuse the same file.
	return nil
}

// GetCoordinatorSet gets the current coordinators from the status
func (client *cliAdminClient) GetCoordinatorSet() (map[string]fdbv1beta2.None, error) {
	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	return fdbstatus.GetCoordinatorsFromStatus(status), nil
}

// SetKnobs sets the knobs that should be used for the commandline call.
func (client *cliAdminClient) SetKnobs(knobs []string) {
	client.knobs = knobs
}

// WithValues will update the logger used by the current AdminClient to contain the provided key value pairs. The provided
// arguments must be even.
func (client *cliAdminClient) WithValues(keysAndValues ...interface{}) {
	newLogger := client.log.WithValues(keysAndValues...)
	client.log = newLogger

	// Update the FDB library client logger
	realFdbClient, ok := client.fdbLibClient.(*realFdbLibClient)
	if ok {
		realFdbClient.logger = newLogger
	}

	// Update the command runner logger
	cmdRunner, ok := client.cmdRunner.(*realCommandRunner)
	if !ok {
		return
	}
	cmdRunner.log = newLogger
}

// SetTimeout will overwrite the default timeout for interacting the FDB cluster.
func (client *cliAdminClient) SetTimeout(timeout time.Duration) {
	client.timeout = timeout
}

// getTimeout will return the timeout that is specified for the admin client or otherwise the MaxCliTimeout.
func (client *cliAdminClient) getTimeout() time.Duration {
	if client.timeout == 0 {
		return MaxCliTimeout
	}

	return client.timeout
}

// GetProcessesUnderMaintenance will return all process groups that are currently stored to be under maintenance.
// The result is a map with the process group ID as key and the start of the maintenance as value.
func (client *cliAdminClient) GetProcessesUnderMaintenance() (map[fdbv1beta2.ProcessGroupID]int64, error) {
	db, err := getFDBDatabase(client.Cluster)
	if err != nil {
		return nil, err
	}

	maintenancePrefix := client.Cluster.GetMaintenancePrefix() + "/"

	maintenanceProcesses, err := db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetReadSystemKeys()
		if err != nil {
			return nil, err
		}

		keyRange, err := fdb.PrefixRange([]byte(client.Cluster.GetMaintenancePrefix()))
		if err != nil {
			return nil, err
		}

		results := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		upgrades := make(map[fdbv1beta2.ProcessGroupID]int64, len(results))
		for _, result := range results {
			resStr := result.Key.String()
			idx := strings.LastIndex(resStr, "/")
			processGroupID := resStr[idx+1:]
			client.log.Info("found instance under maintenance", "result", resStr, "maintenancePrefix", maintenancePrefix, "processGroupID", processGroupID)

			var timestamp int64
			parseErr := binary.Read(bytes.NewBuffer(result.Value), binary.LittleEndian, &timestamp)
			if parseErr != nil {
				client.log.Error(parseErr, "could not parse timestamp, will remove bad entry", "byteValue", string(result.Value))
				// We are setting the value here to 0 to make sure the operator is removing the bad entry.
				upgrades[fdbv1beta2.ProcessGroupID(processGroupID)] = 0
				continue
			}

			upgrades[fdbv1beta2.ProcessGroupID(processGroupID)] = timestamp
		}

		return upgrades, nil
	})

	if err != nil {
		return nil, err
	}

	processesUnderMaintenance, isMap := maintenanceProcesses.(map[fdbv1beta2.ProcessGroupID]int64)
	if !isMap {
		return nil, fmt.Errorf("invalid return value from transaction in GetProcessesUnderMaintenance: %v", maintenanceProcesses)
	}

	return processesUnderMaintenance, nil
}

// RemoveProcessesUnderMaintenance will remove the provided process groups from the list of processes that
// are planned to be taken down for maintenance.
func (client *cliAdminClient) RemoveProcessesUnderMaintenance(processGroupIDs []fdbv1beta2.ProcessGroupID) error {
	db, err := getFDBDatabase(client.Cluster)
	if err != nil {
		return err
	}

	_, err = db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		for _, processGroupID := range processGroupIDs {
			strKey := fmt.Sprintf("%s/%s", client.Cluster.GetMaintenancePrefix(), processGroupID)
			client.log.V(1).Info("removing process from maintenance list", "processGroupID", processGroupID, "key", strKey)
			tr.Clear(fdb.Key(strKey))
		}

		return nil, nil
	})

	return err
}

// SetProcessesUnderMaintenance will add the provided process groups to the list of processes that will be taken
// down for maintenance. The value will be the provided time stamp.
func (client *cliAdminClient) SetProcessesUnderMaintenance(processGroupIDs []fdbv1beta2.ProcessGroupID, timestamp int64) error {
	db, err := getFDBDatabase(client.Cluster)
	if err != nil {
		return err
	}

	timestampByteBuffer := new(bytes.Buffer)
	err = binary.Write(timestampByteBuffer, binary.LittleEndian, timestamp)
	if err != nil {
		return err
	}

	_, err = db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		for _, processGroupID := range processGroupIDs {
			strKey := fmt.Sprintf("%s/%s", client.Cluster.GetMaintenancePrefix(), processGroupID)
			client.log.V(1).Info("adding process to maintenance list", "processGroupID", processGroupID, "timestamp", timestamp, "key", strKey)
			tr.Set(fdb.Key(strKey), timestampByteBuffer.Bytes())
		}

		return nil, nil
	})

	return err
}

// GetVersionFromReachableCoordinators will return the running version based on the reachable coordinators. This method
// can be used during version incompatible upgrades and based on the responses of the coordinators, this method will
// assume the current running version of the cluster. If the fdbcli calls for none of the provided version return
// a majority of reachable coordinators, the default version from the cluster.Status.RunningVersion will be returned.
func (client *cliAdminClient) GetVersionFromReachableCoordinators() string {
	// First we test to get the status from the fdbcli with the current running version defined in cluster.Status.RunningVersion.
	status, _ := client.getStatusFromCli()
	if quorumOfCoordinatorsAreReachable(status) {
		return client.Cluster.Status.RunningVersion
	}

	// If the majority of coordinators are not reachable with the cluster.Status.RunningVersion, we try the desired version
	// if the cluster is currently performing an version incompatible upgrade.
	if client.Cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		// Create a copy of the cluster and make use of the desired version instead of the last observed running version.
		clusterCopy := client.Cluster.DeepCopy()
		clusterCopy.Status.RunningVersion = clusterCopy.Spec.Version
		client.Cluster = clusterCopy

		if quorumOfCoordinatorsAreReachable(status) {
			return clusterCopy.Status.RunningVersion
		}
	}

	return client.Cluster.Status.RunningVersion
}

// quorumOfCoordinatorsAreReachable return false if the status is nil otherwise it will return the value of QuorumReachable.
// QuorumReachable will be true if the client was able to reach a quorum of the coordinators.
func quorumOfCoordinatorsAreReachable(status *fdbv1beta2.FoundationDBStatus) bool {
	if status == nil {
		return false
	}

	return status.Client.Coordinators.QuorumReachable
}
