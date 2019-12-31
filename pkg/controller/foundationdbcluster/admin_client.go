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

package foundationdbcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var maxCommandOutput int = 20

var protocolVersionRegex = regexp.MustCompile("(?m)^protocol (\\w+)$")

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

	// CanSafelyRemove checks whether it is safe to remove processes from the
	// cluster
	CanSafelyRemove(addresses []string) ([]string, error)

	// KillProcesses restarts processes
	KillInstances(addresses []string) error

	// ChangeCoordinators changes the coordinator set
	ChangeCoordinators(addresses []string) (string, error)

	// VersionSupported reports whether we can support a cluster with a given
	// version.
	VersionSupported(version string) (bool, error)

	// GetProtocolVersion determines the protocol version that is used by a
	// version of FDB.
	GetProtocolVersion(version string) (string, error)

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
	clusterFilePath := clusterFile.Name()
	if err != nil {
		return nil, err
	}

	defer clusterFile.Close()
	if err != nil {
		return nil, err
	}
	_, err = clusterFile.WriteString(cluster.Spec.ConnectionString)
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
	// command is the command to execute.
	command string

	// version is the version of FoundationDB we should run.
	version string

	// args provides alternative arguments in place of the exec command.
	args []string

	// timeout is the timeout for the CLI.
	timeout int
}

// getBinaryPath generates the path to the fdbcli binary.
func getBinaryPath(version string) string {
	shortVersion := version[:strings.LastIndex(version, ".")]
	return fmt.Sprintf("%s/%s/fdbcli", os.Getenv("FDB_BINARY_DIR"), shortVersion)
}

// runCommand executes a command in the CLI.
func (client *CliAdminClient) runCommand(command cliCommand) (string, error) {
	version := command.version
	if version == "" {
		version = client.Cluster.Spec.RunningVersion
	}
	binary := getBinaryPath(version)
	timeout := command.timeout
	if timeout == 0 {
		timeout = 10
	}
	args := make([]string, 0, 9)
	args = append(args, command.args...)
	if len(args) == 0 {
		args = append(args, "--exec", command.command)
	}
	args = append(args, "-C", client.clusterFilePath,
		"--timeout", fmt.Sprintf("%d", timeout),
		"--log", "--log-dir", os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE"))
	execCommand := exec.Command(binary, args...)

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
	debugOutput := outputString
	if len(debugOutput) > maxCommandOutput {
		debugOutput = debugOutput[0:maxCommandOutput] + "..."
	}
	log.Info("Command completed", "namespace", client.Cluster.Namespace, "cluster", client.Cluster.Name, "output", debugOutput)
	return outputString, nil
}

// GetStatus gets the database's status
func (client *CliAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	statusString, err := client.runCommand(cliCommand{command: "status json"})
	if err != nil {
		return nil, err
	}
	status := &fdbtypes.FoundationDBStatus{}
	err = json.Unmarshal([]byte(statusString), &status)
	if err != nil {
		return nil, err
	}
	return status, nil
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

// removeAddressFlags strips the flags from the end of the addresses, leaving
// only the IP and port.
func removeAddressFlags(addresses []string) []string {
	results := make([]string, len(addresses))
	for _, address := range addresses {
		components := strings.Split(address, ":")
		results = append(results, fmt.Sprintf("%s:%s", components[0], components[1]))
	}
	return results
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *CliAdminClient) ExcludeInstances(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}

	_, err := client.runCommand(cliCommand{
		command: fmt.Sprintf(
			"exclude %s",
			strings.Join(removeAddressFlags(addresses), " "),
		)})
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
		strings.Join(removeAddressFlags(addresses), " "),
	)})
	return err
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
func (client *CliAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"exclude %s",
		strings.Join(removeAddressFlags(addresses), " "),
	)})
	return nil, err
}

// KillInstances restarts processes
func (client *CliAdminClient) KillInstances(addresses []string) error {

	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"kill; kill %s; status",
		strings.Join(removeAddressFlags(addresses), " "),
	)})
	return err
}

// SetConnectionString changes the coordinator set
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

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *CliAdminClient) VersionSupported(version string) (bool, error) {
	_, err := os.Stat(getBinaryPath(version))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		} else {
			return false, err
		}
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

// Close cleans up any pending resources.
func (client *CliAdminClient) Close() error {
	err := os.Remove(client.clusterFilePath)
	if err != nil {
		return err
	}
	return nil
}

// MockAdminClient provides a mock implementation of the cluster admin interface
type MockAdminClient struct {
	Cluster               *fdbtypes.FoundationDBCluster
	KubeClient            client.Client
	DatabaseConfiguration *fdbtypes.DatabaseConfiguration
	ExcludedAddresses     []string
	ReincludedAddresses   []string
	KilledAddresses       []string
	frozenStatus          *fdbtypes.FoundationDBStatus
}

// adminClientCache provides a cache of mock admin clients.
var adminClientCache = make(map[string]*MockAdminClient)

// NewMockAdminClient creates an admin client for a cluster.
func NewMockAdminClient(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (AdminClient, error) {
	return newMockAdminClientUncast(cluster, kubeClient)
}

// newMockAdminClientUncast creates a mock admin client for a cluster.
func newMockAdminClientUncast(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*MockAdminClient, error) {
	client := adminClientCache[cluster.Name]
	if client == nil {
		client = &MockAdminClient{Cluster: cluster, KubeClient: kubeClient}
		adminClientCache[cluster.Name] = client
	} else {
		client.Cluster = cluster
	}
	return client, nil
}

// ClearMockAdminClients clears the cache of mock Admin clients
func ClearMockAdminClients() {
	adminClientCache = map[string]*MockAdminClient{}
}

// GetStatus gets the database's status
func (client *MockAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	if client.frozenStatus != nil {
		return client.frozenStatus, nil
	}
	pods := &corev1.PodList{}
	err := client.KubeClient.List(context.TODO(), nil, pods)
	if err != nil {
		return nil, err
	}
	status := &fdbtypes.FoundationDBStatus{
		Cluster: fdbtypes.FoundationDBStatusClusterInfo{
			Processes: make(map[string]fdbtypes.FoundationDBStatusProcessInfo, len(pods.Items)),
		},
	}

	coordinators := make(map[string]bool)
	coordinatorAddresses := strings.Split(strings.Split(client.Cluster.Spec.ConnectionString, "@")[1], ",")
	for _, address := range coordinatorAddresses {
		coordinators[address] = false
	}

	exclusionMap := make(map[string]bool, len(client.ExcludedAddresses))
	for _, address := range client.ExcludedAddresses {
		exclusionMap[address] = true
	}

	for _, pod := range pods.Items {
		ip := mockPodIP(&pod)
		podClient := &mockFdbPodClient{Cluster: client.Cluster, Pod: &pod}
		fullAddress := client.Cluster.GetFullAddress(ip)

		_, ipExcluded := exclusionMap[ip]
		_, addressExcluded := exclusionMap[fullAddress]
		excluded := ipExcluded || addressExcluded
		_, isCoordinator := coordinators[fullAddress]
		if isCoordinator && !excluded {
			coordinators[fullAddress] = true
		}
		instance := newFdbInstance(pod)
		command, err := GetStartCommand(client.Cluster, instance, podClient)
		if err != nil {
			return nil, err
		}
		status.Cluster.Processes[pod.Name] = fdbtypes.FoundationDBStatusProcessInfo{
			Address:      fullAddress,
			ProcessClass: pod.Labels["fdb-process-class"],
			CommandLine:  command,
			Excluded:     ipExcluded || addressExcluded,
		}
	}

	for address, reachable := range coordinators {
		status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators, fdbtypes.FoundationDBStatusCoordinator{
			Address:   address,
			Reachable: reachable,
		})
	}

	status.Client.DatabaseStatus.Available = true
	status.Client.DatabaseStatus.Healthy = true

	if client.DatabaseConfiguration != nil {
		status.Cluster.DatabaseConfiguration = *client.DatabaseConfiguration
	} else {
		status.Cluster.DatabaseConfiguration = client.Cluster.DesiredDatabaseConfiguration()
	}

	status.Cluster.FullReplication = true

	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *MockAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
	client.DatabaseConfiguration = configuration.DeepCopy()
	return nil
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *MockAdminClient) ExcludeInstances(addresses []string) error {
	count := len(addresses) + len(client.ExcludedAddresses)
	exclusionMap := make(map[string]bool, count)
	newExclusions := make([]string, 0, count)
	for _, address := range addresses {
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
	client.ExcludedAddresses = newExclusions
	return nil
}

// IncludeInstances removes processes from the exclusion list and allows
// them to take on roles again.
func (client *MockAdminClient) IncludeInstances(addresses []string) error {
	newExclusions := make([]string, 0, len(client.ExcludedAddresses))
	for _, excludedAddress := range client.ExcludedAddresses {
		included := false
		for _, address := range addresses {
			if address == excludedAddress {
				included = true
				client.ReincludedAddresses = append(client.ReincludedAddresses, address)
				break
			}
		}
		if !included {
			newExclusions = append(newExclusions, excludedAddress)
		}
	}
	client.ExcludedAddresses = newExclusions
	return nil
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
func (client *MockAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	return nil, nil
}

// KillInstances restarts processes
func (client *MockAdminClient) KillInstances(addresses []string) error {
	client.KilledAddresses = append(client.KilledAddresses, addresses...)
	client.UnfreezeStatus()
	return nil
}

// ChangeCoordinators changes the coordinator set
func (client *MockAdminClient) ChangeCoordinators(addresses []string) (string, error) {
	connectionString, err := fdbtypes.ParseConnectionString(client.Cluster.Spec.ConnectionString)
	if err != nil {
		return "", err
	}
	connectionString.GenerateNewGenerationID()
	connectionString.Coordinators = addresses
	return connectionString.String(), err
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *MockAdminClient) VersionSupported(version string) (bool, error) {
	return true, nil
}

// GetProtocolVersion determines the protocol version that is used by a
// version of FDB.
func (client *MockAdminClient) GetProtocolVersion(version string) (string, error) {
	return version, nil
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
	client.frozenStatus = status
	return nil
}

// UnfreezeStatus causes the admin client to start recalculating the status
// on every call to GetStatus
func (client *MockAdminClient) UnfreezeStatus() {
	client.frozenStatus = nil
}
