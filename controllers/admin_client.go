/*
 * admin_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2019 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var maxCommandOutput = parseMaxCommandOutput()

var protocolVersionRegex = regexp.MustCompile(`(?m)^protocol (\w+)$`)
var exclusionLinePattern = regexp.MustCompile("(?m)^ +(.*)$")

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

// AdminClient describes an interface for running administrative commands on a
// cluster
type AdminClient interface {
	// GetStatus gets the database's status
	GetStatus() (*fdbtypes.FoundationDBStatus, error)

	// ConfigureDatabase sets the database configuration
	ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error

	// ExcludeInstances starts evacuating processes so that they can be removed
	// from the database.
	ExcludeInstances(addresses []string) error

	// IncludeInstances removes processes from the exclusion list and allows
	// them to take on roles again.
	IncludeInstances(addresses []string) error

	// GetExclusions gets a list of the addresses currently excluded from the
	// database.
	GetExclusions() ([]string, error)

	// CanSafelyRemove checks whether it is safe to remove processes from the
	// cluster.
	//
	// The list returned by this method will be the addresses that are *not*
	// safe to remove.
	CanSafelyRemove(addresses []string) ([]string, error)

	// KillProcesses restarts processes
	KillInstances(addresses []string) error

	// ChangeCoordinators changes the coordinator set
	ChangeCoordinators(addresses []string) (string, error)

	// GetConnectionString fetches the latest connection string.
	GetConnectionString() (string, error)

	// VersionSupported reports whether we can support a cluster with a given
	// version.
	VersionSupported(version string) (bool, error)

	// GetProtocolVersion determines the protocol version that is used by a
	// version of FDB.
	GetProtocolVersion(version string) (string, error)

	// StartBackup starts a new backup.
	StartBackup(url string, snapshotPeriodSeconds int) error

	// StopBackup stops a backup.
	StopBackup(url string) error

	// PauseBackups pauses the backups.
	PauseBackups() error

	// ResumeBackups resumes the backups.
	ResumeBackups() error

	// ModifyBackup modifies the configuration of the backup.
	ModifyBackup(int) error

	// GetBackupStatus gets the status of the current backup.
	GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error)

	// StartRestore starts a new restore.
	StartRestore(url string) error

	// GetRestoreStatus gets the status of the current restore.
	GetRestoreStatus() (string, error)

	// Close shuts down any resources for the client once it is no longer
	// needed.
	Close() error
}

// CliAdminClient provides an implementation of the admin interface using the
// FDB CLI.
type CliAdminClient struct {
	// Cluster is the reference to the cluster model.
	Cluster *fdbtypes.FoundationDBCluster

	// clusterFilePath is the path to the temp file containing the cluster file
	// for this session.
	clusterFilePath string
}

// NewCliAdminClient generates an Admin client for a cluster
func NewCliAdminClient(cluster *fdbtypes.FoundationDBCluster, _ client.Client) (AdminClient, error) {
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

	return &CliAdminClient{Cluster: cluster, clusterFilePath: clusterFilePath}, nil
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
func (client *CliAdminClient) runCommand(command cliCommand) (string, error) {
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
	if command.hasTimeoutArg() {
		args = append(args, "--timeout", fmt.Sprintf("%d", DefaultCLITimeout))
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

	output, err := execCommand.Output()
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
func (client *CliAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()
	// This will call directly the database and fetch the status information
	// from the system key.
	return getStatusFromDB(client.Cluster)
}

// ConfigureDatabase sets the database configuration
func (client *CliAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
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

// RemoveAddressFlags strips the flags from the end of the addresses, leaving
// only the IP and port.
func RemoveAddressFlags(address string) string {
	components := strings.Split(address, ":")
	if len(components) < 2 {
		return address
	}

	return fmt.Sprintf("%s:%s", components[0], components[1])
}

// RemoveAddressFlagsFromAll strips the flags from the end of the addresses,
// leaving only the IP and port.
func RemoveAddressFlagsFromAll(addresses []string) []string {
	results := make([]string, 0, len(addresses))
	for _, address := range addresses {
		results = append(results, RemoveAddressFlags(address))
	}
	return results
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *CliAdminClient) ExcludeInstances(addresses []string) error {
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
func (client *CliAdminClient) IncludeInstances(addresses []string) error {
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
func (client *CliAdminClient) GetExclusions() ([]string, error) {
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
func (client *CliAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
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
func (client *CliAdminClient) KillInstances(addresses []string) error {

	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"kill; kill %s; status",
		strings.Join(RemoveAddressFlagsFromAll(addresses), " "),
	)})
	return err
}

// ChangeCoordinators changes the coordinator set
func (client *CliAdminClient) ChangeCoordinators(addresses []string) (string, error) {
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
func (client *CliAdminClient) GetConnectionString() (string, error) {
	output, err := client.runCommand(cliCommand{command: "status minimal"})
	if err != nil {
		return "", err
	}

	if !strings.Contains(output, "The database is available") {
		return "", fmt.Errorf("Unable to fetch connection string: %s", output)
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
func (client *CliAdminClient) VersionSupported(versionString string) (bool, error) {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}

	if !version.IsAtLeast(MinimumFDBVersion()) {
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
func (client *CliAdminClient) GetProtocolVersion(version string) (string, error) {
	output, err := client.runCommand(cliCommand{args: []string{"--version"}, version: version})
	if err != nil {
		return "", err
	}

	protocolVersionMatch := protocolVersionRegex.FindStringSubmatch(output)

	if protocolVersionMatch == nil || len(protocolVersionMatch) < 2 {
		return "", fmt.Errorf("Failed to parse protocol version for %s. Version output:\n%s", version, output)
	}

	return protocolVersionMatch[1], nil
}

// StartBackup starts a new backup.
func (client *CliAdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
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
func (client *CliAdminClient) StopBackup(url string) error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
		args: []string{
			"discontinue",
		},
	})
	return err
}

// PauseBackups pauses the backups.
func (client *CliAdminClient) PauseBackups() error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
		args: []string{
			"pause",
		},
	})
	return err
}

// ResumeBackups resumes the backups.
func (client *CliAdminClient) ResumeBackups() error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbbackup",
		args: []string{
			"resume",
		},
	})
	return err
}

// ModifyBackup updates the backup parameters.
func (client *CliAdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
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
func (client *CliAdminClient) GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error) {
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
func (client *CliAdminClient) StartRestore(url string) error {
	_, err := client.runCommand(cliCommand{
		binary: "fdbrestore",
		args: []string{
			"start",
			"-r",
			url,
		},
	})
	return err
}

// GetRestoreStatus gets the status of the current restore.
func (client *CliAdminClient) GetRestoreStatus() (string, error) {
	return client.runCommand(cliCommand{
		binary: "fdbrestore",
		args: []string{
			"status",
		},
	})
}

// Close cleans up any pending resources.
func (client *CliAdminClient) Close() error {
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

// MockAdminClient provides a mock implementation of the cluster admin interface
type MockAdminClient struct {
	Cluster               *fdbtypes.FoundationDBCluster
	KubeClient            client.Client
	DatabaseConfiguration *fdbtypes.DatabaseConfiguration
	ExcludedAddresses     []string
	ReincludedAddresses   map[string]bool
	KilledAddresses       []string
	frozenStatus          *fdbtypes.FoundationDBStatus
	Backups               map[string]fdbtypes.FoundationDBBackupStatusBackupDetails
	restoreURL            string
	clientVersions        map[string][]string
	missingProcessGroups  map[string]bool
	additionalProcesses   []fdbtypes.ProcessGroupStatus
	localityInfo          map[string]map[string]string
	incorrectCommandLines map[string]bool
}

// adminClientCache provides a cache of mock admin clients.
var adminClientCache = make(map[string]*MockAdminClient)
var adminClientMutex sync.Mutex

// NewMockAdminClient creates an admin client for a cluster.
func NewMockAdminClient(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (AdminClient, error) {
	return newMockAdminClientUncast(cluster, kubeClient)
}

// newMockAdminClientUncast creates a mock admin client for a cluster.
// nolint:unparam
// is required because we always return a nil error
func newMockAdminClientUncast(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*MockAdminClient, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client := adminClientCache[cluster.Name]

	if client == nil {
		client = &MockAdminClient{
			Cluster:              cluster.DeepCopy(),
			KubeClient:           kubeClient,
			ReincludedAddresses:  make(map[string]bool),
			missingProcessGroups: make(map[string]bool),
			localityInfo:         make(map[string]map[string]string),
		}
		adminClientCache[cluster.Name] = client
		client.Backups = make(map[string]fdbtypes.FoundationDBBackupStatusBackupDetails)
	} else {
		client.Cluster = cluster.DeepCopy()
	}
	return client, nil
}

// ClearMockAdminClients clears the cache of mock Admin clients
func ClearMockAdminClients() {
	adminClientCache = map[string]*MockAdminClient{}
}

// GetStatus gets the database's status
func (client *MockAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.frozenStatus != nil {
		return client.frozenStatus, nil
	}
	pods := &corev1.PodList{}
	err := client.KubeClient.List(context.TODO(), pods)
	if err != nil {
		return nil, err
	}
	status := &fdbtypes.FoundationDBStatus{
		Cluster: fdbtypes.FoundationDBStatusClusterInfo{
			Processes: make(map[string]fdbtypes.FoundationDBStatusProcessInfo, len(pods.Items)),
		},
	}

	coordinators := make(map[string]bool)
	var coordinatorAddresses []string
	if strings.Contains(client.Cluster.Status.ConnectionString, "@") {
		coordinatorAddresses = strings.Split(strings.Split(client.Cluster.Status.ConnectionString, "@")[1], ",")
	} else {
		coordinatorAddresses = []string{}
	}

	for _, address := range coordinatorAddresses {
		coordinators[address] = false
	}

	exclusionMap := make(map[string]bool, len(client.ExcludedAddresses))
	for _, address := range client.ExcludedAddresses {
		exclusionMap[address] = true
	}

	for _, pod := range pods.Items {
		ip := MockPodIP(&pod)
		podClient := &mockFdbPodClient{Cluster: client.Cluster, Pod: &pod}

		processCount, err := getStorageServersPerPodForPod(&pod)
		if err != nil {
			return nil, err
		}

		instance := newFdbInstance(pod)
		instanceID := instance.GetInstanceID()

		if client.missingProcessGroups[instanceID] {
			continue
		}

		for processIndex := 1; processIndex <= processCount; processIndex++ {
			fullAddress := client.Cluster.GetFullAddress(ip, processIndex)

			_, ipExcluded := exclusionMap[ip]
			_, addressExcluded := exclusionMap[fullAddress]
			excluded := ipExcluded || addressExcluded
			_, isCoordinator := coordinators[fullAddress]
			if isCoordinator && !excluded {
				coordinators[fullAddress] = true
			}
			command, err := GetStartCommand(client.Cluster, instance, podClient, processIndex, processCount)
			if err != nil {
				return nil, err
			}
			if client.incorrectCommandLines != nil && client.incorrectCommandLines[instance.GetInstanceID()] {
				command += " --locality_incorrect=1"
			}

			locality := map[string]string{
				"instance_id": instance.GetInstanceID(),
				"zoneid":      pod.Name,
				"dcid":        client.Cluster.Spec.DataCenter,
			}

			for key, value := range client.localityInfo[instance.GetInstanceID()] {
				locality[key] = value
			}

			if processCount > 1 {
				locality["process_id"] = fmt.Sprintf("%s-%d", instance.GetInstanceID(), processIndex)
			}

			status.Cluster.Processes[fmt.Sprintf("%s-%d", pod.Name, processIndex)] = fdbtypes.FoundationDBStatusProcessInfo{
				Address:       fullAddress,
				ProcessClass:  GetProcessClassFromMeta(pod.ObjectMeta),
				CommandLine:   command,
				Excluded:      excluded,
				Locality:      locality,
				Version:       client.Cluster.Status.RunningVersion,
				UptimeSeconds: 60000,
			}
		}

		for _, processGroup := range client.additionalProcesses {
			locality := map[string]string{
				"instance_id": processGroup.ProcessGroupID,
				"zoneid":      processGroup.ProcessGroupID,
			}

			for key, value := range client.localityInfo[instance.GetInstanceID()] {
				locality[key] = value
			}

			fullAddress := client.Cluster.GetFullAddress(processGroup.Addresses[0], 1)

			status.Cluster.Processes[processGroup.ProcessGroupID] = fdbtypes.FoundationDBStatusProcessInfo{
				Address:       fullAddress,
				ProcessClass:  processGroup.ProcessClass,
				Locality:      locality,
				Version:       client.Cluster.Status.RunningVersion,
				UptimeSeconds: 60000,
			}

		}
	}

	if client.clientVersions != nil {
		supportedVersions := make([]fdbtypes.FoundationDBStatusSupportedVersion, 0, len(client.clientVersions))
		for version, addresses := range client.clientVersions {
			protocolVersion, err := client.GetProtocolVersion(version)
			if err != nil {
				return nil, err
			}

			protocolClients := make([]fdbtypes.FoundationDBStatusConnectedClient, 0, len(addresses))
			for _, address := range addresses {
				protocolClients = append(protocolClients, fdbtypes.FoundationDBStatusConnectedClient{
					Address: address,
				})
			}

			supportedVersions = append(supportedVersions, fdbtypes.FoundationDBStatusSupportedVersion{
				ClientVersion:      version,
				ProtocolVersion:    protocolVersion,
				MaxProtocolClients: protocolClients,
			})
		}
		status.Cluster.Clients.SupportedVersions = supportedVersions
	}

	for address, reachable := range coordinators {
		status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators, fdbtypes.FoundationDBStatusCoordinator{
			Address:   address,
			Reachable: reachable,
		})
	}

	status.Client.DatabaseStatus.Available = true
	status.Client.DatabaseStatus.Healthy = true

	if client.DatabaseConfiguration == nil {
		status.Cluster.Layers.Error = "configurationMissing"
	} else {
		status.Cluster.DatabaseConfiguration = *client.DatabaseConfiguration
	}

	if status.Cluster.DatabaseConfiguration.LogSpill == 0 {
		status.Cluster.DatabaseConfiguration.VersionFlags.LogSpill = 2
	}

	status.Cluster.FullReplication = true

	if len(client.Backups) > 0 {
		status.Cluster.Layers.Backup.Tags = make(map[string]fdbtypes.FoundationDBStatusBackupTag, len(client.Backups))
		for tag, tagStatus := range client.Backups {
			status.Cluster.Layers.Backup.Tags[tag] = fdbtypes.FoundationDBStatusBackupTag{
				CurrentContainer: tagStatus.URL,
				RunningBackup:    tagStatus.Running,
				Restorable:       true,
			}
			status.Cluster.Layers.Backup.Paused = tagStatus.Paused
		}
	}

	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *MockAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.DatabaseConfiguration = configuration.DeepCopy()
	return nil
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *MockAdminClient) ExcludeInstances(addresses []string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	count := len(addresses) + len(client.ExcludedAddresses)
	exclusionMap := make(map[string]bool, count)
	newExclusions := make([]string, 0, count)
	for _, address := range addresses {
		if !isValidAddress(address) {
			return fmt.Errorf("Invalid exclusion address %s", address)
		}

		if !exclusionMap[address] {
			exclusionMap[address] = true
			newExclusions = append(newExclusions, address)
		}
	}
	for _, address := range client.ExcludedAddresses {
		if !exclusionMap[address] {
			exclusionMap[address] = true
			newExclusions = append(newExclusions, address)
		}
	}
	if len(newExclusions) == 0 {
		newExclusions = nil
	}
	client.ExcludedAddresses = newExclusions
	return nil
}

func isValidAddress(address string) bool {
	return net.ParseIP(address) != nil
}

// IncludeInstances removes instances from the exclusion list and allows
// them to take on roles again.
func (client *MockAdminClient) IncludeInstances(addresses []string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	newExclusions := make([]string, 0, len(client.ExcludedAddresses))
	for _, address := range addresses {
		if !isValidAddress(address) {
			return fmt.Errorf("Invalid exclusion address %s", address)
		}
	}
	for _, excludedAddress := range client.ExcludedAddresses {
		included := false
		for _, address := range addresses {
			if address == excludedAddress {
				included = true
				client.ReincludedAddresses[address] = true
				break
			}
		}
		if !included {
			newExclusions = append(newExclusions, excludedAddress)
		}
	}
	if len(newExclusions) == 0 {
		newExclusions = nil
	}
	client.ExcludedAddresses = newExclusions
	return nil
}

// CanSafelyRemove checks whether it is safe to remove instances from the
// cluster
//
// The list returned by this method will be the addresses that are *not*
// safe to remove.
func (client *MockAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	return nil, nil
}

// GetExclusions gets a list of the addresses currently excluded from the
// database.
func (client *MockAdminClient) GetExclusions() ([]string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return client.ExcludedAddresses, nil
}

// KillInstances restarts processes
func (client *MockAdminClient) KillInstances(addresses []string) error {
	adminClientMutex.Lock()
	client.KilledAddresses = append(client.KilledAddresses, addresses...)
	adminClientMutex.Unlock()

	client.UnfreezeStatus()
	return nil
}

// ChangeCoordinators changes the coordinator set
func (client *MockAdminClient) ChangeCoordinators(addresses []string) (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	connectionString, err := fdbtypes.ParseConnectionString(client.Cluster.Status.ConnectionString)
	if err != nil {
		return "", err
	}
	err = connectionString.GenerateNewGenerationID()
	if err != nil {
		return "", err
	}
	connectionString.Coordinators = addresses
	return connectionString.String(), err
}

// GetConnectionString fetches the latest connection string.
func (client *MockAdminClient) GetConnectionString() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return client.Cluster.Status.ConnectionString, nil
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *MockAdminClient) VersionSupported(versionString string) (bool, error) {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}

	if !version.IsAtLeast(MinimumFDBVersion()) {
		return false, nil
	}

	return true, nil
}

// GetProtocolVersion determines the protocol version that is used by a
// version of FDB.
func (client *MockAdminClient) GetProtocolVersion(version string) (string, error) {
	return version, nil
}

// StartBackup starts a new backup.
func (client *MockAdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.Backups["default"] = fdbtypes.FoundationDBBackupStatusBackupDetails{
		URL:                   url,
		Running:               true,
		SnapshotPeriodSeconds: snapshotPeriodSeconds,
	}
	return nil
}

// PauseBackups pauses backups.
func (client *MockAdminClient) PauseBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		backup.Paused = true
		client.Backups[tag] = backup
	}
	return nil
}

// ResumeBackups resumes backups.
func (client *MockAdminClient) ResumeBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		backup.Paused = false
		client.Backups[tag] = backup
	}
	return nil
}

// ModifyBackup reconfigures the backup.
func (client *MockAdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	backup := client.Backups["default"]
	backup.SnapshotPeriodSeconds = snapshotPeriodSeconds
	client.Backups["default"] = backup
	return nil
}

// StopBackup stops a backup.
func (client *MockAdminClient) StopBackup(url string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		if backup.URL == url {
			backup.Running = false
			client.Backups[tag] = backup
			return nil
		}
	}
	return fmt.Errorf("No backup found for URL %s", url)
}

// GetBackupStatus gets the status of the current backup.
func (client *MockAdminClient) GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	status := &fdbtypes.FoundationDBLiveBackupStatus{}

	tag := "default"
	backup, present := client.Backups[tag]
	if present {
		status.DestinationURL = backup.URL
		status.Status.Running = backup.Running
		status.BackupAgentsPaused = backup.Paused
		status.SnapshotIntervalSeconds = backup.SnapshotPeriodSeconds
	}

	return status, nil
}

// StartRestore starts a new restore.
func (client *MockAdminClient) StartRestore(url string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.restoreURL = url
	return nil
}

// GetRestoreStatus gets the status of the current restore.
func (client *MockAdminClient) GetRestoreStatus() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return fmt.Sprintf("%s\n", client.restoreURL), nil
}

// MockClientVersion returns a mocked client version
func (client *MockAdminClient) MockClientVersion(version string, clients []string) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.clientVersions == nil {
		client.clientVersions = make(map[string][]string)
	}
	client.clientVersions[version] = clients
}

// MockAdditionalProcesses adds additional processes to the cluster status.
func (client *MockAdminClient) MockAdditionalProcesses(processes []fdbtypes.ProcessGroupStatus) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.additionalProcesses = append(client.additionalProcesses, processes...)
}

// MockMissingProcessGroup updates the mock for whether a process group should
// be missing from the cluster status.
func (client *MockAdminClient) MockMissingProcessGroup(instanceID string, missing bool) {
	client.missingProcessGroups[instanceID] = missing
}

// MockLocalityInfo sets mock locality information for a process.
func (client *MockAdminClient) MockLocalityInfo(processGroupID string, locality map[string]string) {
	client.localityInfo[processGroupID] = locality
}

// MockIncorrectCommandLine updates the mock for whether a process group should
// be have an incorrect command-line.
func (client *MockAdminClient) MockIncorrectCommandLine(instanceID string, incorrect bool) {
	if client.incorrectCommandLines == nil {
		client.incorrectCommandLines = make(map[string]bool)
	}
	client.incorrectCommandLines[instanceID] = incorrect
}

// Close shuts down any resources for the client once it is no longer
// needed.
func (client *MockAdminClient) Close() error {
	return nil
}

// FreezeStatus causes the GetStatus method to return its current value until
// UnfreezeStatus is called, or another method is called which would invalidate
// the status.
func (client *MockAdminClient) FreezeStatus() error {
	status, err := client.GetStatus()
	if err != nil {
		return err
	}

	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.frozenStatus = status
	return nil
}

// UnfreezeStatus causes the admin client to start recalculating the status
// on every call to GetStatus
func (client *MockAdminClient) UnfreezeStatus() {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.frozenStatus = nil
}
