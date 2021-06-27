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
	ctx "context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// CheckClientCompatibility confirms that all clients are compatible with the
// version of FoundationDB configured on the cluster.
type CheckClientCompatibility struct{}

// Reconcile runs the reconciler's work.
func (c CheckClientCompatibility) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) *Requeue {
	if !cluster.Status.Configured {
		return nil
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &Requeue{Error: err}
	}
	defer adminClient.Close()

	runningVersion, err := fdbtypes.ParseFdbVersion(cluster.Status.RunningVersion)
	if err != nil {
		return &Requeue{Error: err}
	}

	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return &Requeue{Error: err}
	}

	if !version.IsAtLeast(runningVersion) {
		return &Requeue{Message: "cluster downgrade operation is not supported"}
	}

	if version.IsProtocolCompatible(runningVersion) {
		return nil
	}

	status, err := adminClient.GetStatus()
	if err != nil {
		return &Requeue{Error: err}
	}

	protocolVersion, err := adminClient.GetProtocolVersion(cluster.Spec.Version)
	if err != nil {
		return &Requeue{Error: err}
	}

	if !cluster.Spec.IgnoreUpgradabilityChecks {
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
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "UnsupportedClient", message)
			log.Info("Deferring reconciliation due to unsupported clients", "namespace", cluster.Namespace, "name", cluster.Name, "message", message)
			return &Requeue{Message: message, Delay: 1 * time.Minute}
		}
	}

	return nil
}
