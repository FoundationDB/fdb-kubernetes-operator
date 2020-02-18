/*
 * check_client_compatibility.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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
	ctx "context"
	"fmt"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// CheckClientCompatibility confirms that all clients are compatible with the
// version of FoundationDB configured on the cluster.
type CheckClientCompatibility struct{}

func (c CheckClientCompatibility) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if !cluster.Spec.Configured {
		return true, nil
	}

	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}

	runningVersion, err := fdbtypes.ParseFdbVersion(cluster.Spec.RunningVersion)
	if err != nil {
		return false, err
	}

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return false, err
	}

	if !version.IsAtLeast(runningVersion) {
		return false, fmt.Errorf("cluster downgrade operation is not supported")
	}

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}

	protocolVersion, err := adminClient.GetProtocolVersion(cluster.Spec.Version)
	if err != nil {
		return false, err
	}

	var unsupportedClients []string
	if runningVersion.HasMaxProtocolClientsInStatus() {
		unsupportedClients = make([]string, 0)
		for _, versionInfo := range status.Cluster.Clients.SupportedVersions {
			if versionInfo.ProtocolVersion == "Unknown" {
				continue
			}
			match := versionInfo.ProtocolVersion == protocolVersion

			if !match {
				for _, client := range versionInfo.MaxProtocolClients {
					unsupportedClients = append(unsupportedClients, client.Description())
				}
			}
		}
	} else {
		clientsSupported := make(map[string]bool)
		for _, versionInfo := range status.Cluster.Clients.SupportedVersions {
			if versionInfo.ProtocolVersion == "Unknown" {
				continue
			}
			match := versionInfo.ProtocolVersion == protocolVersion
			for _, client := range versionInfo.ConnectedClients {
				description := client.Description()
				if match {
					clientsSupported[description] = true
				} else if !clientsSupported[description] {
					clientsSupported[description] = false
				}
			}
		}
		unsupportedClients = make([]string, 0, len(clientsSupported))
		for client, supported := range clientsSupported {
			if !supported {
				unsupportedClients = append(unsupportedClients, client)
			}
		}
	}

	if len(unsupportedClients) > 0 {
		message := fmt.Sprintf(
			"%d clients do not support version %s: %s", len(unsupportedClients),
			cluster.Spec.Version, strings.Join(unsupportedClients, ", "),
		)
		r.Recorder.Event(cluster, "Normal", "UnsupportedClient", message)
		log.Info("Deferring reconciliation due to unsupported clients", "namespace", cluster.Namespace, "name", cluster.Name, "message", message)
		return false, nil
	}

	return true, nil
}

func (c CheckClientCompatibility) RequeueAfter() time.Duration {
	return 1 * time.Minute
}
