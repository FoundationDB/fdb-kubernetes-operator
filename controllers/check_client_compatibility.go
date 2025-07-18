/*
 * check_client_compatibility.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// checkClientCompatibility confirms that all clients are compatible with the
// version of FoundationDB configured on the cluster.
type checkClientCompatibility struct{}

// reconcile runs the reconciler's work.
func (c checkClientCompatibility) reconcile(
	_ context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	if !cluster.Status.Configured && !cluster.IsBeingUpgraded() {
		return nil
	}

	if cluster.Spec.IgnoreUpgradabilityChecks {
		return nil
	}

	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.Status.RunningVersion)
	if err != nil {
		return &requeue{curError: err}
	}

	version, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return &requeue{curError: err}
	}

	if !runningVersion.SupportsVersionChange(version) {
		return &requeue{
			message: fmt.Sprintf(
				"cluster version change from version %s to version %s is not supported",
				runningVersion,
				version,
			),
		}
	}

	if version.IsProtocolCompatible(runningVersion) {
		return nil
	}

	adminClient, err := r.getAdminClient(logger, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	defer func() {
		_ = adminClient.Close()
	}()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	ignoredLogGroups := make(map[fdbv1beta2.LogGroup]fdbv1beta2.None)
	for _, logGroup := range cluster.GetIgnoreLogGroupsForUpgrade() {
		ignoredLogGroups[logGroup] = fdbv1beta2.None{}
	}

	unsupportedClients, err := getUnsupportedClients(status, cluster.Spec.Version, ignoredLogGroups)
	if err != nil {
		return &requeue{curError: err}
	}

	if len(unsupportedClients) > 0 {
		message := fmt.Sprintf(
			"%d clients do not support version %s: %s", len(unsupportedClients),
			cluster.Spec.Version, strings.Join(unsupportedClients, ", "),
		)
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UnsupportedClient", message)
		logger.Info("Deferring reconciliation due to unsupported clients", "message", message)
		return &requeue{message: message, delay: 1 * time.Minute}
	}

	return nil
}

// getUnsupportedClients check if all clients supports at least the target version. If a process supports a newer version than
// the target version, then the assumption is that the client also supports the older version, including the target version.
// Other checks could fail because the FDB cluster only maintains a list of samples for the client information, that list
// has an upper limit on how many samples will be tracked. Each list for a specific version has its own limit, so it's
// possible that clients are in different lists.
func getUnsupportedClients(
	status *fdbv1beta2.FoundationDBStatus,
	targetVersion string,
	ignoredLogGroups map[fdbv1beta2.LogGroup]fdbv1beta2.None,
) ([]string, error) {
	var unsupportedClients []string

	processAddresses := map[string]fdbv1beta2.None{}
	for _, process := range status.Cluster.Processes {
		processAddresses[process.Address.MachineAddress()] = fdbv1beta2.None{}
	}

	version, err := fdbv1beta2.ParseFdbVersion(targetVersion)
	if err != nil {
		return nil, err
	}

	for _, versionInfo := range status.Cluster.Clients.SupportedVersions {
		if versionInfo.ProtocolVersion == "Unknown" {
			continue
		}

		clientVersion, err := fdbv1beta2.ParseFdbVersion(versionInfo.ClientVersion)
		if err != nil {
			return nil, err
		}

		// If the version is not protocol compatible or newer than the targeted version, then the current client
		// doesn't support the targeted version.
		if !clientVersion.IsProtocolCompatible(version) && !clientVersion.IsAtLeast(version) {
			for _, client := range versionInfo.MaxProtocolClients {
				if _, ok := ignoredLogGroups[client.LogGroup]; ok {
					continue
				}

				addr, err := fdbv1beta2.ParseProcessAddress(client.Address)
				// In case we are not able to parse the address, we assume it is an unsupported client.
				if err != nil {
					unsupportedClients = append(unsupportedClients, client.Description())
					continue
				}

				// If the address is from a running process, it's probably something running in one of the FoundationDB
				// Pods, like someone manually ran `fdbcli`.
				if _, ok := processAddresses[addr.MachineAddress()]; ok {
					continue
				}

				unsupportedClients = append(unsupportedClients, client.Description())
			}
		}
	}

	return unsupportedClients, nil
}
