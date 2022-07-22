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
	"fmt"
	"os"
	"os/exec"
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
	fdbcliStr = "fdbcli"
)

type timeoutError struct {
	err error
}

func (timeoutErr timeoutError) Error() string {
	return fmt.Sprintf("timeout: %s", timeoutErr.err.Error())
}

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

// cliAdminClient provides an implementation of the admin interface using the
// FDB CLI.
type cliAdminClient struct {
	// Cluster is the reference to the cluster model.
	Cluster *fdbv1beta2.FoundationDBCluster

	// clusterFilePath is the path to the temp file containing the cluster file
	// for this session.
	clusterFilePath string

	// custom parameters that should be set.
	knobs []string

	// Whether the admin client should be able to run operations through the
	// client library rather than the CLI.
	useClientLibrary bool

	// log implementation for logging output
	log logr.Logger
}

// NewCliAdminClient generates an Admin client for a cluster
func NewCliAdminClient(cluster *fdbv1beta2.FoundationDBCluster, _ client.Client, log logr.Logger) (fdbadminclient.AdminClient, error) {
	clusterFile, err := os.CreateTemp("", "")
	if err != nil {
		return nil, err
	}
	clusterFilePath := clusterFile.Name()

	defer clusterFile.Close()
	_, err = clusterFile.WriteString(cluster.Status.ConnectionString)
	if err != nil {
		return nil, err
	}
	err = clusterFile.Close()
	if err != nil {
		return nil, err
	}

	return &cliAdminClient{Cluster: cluster, clusterFilePath: clusterFilePath, useClientLibrary: true, log: log}, nil
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
	return command.binary == "" || command.binary == fdbcliStr
}

// hasDashInLogDir determines whether a command has a log-dir argument or a
// logdir argument.
func (command cliCommand) hasDashInLogDir() bool {
	return command.binary == "" || command.binary == fdbcliStr
}

// getClusterFileFlag gets the flag this command uses for its cluster file
// argument.
func (command cliCommand) getClusterFileFlag() string {
	if command.binary == "fdbrestore" {
		return "--dest_cluster_file"
	}

	return "-C"
}

// getTimeout returns the timeout for the command
func (command cliCommand) getTimeout() int {
	if command.timeout != 0 {
		return int(command.timeout.Seconds())
	}

	return DefaultCLITimeout
}

// getBinaryPath generates the path to an FDB binary.
func getBinaryPath(binaryName string, version string) string {
	parsed, _ := fdbv1beta2.ParseFdbVersion(version)
	return fmt.Sprintf("%s/%s/%s", os.Getenv("FDB_BINARY_DIR"), parsed.GetBinaryVersion(), binaryName)
}

// runCommand executes a command in the CLI.
func (client *cliAdminClient) runCommand(command cliCommand) (string, error) {
	version := command.version
	if version == "" {
		version = client.Cluster.Status.RunningVersion
	}
	if version == "" {
		version = client.Cluster.Spec.Version
	}

	binaryName := command.binary
	if binaryName == "" {
		binaryName = fdbcliStr
	}

	binary := getBinaryPath(binaryName, version)
	hardTimeout := command.getTimeout()
	args := make([]string, 0, 9)
	args = append(args, command.args...)
	if len(args) == 0 {
		args = append(args, "--exec", command.command)
	}

	args = append(args, command.getClusterFileFlag(), client.clusterFilePath, "--log")
	// We only want to pass the knobs to fdbbackup and fdbrestore
	if binaryName != fdbcliStr {
		args = append(args, client.knobs...)
	}

	traceDir := os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE")
	if traceDir != "" {
		args = append(args, "--log")

		if binaryName == fdbcliStr {
			format := os.Getenv("FDB_NETWORK_OPTION_TRACE_FORMAT")
			if format == "" {
				format = "xml"
			}

			args = append(args, "--trace_format", format)
		}
		if command.hasDashInLogDir() {
			args = append(args, "--log-dir", traceDir)
		} else {
			args = append(args, "--logdir", traceDir)
		}
	}

	if command.hasTimeoutArg() {
		args = append(args, "--timeout", strconv.Itoa(command.getTimeout()))
		hardTimeout += command.getTimeout()
	}
	timeoutContext, cancelFunction := context.WithTimeout(context.Background(), time.Second*time.Duration(hardTimeout))
	defer cancelFunction()
	execCommand := exec.CommandContext(timeoutContext, binary, args...)

	client.log.Info("Running command", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "path", execCommand.Path, "args", execCommand.Args)

	output, err := execCommand.CombinedOutput()
	if err != nil {
		exitError, canCast := err.(*exec.ExitError)
		if canCast {
			client.log.Error(exitError, "Error from FDB command", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "code", exitError.ProcessState.ExitCode(), "stdout", string(output), "stderr", string(exitError.Stderr))
		}

		// If we hit a timeout report it as a timeout error
		if strings.Contains(string(output), "Specified timeout reached") {
			return "", timeoutError{err: err}
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
	client.log.Info("Command completed", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "output", debugOutput)

	return outputString, nil
}

// runCommandWithBackoff is a wrapper around runCommand which allows retrying commands if they hit a timeout.
func (client *cliAdminClient) runCommandWithBackoff(command string) (string, error) {
	maxTimeoutInSeconds := 40
	currentTimeoutInSeconds := DefaultCLITimeout

	var rawResult string
	var err error

	// This method will be retrying to get the status if a timeout is seen. The timeout will be doubled everytime we try
	// it with the default timeout of 10 we will try it 3 times with the following timeouts: 10s - 20s - 40s. We have
	// seen that during upgrades of version incompatible version, when not all coordinators are properly restarted that
	// the response time will be increased.
	for currentTimeoutInSeconds <= maxTimeoutInSeconds {
		rawResult, err = client.runCommand(cliCommand{command: command, timeout: time.Duration(currentTimeoutInSeconds) * time.Second})
		if err == nil {
			break
		}

		if _, ok := err.(timeoutError); ok {
			client.log.Info("timeout issue will retry with higher timeout")
			currentTimeoutInSeconds *= 2
			continue
		}

		// If any error other than a timeout happens return this error and don't retry.
		return "", err
	}

	return rawResult, err
}

func (client *cliAdminClient) getStatus(useClientLibrary bool) (*fdbv1beta2.FoundationDBStatus, error) {
	var contents []byte
	var err error

	if useClientLibrary {
		// This will call directly the database and fetch the status information
		// from the system key space.
		contents, err = getStatusFromDB(client.Cluster, client.log)
	} else {
		var rawResult string
		rawResult, err = client.runCommandWithBackoff("status json")
		if err != nil {
			return nil, err
		}

		contents, err = internal.RemoveWarningsInJSON(rawResult)
	}

	if err != nil {
		return nil, err
	}

	status := &fdbv1beta2.FoundationDBStatus{}
	err = json.Unmarshal(contents, status)
	if err != nil {
		return nil, err
	}
	client.log.V(1).Info("Fetched status JSON", "status", status)

	return status, nil
}

// GetStatus gets the database's status
func (client *cliAdminClient) GetStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	status, err := client.getStatus(client.useClientLibrary)
	if err != nil {
		return nil, err
	}

	// There is a limitation in the multi version client if the cluster is only partially upgraded e.g. because not
	// all fdbserver processes are restarted, then the multi version client sometimes picks the wrong version
	// to connect to the cluster. This will result in an empty status only reporting the unreachable coordinators.
	// In this case we want to fall back to use fdbcli which is version specific and will work.
	if len(status.Cluster.Processes) == 0 && client.useClientLibrary && client.Cluster.Status.Configured {
		client.log.Info("retry fetching status with fdbcli instead of using the client library")
		return client.getStatus(false)
	}

	return status, nil
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

// ExcludeProcesses starts evacuating processes so that they can be removed from the database.
func (client *cliAdminClient) ExcludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	version, err := fdbv1beta2.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return err
	}

	if version.HasNonBlockingExcludes(client.Cluster.GetUseNonBlockingExcludes()) {
		_, err = client.runCommand(cliCommand{
			command: fmt.Sprintf(
				"exclude no_wait %s",
				fdbv1beta2.ProcessAddressesString(addresses, " "),
			)})
	} else {
		_, err = client.runCommand(cliCommand{
			command: fmt.Sprintf(
				"exclude %s",
				fdbv1beta2.ProcessAddressesString(addresses, " "),
			)})
	}
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
}

// getRemainingAndExcludedFromStatus checks which processes of the input address list are excluded in the cluster and which are not.
func getRemainingAndExcludedFromStatus(status *fdbv1beta2.FoundationDBStatus, addresses []fdbv1beta2.ProcessAddress) exclusionStatus {
	notExcludedAddresses := map[string]fdbv1beta2.None{}
	fullyExcludedAddresses := map[string]fdbv1beta2.None{}
	visitedAddresses := map[string]fdbv1beta2.None{}
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
		inProgress:    make([]fdbv1beta2.ProcessAddress, 0, len(addresses)-len(notExcludedAddresses)-len(fullyExcludedAddresses)),
		fullyExcluded: make([]fdbv1beta2.ProcessAddress, 0, len(fullyExcludedAddresses)),
		notExcluded:   make([]fdbv1beta2.ProcessAddress, 0, len(notExcludedAddresses)),
	}

	for _, addr := range addresses {
		// Those addresses are not excluded, so it's not safe to start the exclude command to check
		// if they are fully excluded. If we didn't visit that address (absent in the cluster status) we assume
		// it's safe to run the exclude command against it.
		_, visited := visitedAddresses[addr.MachineAddress()]
		if _, ok := notExcludedAddresses[addr.MachineAddress()]; ok && visited {
			exclusions.notExcluded = append(exclusions.notExcluded, addr)
			continue
		}

		if _, ok := fullyExcludedAddresses[addr.MachineAddress()]; ok {
			exclusions.fullyExcluded = append(exclusions.fullyExcluded, addr)
			continue
		}

		exclusions.inProgress = append(exclusions.inProgress, addr)
	}

	return exclusions
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
//
// The list returned by this method will be the addresses that are *not*
// safe to remove.
func (client *cliAdminClient) CanSafelyRemove(addresses []fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, error) {
	version, err := fdbv1beta2.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return nil, err
	}

	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	exclusions := getRemainingAndExcludedFromStatus(status, addresses)
	client.log.Info("Filtering excluded processes",
		"namespace", client.Cluster.Namespace,
		"cluster", client.Cluster.Name,
		"inProgress", exclusions.inProgress,
		"fullyExcluded", exclusions.fullyExcluded,
		"notExcluded", exclusions.notExcluded)

	if version.HasNonBlockingExcludes(client.Cluster.GetUseNonBlockingExcludes()) {
		output, err := client.runCommand(cliCommand{command: fmt.Sprintf(
			"exclude no_wait %s",
			fdbv1beta2.ProcessAddressesString(exclusions.inProgress, " "),
		)})
		if err != nil {
			return nil, err
		}
		exclusionResults := parseExclusionOutput(output)
		client.log.Info("Checking exclusion results", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "addresses", exclusions.inProgress, "results", exclusionResults)
		for _, address := range exclusions.inProgress {
			if exclusionResults[address.String()] != "Success" && exclusionResults[address.String()] != "Missing" {
				exclusions.notExcluded = append(exclusions.notExcluded, address)
			}
		}

		return exclusions.notExcluded, nil
	}

	// We expect that this command always run without a timeout error since all processes should be fully excluded.
	// We run this only as an additional safety check.
	_, err = client.runCommand(cliCommand{command: fmt.Sprintf(
		"exclude %s",
		fdbv1beta2.ProcessAddressesString(exclusions.fullyExcluded, " "),
	)})

	if err != nil {
		return nil, err
	}

	_, err = client.runCommand(cliCommand{command: fmt.Sprintf(
		"exclude %s",
		fdbv1beta2.ProcessAddressesString(exclusions.inProgress, " "),
	)})

	// When we hit a timeout error here we know that at least one of the inProgress is still not fully excluded for safety
	// we just return the whole slice and don't do any further distinction. We have to return all addresses that are not excluded
	// and are still in progress, but we don't want to return an error to block further actions on the successfully excluded
	// addresses.
	if err != nil {
		if internal.IsTimeoutError(err) {
			return append(exclusions.notExcluded, exclusions.inProgress...), nil
		}

		return nil, err
	}

	return exclusions.notExcluded, nil
}

// parseExclusionOutput extracts the exclusion status for each address from
// the output of an exclusion command.
func parseExclusionOutput(output string) map[string]string {
	results := make(map[string]string)
	var regex = regexp.MustCompile(`\s*(\[?[\w.:\]?]+)(\([\w ]*\))?\s*-+(.*)`)
	matches := regex.FindAllStringSubmatch(output, -1)
	for _, match := range matches {
		address := match[1]
		status := match[len(match)-1]
		if strings.Contains(status, "Successfully excluded") {
			results[address] = "Success"
		} else if strings.Contains(status, "WARNING: Missing from cluster") {
			results[address] = "Missing"
		} else if strings.Contains(status, "Exclusion in progress") {
			results[address] = "In Progress"
		} else {
			results[address] = "Unknown"
		}
	}
	return results
}

// KillProcesses restarts processes
func (client *cliAdminClient) KillProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"kill; kill %[1]s; sleep 1; kill %[1]s; sleep 1; kill %[1]s",
		fdbv1beta2.ProcessAddressesStringWithoutFlags(addresses, " "),
	)})
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

// GetConnectionString fetches the latest connection string.
func (client *cliAdminClient) GetConnectionString() (string, error) {
	var connectionStringBytes []byte
	var err error

	if client.Cluster.UseManagementAPI() {
		// This will call directly the database and fetch the connection string
		// from the system key space.
		connectionStringBytes, err = getConnectionStringFromDB(client.Cluster)
	} else {
		var output string
		output, err = client.runCommandWithBackoff("status minimal")
		if err != nil {
			return "", err
		}

		if !strings.Contains(output, "The database is available") {
			return "", fmt.Errorf("unable to fetch connection string: %s", output)
		}

		connectionStringBytes, err = os.ReadFile(client.clusterFilePath)
	}
	if err != nil {
		return "", err
	}
	var connectionString fdbv1beta2.ConnectionString
	connectionString, err = fdbv1beta2.ParseConnectionString(string(connectionStringBytes))
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

	_, err = os.Stat(getBinaryPath("fdbcli", versionString))
	if err != nil {
		if os.IsNotExist(err) {
			return false, err
		}
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

// StartBackup starts a new backup.
func (client *cliAdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
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
		binary: "fdbbackup",
		args: []string{
			"discontinue",
		},
	})
	return err
}

// PauseBackups pauses the backups.
func (client *cliAdminClient) PauseBackups() error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
		args: []string{
			"pause",
		},
	})
	return err
}

// ResumeBackups resumes the backups.
func (client *cliAdminClient) ResumeBackups() error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
		args: []string{
			"resume",
		},
	})
	return err
}

// ModifyBackup updates the backup parameters.
func (client *cliAdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
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
		binary: "fdbbackup",
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
		binary: "fdbrestore",
		args:   args,
	})
	return err
}

// GetRestoreStatus gets the status of the current restore.
func (client *cliAdminClient) GetRestoreStatus() (string, error) {
	return client.runCommand(cliCommand{
		binary: "fdbrestore",
		args: []string{
			"status",
		},
	})
}

// Close cleans up any pending resources.
func (client *cliAdminClient) Close() error {
	err := os.Remove(client.clusterFilePath)
	if err != nil {
		return err
	}
	return nil
}

// GetCoordinatorSet gets the current coordinators from the status
func (client *cliAdminClient) GetCoordinatorSet() (map[string]struct{}, error) {
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
