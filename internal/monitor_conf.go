/*
 * monitor_conf.go
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

package internal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	"k8s.io/utils/pointer"
)

// GetStartCommand builds the expected start command for a process group.
func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, podClient podclient.FdbPodClient, processNumber int, processCount int) (string, error) {
	imageType := GetDesiredImageType(cluster)
	config, err := GetMonitorProcessConfiguration(cluster, processClass, processCount, imageType)
	if err != nil {
		return "", err
	}

	substitutions, err := podClient.GetVariableSubstitutions()
	if err != nil {
		return "", err
	}

	if substitutions == nil {
		return "", nil
	}

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return "", err
	}

	if !version.SupportsUsingBinariesFromMainContainer() {
		substitutions["BINARY_DIR"] = fmt.Sprintf("/var/dynamic-conf/bin/%s", cluster.Spec.Version)
	}

	config.BinaryPath = fmt.Sprintf("%s/fdbserver", substitutions["BINARY_DIR"])

	arguments, err := config.GenerateArguments(processNumber, substitutions)

	if err != nil {
		return "", err
	}

	if imageType == FDBImageTypeUnified {
		return strings.Join(arguments, " "), nil
	}

	command := arguments[0]
	arguments = arguments[1:]
	sort.Slice(arguments, func(i, j int) bool {
		return strings.Compare(arguments[i], arguments[j]) < 0
	})
	return command + " " + strings.Join(arguments, " "), nil
}

// extractPlaceholderEnvVars builds a map of every environment variable
// referenced in the monitor conf.
func extractPlaceholderEnvVars(env map[string]string, arguments []monitorapi.Argument) {
	for _, argument := range arguments {
		if argument.ArgumentType == monitorapi.EnvironmentArgumentType {
			env[argument.Source] = fmt.Sprintf("$%s", argument.Source)
		} else if argument.ArgumentType == monitorapi.ConcatenateArgumentType {
			extractPlaceholderEnvVars(env, argument.Values)
		}
	}
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, podClient podclient.FdbPodClient, serversPerPod int) (string, error) {
	if cluster.Status.ConnectionString == "" {
		return "", nil
	}

	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)

	var substitutions map[string]string
	var err error

	if podClient != nil {
		substitutions, err = podClient.GetVariableSubstitutions()
		if err != nil {
			return "", err
		}
	}

	// Don't instantiate any servers if the `EmptyMonitorConf` buggify option is engaged.
	if !cluster.Spec.Buggify.EmptyMonitorConf {
		for i := 1; i <= serversPerPod; i++ {
			confLines = append(confLines, fmt.Sprintf("[fdbserver.%d]", i))
			commands, err := getMonitorConfStartCommandLines(cluster, processClass, substitutions, i, serversPerPod)
			if err != nil {
				return "", err
			}
			confLines = append(confLines, commands...)
		}
	}

	return strings.Join(confLines, "\n"), nil
}

func getMonitorConfStartCommandLines(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, substitutions map[string]string, processNumber int, processCount int) ([]string, error) {
	confLines := make([]string, 0, 20)

	config, err := GetMonitorProcessConfiguration(cluster, processClass, processCount, FDBImageTypeSplit)
	if err != nil {
		return nil, err
	}

	if substitutions == nil {
		substitutions = make(map[string]string)
		extractPlaceholderEnvVars(substitutions, config.Arguments)
	}

	var binaryDir string

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return nil, err
	}

	if version.SupportsUsingBinariesFromMainContainer() {
		substitution, hasSubstitution := substitutions["BINARY_DIR"]
		if hasSubstitution {
			binaryDir = substitution
		} else {
			binaryDir = "$BINARY_DIR"
		}
	} else {
		binaryDir = fmt.Sprintf("/var/dynamic-conf/bin/%s", cluster.Spec.Version)
	}

	confLines = append(confLines, fmt.Sprintf("command = %s/fdbserver", binaryDir))
	for _, argument := range config.Arguments {
		command, err := argument.GenerateArgument(processNumber, substitutions)
		if err != nil {
			return nil, err
		}
		confLines = append(confLines, strings.Replace(strings.TrimPrefix(command, "--"), "=", " = ", 1))
	}

	return confLines, nil
}

// GetMonitorProcessConfiguration builds the monitor conf template for the unifed image.
func GetMonitorProcessConfiguration(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, processCount int, imageType FDBImageType) (monitorapi.ProcessConfiguration, error) {
	configuration := monitorapi.ProcessConfiguration{
		Version: cluster.Spec.Version,
	}

	if cluster.Status.ConnectionString == "" {
		// Return a placeholder configuration with the servers off until we
		// have the initial connection string.
		configuration.RunServers = pointer.Bool(false)
	}

	logGroup := cluster.Spec.LogGroup
	if logGroup == "" {
		logGroup = cluster.Name
	}

	var zoneVariable string
	if strings.HasPrefix(cluster.Spec.FaultDomain.ValueFrom, "$") {
		zoneVariable = cluster.Spec.FaultDomain.ValueFrom[1:]
	} else {
		zoneVariable = "FDB_ZONE_ID"
	}

	sampleAddresses := cluster.GetFullAddressList("FDB_PUBLIC_IP", false, 1)

	configuration.Arguments = append(configuration.Arguments,
		monitorapi.Argument{Value: "--cluster_file=/var/fdb/data/fdb.cluster"},
		monitorapi.Argument{Value: "--seed_cluster_file=/var/dynamic-conf/fdb.cluster"},
		monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: buildIPArgument("public_address", "FDB_PUBLIC_IP", imageType, sampleAddresses)},
		monitorapi.Argument{Value: fmt.Sprintf("--class=%s", processClass)},
		monitorapi.Argument{Value: "--logdir=/var/log/fdb-trace-logs"},
		monitorapi.Argument{Value: fmt.Sprintf("--loggroup=%s", logGroup)},
	)

	if processCount > 1 {
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{
			ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
				{Value: "--datadir=/var/fdb/data/"},
				{ArgumentType: monitorapi.ProcessNumberArgumentType},
			},
		})
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
			{Value: "--locality_process_id="},
			{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
			{Value: "-"},
			{ArgumentType: monitorapi.ProcessNumberArgumentType},
		}})
	} else {
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{Value: "--datadir=/var/fdb/data"})
	}

	configuration.Arguments = append(configuration.Arguments,
		monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
			{Value: "--locality_instance_id="},
			{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_INSTANCE_ID"},
		}},
		monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
			{Value: "--locality_machineid="},
			{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "FDB_MACHINE_ID"},
		}},
		monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
			{Value: "--locality_zoneid="},
			{ArgumentType: monitorapi.EnvironmentArgumentType, Source: zoneVariable},
		}},
	)

	if cluster.NeedsExplicitListenAddress() && cluster.Status.HasListenIPsForAllPods {
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: buildIPArgument("listen_address", "FDB_POD_IP", imageType, sampleAddresses)})
	}

	if cluster.Spec.MainContainer.PeerVerificationRules != "" {
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{Value: fmt.Sprintf("--tls_verify_peers=%s", cluster.Spec.MainContainer.PeerVerificationRules)})
	}

	podSettings := cluster.GetProcessSettings(processClass)

	if podSettings.CustomParameters != nil {
		equalPattern, err := regexp.Compile(`\s*=\s*`)
		if err != nil {
			return configuration, err
		}
		for _, argument := range *podSettings.CustomParameters {
			sanitizedArgument := "--" + equalPattern.ReplaceAllString(argument, "=")
			configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{Value: sanitizedArgument})
		}
	}

	if cluster.Spec.DataCenter != "" {
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{Value: fmt.Sprintf("--locality_dcid=%s", cluster.Spec.DataCenter)})
	}

	if cluster.Spec.DataHall != "" {
		configuration.Arguments = append(configuration.Arguments, monitorapi.Argument{Value: fmt.Sprintf("--locality_data_hall=%s", cluster.Spec.DataHall)})
	}

	return configuration, nil
}

// buildIPArgument builds an argument that takes an IP address from an environment variable
func buildIPArgument(parameter string, environmentVariable string, imageType FDBImageType, sampleAddresses []fdbtypes.ProcessAddress) []monitorapi.Argument {
	var leftIPWrap string
	var rightIPWrap string
	if imageType == FDBImageTypeUnified {
		leftIPWrap = "["
		rightIPWrap = "]"
	} else {
		leftIPWrap = ""
		rightIPWrap = ""
	}
	arguments := []monitorapi.Argument{{Value: fmt.Sprintf("--%s=%s", parameter, leftIPWrap)}}

	for indexOfAddress, address := range sampleAddresses {
		if indexOfAddress != 0 {
			arguments = append(arguments, monitorapi.Argument{Value: fmt.Sprintf(",%s", leftIPWrap)})
		}

		arguments = append(arguments,
			monitorapi.Argument{ArgumentType: monitorapi.EnvironmentArgumentType, Source: environmentVariable},
			monitorapi.Argument{Value: fmt.Sprintf("%s:", rightIPWrap)},
			monitorapi.Argument{ArgumentType: monitorapi.ProcessNumberArgumentType, Offset: address.Port - 2, Multiplier: 2},
		)

		flags := make([]string, 0, len(address.Flags))
		for flag, set := range address.Flags {
			if set {
				flags = append(flags, flag)
			}
		}

		sort.Slice(flags, func(i int, j int) bool {
			return flags[i] < flags[j]
		})

		if len(flags) > 0 {
			arguments = append(arguments, monitorapi.Argument{Value: fmt.Sprintf(":%s", strings.Join(flags, ":"))})
		}
	}
	return arguments
}
