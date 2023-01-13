/*
 * fault_tolerance.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
)

func hasDesiredFaultTolerance(expectedFaultTolerance int, maxZoneFailuresWithoutLosingData int, maxZoneFailuresWithoutLosingAvailability int) bool {
	// Only if both max zone failures for availability and data loss are greater or equal to the expected fault tolerance we know that we meet
	// our fault tolerance requirements.
	return maxZoneFailuresWithoutLosingData >= expectedFaultTolerance && maxZoneFailuresWithoutLosingAvailability >= expectedFaultTolerance
}

// HasDesiredFaultTolerance checks if the cluster has the desired fault tolerance.
func HasDesiredFaultTolerance(log logr.Logger, adminClient fdbadminclient.AdminClient, cluster *fdbv1beta2.FoundationDBCluster) (bool, error) {
	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}

	return HasDesiredFaultToleranceFromStatus(log, status, cluster), nil
}

// HasDesiredFaultToleranceFromStatus checks if the cluster has the desired fault tolerance based on the provided status.
func HasDesiredFaultToleranceFromStatus(log logr.Logger, status *fdbv1beta2.FoundationDBStatus, cluster *fdbv1beta2.FoundationDBCluster) bool {
	if !status.Client.DatabaseStatus.Available {
		log.Info("Cluster is not available",
			"namespace", cluster.Namespace,
			"cluster", cluster.Name)

		return false
	}

	expectedFaultTolerance := cluster.DesiredFaultTolerance()
	log.Info("Check desired fault tolerance",
		"expectedFaultTolerance", expectedFaultTolerance,
		"maxZoneFailuresWithoutLosingData", status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData,
		"maxZoneFailuresWithoutLosingAvailability", status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability)

	return hasDesiredFaultTolerance(
		expectedFaultTolerance,
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData,
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability)
}
