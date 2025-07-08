/*
 * coordinator.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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

package coordinator

import (
	"fmt"
	"math"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/locality"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/go-logr/logr"
)

// ChangeCoordinators will change the coordinators and set the new connection string on the FoundationDBCluster resource.
func ChangeCoordinators(
	logger logr.Logger,
	adminClient fdbadminclient.AdminClient,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
) error {
	var pendingRemovals map[fdbv1beta2.ProcessGroupID]time.Time
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		var err error
		pendingRemovals, err = adminClient.GetPendingForRemoval("")
		if err != nil {
			return err
		}
	}

	coordinators, err := SelectCoordinators(logger, cluster, status, pendingRemovals)
	if err != nil {
		return err
	}

	logger.Info("Final coordinators candidates", "coordinators", coordinators)
	connectionString, err := adminClient.ChangeCoordinators(coordinators)
	if err != nil {
		return err
	}
	cluster.Status.ConnectionString = connectionString

	return nil
}

// selectCandidates is a helper for Reconcile that picks non-excluded, not-being-removed class-matching process groups.
func selectCandidates(
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	pendingRemovals map[fdbv1beta2.ProcessGroupID]time.Time,
) ([]locality.Info, error) {
	candidates := make([]locality.Info, 0, len(status.Cluster.Processes))
	for _, process := range status.Cluster.Processes {
		if process.Excluded || process.UnderMaintenance {
			continue
		}

		if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
			continue
		}

		// Ignore processes with missing locality, see: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/1254
		if len(process.Locality) == 0 {
			continue
		}

		// If the cluster should be using DNS in the cluster file we should make sure the locality is set.
		if cluster.UseDNSInClusterFile() {
			_, ok := process.Locality[fdbv1beta2.FDBLocalityDNSNameKey]
			if !ok {
				continue
			}
		}

		if cluster.ProcessGroupIsBeingRemoved(
			fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]),
		) {
			continue
		}

		if _, shouldBeRemoved := pendingRemovals[fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])]; shouldBeRemoved {
			continue
		}

		currentLocality, err := locality.InfoForProcess(
			process,
			cluster.Spec.MainContainer.EnableTLS,
		)
		if err != nil {
			return nil, err
		}

		priority := cluster.GetClassCandidatePriority(process.ProcessClass)
		// If the process is not running in the desired version or the binary is running from the shared volumes
		// that means this process is pending a Pod recreation and will therefore be down for some time.
		// We reduce the priority in this case to reduce the risk of successive coordinator changes. Reducing the
		// priority should help in reducing the overall coordinator changes.
		// See: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/2015
		if process.Version != cluster.Spec.Version ||
			strings.HasPrefix(process.CommandLine, "/var/") {
			// math.MinInt64 is the lowest possible priority. By adding the actual priority we make sure that we
			// still keep the priorities, even if all processes are not yet upgraded.
			if priority < 0 {
				priority = math.MinInt
			} else {
				priority += math.MinInt
			}
		}

		currentLocality.Priority = priority
		candidates = append(candidates, currentLocality)
	}

	return candidates, nil
}

// selectCoordinatorsLocalities will return a set of new coordinators.
func selectCoordinatorsLocalities(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	pendingRemovals map[fdbv1beta2.ProcessGroupID]time.Time,
) ([]locality.Info, error) {
	var err error
	coordinatorCount := cluster.DesiredCoordinatorCount()

	candidates, err := selectCandidates(cluster, status, pendingRemovals)
	if err != nil {
		return nil, err
	}

	coordinators, err := locality.ChooseDistributedProcesses(
		cluster,
		candidates,
		coordinatorCount,
		locality.ProcessSelectionConstraint{
			HardLimits: locality.GetHardLimits(cluster),
		},
	)

	logger.Info("Current coordinators", "coordinators", coordinators, "error", err)
	if err != nil {
		return nil, err
	}

	coordinatorStatus := make(map[string]bool, len(status.Client.Coordinators.Coordinators))
	for _, coordinator := range coordinators {
		coordinatorStatus[GetCoordinatorAddress(cluster, coordinator).String()] = false
	}

	hasValidCoordinators, allAddressesValid, err := locality.CheckCoordinatorValidity(
		logger,
		cluster,
		status,
		coordinatorStatus,
	)
	if err != nil {
		return nil, err
	}

	if !hasValidCoordinators {
		return nil, fmt.Errorf("new coordinators are not valid")
	}

	if !allAddressesValid {
		return nil, fmt.Errorf("new coordinators contain invalid addresses")
	}

	return coordinators, nil
}

// SelectCoordinators will return a set of new coordinators.
func SelectCoordinators(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	pendingRemovals map[fdbv1beta2.ProcessGroupID]time.Time,
) ([]fdbv1beta2.ProcessAddress, error) {
	coordinators, err := selectCoordinatorsLocalities(logger, cluster, status, pendingRemovals)
	if err != nil {
		return nil, err
	}

	coordinatorAddresses := make([]fdbv1beta2.ProcessAddress, len(coordinators))
	for index, process := range coordinators {
		coordinatorAddresses[index] = GetCoordinatorAddress(cluster, process)
	}

	return coordinatorAddresses, nil
}

// GetCoordinatorAddress returns the coordinator address.
func GetCoordinatorAddress(
	cluster *fdbv1beta2.FoundationDBCluster,
	locality locality.Info,
) fdbv1beta2.ProcessAddress {
	dnsName := locality.LocalityData[fdbv1beta2.FDBLocalityDNSNameKey]

	address := locality.Address

	if cluster.UseDNSInClusterFile() && dnsName != "" {
		return fdbv1beta2.ProcessAddress{
			StringAddress: dnsName,
			Port:          address.Port,
			Flags:         address.Flags,
		}
	}

	return address
}
