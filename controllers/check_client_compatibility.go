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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// checkClientCompatibility confirms that all clients are compatible with the
// version of FoundationDB configured on the cluster.
type checkClientCompatibility struct{}

// reconcile runs the reconciler's work.
func (c checkClientCompatibility) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	if !cluster.Status.Configured && !cluster.IsBeingUpgraded() {
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

	if !version.IsAtLeast(runningVersion) && !version.IsProtocolCompatible(runningVersion) {
		return &requeue{message: fmt.Sprintf("cluster downgrade operation is only supported for protocol compatible versions, running version %s and desired version %s are not compatible", runningVersion, version)}
	}

	if version.IsProtocolCompatible(runningVersion) {
		return nil
	}

	if cluster.Spec.IgnoreUpgradabilityChecks {
		return nil
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return &requeue{curError: err}
	}
	defer adminClient.Close()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	protocolVersion, err := adminClient.GetProtocolVersion(cluster.Spec.Version)
	if err != nil {
		return &requeue{curError: err}
	}

	ignoredLogGroups := make(map[fdbv1beta2.LogGroup]fdbv1beta2.None)
	for _, logGroup := range cluster.GetIgnoreLogGroupsForUpgrade() {
		ignoredLogGroups[logGroup] = fdbv1beta2.None{}
	}
	unsupportedClients := getUnsupportedClients(status.Cluster.Clients.SupportedVersions, protocolVersion, ignoredLogGroups)

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

func getUnsupportedClients(supportedVersions []fdbv1beta2.FoundationDBStatusSupportedVersion, protocolVersion string, ignoredLogGroups map[fdbv1beta2.LogGroup]fdbv1beta2.None) []string {
	var unsupportedClients []string
	for _, versionInfo := range supportedVersions {
		if versionInfo.ProtocolVersion == "Unknown" {
			continue
		}

		if versionInfo.ProtocolVersion != protocolVersion {
			for _, client := range versionInfo.MaxProtocolClients {
				if _, ok := ignoredLogGroups[client.LogGroup]; ok {
					continue
				}
				unsupportedClients = append(unsupportedClients, client.Description())
			}
		}
	}
	return unsupportedClients
}
