/*
 * locality.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package locality

import (
	"errors"
	"fmt"
	"math"
	"sort"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient"
	"github.com/go-logr/logr"
)

// Info captures information about a process for the purposes of
// choosing diverse locality.
type Info struct {
	// The process group ID
	ID string

	// The process's public address.
	Address fdbv1beta2.ProcessAddress

	// The locality map.
	LocalityData map[string]string

	Class fdbv1beta2.ProcessClass
}

// Sort processes by their priority and their ID.
// We have to do this to ensure we get a deterministic result for selecting the candidates
// otherwise we get a (nearly) random result since processes are stored in a map which is by definition
// not sorted and doesn't return values in a stable way.
func sortLocalities(cluster *fdbv1beta2.FoundationDBCluster, processes []Info) {
	// Sort the processes for ID to ensure we have a stable input
	sort.SliceStable(processes, func(i, j int) bool {
		p1 := cluster.GetClassCandidatePriority(processes[i].Class)
		p2 := cluster.GetClassCandidatePriority(processes[j].Class)

		// If both have the same priority sort them by the process ID
		if p1 == p2 {
			return processes[i].ID < processes[j].ID
		}

		// prefer processes with a higher priority
		return p1 > p2
	})
}

// InfoForProcess converts the process information from the JSON status
// into locality info for selecting processes.
func InfoForProcess(process fdbv1beta2.FoundationDBStatusProcessInfo, mainContainerTLS bool) (Info, error) {
	addresses, err := fdbv1beta2.ParseProcessAddressesFromCmdline(process.CommandLine)
	if err != nil {
		return Info{}, err
	}

	var addr fdbv1beta2.ProcessAddress
	// Iterate the addresses and set the expected address as process address
	// e.g. if we want to use TLS set it to the tls address otherwise use the non-TLS.
	for _, address := range addresses {
		if address.Flags["tls"] == mainContainerTLS {
			addr = address
			break
		}
	}

	return Info{
		ID:           process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
		Address:      addr,
		LocalityData: process.Locality,
		Class:        process.ProcessClass,
	}, nil
}

// InfoFromSidecar converts the process information from the sidecar's
// context into locality info for selecting processes.
func InfoFromSidecar(cluster *fdbv1beta2.FoundationDBCluster, client podclient.FdbPodClient) (Info, error) {
	substitutions, err := client.GetVariableSubstitutions()
	if err != nil {
		return Info{}, err
	}

	if substitutions == nil {
		return Info{}, nil
	}

	// This locality information is only used during the initial cluster file generation.
	// So it should be good to only use the first process address here.
	// This has the implication that in the initial cluster file only the first processes will be used.
	return Info{
		ID:      substitutions["FDB_INSTANCE_ID"],
		Address: cluster.GetFullAddress(substitutions["FDB_PUBLIC_IP"], 1),
		LocalityData: map[string]string{
			fdbv1beta2.FDBLocalityZoneIDKey:  substitutions["FDB_ZONE_ID"],
			fdbv1beta2.FDBLocalityDNSNameKey: substitutions["FDB_DNS_NAME"],
		},
	}, nil
}

// notEnoughProcessesError is returned when we cannot recruit enough processes.
type notEnoughProcessesError struct {
	// desired defines the number of processes we wanted to recruit.
	Desired int

	// chosen defines the number of processes we were able to recruit.
	Chosen int

	// options defines the processes that we were selecting from.
	Options []Info
}

// Error formats an error message.
func (err notEnoughProcessesError) Error() string {
	return fmt.Sprintf("Could only select %d processes, but %d are required", err.Chosen, err.Desired)
}

// ProcessSelectionConstraint defines constraints on how we choose processes
// in chooseDistributedProcesses
type ProcessSelectionConstraint struct {
	// Fields defines the locality fields we should consider when selecting
	// processes.
	Fields []string

	// HardLimits defines a maximum number of processes to recruit on any single
	// value for a given locality field.
	HardLimits map[string]int
}

// ChooseDistributedProcesses recruits a maximally well-distributed set
// of processes from a set of potential candidates.
func ChooseDistributedProcesses(cluster *fdbv1beta2.FoundationDBCluster, processes []Info, count int, constraint ProcessSelectionConstraint) ([]Info, error) {
	chosen := make([]Info, 0, count)
	chosenIDs := make(map[string]bool, count)

	fields := constraint.Fields
	if len(fields) == 0 {
		fields = []string{fdbv1beta2.FDBLocalityZoneIDKey, fdbv1beta2.FDBLocalityDCIDKey}
	}

	chosenCounts := make(map[string]map[string]int, len(fields))
	hardLimits := make(map[string]int, len(fields))
	currentLimits := make(map[string]int, len(fields))

	if constraint.HardLimits != nil {
		for field, limit := range constraint.HardLimits {
			hardLimits[field] = limit
		}
	}

	for _, field := range fields {
		chosenCounts[field] = make(map[string]int)
		if hardLimits[field] == 0 {
			hardLimits[field] = count
		}
		currentLimits[field] = 1
	}

	// Sort the processes to ensure a deterministic result
	sortLocalities(cluster, processes)

	for len(chosen) < count {
		choseAny := false

		for _, process := range processes {
			if chosenIDs[process.ID] {
				continue
			}

			eligible := true
			for _, field := range fields {
				value := process.LocalityData[field]
				if chosenCounts[field][value] >= currentLimits[field] {
					eligible = false
					break
				}
			}

			if !eligible {
				continue
			}

			chosen = append(chosen, process)
			chosenIDs[process.ID] = true

			choseAny = true

			for _, field := range fields {
				value := process.LocalityData[field]
				chosenCounts[field][value]++
			}

			if len(chosen) == count {
				break
			}
		}

		if !choseAny {
			incrementedLimits := false
			for indexOfField := len(fields) - 1; indexOfField >= 0; indexOfField-- {
				field := fields[indexOfField]
				if currentLimits[field] < hardLimits[field] {
					currentLimits[field]++
					incrementedLimits = true
					break
				}
			}
			if !incrementedLimits {
				return chosen, notEnoughProcessesError{Desired: count, Chosen: len(chosen), Options: processes}
			}
		}
	}

	return chosen, nil
}

// GetHardLimits returns the distribution of localities.
func GetHardLimits(cluster *fdbv1beta2.FoundationDBCluster) map[string]int {
	if cluster.Spec.DatabaseConfiguration.UsableRegions <= 1 {
		return map[string]int{fdbv1beta2.FDBLocalityZoneIDKey: 1}
	}

	// TODO (johscheuer): should we calculate that based on the number of DCs?
	maxCoordinatorsPerDC := int(math.Floor(float64(cluster.DesiredCoordinatorCount()) / 2.0))

	return map[string]int{fdbv1beta2.FDBLocalityZoneIDKey: 1, fdbv1beta2.FDBLocalityDCIDKey: maxCoordinatorsPerDC}
}

// CheckCoordinatorValidity determines if the cluster's current coordinators
// meet the fault tolerance requirements.
//
// The first return value will be whether the coordinators are valid.
// The second return value will be whether the processes have their TLS flags
// matching the cluster spec.
// The third return value will hold any errors encountered when checking the
// coordinators.
func CheckCoordinatorValidity(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, coordinatorStatus map[string]bool) (bool, bool, error) {
	if len(coordinatorStatus) == 0 {
		return false, false, errors.New("unable to get coordinator status")
	}

	allAddressesValid := true
	allEligible := true
	allUsingCorrectAddress := true

	coordinatorZones := make(map[string]int, len(coordinatorStatus))
	coordinatorDCs := make(map[string]int, len(coordinatorStatus))
	processGroups := make(map[string]*fdbv1beta2.ProcessGroupStatus)
	for _, processGroup := range cluster.Status.ProcessGroups {
		processGroups[processGroup.ProcessGroupID] = processGroup
	}

	for _, process := range status.Cluster.Processes {
		pLogger := logger.WithValues("process", process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])
		if process.Address.IsEmpty() {
			pLogger.Info("Skip process with empty address")
			continue
		}

		if process.ProcessClass == fdbv1beta2.ProcessClassTest {
			pLogger.Info("Ignoring tester process")
			continue
		}

		processGroupStatus := processGroups[process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]]
		pendingRemoval := processGroupStatus != nil && processGroupStatus.IsMarkedForRemoval()
		if processGroupStatus != nil && cluster.SkipProcessGroup(processGroupStatus) {
			pLogger.Info("Skipping process group with pending Pod",
				"namespace", cluster.Namespace,
				"cluster", cluster.Name,
				"processGroupID", process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey],
				"class", process.ProcessClass)
			continue
		}

		addresses, err := fdbv1beta2.ParseProcessAddressesFromCmdline(process.CommandLine)
		if err != nil {
			// We will end here in the error case when the address
			// is not parsable e.g. no IP address is assigned.
			allAddressesValid = false
			// add: command_line
			pLogger.Info("Could not parse address from command_line", "command_line", process.CommandLine)
			continue
		}

		var ipAddress fdbv1beta2.ProcessAddress
		for _, addr := range addresses {
			if addr.Flags["tls"] == cluster.Spec.MainContainer.EnableTLS {
				ipAddress = addr
				break
			}
		}

		coordinatorAddress := ""
		_, isCoordinatorWithIP := coordinatorStatus[ipAddress.String()]
		if isCoordinatorWithIP {
			coordinatorAddress = ipAddress.String()
		}

		dnsName := process.Locality[fdbv1beta2.FDBLocalityDNSNameKey]
		dnsAddress := fdbv1beta2.ProcessAddress{
			StringAddress: dnsName,
			Port:          ipAddress.Port,
			Flags:         ipAddress.Flags,
		}
		_, isCoordinatorWithDNS := coordinatorStatus[dnsAddress.String()]

		if !isCoordinatorWithDNS {
			dnsAddress = ipAddress
			dnsAddress.FromHostname = true
			_, isCoordinatorWithDNS = coordinatorStatus[dnsAddress.String()]
		}

		if isCoordinatorWithDNS {
			coordinatorAddress = dnsAddress.String()
		}

		if coordinatorAddress != "" && !process.Excluded && !pendingRemoval {
			coordinatorStatus[coordinatorAddress] = true
		}

		if coordinatorAddress != "" {
			coordinatorZones[process.Locality[fdbv1beta2.FDBLocalityZoneIDKey]]++
			coordinatorDCs[process.Locality[fdbv1beta2.FDBLocalityDCIDKey]]++

			if !cluster.IsEligibleAsCandidate(process.ProcessClass) {
				pLogger.Info("Process class of process is not eligible as coordinator", "class", process.ProcessClass, "address", coordinatorAddress)
				allEligible = false
			}

			useDNS := cluster.UseDNSInClusterFile() && dnsName != ""
			if (isCoordinatorWithIP && useDNS) || (isCoordinatorWithDNS && !useDNS) {
				pLogger.Info("Coordinator is not using the correct address type", "coordinatorList", coordinatorStatus, "address", coordinatorAddress, "expectingDNS", useDNS, "usingDNS", isCoordinatorWithDNS)
				allUsingCorrectAddress = false
			}
		}

		if ipAddress.IPAddress == nil {
			pLogger.Info("Process has invalid IP address", "addresses", addresses)
			allAddressesValid = false
		}
	}

	desiredCount := cluster.DesiredCoordinatorCount()
	hasEnoughZones := len(coordinatorZones) == desiredCount
	if !hasEnoughZones {
		logger.Info("Cluster does not have coordinators in the correct number of zones", "desiredCount", desiredCount, "coordinatorZones", coordinatorZones)
	}

	var maxCoordinatorsPerDC int
	hasEnoughDCs := true
	if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 {
		maxCoordinatorsPerDC = int(math.Floor(float64(desiredCount) / 2.0))

		for dc, count := range coordinatorDCs {
			if count > maxCoordinatorsPerDC {
				logger.Info("Cluster has too many coordinators in a single DC", "DC", dc, "count", count, "max", maxCoordinatorsPerDC)
				hasEnoughDCs = false
			}
		}
	}

	allHealthy := true
	for address, healthy := range coordinatorStatus {
		if !healthy {
			allHealthy = false
			logger.Info("Cluster has an unhealthy coordinator", "address", address)
		}
	}

	return hasEnoughDCs && hasEnoughZones && allHealthy && allUsingCorrectAddress && allEligible, allAddressesValid, nil
}
