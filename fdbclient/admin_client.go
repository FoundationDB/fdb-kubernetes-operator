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
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
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

	// custom parameters that should be set.
	knobs []string

	// log implementation for logging output
	log logr.Logger

	// cmdRunner is an interface to run commands. In the real runner we use the exec package to execute binaries. In
	// the mock runner we can define mocked output for better integration tests.
	cmdRunner commandRunner

	// fdbLibClient is an interface to interact with a FDB cluster over the FDB client libraries. In the real fdb lib client
	// we will issue the actual requests against FDB. In the mock runner we will return predefined output.
	fdbLibClient fdbLibClient

	// timeout defines the timeout that should be used for interacting with FDB.
	timeout time.Duration
}

// NewCliAdminClient generates an Admin client for a cluster
func NewCliAdminClient(cluster *fdbv1beta2.FoundationDBCluster, _ client.Client, logger logr.Logger) (fdbadminclient.AdminClient, error) {
	return &cliAdminClient{
		Cluster:   cluster,
		log:       logger,
		cmdRunner: &realCommandRunner{log: logger},
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

func (client *cliAdminClient) getArgsAndTimeout(command cliCommand, clusterFile string) ([]string, time.Duration) {
	args := make([]string, len(command.args))
	copy(args, command.args)
	if len(args) == 0 {
		args = append(args, "--exec", command.command)
	}

	// If we want to print out the version we don't have to pass the cluster file path
	if len(args) == 0 || args[0] != "--version" {
		args = append(args, command.getClusterFileFlag(), clusterFile)
	}

	// We only want to pass the knobs to fdbbackup and fdbrestore
	if !command.isFdbCli() {
		args = append(args, client.knobs...)
	}

	traceDir := os.Getenv(fdbv1beta2.EnvNameFDBTraceLogDirPath)
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
	clusterFile, err := createClusterFileForCommandLine(client.Cluster)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = clusterFile.Close()
		_ = os.Remove(clusterFile.Name())
	}()

	args, hardTimeout := client.getArgsAndTimeout(command, clusterFile.Name())
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
func (client *cliAdminClient) getStatusFromCli(checkForProcesses bool) (*fdbv1beta2.FoundationDBStatus, error) {
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

	return parseMachineReadableStatus(client.log, contents, checkForProcesses)
}

// getStatus uses fdbcli to connect to the FDB cluster, if the cluster is upgraded and the initial version returns no processes
// the new version for fdbcli will be tried.
func (client *cliAdminClient) getStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	status, err := client.getStatusFromCli(true)

	// If the cluster is under an upgrade and the getStatus call returns an error, we have to retry it with the new version,
	// as it could be that the wrong version was selected.
	if client.Cluster.IsBeingUpgradedWithVersionIncompatibleVersion() && internal.IsTimeoutError(err) {
		client.log.V(1).Info("retry fetching status with version specified in spec.Version", "error", err, "status", status)
		// Create a copy of the cluster and make use of the desired version instead of the last observed running version.
		clusterCopy := client.Cluster.DeepCopy()
		clusterCopy.Status.RunningVersion = clusterCopy.Spec.Version
		client.Cluster = clusterCopy

		return client.getStatusFromCli(true)
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
	if client.Cluster.Status.Configured && internal.IsTimeoutError(err) && client.Cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		client.log.Info("retry fetching status with fdbcli instead of using the client library")
		status, err = client.getStatus()
	}

	client.log.V(1).Info("Completed GetStatus() call", "error", err, "status", status, "duration", time.Since(startTime).String())

	return status, err
}

// ConfigureDatabase sets the database configuration
func (client *cliAdminClient) ConfigureDatabase(configuration fdbv1beta2.DatabaseConfiguration, newDatabase bool) error {
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

// SetMaintenanceZone places zone into maintenance mode
func (client *cliAdminClient) SetMaintenanceZone(zone string, timeoutSeconds int) error {
	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		client.log.Info("setting maintenance zone with management API", "zone", zone, "timeoutSeconds", timeoutSeconds)
		return client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
			// The value is a literal text of a non-negative double which represents the remaining time for the zone to be in maintenance.
			tr.Set(fdb.Key(path.Join("\xff\xff/management/maintenance/", zone)), []byte(fmt.Sprintf("%d.0", timeoutSeconds)))
			return nil
		})
	}

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
	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		client.log.Info("reset maintenance zone with management API")
		return client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
			keyRange, err := fdb.PrefixRange([]byte("\xff\xff/management/maintenance/"))
			if err != nil {
				return err
			}

			tr.ClearRange(keyRange)
			return nil
		})
	}

	_, err := client.runCommand(cliCommand{
		command: "maintenance off",
	})

	return err
}

// ExcludeProcesses starts evacuating processes so that they can be removed from the database.
func (client *cliAdminClient) ExcludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	return client.ExcludeProcessesWithNoWait(addresses, client.Cluster.GetUseNonBlockingExcludes())
}

// getAddressStringsWithoutPorts will return a string with addresses or localities that can be used for exclusion
// or inclusion. If any of the addresses defines a port, it will be reset by this method to ensure the whole pod gets
// excluded or included.
func getAddressStringsWithoutPorts(addresses []fdbv1beta2.ProcessAddress) string {
	// Ensure that the ports are set to 0, as the operator will always exclude whole pods.
	for idx, address := range addresses {
		if address.Port != 0 {
			addresses[idx].Port = 0
		}
	}

	return fdbv1beta2.ProcessAddressesStringWithoutFlags(addresses, " ")
}

// ExcludeProcessesWithNoWait starts evacuating processes so that they can be removed from the database. If noWait is
// set to true, the exclude command will not block until all data is moved away from the processes.
func (client *cliAdminClient) ExcludeProcessesWithNoWait(addresses []fdbv1beta2.ProcessAddress, noWait bool) error {
	if len(addresses) == 0 {
		return nil
	}

	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		localitiesToExclude, addressesToExclude := getAddressesAndLocalities(addresses)
		client.log.Info("exclude processes with management API", "addressesToExclude", addressesToExclude, "localitiesToExclude", localitiesToExclude)
		err := client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
			for _, addr := range addressesToExclude {
				tr.Set(fdb.Key(path.Join("\xff\xff/management/excluded/", addr)), []byte{})
			}

			for _, locality := range localitiesToExclude {
				tr.Set(fdb.Key(path.Join("\xff\xff/management/excluded_locality/", locality)), []byte{})
			}

			return nil
		})

		if err != nil {
			return err
		}

		if noWait {
			return nil
		}

		// Ensure we are stopping the check after the timeout time.
		timeout := time.Now().Add(client.timeout)
		for {
			err = client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
				inProgressKeyRange, err := fdb.PrefixRange([]byte("\xff\xff/management/in_progress_exclusion/"))
				if err != nil {
					return err
				}

				inProgressResults := tr.GetRange(inProgressKeyRange, fdb.RangeOptions{}).GetSliceOrPanic()
				inProgressExclusions := map[string]fdbv1beta2.None{}
				for _, result := range inProgressResults {
					inProgressExclusions[path.Base(result.Key.String())] = fdbv1beta2.None{}
				}

				client.log.V(1).Info("found results for in progress exclusions", "inProgressResults", len(inProgressResults), "inProgressExclusions", inProgressExclusions)
				var inProgress []string
				if len(inProgressExclusions) > 0 {
					for _, addr := range addressesToExclude {
						if _, ok := inProgressExclusions[addr]; ok {
							inProgress = append(inProgress, addr)
						}
					}

					for _, locality := range localitiesToExclude {
						if _, ok := inProgressExclusions[locality]; ok {
							inProgress = append(inProgress, locality)
						}
					}
				}

				if len(inProgress) > 0 {
					return fmt.Errorf("exclusion still in progress: %s", strings.Join(inProgress, ","))
				}

				return nil
			})

			if err == nil {
				return nil
			}

			client.log.V(1).Info("checking exclusion state", "err", err.Error())
			if time.Now().After(timeout) {
				return err
			}

			time.Sleep(1 * time.Second)
		}
	}

	var excludeCommand strings.Builder
	excludeCommand.WriteString("exclude ")
	if noWait {
		excludeCommand.WriteString("no_wait ")
	}

	excludeCommand.WriteString(getAddressStringsWithoutPorts(addresses))

	_, err := client.runCommand(cliCommand{command: excludeCommand.String(), timeout: client.getTimeout()})

	return err
}

func getAddressesAndLocalities(processAddresses []fdbv1beta2.ProcessAddress) ([]string, []string) {
	localities := make([]string, 0, len(processAddresses))
	addresses := make([]string, 0, len(processAddresses))

	for _, address := range processAddresses {
		address.Port = 0
		addr := address.String()
		if strings.HasPrefix(addr, fdbv1beta2.FDBLocalityExclusionPrefix) {
			localities = append(localities, addr)
			continue
		}

		addresses = append(addresses, addr)
	}

	return localities, addresses
}

// IncludeProcesses removes processes from the exclusion list and allows them to take on roles again.
func (client *cliAdminClient) IncludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		localitiesToInclude, addressesToInclude := getAddressesAndLocalities(addresses)
		client.log.V(1).Info("include processes with management API", "addressesToInclude", addressesToInclude, "localitiesToInclude", localitiesToInclude)
		return client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
			for _, addr := range addressesToInclude {
				tr.Clear(fdb.Key(path.Join("\xff\xff/management/excluded/", addr)))
			}

			for _, locality := range localitiesToInclude {
				tr.Clear(fdb.Key(path.Join("\xff\xff/management/excluded_locality/", locality)))
			}

			return nil
		})
	}

	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"include %s",
		getAddressStringsWithoutPorts(addresses),
	)})

	return err
}

// GetExclusions gets a list of the addresses currently excluded from the
// database.
func (client *cliAdminClient) GetExclusions() ([]fdbv1beta2.ProcessAddress, error) {
	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeFdbcli {
		status, err := client.GetStatus()
		if err != nil {
			return nil, err
		}

		return fdbstatus.GetExclusions(status)
	}

	var currentExclusions []fdbv1beta2.ProcessAddress
	client.log.V(1).Info("getting current exclusions with management API")
	err := client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
		exclusions, err := tr.GetRange(fdb.KeyRange{
			Begin: fdb.Key("\xff\xff/management/excluded/"),
			End:   fdb.Key("\xff\xff/management/excluded0"),
		}, fdb.RangeOptions{}).GetSliceWithError()
		if err != nil {
			return err
		}

		for _, exclusion := range exclusions {
			addr := path.Base(exclusion.Key.String())
			client.log.V(1).Info("found excluded addr", "addr", addr)
			parsed, err := fdbv1beta2.ParseProcessAddress(addr)
			if err != nil {
				return err
			}

			currentExclusions = append(currentExclusions, parsed)
		}

		exclusions, err = tr.GetRange(fdb.KeyRange{
			Begin: fdb.Key("\xff\xff/management/excluded_locality/"),
			End:   fdb.Key("\xff\xff/management/excluded_locality0"),
		}, fdb.RangeOptions{}).GetSliceWithError()
		if err != nil {
			return err
		}

		for _, exclusion := range exclusions {
			locality := path.Base(exclusion.Key.String())
			client.log.V(1).Info("found excluded locality", "locality", locality)
			currentExclusions = append(currentExclusions, fdbv1beta2.ProcessAddress{
				StringAddress: locality,
			})
		}

		return nil
	})

	client.log.V(1).Info("done getting current exclusions with management API", "currentExclusions", currentExclusions, "err", err)
	if err != nil {
		return nil, err
	}

	return currentExclusions, nil
}

func getKillCommand(addresses []fdbv1beta2.ProcessAddress, isUpgrade bool) string {
	var killCmd strings.Builder

	addrString := fdbv1beta2.ProcessAddressesStringWithoutFlags(addresses, " ")
	killCmd.WriteString("kill; kill ")
	killCmd.WriteString(addrString)

	if isUpgrade {
		killCmd.WriteString("; sleep 1; kill ")
		killCmd.WriteString(addrString)
	}
	killCmd.WriteString("; sleep 5")

	return killCmd.String()
}

func (client *cliAdminClient) killWithManagementAPI(addresses []fdbv1beta2.ProcessAddress) error {
	db, err := getFDBDatabase(client.Cluster)
	if err != nil {
		return err
	}

	client.log.Info("reboot processes with management API")
	var rebootErrors []error
	for _, addr := range addresses {
		err = db.RebootWorker(addr.StringWithoutFlags(), false, 0)
		if err != nil {
			rebootErrors = append(rebootErrors, err)
		}
	}

	return errors.Join(rebootErrors...)
}

// KillProcesses restarts processes
func (client *cliAdminClient) KillProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		return client.killWithManagementAPI(addresses)
	}

	// Run the kill command once with the max timeout to reduce the risk of multiple recoveries happening.
	_, err := client.runCommand(cliCommand{command: getKillCommand(addresses, false), timeout: client.getTimeout()})

	return err
}

// KillProcessesForUpgrade restarts processes for upgrades, this will issue 2 kill commands to make sure all
// processes are restarted.
func (client *cliAdminClient) KillProcessesForUpgrade(addresses []fdbv1beta2.ProcessAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		return client.killWithManagementAPI(addresses)
	}

	// Run the kill command once with the max timeout to reduce the risk of multiple recoveries happening.
	_, err := client.runCommand(cliCommand{command: getKillCommand(addresses, true), timeout: client.getTimeout()})

	return err
}

// ChangeCoordinators changes the coordinator set
func (client *cliAdminClient) ChangeCoordinators(addresses []fdbv1beta2.ProcessAddress) (string, error) {
	if client.Cluster.GetDatabaseInteractionMode() == fdbv1beta2.DatabaseInteractionModeMgmtAPI {
		coordinatorString := fdbv1beta2.ProcessAddressesString(addresses, ",")
		client.log.Info("change coordinators with management API", "coordinatorString", coordinatorString)
		err := client.executeTransactionForManagementAPI(func(tr fdb.Transaction) error {
			tr.Set(fdb.Key("\xff\xff/configuration/coordinators/processes"), []byte(coordinatorString))
			return nil
		})

		if err != nil {
			return "", err
		}
	} else {
		_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
			"coordinators %s",
			fdbv1beta2.ProcessAddressesString(addresses, " "),
		)})
		if err != nil {
			return "", err
		}
	}

	return getConnectionStringFromDB(client.fdbLibClient, client.getTimeout())
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *cliAdminClient) VersionSupported(versionString string) (bool, error) {
	// TODO(johscheuer): In the future make use of GetClientStatus(). Available from 7.4:
	// https://github.com/apple/foundationdb/blob/release-7.4/bindings/go/src/fdb/database.go#L140-L161
	// Should we backport this feature to the 7.1 bindings?
	_, err := os.Stat(getBinaryPath(fdbcliStr, versionString))
	if err != nil {
		return false, err
	}

	return true, nil
}

// GetProtocolVersion determines the protocol version that is used by a
// version of FDB.
func (client *cliAdminClient) GetProtocolVersion(version string) (string, error) {
	// TODO(johscheuer): In the future make use of GetClientStatus(). Available from 7.4:
	// https://github.com/apple/foundationdb/blob/release-7.4/bindings/go/src/fdb/database.go#L140-L161
	// Should we backport this feature to the 7.1 bindings?
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

	statusBytes, err := fdbstatus.RemoveWarningsInJSON(statusString)
	if err != nil {
		return nil, err
	}

	status := &fdbv1beta2.FoundationDBLiveBackupStatus{}
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
	maintenancePrefix := client.Cluster.GetMaintenancePrefix() + "/"

	var upgrades map[fdbv1beta2.ProcessGroupID]int64
	err := client.executeTransaction(func(tr fdb.Transaction) error {
		keyRange, err := fdb.PrefixRange([]byte(client.Cluster.GetMaintenancePrefix()))
		if err != nil {
			return err
		}

		results := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		upgrades = make(map[fdbv1beta2.ProcessGroupID]int64, len(results))
		for _, result := range results {
			resStr := result.Key.String()
			processGroupID := path.Base(resStr)
			client.log.V(1).Info("found instance under maintenance", "result", resStr, "maintenancePrefix", maintenancePrefix, "processGroupID", processGroupID)

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
		return nil
	})

	if err != nil {
		return nil, err
	}

	return upgrades, nil
}

// RemoveProcessesUnderMaintenance will remove the provided process groups from the list of processes that
// are planned to be taken down for maintenance.
func (client *cliAdminClient) RemoveProcessesUnderMaintenance(processGroupIDs []fdbv1beta2.ProcessGroupID) error {
	return client.executeTransaction(func(tr fdb.Transaction) error {
		for _, processGroupID := range processGroupIDs {
			strKey := path.Join(client.Cluster.GetMaintenancePrefix(), string(processGroupID))
			client.log.V(1).Info("removing process from maintenance list", "processGroupID", processGroupID, "key", strKey)
			tr.Clear(fdb.Key(strKey))
		}

		return nil
	})
}

// SetProcessesUnderMaintenance will add the provided process groups to the list of processes that will be taken
// down for maintenance. The value will be the provided time stamp.
func (client *cliAdminClient) SetProcessesUnderMaintenance(processGroupIDs []fdbv1beta2.ProcessGroupID, timestamp int64) error {
	timestampByteBuffer := new(bytes.Buffer)
	err := binary.Write(timestampByteBuffer, binary.LittleEndian, timestamp)
	if err != nil {
		return err
	}

	return client.executeTransaction(func(tr fdb.Transaction) error {
		for _, processGroupID := range processGroupIDs {
			strKey := fmt.Sprintf("%s/%s", client.Cluster.GetMaintenancePrefix(), processGroupID)
			client.log.V(1).Info("adding process to maintenance list", "processGroupID", processGroupID, "timestamp", timestamp, "key", strKey)
			tr.Set(fdb.Key(strKey), timestampByteBuffer.Bytes())
		}
		return nil
	})
}

// GetVersionFromReachableCoordinators will return the running version based on the reachable coordinators. This method
// can be used during version incompatible upgrades and based on the responses of the coordinators, this method will
// assume the current running version of the cluster. If the fdbcli calls for none of the provided version return
// a majority of reachable coordinators, an empty string will be returned.
func (client *cliAdminClient) GetVersionFromReachableCoordinators() string {
	// First we test to get the status from the fdbcli with the current running version defined in cluster.Status.RunningVersion.
	status, _ := client.getStatusFromCli(false)
	if quorumOfCoordinatorsAreReachable(status) {
		return client.Cluster.GetRunningVersion()
	}

	// If the majority of coordinators are not reachable with the cluster.Status.RunningVersion, we try the desired version
	// if the cluster is currently performing an version incompatible upgrade.
	if client.Cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		// Create a copy of the cluster and make use of the desired version instead of the last observed running version.
		clusterCopy := client.Cluster.DeepCopy()
		clusterCopy.Status.RunningVersion = clusterCopy.Spec.Version
		client.Cluster = clusterCopy
		status, _ = client.getStatusFromCli(false)
		if quorumOfCoordinatorsAreReachable(status) {
			return clusterCopy.GetRunningVersion()
		}
	}

	return ""
}

// quorumOfCoordinatorsAreReachable return false if the status is nil otherwise it will return the value of QuorumReachable.
// QuorumReachable will be true if the client was able to reach a quorum of the coordinators.
func quorumOfCoordinatorsAreReachable(status *fdbv1beta2.FoundationDBStatus) bool {
	if status == nil {
		return false
	}

	return status.Client.Coordinators.QuorumReachable
}

const (
	pendingForRemoval   = "pendingForRemoval"
	pendingForExclusion = "pendingForExclusion"
	pendingForInclusion = "pendingForInclusion"
	pendingForRestart   = "pendingForRestart"
	readyForExclusion   = "readyForExclusion"
	readyForInclusion   = "readyForInclusion"
	readyForRestart     = "readyForRestart"
	processAddresses    = "processAddresses"
)

// UpdatePendingForRemoval updates the set of process groups that are marked for removal, an update can be either the addition or removal of a process group.
func (client *cliAdminClient) UpdatePendingForRemoval(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(pendingForRemoval, updates)
}

// UpdatePendingForExclusion updates the set of process groups that should be excluded, an update can be either the addition or removal of a process group.
func (client *cliAdminClient) UpdatePendingForExclusion(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(pendingForExclusion, updates)
}

// UpdatePendingForInclusion updates the set of process groups that should be included, an update can be either the addition or removal of a process group.
func (client *cliAdminClient) UpdatePendingForInclusion(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(pendingForInclusion, updates)
}

// UpdatePendingForRestart updates the set of process groups that should be restarted, an update can be either the addition or removal of a process group.
func (client *cliAdminClient) UpdatePendingForRestart(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(pendingForRestart, updates)
}

// UpdateReadyForExclusion updates the set of process groups that are ready to be excluded, an update can be either the addition or removal of a process group.
func (client *cliAdminClient) UpdateReadyForExclusion(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(readyForExclusion, updates)
}

// UpdateReadyForInclusion updates the set of process groups that are ready to be included, an update can be either the addition or removal of a process group.
func (client *cliAdminClient) UpdateReadyForInclusion(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(readyForInclusion, updates)
}

// UpdateReadyForRestart updates the set of process groups that are ready to be restarted, an update can be either the addition or removal of a process group
func (client *cliAdminClient) UpdateReadyForRestart(updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	return client.fdbLibClient.updateGlobalCoordinationKeys(readyForRestart, updates)
}

// GetPendingForRemoval gets the process group IDs for all process groups that are marked for removal.
func (client *cliAdminClient) GetPendingForRemoval(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(pendingForRemoval, prefix))
}

// GetPendingForExclusion gets the process group IDs for all process groups that should be excluded.
func (client *cliAdminClient) GetPendingForExclusion(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(pendingForExclusion, prefix))
}

// GetPendingForInclusion gets the process group IDs for all the process groups that should be included.
func (client *cliAdminClient) GetPendingForInclusion(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(pendingForInclusion, prefix))
}

// GetPendingForRestart gets the process group IDs for all the process groups that should be restarted.
func (client *cliAdminClient) GetPendingForRestart(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(pendingForRestart, prefix))
}

// GetReadyForExclusion gets the process group IDs for all the process groups that are ready to be excluded.
func (client *cliAdminClient) GetReadyForExclusion(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(readyForExclusion, prefix))
}

// GetReadyForInclusion gets the process group IDs for all the process groups that are ready to be included.
func (client *cliAdminClient) GetReadyForInclusion(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(readyForInclusion, prefix))
}

// GetReadyForRestart gets the process group IDs fir all the process groups that are ready to be restarted.
func (client *cliAdminClient) GetReadyForRestart(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	return client.fdbLibClient.getGlobalCoordinationKeys(path.Join(readyForRestart, prefix))
}

// ClearReadyForRestart removes all the process group IDs for all the process groups that are ready to be restarted.
func (client *cliAdminClient) ClearReadyForRestart() error {
	return client.fdbLibClient.clearGlobalCoordinationKeys(readyForRestart)
}

func (client *cliAdminClient) UpdateProcessAddresses(updates map[fdbv1beta2.ProcessGroupID][]string) error {
	return client.fdbLibClient.updateProcessAddresses(updates)
}

func (client *cliAdminClient) GetProcessAddresses(prefix string) (map[fdbv1beta2.ProcessGroupID][]string, error) {
	return client.fdbLibClient.getProcessAddresses(prefix)
}

// executeTransactionForManagementAPI will run an operation for the management API. This method handles all the common options.
func (client *cliAdminClient) executeTransactionForManagementAPI(operation func(transaction fdb.Transaction) error) error {
	return client.fdbLibClient.executeTransactionForManagementAPI(operation, client.timeout)
}

// executeTransaction will run a transaction for the target cluster. This method will handle all the common options.
func (client *cliAdminClient) executeTransaction(operation func(transaction fdb.Transaction) error) error {
	return client.fdbLibClient.executeTransaction(operation, client.timeout)
}
