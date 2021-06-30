/*
 * admin_client.go
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
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

var exclusionLinePattern = regexp.MustCompile("(?m)^ +(.*)$")
var protocolVersionRegex = regexp.MustCompile(`(?m)^protocol (\w+)$`)

// cliAdminClient provides an implementation of the admin interface using the
// FDB CLI.
type cliAdminClient struct {
	// Cluster is the reference to the cluster model.
	Cluster *fdbtypes.FoundationDBCluster

	// clusterFilePath is the path to the temp file containing the cluster file
	// for this session.
	clusterFilePath string
}

// NewCliAdminClient generates an Admin client for a cluster
func NewCliAdminClient(cluster *fdbtypes.FoundationDBCluster, _ client.Client) (controllers.AdminClient, error) {
	clusterFile, err := ioutil.TempFile("", "")
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

	return &cliAdminClient{Cluster: cluster, clusterFilePath: clusterFilePath}, nil
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
}

// hasTimeoutArg determines whether a command accepts a timeout argument.
func (command cliCommand) hasTimeoutArg() bool {
	return command.binary == "" || command.binary == "fdbcli"
}

// hasDashInLogDir determines whether a command has a log-dir argument or a
// logdir argument.
func (command cliCommand) hasDashInLogDir() bool {
	return command.binary == "" || command.binary == "fdbcli"
}

// getClusterFileFlag gets the flag this command uses for its cluster file
// argument.
func (command cliCommand) getClusterFileFlag() string {
	if command.binary == "fdbrestore" {
		return "--dest_cluster_file"
	}
	return "-C"
}

// getBinaryPath generates the path to an FDB binary.
func getBinaryPath(binaryName string, version string) string {
	shortVersion := version[:strings.LastIndex(version, ".")]
	return fmt.Sprintf("%s/%s/%s", os.Getenv("FDB_BINARY_DIR"), shortVersion, binaryName)
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
		binaryName = "fdbcli"
	}

	binary := getBinaryPath(binaryName, version)
	hardTimeout := DefaultCLITimeout
	args := make([]string, 0, 9)
	args = append(args, command.args...)
	if len(args) == 0 {
		args = append(args, "--exec", command.command)
	}

	args = append(args, command.getClusterFileFlag(), client.clusterFilePath, "--log")

	if binaryName == "fdbcli" {
		format := os.Getenv("FDB_NETWORK_OPTION_TRACE_FORMAT")
		if format == "" {
			format = "xml"
		}

		args = append(args, "--trace_format", format)
	}
	if command.hasTimeoutArg() {
		args = append(args, "--timeout", strconv.Itoa(DefaultCLITimeout))
		hardTimeout += DefaultCLITimeout
	}
	if command.hasDashInLogDir() {
		args = append(args, "--log-dir", os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE"))
	} else {
		args = append(args, "--logdir", os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE"))
	}
	timeoutContext, cancelFunction := context.WithTimeout(context.Background(), time.Second*time.Duration(hardTimeout))
	defer cancelFunction()
	execCommand := exec.CommandContext(timeoutContext, binary, args...)

	log.Info("Running command", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "path", execCommand.Path, "args", execCommand.Args)

	output, err := execCommand.CombinedOutput()
	if err != nil {
		exitError, canCast := err.(*exec.ExitError)
		if canCast {
			log.Error(exitError, "Error from FDB command", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "code", exitError.ProcessState.ExitCode(), "stdout", string(output), "stderr", string(exitError.Stderr))
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
	log.Info("Command completed", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "output", debugOutput)
	return outputString, nil
}

// GetStatus gets the database's status
func (client *cliAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()
	// This will call directly the database and fetch the status information
	// from the system key space.
	return getStatusFromDB(client.Cluster)
}

// ConfigureDatabase sets the database configuration
func (client *cliAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
	configurationString, err := configuration.GetConfigurationString()
	if err != nil {
		return err
	}

	if newDatabase {
		configurationString = "new " + configurationString
	}

	_, err = client.runCommand(cliCommand{command: fmt.Sprintf("configure %s", configurationString)})
	return err
}

// removeAddressFlags strips the flags from the end of the addresses, leaving
// only the IP and port.
func removeAddressFlags(address string) string {
	components := strings.Split(address, ":")
	if len(components) < 2 {
		return address
	}

	return fmt.Sprintf("%s:%s", components[0], components[1])
}

// removeAddressFlagsFromAll strips the flags from the end of the addresses,
// leaving only the IP and port.
func removeAddressFlagsFromAll(addresses []string) []string {
	results := make([]string, 0, len(addresses))
	for _, address := range addresses {
		results = append(results, removeAddressFlags(address))
	}
	return results
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *cliAdminClient) ExcludeInstances(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}

	version, err := fdbtypes.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return err
	}

	if version.HasNonBlockingExcludes() {
		_, err = client.runCommand(cliCommand{
			command: fmt.Sprintf(
				"exclude no_wait %s",
				strings.Join(addresses, " "),
			)})
	} else {
		_, err = client.runCommand(cliCommand{
			command: fmt.Sprintf(
				"exclude %s",
				strings.Join(addresses, " "),
			)})
	}
	return err
}

// IncludeInstances removes processes from the exclusion list and allows
// them to take on roles again.
func (client *cliAdminClient) IncludeInstances(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"include %s",
		strings.Join(addresses, " "),
	)})
	return err
}

// GetExclusions gets a list of the addresses currently excluded from the
// database.
func (client *cliAdminClient) GetExclusions() ([]string, error) {
	output, err := client.runCommand(cliCommand{command: "exclude"})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(output, "\n")
	exclusions := make([]string, 0, len(lines))
	for _, line := range lines {
		exclusionMatch := exclusionLinePattern.FindStringSubmatch(line)
		if exclusionMatch != nil {
			exclusions = append(exclusions, exclusionMatch[1])
		}
	}
	return exclusions, nil
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
//
// The list returned by this method will be the addresses that are *not*
// safe to remove.
func (client *cliAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	version, err := fdbtypes.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return nil, err
	}

	if version.HasNonBlockingExcludes() {
		output, err := client.runCommand(cliCommand{command: fmt.Sprintf(
			"exclude no_wait %s",
			strings.Join(addresses, " "),
		)})
		if err != nil {
			return nil, err
		}
		exclusionResults := parseExclusionOutput(output)
		log.Info("Checking exclusion results", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "addresses", addresses, "results", exclusionResults)
		remaining := make([]string, 0, len(addresses))
		for _, address := range addresses {
			if exclusionResults[address] != "Success" && exclusionResults[address] != "Missing" {
				remaining = append(remaining, address)
			}
		}
		return remaining, nil
	}
	_, err = client.runCommand(cliCommand{command: fmt.Sprintf(
		"exclude %s",
		strings.Join(addresses, " "),
	)})
	return nil, err
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

// KillInstances restarts processes
func (client *cliAdminClient) KillInstances(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"kill; kill %s; status",
		strings.Join(removeAddressFlagsFromAll(addresses), " "),
	)})
	return err
}

// ChangeCoordinators changes the coordinator set
func (client *cliAdminClient) ChangeCoordinators(addresses []string) (string, error) {
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"coordinators %s",
		strings.Join(addresses, " "),
	)})
	if err != nil {
		return "", err
	}

	connectionStringBytes, err := ioutil.ReadFile(client.clusterFilePath)
	if err != nil {
		return "", err
	}

	connectionString, err := fdbtypes.ParseConnectionString(string(connectionStringBytes))
	if err != nil {
		return "", err
	}
	return connectionString.String(), nil
}

// GetConnectionString fetches the latest connection string.
func (client *cliAdminClient) GetConnectionString() (string, error) {
	output, err := client.runCommand(cliCommand{command: "status minimal"})
	if err != nil {
		return "", err
	}

	if !strings.Contains(output, "The database is available") {
		return "", fmt.Errorf("unable to fetch connection string: %s", output)
	}

	connectionStringBytes, err := ioutil.ReadFile(client.clusterFilePath)
	if err != nil {
		return "", err
	}

	connectionString, err := fdbtypes.ParseConnectionString(string(connectionStringBytes))
	if err != nil {
		return "", err
	}
	return connectionString.String(), nil
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *cliAdminClient) VersionSupported(versionString string) (bool, error) {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}

	if !version.IsAtLeast(controllers.MinimumFDBVersion()) {
		return false, nil
	}

	_, err = os.Stat(getBinaryPath("fdbcli", versionString))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
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
func (client *cliAdminClient) StopBackup(url string) error {
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
func (client *cliAdminClient) GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error) {
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

	status := &fdbtypes.FoundationDBLiveBackupStatus{}
	statusString, err = removeWarningsInJSON(statusString)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal([]byte(statusString), &status)
	if err != nil {
		return nil, err
	}

	return status, nil
}

// StartRestore starts a new restore.
func (client *cliAdminClient) StartRestore(url string, keyRanges []fdbtypes.FoundationDBKeyRange) error {
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

func removeWarningsInJSON(jsonString string) (string, error) {
	idx := strings.Index(jsonString, "{")
	if idx == -1 {
		return "", fmt.Errorf("the JSON string doesn't contain a starting '{'")
	}

	return strings.TrimSpace(jsonString[idx:]), nil
}

// GetCoordinatorSet gets the current coordinators from the status
func (client *cliAdminClient) GetCoordinatorSet() (map[string]internal.None, error) {
	status, err := getStatusFromDB(client.Cluster)
	if err != nil {
		return nil, err
	}

	return internal.GetCoordinatorsFromStatus(status), nil
}
