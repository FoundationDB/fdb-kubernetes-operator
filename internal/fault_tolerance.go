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
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
)

func hasDesiredFaultTolerance(expectedFaultTolerance int, maxZoneFailuresWithoutLosingData int, maxZoneFailuresWithoutLosingAvailability int) bool {
	// Only if both max zone failures for availability and data loss are greater or equal to the expected fault tolerance we know that we meet
	// our fault tolerance requirements.
	return maxZoneFailuresWithoutLosingData >= expectedFaultTolerance && maxZoneFailuresWithoutLosingAvailability >= expectedFaultTolerance
}

// HasDesiredFaultTolerance checks if the cluster has the desired fault tolerance.
func HasDesiredFaultTolerance(adminClient fdbadminclient.AdminClient, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	version, err := fdbtypes.ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return false, err
	}

	if !version.HasZoneFaultToleranceInStatus() {
		return true, nil
	}

	status, err := adminClient.GetStatus()
	if err != nil {
		return false, err
	}

	if !status.Client.DatabaseStatus.Available {
		//log.V(0).Info("Cluster is not available",
		//	"namespace", cluster.Namespace,
		//	"cluster", cluster.Name)

		return false, nil
	}

	expectedFaultTolerance := cluster.DesiredFaultTolerance()
	//log.V(0).Info("Check desired fault tolerance",
	//	"namespace", cluster.Namespace,
	//	"cluster", cluster.Name,
	//	"expectedFaultTolerance", expectedFaultTolerance,
	//	"maxZoneFailuresWithoutLosingData", status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData,
	//	"maxZoneFailuresWithoutLosingAvailability", status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability)

	return hasDesiredFaultTolerance(
		expectedFaultTolerance,
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData,
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability), nil
}
