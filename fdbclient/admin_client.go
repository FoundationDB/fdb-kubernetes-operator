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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

var adminClientMutex sync.Mutex

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

// getMaxTimeout returns the maximum timeout, this is either the default of 40 seconds or if the provided default timeout
// is higher it will be the default cli timeout.
func (client *cliAdminClient) getMaxTimeout() time.Duration {
	if DefaultCLITimeout > 40*time.Second {
		return DefaultCLITimeout
	}

	return 40 * time.Second
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

	args = append(args, command.getClusterFileFlag(), client.clusterFilePath, "--log")
	// We only want to pass the knobs to fdbbackup and fdbrestore
	if command.isFdbCli() {
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
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			client.log.Error(exitError, "Error from FDB command", "code", exitError.ProcessState.ExitCode(), "stdout", string(output), "stderr", string(exitError.Stderr))
		}

		// If we hit a timeout report it as a timeout error
		if strings.Contains(string(output), "Specified timeout reached") {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out
			return "", &fdbv1beta2.TimeoutError{Err: err}
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

// runCommandWithBackoff is a wrapper around runCommand which allows retrying commands if they hit a timeout.
func (client *cliAdminClient) runCommandWithBackoff(command string) (string, error) {
	currentTimeout := DefaultCLITimeout

	var rawResult string
	var err error

	// This method will be retrying to get the status if a timeout is seen. The timeout will be doubled everytime we try
	// it with the default timeout of 10s we will try it 3 times with the following timeouts: 10s - 20s - 40s. We have
	// seen that during upgrades of version incompatible version, when not all coordinators are properly restarted that
	// the response time will be increased.
	for currentTimeout <= client.getMaxTimeout() {
		rawResult, err = client.runCommand(cliCommand{command: command, timeout: currentTimeout})
		if err == nil {
			break
		}

		var timoutError *fdbv1beta2.TimeoutError
		if errors.As(err, &timoutError) {
			client.log.Info("timeout issue will retry with higher timeout")
			currentTimeout *= 2
			continue
		}

		// If any error other than a timeout happens return this error and don't retry.
		return "", err
	}

	return rawResult, err
}

// getStatusFromCli uses the fdbcli to connect to the FDB cluster
func (client *cliAdminClient) getStatusFromCli() (*fdbv1beta2.FoundationDBStatus, error) {
	output, err := client.runCommandWithBackoff("status json")
	if err != nil {
		return nil, err
	}

	contents, err := internal.RemoveWarningsInJSON(output)
	if err != nil {
		return nil, err
	}

	status := &fdbv1beta2.FoundationDBStatus{}
	err = json.Unmarshal(contents, status)
	if err != nil {
		return nil, err
	}

	return status, nil
}

// getStatus uses fdbcli to connect to the FDB cluster, if the cluster is upgraded and the initial version returns no processes
// the new version for fdbcli will be tried.
func (client *cliAdminClient) getStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	status, err := client.getStatusFromCli()
	if err != nil {
		return nil, err
	}

	// If the status doesn't contain any processes and we are doing an upgrade, we probably use the wrong fdbcli version
	// and we have to fallback to the on specified in out spec.version.
	if (status == nil || len(status.Cluster.Processes) == 0) && client.Cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		// Create a copy of the cluster and make use of the desired version instead of the last observed running version.
		clusterCopy := client.Cluster.DeepCopy()
		clusterCopy.Status.RunningVersion = clusterCopy.Spec.Version
		client.Cluster = clusterCopy

		return client.getStatusFromCli()
	}

	return status, nil
}

// GetStatus gets the database's status
func (client *cliAdminClient) GetStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	// This will call directly the database and fetch the status information from the system key space.
	status, err := getStatusFromDB(client.fdbLibClient)
	// There is a limitation in the multi version client if the cluster is only partially upgraded e.g. because not
	// all fdbserver processes are restarted, then the multi version client sometimes picks the wrong version
	// to connect to the cluster. This will result in an empty status only reporting the unreachable coordinators.
	// In this case we want to fall back to use fdbcli which is version specific and will (hopefully) work.
	// If we hit a timeout we will also use fdbcli to retry the get status command.
	client.log.V(1).Info("Result from multi version client (bindings)", "error", err, "status", status)
	if client.Cluster.Status.Configured {
		if (err != nil && internal.IsTimeoutError(err)) || (status == nil || len(status.Cluster.Processes) == 0) {
			client.log.Info("retry fetching status with fdbcli instead of using the client library")
			status, err = client.getStatus()
		}
	}

	client.log.V(1).Info("Completed GetStatus() call", "error", err, "status", status)

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
	mode, err := client.fdbLibClient.getValueFromDBUsingKey("\xff/maintenance", DefaultCLITimeout)
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

	_, err = client.runCommand(cliCommand{command: excludeCommand.String(), timeout: client.getMaxTimeout()})

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
	excludedServers := status.Cluster.DatabaseConfiguration.ExcludedServers
	exclusions := make([]fdbv1beta2.ProcessAddress, 0, len(excludedServers))
	for _, excludedServer := range excludedServers {
		if excludedServer.Address != "" {
			pAddr, err := fdbv1beta2.ParseProcessAddress(excludedServer.Address)
			if err != nil {
				return nil, err
			}
			exclusions = append(exclusions, pAddr)
		} else {
			exclusions = append(exclusions, fdbv1beta2.ProcessAddress{StringAddress: excludedServer.Locality})
		}
	}
	return exclusions, nil
}

// exclusionStatus represents the current status of processes that should be excluded.
// This can include processes that are currently in the progress of being excluded (data movement),
// processes that are fully excluded and don't serve any roles and processes that are not marked for
// exclusion.
type exclusionStatus struct {
	// inProgress contains all addresses that are excluded in the cluster and the exclude command can be used to verify if it's safe to remove this address.
	inProgress []fdbv1beta2.ProcessAddress
	// fullyExcluded contains all addresses that are excluded and don't have any roles assigned, this is a sign that the process is "fully" excluded and safe to remove.
	fullyExcluded []fdbv1beta2.ProcessAddress
	// notExcluded contains all addresses that are part of the input list and are not marked as excluded in the cluster, those addresses are not safe to remove.
	notExcluded []fdbv1beta2.ProcessAddress
	// missingInStatus contains all addresses that are part of the input list but are not appearing in the cluster status json.
	missingInStatus []fdbv1beta2.ProcessAddress
}

// getRemainingAndExcludedFromStatus checks which processes of the input address list are excluded in the cluster and which are not.
func getRemainingAndExcludedFromStatus(status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress) exclusionStatus {
	notExcludedAddresses := map[string]fdbv1beta2.None{}
	fullyExcludedAddresses := map[string]fdbv1beta2.None{}
	visitedAddresses := map[string]fdbv1beta2.None{}

	// If there are more than 1 active generations we can not handout any information about excluded processes based on
	// the cluster status information as only the latest log processes will have the log process role. If we don't check
	// for the active generations we have the risk to remove a log process that still has mutations on it that must be
	// popped.
	if status.Cluster.RecoveryState.ActiveGenerations > 1 {
		return exclusionStatus{
			inProgress:      nil,
			fullyExcluded:   nil,
			notExcluded:     addresses,
			missingInStatus: nil,
		}
	}

	for _, addr := range addresses {
		notExcludedAddresses[addr.MachineAddress()] = fdbv1beta2.None{}
	}

	// Check in the status output which processes are already marked for exclusion in the cluster
	for _, process := range status.Cluster.Processes {
		if _, ok := notExcludedAddresses[process.Address.MachineAddress()]; !ok {
			continue
		}

		visitedAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
		if !process.Excluded {
			continue
		}

		if len(process.Roles) == 0 {
			fullyExcludedAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
		}

		delete(notExcludedAddresses, process.Address.MachineAddress())
	}

	exclusions := exclusionStatus{
		inProgress:      make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(notExcludedAddresses)-len(fullyExcludedAddresses)),
		fullyExcluded:   make([]fdbv1beta2.ProcessAddress, 0, len(fullyExcludedAddresses)),
		notExcluded:     make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
		missingInStatus: make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
	}

	for _, addr := range addresses {
		// If we didn't visit that address (absent in the cluster status) we assume it's safe to run the exclude command against it.
		// We have to run the exclude command against those addresses, to make sure they are not serving any roles.
		if _, ok := visitedAddresses[addr.MachineAddress()]; !ok {
			exclusions.missingInStatus = append(exclusions.missingInStatus, addr)
			continue
		}

		// Those addresses are not excluded, so it's not safe to start the exclude command to check if they are fully excluded.
		if _, ok := notExcludedAddresses[addr.MachineAddress()]; ok {
			exclusions.notExcluded = append(exclusions.notExcluded, addr)
			continue
		}

		// Those are the processes that are marked as excluded and are not serving any roles. It's safe to delete Pods
		// that host those processes.
		if _, ok := fullyExcludedAddresses[addr.MachineAddress()]; ok {
			exclusions.fullyExcluded = append(exclusions.fullyExcluded, addr)
			continue
		}

		// Those are the processes that are marked as excluded but still serve at least one role.
		exclusions.inProgress = append(exclusions.inProgress, addr)
	}

	return exclusions
}

// CanSafelyRemove checks whether it is safe to remove processes from the cluster
//
// The list returned by this method will be the addresses that are *not* safe to remove.
func (client *cliAdminClient) CanSafelyRemove(addresses []fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, error) {
	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	exclusions := getRemainingAndExcludedFromStatus(status, addresses)
	client.log.Info("Filtering excluded processes",
		"inProgress", exclusions.inProgress,
		"fullyExcluded", exclusions.fullyExcluded,
		"notExcluded", exclusions.notExcluded,
		"missingInStatus", exclusions.missingInStatus)

	notSafeToDelete := append(exclusions.notExcluded, exclusions.inProgress...)
	// When we have at least one process that is missing in the status, we have to issue the exclude command to make sure, that those
	// missing processes can be removed or not.
	if len(exclusions.missingInStatus) > 0 {
		err = client.ExcludeProcesses(exclusions.missingInStatus)
		// When we hit a timeout error here we know that at least one of the missingInStatus is still not fully excluded for safety
		// we just return the whole slice and don't do any further distinction. We have to return all addresses that are not excluded
		// and are still in progress, but we don't want to return an error to block further actions on the successfully excluded
		// addresses.
		if err != nil {
			if internal.IsTimeoutError(err) {
				return append(exclusions.notExcluded, exclusions.missingInStatus...), nil
			}

			return nil, err
		}
	}

	// All processes that are either not yet marked as excluded or still serving at least one role, cannot be removed safely.
	return notSafeToDelete, nil
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
	_, err := client.runCommandWithBackoff(killCommand)

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
	outputBytes, err := getConnectionStringFromDB(client.fdbLibClient)

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
	version, err := fdbv1beta2.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}

	if !version.IsSupported() {
		return false, nil
	}

	_, err = os.Stat(getBinaryPath(fdbcliStr, versionString))
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
	statusBytes, err := internal.RemoveWarningsInJSON(statusString)
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

	return internal.GetCoordinatorsFromStatus(status), nil
}

// SetKnobs sets the knobs that should be used for the commandline call.
func (client *cliAdminClient) SetKnobs(knobs []string) {
	client.knobs = knobs
}
