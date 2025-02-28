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
	"cmp"
	"errors"
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podclient"
	"github.com/go-logr/logr"
	"math"
	"slices"
	"strings"
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

	// Class defines the process class of this locality.
	Class fdbv1beta2.ProcessClass

	// Priority defines the priority of this process. The priority will be used in sortLocalities and is defined based
	// on the CoordinatorSelection setting in the FoundationDBCluster resource.
	Priority int
}

// Sort processes by their priority and their ID.
// We have to do this to ensure we get a deterministic result for selecting the candidates
// otherwise we get a (nearly) random result since processes are stored in a map which is by definition
// not sorted and doesn't return values in a stable way.
func sortLocalities(primaryDC string, processes []Info) {
	slices.SortStableFunc(processes, func(a, b Info) int {
		if primaryDC != "" {
			aDCLocality := a.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]
			bDCLocality := b.LocalityData[fdbv1beta2.FDBLocalityDCIDKey]

			if aDCLocality != bDCLocality {
				if aDCLocality == primaryDC {
					return -1
				}

				if bDCLocality == primaryDC {
					return 1
				}
			}
		}

		// If both have the same priority sort them by the process ID
		if a.Priority == b.Priority {
			return cmp.Compare(a.ID, b.ID)
		}

		// Prefer processes with a higher priority
		if a.Priority > b.Priority {
			return -1
		}

		return 1
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
// This method is only used during the initial bootstrapping of the cluster when no fdbserver processes are running.
func InfoFromSidecar(cluster *fdbv1beta2.FoundationDBCluster, client podclient.FdbPodClient) (Info, error) {
	substitutions, err := client.GetVariableSubstitutions()
	if err != nil {
		return Info{}, err
	}

	if substitutions == nil {
		return Info{}, nil
	}

	// Take the zone ID from the FDB_ZONE_ID if present.
	zoneID, present := substitutions[fdbv1beta2.EnvNameZoneID]
	if !present {
		// If the FDB_ZONE_ID is not present, the user specified another environment variable that represents the
		// zone ID.
		var zoneVariable string
		if strings.HasPrefix(cluster.Spec.FaultDomain.ValueFrom, "$") {
			zoneVariable = cluster.Spec.FaultDomain.ValueFrom[1:]
		} else {
			zoneVariable = fdbv1beta2.EnvNameZoneID
		}

		zoneID = substitutions[zoneVariable]
	}

	if zoneID == "" {
		return Info{}, errors.New("no zone ID found in Sidecar information")
	}

	// This locality information is only used during the initial cluster file generation.
	// So it should be good to only use the first process address here.
	// This has the implication that in the initial cluster file only the first processes will be used.
	return Info{
		ID:      substitutions[fdbv1beta2.EnvNameInstanceID],
		Address: cluster.GetFullAddress(substitutions[fdbv1beta2.EnvNamePublicIP], 1),
		LocalityData: map[string]string{
			fdbv1beta2.FDBLocalityZoneIDKey:  zoneID,
			fdbv1beta2.FDBLocalityDNSNameKey: substitutions[fdbv1beta2.EnvNameDNSName],
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

// ChooseDistributedProcesses recruits a maximally well-distributed set of processes from a set of potential candidates.
func ChooseDistributedProcesses(cluster *fdbv1beta2.FoundationDBCluster, processes []Info, count int, constraint ProcessSelectionConstraint) ([]Info, error) {
	chosen := make([]Info, 0, count)
	chosenIDs := make(map[string]bool, count)
	primaryDC := cluster.DesiredDatabaseConfiguration().GetPrimaryDCID()

	fields := constraint.Fields
	if len(fields) == 0 {
		fields = []string{fdbv1beta2.FDBLocalityZoneIDKey, fdbv1beta2.FDBLocalityDCIDKey}
		if cluster.Spec.DatabaseConfiguration.RedundancyMode == fdbv1beta2.RedundancyModeThreeDataHall {
			fields = append(fields, fdbv1beta2.FDBLocalityDataHallKey)
		}
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

	// Sort the processes to ensure a deterministic result.
	sortLocalities(primaryDC, processes)

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

	if len(chosen) != count {
		return chosen, notEnoughProcessesError{Desired: count, Chosen: len(chosen), Options: processes}
	}

	return chosen, nil
}

// GetHardLimits returns the distribution of localities.
func GetHardLimits(cluster *fdbv1beta2.FoundationDBCluster) map[string]int {
	if cluster.Spec.DatabaseConfiguration.UsableRegions <= 1 {
		// For the three_data_hall redundancy mode we will recruit 9 coordinators and those hard limits are only used
		// for selecting coordinators. We want to make sure we select coordinators across as many fault domains as possible.
		if cluster.Spec.DatabaseConfiguration.RedundancyMode == fdbv1beta2.RedundancyModeThreeDataHall {
			return map[string]int{
				// Assumption here is that we have 3 data halls and we want to spread the coordinators
				// equally across those 3 data halls.
				fdbv1beta2.FDBLocalityDataHallKey: 3,
				fdbv1beta2.FDBLocalityZoneIDKey:   1,
			}
		}

		return map[string]int{fdbv1beta2.FDBLocalityZoneIDKey: 1}
	}

	maxCoordinatorsPerDC := int(math.Ceil(float64(cluster.DesiredCoordinatorCount()) / float64(cluster.Spec.DatabaseConfiguration.CountUniqueDataCenters())))
	return map[string]int{
		fdbv1beta2.FDBLocalityDCIDKey:   maxCoordinatorsPerDC,
		fdbv1beta2.FDBLocalityZoneIDKey: 1,
	}
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
	hardLimits := GetHardLimits(cluster)
	coordinatorLocalities := make(map[string]map[string]int)
	// Track what fields should be validated.
	fieldsToValidate := make([]string, 0, len(hardLimits))
	for field := range hardLimits {
		fieldsToValidate = append(fieldsToValidate, field)
	}

	// Track the coordinator localities to verify if hard limits satisfy the fault tolerance requirements.
	for _, field := range fieldsToValidate {
		coordinatorLocalities[field] = make(map[string]int)
	}

	for _, process := range status.Cluster.Processes {
		processGroupID := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]

		pLogger := logger.WithValues("process", processGroupID)
		if process.Address.IsEmpty() {
			pLogger.Info("Skip process with empty address")
			continue
		}

		if len(process.Locality) == 0 {
			pLogger.Info("Skip process with empty localities")
			continue
		}

		if process.ProcessClass == fdbv1beta2.ProcessClassTest {
			pLogger.V(1).Info("Ignoring tester process")
			continue
		}

		addresses, err := fdbv1beta2.ParseProcessAddressesFromCmdline(process.CommandLine)
		if err != nil {
			// We will end here in the error case when the address
			// is not parsable e.g. no IP address is assigned.
			allAddressesValid = false
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

		if coordinatorAddress != "" {
			if process.Excluded {
				pLogger.Info("Coordinator process is excluded, marking it as unhealthy", "class", process.ProcessClass, "address", coordinatorAddress)
			} else {
				// The process is not excluded and has an address, so we assume that the coordinator is healthy.
				coordinatorStatus[coordinatorAddress] = true
			}

			if process.UnderMaintenance {
				pLogger.Info("Coordinator process is under maintenance", "class", process.ProcessClass, "address", coordinatorAddress)
			}
		}

		if coordinatorAddress != "" {
			for _, field := range fieldsToValidate {
				locality, ok := process.Locality[field]
				// If the field is not set ignore it.
				if !ok {
					continue
				}
				coordinatorLocalities[field][locality]++
			}

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

	// Check if the coordinators are distributed across the localities based on the hard limit requirements.
	hasCorrectLocalityDistribution := true
	for field, maxValue := range hardLimits {
		for locality, currentValue := range coordinatorLocalities[field] {
			if currentValue > maxValue {
				logger.Info("Cluster does not have coordinators in the correct number of localities", "desiredCount", maxValue, "currentCount", currentValue, "locality", locality)
				hasCorrectLocalityDistribution = false
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

	// Verify that enough coordinators are running.
	desiredCoordinatorCount := cluster.DesiredCoordinatorCount()
	runningCoordinators := len(coordinatorLocalities[fdbv1beta2.FDBLocalityZoneIDKey])
	hasEnoughCoordinators := runningCoordinators == desiredCoordinatorCount
	if !hasEnoughCoordinators {
		logger.Info("Cluster has not enough running coordinators", "runningCoordinators", runningCoordinators, "desiredCount", desiredCoordinatorCount)
	}

	return hasEnoughCoordinators && hasCorrectLocalityDistribution && allHealthy && allUsingCorrectAddress && allEligible, allAddressesValid, nil
}
