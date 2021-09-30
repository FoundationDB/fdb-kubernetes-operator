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
)

// GetStartCommand builds the expected start command for an instance.
func GetStartCommand(cluster *fdbtypes.FoundationDBCluster, processCless fdbtypes.ProcessClass, podClient FdbPodClient, processNumber int, processCount int) (string, error) {
	lines, err := getStartCommandLines(cluster, processCless, podClient, processNumber, processCount)
	if err != nil {
		return "", err
	}

	regex := regexp.MustCompile(`^(\w+)\s*=\s*(.*)`)
	firstComponents := regex.FindStringSubmatch(lines[0])
	command := firstComponents[2]
	sort.Slice(lines, func(i, j int) bool {
		return strings.Compare(lines[i], lines[j]) < 0
	})
	for _, line := range lines {
		components := regex.FindStringSubmatch(line)
		if components[1] == "command" {
			continue
		}
		command += " --" + components[1] + "=" + components[2]
	}

	return command, nil
}

// GetMonitorConf builds the monitor conf template
func GetMonitorConf(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, podClient FdbPodClient, serversPerPod int) (string, error) {
	if cluster.Status.ConnectionString == "" {
		return "", nil
	}

	confLines := make([]string, 0, 20)
	confLines = append(confLines,
		"[general]",
		"kill_on_configuration_change = false",
		"restart_delay = 60",
	)

	// Don't instantiate any servers if the `EmptyMonitorConf` buggify option is engaged.
	if !cluster.Spec.Buggify.EmptyMonitorConf {
		for i := 1; i <= serversPerPod; i++ {
			confLines = append(confLines, fmt.Sprintf("[fdbserver.%d]", i))
			commands, err := getStartCommandLines(cluster, processClass, podClient, i, serversPerPod)
			if err != nil {
				return "", err
			}
			confLines = append(confLines, commands...)
		}
	}

	return strings.Join(confLines, "\n"), nil
}

func getStartCommandLines(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, podClient FdbPodClient, processNumber int, processCount int) ([]string, error) {
	confLines := make([]string, 0, 20)

	var substitutions map[string]string

	if podClient == nil {
		substitutions = map[string]string{}
	} else {
		subs, err := podClient.GetVariableSubstitutions()
		if err != nil {
			return nil, err
		}
		substitutions = subs
	}

	logGroup := cluster.Spec.LogGroup
	if logGroup == "" {
		logGroup = cluster.Name
	}

	var zoneVariable string
	if strings.HasPrefix(cluster.Spec.FaultDomain.ValueFrom, "$") {
		zoneVariable = cluster.Spec.FaultDomain.ValueFrom
	} else {
		zoneVariable = "$FDB_ZONE_ID"
	}

	var binaryDir string

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return nil, err
	}

	if version.SupportsUsingBinariesFromMainContainer() {
		binaryDir = "$BINARY_DIR"
	} else {
		binaryDir = fmt.Sprintf("/var/dynamic-conf/bin/%s", cluster.Spec.Version)
	}

	confLines = append(confLines,
		fmt.Sprintf("command = %s/fdbserver", binaryDir),
		"cluster_file = /var/fdb/data/fdb.cluster",
		"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
		fmt.Sprintf("public_address = %s", fdbtypes.ProcessAddressesString(cluster.GetFullAddressList("$FDB_PUBLIC_IP", false, processNumber), ",")),
		fmt.Sprintf("class = %s", processClass),
		"logdir = /var/log/fdb-trace-logs",
		fmt.Sprintf("loggroup = %s", logGroup))

	if processCount <= 1 {
		confLines = append(confLines, "datadir = /var/fdb/data")
	} else {
		confLines = append(confLines, fmt.Sprintf("datadir = /var/fdb/data/%d", processNumber), fmt.Sprintf("locality_process_id = $FDB_INSTANCE_ID-%d", processNumber))
	}

	confLines = append(confLines,
		"locality_instance_id = $FDB_INSTANCE_ID",
		"locality_machineid = $FDB_MACHINE_ID",
		fmt.Sprintf("locality_zoneid = %s", zoneVariable))

	if cluster.Spec.DataCenter != "" {
		confLines = append(confLines, fmt.Sprintf("locality_dcid = %s", cluster.Spec.DataCenter))
	}

	if cluster.Spec.DataHall != "" {
		confLines = append(confLines, fmt.Sprintf("locality_data_hall = %s", cluster.Spec.DataHall))
	}

	if cluster.Spec.MainContainer.PeerVerificationRules != "" {
		confLines = append(confLines, fmt.Sprintf("tls_verify_peers = %s", cluster.Spec.MainContainer.PeerVerificationRules))
	}

	if cluster.NeedsExplicitListenAddress() && cluster.Status.HasListenIPsForAllPods {
		confLines = append(confLines, fmt.Sprintf("listen_address = %s", fdbtypes.ProcessAddressesString(cluster.GetFullAddressList("$FDB_POD_IP", false, processNumber), ",")))
	}

	podSettings := cluster.GetProcessSettings(processClass)

	if podSettings.CustomParameters != nil {
		confLines = append(confLines, *podSettings.CustomParameters...)
	}

	for index := range confLines {
		for key, value := range substitutions {
			confLines[index] = strings.Replace(confLines[index], "$"+key, value, -1)
		}
	}
	return confLines, nil
}
