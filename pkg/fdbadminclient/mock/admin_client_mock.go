/*
 * admin_client_mock.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2022 Apple Inc. and the FoundationDB project authors
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

package mock

import (
	"context"
	"fmt"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	"k8s.io/utils/pointer"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient/mock"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AdminClient provides a mock implementation of the cluster admin interface
type AdminClient struct {
	Cluster                                  *fdbv1beta2.FoundationDBCluster
	KubeClient                               client.Client
	DatabaseConfiguration                    *fdbv1beta2.DatabaseConfiguration
	ExcludedAddresses                        map[string]fdbv1beta2.None
	KilledAddresses                          map[string]fdbv1beta2.None
	Knobs                                    map[string]fdbv1beta2.None
	missingLocalities                        map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None
	missingProcessGroups                     map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None
	incorrectCommandLines                    map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None
	FrozenStatus                             *fdbv1beta2.FoundationDBStatus
	Backups                                  map[string]fdbv1beta2.FoundationDBBackupStatusBackupDetails
	clientVersions                           map[string][]string
	currentCommandLines                      map[string]string
	VersionProcessGroups                     map[fdbv1beta2.ProcessGroupID]string
	ReincludedAddresses                      map[string]bool
	additionalProcesses                      []fdbv1beta2.ProcessGroupStatus
	localityInfo                             map[fdbv1beta2.ProcessGroupID]map[string]string
	MaxZoneFailuresWithoutLosingData         *int
	MaxZoneFailuresWithoutLosingAvailability *int
	MaintenanceZone                          fdbv1beta2.FaultDomain
	restoreURL                               string
	maintenanceZoneStartTimestamp            time.Time
	uptimeSecondsForMaintenanceZone          float64
	TeamTracker                              []fdbv1beta2.FoundationDBStatusTeamTracker
	Logs                                     []fdbv1beta2.FoundationDBStatusLogInfo
	mockError                                error
	mockGetConnectionStringError             error
	LagInfo                                  map[string]fdbv1beta2.FoundationDBStatusLagInfo
	processesUnderMaintenance                map[fdbv1beta2.ProcessGroupID]int64
}

// adminClientCache provides a cache of mock admin clients.
var adminClientCache = make(map[string]*AdminClient)
var adminClientMutex sync.Mutex

// NewMockAdminClient creates an admin client for a cluster.
func NewMockAdminClient(cluster *fdbv1beta2.FoundationDBCluster, kubeClient client.Client) (fdbadminclient.AdminClient, error) {
	return NewMockAdminClientUncast(cluster, kubeClient)
}

// NewMockAdminClientUncast creates a mock admin client for a cluster.
// nolint:unparam
// is required because we always return a nil error
func NewMockAdminClientUncast(cluster *fdbv1beta2.FoundationDBCluster, kubeClient client.Client) (*AdminClient, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	cachedClient := adminClientCache[cluster.Name]

	if cachedClient == nil {
		cachedClient = &AdminClient{
			Cluster:                   cluster.DeepCopy(),
			KubeClient:                kubeClient,
			ExcludedAddresses:         make(map[string]fdbv1beta2.None),
			ReincludedAddresses:       make(map[string]bool),
			KilledAddresses:           make(map[string]fdbv1beta2.None),
			missingProcessGroups:      make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None),
			missingLocalities:         make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None),
			incorrectCommandLines:     make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None),
			localityInfo:              make(map[fdbv1beta2.ProcessGroupID]map[string]string),
			currentCommandLines:       make(map[string]string),
			Knobs:                     make(map[string]fdbv1beta2.None),
			VersionProcessGroups:      make(map[fdbv1beta2.ProcessGroupID]string),
			LagInfo:                   make(map[string]fdbv1beta2.FoundationDBStatusLagInfo),
			processesUnderMaintenance: make(map[fdbv1beta2.ProcessGroupID]int64),
		}
		adminClientCache[cluster.Name] = cachedClient
		cachedClient.Backups = make(map[string]fdbv1beta2.FoundationDBBackupStatusBackupDetails)
	} else {
		cachedClient.Cluster = cluster.DeepCopy()
	}

	return cachedClient, nil
}

// ClearMockAdminClients clears the cache of mock Admin clients
func ClearMockAdminClients() {
	adminClientCache = map[string]*AdminClient{}
}

// GetStatus gets the database's status
func (client *AdminClient) GetStatus() (*fdbv1beta2.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return nil, client.mockError
	}

	if client.FrozenStatus != nil {
		return client.FrozenStatus, nil
	}
	pods := &corev1.PodList{}
	err := client.KubeClient.List(context.TODO(), pods, internal.GetPodListOptions(client.Cluster, "", "")...)
	if err != nil {
		return nil, err
	}
	status := &fdbv1beta2.FoundationDBStatus{
		Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
			Processes: make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo, len(pods.Items)),
		},
	}

	coordinators := make(map[string]bool)
	var coordinatorAddresses []string
	if strings.Contains(client.Cluster.Status.ConnectionString, "@") {
		coordinatorAddresses = strings.Split(strings.Split(client.Cluster.Status.ConnectionString, "@")[1], ",")
		status.Cluster.ConnectionString = client.Cluster.Status.ConnectionString
	} else {
		coordinatorAddresses = []string{}
	}

	for _, address := range coordinatorAddresses {
		coordinators[address] = false
	}

	for _, pod := range pods.Items {
		processClass, err := podmanager.GetProcessClass(client.Cluster, &pod)
		if err != nil {
			return nil, err
		}

		processCount, err := internal.GetServersPerPodForPod(&pod, processClass)
		if err != nil {
			return nil, err
		}

		processGroupID := podmanager.GetProcessGroupID(client.Cluster, &pod)
		if _, ok := client.missingProcessGroups[processGroupID]; ok {
			continue
		}

		podClient, _ := mock.NewMockFdbPodClient(client.Cluster, &pod)
		subs, err := podClient.GetVariableSubstitutions()
		if err != nil {
			return nil, err
		}

		processIP, ok := subs[fdbv1beta2.EnvNamePublicIP]
		if !ok {
			processIP = pod.Status.PodIP
		}

		for processIndex := 1; processIndex <= processCount; processIndex++ {
			var fdbRoles []fdbv1beta2.FoundationDBStatusProcessRoleInfo

			fullAddress := client.Cluster.GetFullAddress(processIP, processIndex)
			_, ipExcluded := client.ExcludedAddresses[processIP]
			_, addressExcluded := client.ExcludedAddresses[fullAddress.String()]
			excluded := ipExcluded || addressExcluded

			pClass, err := podmanager.GetProcessClass(client.Cluster, &pod)
			if err != nil {
				return nil, err
			}

			command, hasCommandLine := client.currentCommandLines[fullAddress.StringWithoutFlags()]
			if !hasCommandLine {
				// We only set the command if we don't have the commandline "cached"
				command, err = internal.GetStartCommand(client.Cluster, pClass, podClient, processIndex, processCount, internal.GetImageType(&pod))
				if err != nil {
					return nil, err
				}

				client.currentCommandLines[fullAddress.StringWithoutFlags()] = command
			}

			if _, ok := client.incorrectCommandLines[processGroupID]; ok {
				command += " --locality_incorrect=1"
			}

			var locality map[string]string
			if _, ok := client.missingLocalities[processGroupID]; ok {
				locality = map[string]string{}
			} else {
				locality = map[string]string{
					fdbv1beta2.FDBLocalityInstanceIDKey: string(processGroupID),
					fdbv1beta2.FDBLocalityZoneIDKey:     pod.Name,
					fdbv1beta2.FDBLocalityDCIDKey:       client.Cluster.Spec.DataCenter,
				}

				for key, value := range client.localityInfo[processGroupID] {
					locality[key] = value
				}

				for _, container := range pod.Spec.Containers {
					for _, envVar := range container.Env {
						if envVar.Name == fdbv1beta2.EnvNameDNSName {
							locality[fdbv1beta2.FDBLocalityDNSNameKey] = envVar.Value

							if client.Cluster.UseDNSInClusterFile() {
								fullAddress.StringAddress = envVar.Value
							}
							// TODO (johscheuer): This should be set to true. This will add additional information to the
							// return address and needs some additional refactoring in the mock code.
							// fullAddress.FromHostname = true
						}
					}
				}

				if processCount > 1 {
					locality[fdbv1beta2.FDBLocalityProcessIDKey] = fmt.Sprintf("%s-%d", processGroupID, processIndex)
				}
			}

			var uptimeSeconds float64 = 60000
			underMaintenance := false
			if string(client.MaintenanceZone) == locality[fdbv1beta2.FDBLocalityZoneIDKey] || client.MaintenanceZone == "simulation" {
				if client.uptimeSecondsForMaintenanceZone != 0.0 {
					uptimeSeconds = client.uptimeSecondsForMaintenanceZone
				} else {
					uptimeSeconds = time.Since(client.maintenanceZoneStartTimestamp).Seconds()
				}
				underMaintenance = true
			}

			_, isCoordinator := coordinators[fullAddress.String()]
			if isCoordinator && !excluded && !underMaintenance {
				coordinators[fullAddress.String()] = true
				fdbRoles = append(fdbRoles, fdbv1beta2.FoundationDBStatusProcessRoleInfo{Role: string(fdbv1beta2.ProcessRoleCoordinator)})
			}

			version, ok := client.VersionProcessGroups[processGroupID]
			if !ok {
				if client.Cluster.VersionCompatibleUpgradeInProgress() {
					for _, container := range pod.Spec.Containers {
						if container.Name != fdbv1beta2.MainContainerName {
							continue
						}

						parts := strings.Split(container.Image, ":")
						if len(parts) > 1 {
							parsedVersion, err := fdbv1beta2.ParseFdbVersion(parts[1])
							if err != nil {
								continue
							}

							version = parsedVersion.String()
						}
					}
				}

				if version == "" {
					version = client.Cluster.Status.RunningVersion
				}
			}

			status.Cluster.Processes[fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%d", pod.Name, processIndex))] = fdbv1beta2.FoundationDBStatusProcessInfo{
				Address:          fullAddress,
				ProcessClass:     internal.GetProcessClassFromMeta(client.Cluster, pod.ObjectMeta),
				CommandLine:      command,
				Excluded:         excluded,
				UnderMaintenance: underMaintenance,
				Locality:         locality,
				Version:          version,
				UptimeSeconds:    uptimeSeconds,
				Roles:            fdbRoles,
			}
		}
	}

	for _, processGroup := range client.additionalProcesses {
		locality := map[string]string{
			fdbv1beta2.FDBLocalityInstanceIDKey: string(processGroup.ProcessGroupID),
			fdbv1beta2.FDBLocalityZoneIDKey:     string(processGroup.ProcessGroupID),
		}

		for key, value := range client.localityInfo[processGroup.ProcessGroupID] {
			locality[key] = value
		}

		var uptimeSeconds float64 = 60000
		underMaintenance := false
		if string(client.MaintenanceZone) == locality[fdbv1beta2.FDBLocalityZoneIDKey] || client.MaintenanceZone == "simulation" {
			if client.uptimeSecondsForMaintenanceZone != 0.0 {
				uptimeSeconds = client.uptimeSecondsForMaintenanceZone
			} else {
				uptimeSeconds = time.Since(client.maintenanceZoneStartTimestamp).Seconds()
			}
			underMaintenance = true
		}

		fullAddress := client.Cluster.GetFullAddress(processGroup.Addresses[0], 1)
		status.Cluster.Processes[processGroup.ProcessGroupID] = fdbv1beta2.FoundationDBStatusProcessInfo{
			Address:          fullAddress,
			ProcessClass:     processGroup.ProcessClass,
			Locality:         locality,
			UnderMaintenance: underMaintenance,
			Version:          client.Cluster.Status.RunningVersion,
			UptimeSeconds:    uptimeSeconds,
		}
	}

	if client.clientVersions != nil {
		supportedVersions := make([]fdbv1beta2.FoundationDBStatusSupportedVersion, 0, len(client.clientVersions))
		for version, addresses := range client.clientVersions {
			protocolVersion, err := client.GetProtocolVersion(version)
			if err != nil {
				return nil, err
			}

			protocolClients := make([]fdbv1beta2.FoundationDBStatusConnectedClient, 0, len(addresses))
			for _, address := range addresses {
				protocolClients = append(protocolClients, fdbv1beta2.FoundationDBStatusConnectedClient{
					Address:  address,
					LogGroup: fdbv1beta2.LogGroup(client.Cluster.Name),
				})
			}

			supportedVersions = append(supportedVersions, fdbv1beta2.FoundationDBStatusSupportedVersion{
				ClientVersion:      version,
				ProtocolVersion:    protocolVersion,
				MaxProtocolClients: protocolClients,
			})
		}
		status.Cluster.Clients.SupportedVersions = supportedVersions
	}

	var countReachableCoordinators int
	for address, reachable := range coordinators {
		pAddr, err := fdbv1beta2.ParseProcessAddress(address)
		if err != nil {
			return nil, err
		}

		status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators, fdbv1beta2.FoundationDBStatusCoordinator{
			Address:   pAddr,
			Reachable: reachable,
		})

		if reachable {
			countReachableCoordinators++
		}
	}

	minReachableCoordinators := (client.Cluster.DesiredCoordinatorCount() + 1) / 2
	status.Client.Coordinators.QuorumReachable = countReachableCoordinators >= minReachableCoordinators
	status.Client.DatabaseStatus.Available = true
	status.Client.DatabaseStatus.Healthy = true

	if client.DatabaseConfiguration == nil {
		status.Cluster.Layers.Error = "configurationMissing"
	} else {
		status.Cluster.DatabaseConfiguration = *client.DatabaseConfiguration
	}

	if status.Cluster.DatabaseConfiguration.LogSpill == 0 {
		status.Cluster.DatabaseConfiguration.VersionFlags.LogSpill = 2
	}

	if len(client.ExcludedAddresses) > 0 {
		status.Cluster.DatabaseConfiguration.ExcludedServers = make([]fdbv1beta2.ExcludedServers, 0, len(client.ExcludedAddresses))
	}
	for excludedAddresses := range client.ExcludedAddresses {
		if net.ParseIP(excludedAddresses) != nil {
			status.Cluster.DatabaseConfiguration.ExcludedServers = append(status.Cluster.DatabaseConfiguration.ExcludedServers, fdbv1beta2.ExcludedServers{Address: excludedAddresses})
		} else {
			status.Cluster.DatabaseConfiguration.ExcludedServers = append(status.Cluster.DatabaseConfiguration.ExcludedServers, fdbv1beta2.ExcludedServers{Locality: excludedAddresses})
		}
	}

	if client.Cluster.Status.RunningVersion != "" {
		parsedVersion, err := fdbv1beta2.ParseFdbVersion(client.Cluster.Status.RunningVersion)
		if err != nil {
			return nil, err
		}

		if parsedVersion.SupportsStorageMigrationConfiguration() {
			if status.Cluster.DatabaseConfiguration.StorageMigrationType == nil {
				wiggleTypeDisabled := fdbv1beta2.StorageMigrationTypeDisabled
				status.Cluster.DatabaseConfiguration.StorageMigrationType = &wiggleTypeDisabled
			}

			if status.Cluster.DatabaseConfiguration.PerpetualStorageWiggleEngine == nil {
				storageEngineNone := fdbv1beta2.StorageEngineNone
				status.Cluster.DatabaseConfiguration.PerpetualStorageWiggleEngine = &storageEngineNone
			}

			if status.Cluster.DatabaseConfiguration.PerpetualStorageWiggleLocality == nil {
				status.Cluster.DatabaseConfiguration.PerpetualStorageWiggleLocality = pointer.String("0")
			}
		}
	}

	status.Cluster.FullReplication = true
	status.Cluster.Data.State.Healthy = true
	status.Cluster.Data.State.Name = "healthy"
	if len(client.TeamTracker) == 0 {
		status.Cluster.Data.TeamTrackers = []fdbv1beta2.FoundationDBStatusTeamTracker{
			{
				Primary: true,
				State: fdbv1beta2.FoundationDBStatusDataState{
					Description:          "primary",
					Healthy:              true,
					MinReplicasRemaining: fdbv1beta2.MinimumFaultDomains(client.Cluster.Spec.DatabaseConfiguration.RedundancyMode),
				},
			},
		}

		if len(client.Cluster.Spec.DatabaseConfiguration.Regions) > 1 {
			status.Cluster.Data.TeamTrackers = append(status.Cluster.Data.TeamTrackers, fdbv1beta2.FoundationDBStatusTeamTracker{
				Primary: false,
				State: fdbv1beta2.FoundationDBStatusDataState{
					Description:          "remote",
					Healthy:              true,
					MinReplicasRemaining: fdbv1beta2.MinimumFaultDomains(client.Cluster.Spec.DatabaseConfiguration.RedundancyMode),
				},
			})
		}
	} else {
		status.Cluster.Data.TeamTrackers = client.TeamTracker
	}

	if len(client.Logs) == 0 {
		logInformation := fdbv1beta2.FoundationDBStatusLogInfo{
			Current:              true,
			LogFaultTolerance:    2,
			LogReplicationFactor: 3,
		}

		if len(client.Cluster.Spec.DatabaseConfiguration.Regions) > 1 {
			logInformation.RemoteLogFaultTolerance = 2
			logInformation.RemoteLogReplicationFactor = 3
			logInformation.SatelliteLogFaultTolerance = 1
			logInformation.SatelliteLogReplicationFactor = 2
		}
		status.Cluster.Logs = []fdbv1beta2.FoundationDBStatusLogInfo{
			logInformation,
		}
	} else {
		status.Cluster.Logs = client.Logs
	}

	if len(client.Backups) > 0 {
		status.Cluster.Layers.Backup.Tags = make(map[string]fdbv1beta2.FoundationDBStatusBackupTag, len(client.Backups))
		for tag, tagStatus := range client.Backups {
			status.Cluster.Layers.Backup.Tags[tag] = fdbv1beta2.FoundationDBStatusBackupTag{
				CurrentContainer: tagStatus.URL,
				RunningBackup:    tagStatus.Running,
				Restorable:       true,
			}
			status.Cluster.Layers.Backup.Paused = tagStatus.Paused
		}
	}
	faultToleranceSubtractor := 0
	if client.MaintenanceZone != "" {
		faultToleranceSubtractor = 1
	}
	if client.MaxZoneFailuresWithoutLosingData == nil {
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData = client.Cluster.DesiredFaultTolerance() - faultToleranceSubtractor
	}

	if client.MaxZoneFailuresWithoutLosingAvailability == nil {
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability = client.Cluster.DesiredFaultTolerance() - faultToleranceSubtractor
	}
	status.Cluster.MaintenanceZone = client.MaintenanceZone

	if len(client.LagInfo) > 0 {
		limitingDurabilityLag, ok := client.GetLimitingDurabilityLag()
		if ok {
			status.Cluster.Qos.LimitingDurabilityLagStorageServer = limitingDurabilityLag
		}

		worstDataLag, ok := client.GetWorstDataLag()
		if ok {
			status.Cluster.Qos.WorstDataLagStorageServer = worstDataLag
		}

		worstDurabilityLag, ok := client.GetWorstDurabilityLag()
		if ok {
			status.Cluster.Qos.WorstDurabilityLagStorageServer = worstDurabilityLag
		}
	}

	status.Cluster.RecoveryState = fdbv1beta2.RecoveryState{
		Name:                      "fully_recovered",
		SecondsSinceLastRecovered: 600.0,
		ActiveGenerations:         1,
	}

	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *AdminClient) ConfigureDatabase(configuration fdbv1beta2.DatabaseConfiguration, _ bool, version string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	client.DatabaseConfiguration = configuration.DeepCopy()

	ver, err := fdbv1beta2.ParseFdbVersion(version)
	if err != nil {
		return err
	}

	if !ver.HasSeparatedProxies() {
		client.DatabaseConfiguration.GrvProxies = 0
		client.DatabaseConfiguration.CommitProxies = 0
	}

	if ver.SupportsStorageMigrationConfiguration() {
		if configuration.StorageMigrationType == nil {
			migrationType := fdbv1beta2.StorageMigrationTypeDisabled
			client.DatabaseConfiguration.StorageMigrationType = &migrationType
		}

		if configuration.PerpetualStorageWiggleEngine == nil {
			storageEngineNone := fdbv1beta2.StorageEngineNone
			client.DatabaseConfiguration.PerpetualStorageWiggleEngine = &storageEngineNone
		}

		if configuration.PerpetualStorageWiggleLocality == nil {
			client.DatabaseConfiguration.PerpetualStorageWiggleLocality = pointer.String("0")
		}
	}

	return nil
}

// ExcludeProcesses starts evacuating processes so that they can be removed
// from the database.
func (client *AdminClient) ExcludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	for _, pAddr := range addresses {
		address := pAddr.String()
		client.ExcludedAddresses[address] = fdbv1beta2.None{}
	}

	return nil
}

// ExcludeProcessesWithNoWait starts evacuating processes so that they can be removed from the database. If noWait is
// set to true, the exclude command will not block until all data is moved away from the processes.
func (client *AdminClient) ExcludeProcessesWithNoWait(addresses []fdbv1beta2.ProcessAddress, _ bool) error {
	return client.ExcludeProcesses(addresses)
}

// IncludeProcesses removes processes from the exclusion list and allows
// them to take on roles again.
func (client *AdminClient) IncludeProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for _, address := range addresses {
		addr := address.String()
		_, ok := client.ExcludedAddresses[addr]
		if ok {
			client.ReincludedAddresses[addr] = true
			delete(client.ExcludedAddresses, addr)
		}
	}

	return nil
}

// CanSafelyRemove checks whether it is safe to remove the process group from the
// cluster
//
// The list returned by this method will be the addresses that are *not*
// safe to remove.
func (client *AdminClient) CanSafelyRemove(addresses []fdbv1beta2.ProcessAddress) ([]fdbv1beta2.ProcessAddress, error) {
	if client.mockError != nil {
		return nil, client.mockError
	}

	skipExclude := map[string]fdbv1beta2.None{}

	// Check which process groups have the skip exclusion flag or are already
	// excluded
	for _, pg := range client.Cluster.Status.ProcessGroups {
		if !(pg.IsExcluded()) {
			continue
		}

		for _, addr := range pg.Addresses {
			skipExclude[addr] = fdbv1beta2.None{}
		}
	}

	// Add all process groups that are excluded in the client
	for addr := range client.ExcludedAddresses {
		skipExclude[addr] = fdbv1beta2.None{}
	}

	// Filter out all excluded process groups and also all process groups
	// that skip exclusion
	remaining := make([]fdbv1beta2.ProcessAddress, 0, len(addresses))

	for _, addr := range addresses {
		// Is already excluded or skipped
		if _, ok := skipExclude[addr.String()]; ok {
			continue
		}

		remaining = append(remaining, addr)
	}

	return remaining, nil
}

// GetExclusions gets a list of the addresses currently excluded from the
// database.
func (client *AdminClient) GetExclusions() ([]fdbv1beta2.ProcessAddress, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return nil, client.mockError
	}

	pAddrs := make([]fdbv1beta2.ProcessAddress, 0, len(client.ExcludedAddresses))
	for addr := range client.ExcludedAddresses {
		ip := net.ParseIP(addr)
		if ip == nil {
			pAddrs = append(pAddrs, fdbv1beta2.ProcessAddress{StringAddress: addr})
		} else {
			pAddrs = append(pAddrs, fdbv1beta2.ProcessAddress{
				IPAddress: net.ParseIP(addr),
				Port:      0,
				Flags:     nil,
			})
		}
	}
	return pAddrs, nil
}

// KillProcesses restarts processes
func (client *AdminClient) KillProcesses(addresses []fdbv1beta2.ProcessAddress) error {
	adminClientMutex.Lock()

	if client.mockError != nil {
		return client.mockError
	}

	for _, addr := range addresses {
		client.KilledAddresses[addr.String()] = fdbv1beta2.None{}
		// Remove the commandline from the cached status and let it be recomputed in the next GetStatus request.
		// This reflects that the commandline will only be updated if the processes are actually be restarted.
		delete(client.currentCommandLines, addr.StringWithoutFlags())
	}
	adminClientMutex.Unlock()

	if client.Cluster.Status.RunningVersion != client.Cluster.Spec.Version {
		// We have to do this in the mock client, in the real world the tryConnectionOptions in update_status,
		// will update the version.
		client.Cluster.Status.RunningVersion = client.Cluster.Spec.Version
		err := client.KubeClient.Status().Update(context.TODO(), client.Cluster)
		if err != nil {
			return err
		}
	}

	client.UnfreezeStatus()
	return nil
}

// KillProcessesForUpgrade restarts processes for upgrades, this will issue 2 kill commands to make sure all
// processes are restarted.
func (client *AdminClient) KillProcessesForUpgrade(addresses []fdbv1beta2.ProcessAddress) error {
	return client.KillProcesses(addresses)
}

// ChangeCoordinators changes the coordinator set
func (client *AdminClient) ChangeCoordinators(addresses []fdbv1beta2.ProcessAddress) (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return "", client.mockError
	}

	connectionString, err := fdbv1beta2.ParseConnectionString(client.Cluster.Status.ConnectionString)
	if err != nil {
		return "", err
	}
	err = connectionString.GenerateNewGenerationID()
	if err != nil {
		return "", err
	}
	newCoord := make([]string, len(addresses))
	for idx, coord := range addresses {
		newCoord[idx] = coord.String()
	}

	connectionString.Coordinators = newCoord
	return connectionString.String(), err
}

// GetConnectionString fetches the latest connection string.
func (client *AdminClient) GetConnectionString() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()
	if client.mockGetConnectionStringError != nil {
		return "", client.mockGetConnectionStringError
	}

	return client.Cluster.Status.ConnectionString, nil
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *AdminClient) VersionSupported(versionString string) (bool, error) {
	if client.mockError != nil {
		return false, client.mockError
	}

	version, err := fdbv1beta2.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}

	if !version.IsSupported() {
		return false, nil
	}

	return true, nil
}

// GetProtocolVersion determines the protocol version that is used by a
// version of FDB.
func (client *AdminClient) GetProtocolVersion(version string) (string, error) {
	if client.mockError != nil {
		return "", client.mockError
	}

	return version, nil
}

// StartBackup starts a new backup.
func (client *AdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	client.Backups["default"] = fdbv1beta2.FoundationDBBackupStatusBackupDetails{
		URL:                   url,
		Running:               true,
		SnapshotPeriodSeconds: snapshotPeriodSeconds,
	}
	return nil
}

// PauseBackups pauses backups.
func (client *AdminClient) PauseBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	for tag, backup := range client.Backups {
		backup.Paused = true
		client.Backups[tag] = backup
	}
	return nil
}

// ResumeBackups resumes backups.
func (client *AdminClient) ResumeBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	for tag, backup := range client.Backups {
		backup.Paused = false
		client.Backups[tag] = backup
	}
	return nil
}

// ModifyBackup reconfigures the backup.
func (client *AdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	backup := client.Backups["default"]
	backup.SnapshotPeriodSeconds = snapshotPeriodSeconds
	client.Backups["default"] = backup
	return nil
}

// StopBackup stops a backup.
func (client *AdminClient) StopBackup(url string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	for tag, backup := range client.Backups {
		if backup.URL == url {
			backup.Running = false
			client.Backups[tag] = backup
			return nil
		}
	}
	return fmt.Errorf("no backup found for URL %s", url)
}

// GetBackupStatus gets the status of the current backup.
func (client *AdminClient) GetBackupStatus() (*fdbv1beta2.FoundationDBLiveBackupStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return nil, client.mockError
	}

	status := &fdbv1beta2.FoundationDBLiveBackupStatus{}

	tag := "default"
	backup, present := client.Backups[tag]
	if present {
		status.DestinationURL = backup.URL
		status.Status.Running = backup.Running
		status.BackupAgentsPaused = backup.Paused
		status.SnapshotIntervalSeconds = backup.SnapshotPeriodSeconds
	}

	return status, nil
}

// StartRestore starts a new restore.
func (client *AdminClient) StartRestore(url string, _ []fdbv1beta2.FoundationDBKeyRange) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return client.mockError
	}

	client.restoreURL = url
	return nil
}

// GetRestoreStatus gets the status of the current restore.
func (client *AdminClient) GetRestoreStatus() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.mockError != nil {
		return "", client.mockError
	}

	return fmt.Sprintf("%s\n", client.restoreURL), nil
}

// MockClientVersion returns a mocked client version
func (client *AdminClient) MockClientVersion(version string, clients []string) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.clientVersions == nil {
		client.clientVersions = make(map[string][]string)
	}
	client.clientVersions[version] = clients
}

// MockAdditionalProcesses adds additional processes to the cluster status.
func (client *AdminClient) MockAdditionalProcesses(processes []fdbv1beta2.ProcessGroupStatus) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.additionalProcesses = append(client.additionalProcesses, processes...)
}

// MockMissingProcessGroup updates the mock for whether a process group should
// be missing from the cluster status.
func (client *AdminClient) MockMissingProcessGroup(processGroupID fdbv1beta2.ProcessGroupID, missing bool) {
	if missing {
		client.missingProcessGroups[processGroupID] = fdbv1beta2.None{}
		return
	}

	delete(client.missingProcessGroups, processGroupID)
}

// MockLocalityInfo sets mock locality information for a process.
func (client *AdminClient) MockLocalityInfo(processGroupID fdbv1beta2.ProcessGroupID, locality map[string]string) {
	client.localityInfo[processGroupID] = locality
}

// MockIncorrectCommandLine updates the mock for whether a process group should
// be have an incorrect command-line.
func (client *AdminClient) MockIncorrectCommandLine(processGroupID fdbv1beta2.ProcessGroupID, incorrect bool) {
	if incorrect {
		client.incorrectCommandLines[processGroupID] = fdbv1beta2.None{}
		return
	}

	delete(client.incorrectCommandLines, processGroupID)
}

// MockMissingLocalities updates the mock to remove the localities for the provided process group.
func (client *AdminClient) MockMissingLocalities(processGroupID fdbv1beta2.ProcessGroupID, missingLocalities bool) {
	if missingLocalities {
		client.missingLocalities[processGroupID] = fdbv1beta2.None{}
		return
	}

	delete(client.missingLocalities, processGroupID)
}

// Close shuts down any resources for the client once it is no longer
// needed.
func (client *AdminClient) Close() error {
	return client.mockError
}

// FreezeStatus causes the GetStatus method to return its current value until
// UnfreezeStatus is called, or another method is called which would invalidate
// the status.
func (client *AdminClient) FreezeStatus() error {
	if client.mockError != nil {
		return client.mockError
	}

	status, err := client.GetStatus()
	if err != nil {
		return err
	}

	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.FrozenStatus = status
	return nil
}

// UnfreezeStatus causes the admin client to start recalculating the status
// on every call to GetStatus
func (client *AdminClient) UnfreezeStatus() {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.FrozenStatus = nil
}

// GetCoordinatorSet gets the current coordinators from the status
func (client *AdminClient) GetCoordinatorSet() (map[string]fdbv1beta2.None, error) {
	if client.mockError != nil {
		return nil, client.mockError
	}

	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	return fdbstatus.GetCoordinatorsFromStatus(status), nil
}

// SetKnobs sets the knobs that should be used for the commandline call.
func (client *AdminClient) SetKnobs(knobs []string) {
	client.Knobs = make(map[string]fdbv1beta2.None, len(knobs))
	for _, knob := range knobs {
		client.Knobs[knob] = fdbv1beta2.None{}
	}
}

// GetMaintenanceZone gets current maintenance zone, if any
func (client *AdminClient) GetMaintenanceZone() (string, error) {
	if client.mockError != nil {
		return "", client.mockError
	}

	return string(client.MaintenanceZone), nil
}

// SetMaintenanceZone places zone into maintenance mode
func (client *AdminClient) SetMaintenanceZone(zone string, _ int) error {
	if client.mockError != nil {
		return client.mockError
	}

	client.MaintenanceZone = fdbv1beta2.FaultDomain(zone)
	client.maintenanceZoneStartTimestamp = time.Now()
	return nil
}

// ResetMaintenanceMode resets the maintenance zone
func (client *AdminClient) ResetMaintenanceMode() error {
	if client.mockError != nil {
		return client.mockError
	}

	client.MaintenanceZone = ""
	return nil
}

// MockUptimeSecondsForMaintenanceZone mocks the uptime for maintenance zone
func (client *AdminClient) MockUptimeSecondsForMaintenanceZone(seconds float64) {
	client.uptimeSecondsForMaintenanceZone = seconds
}

// MockError mocks an error that will be returned when making any calls to the mock client. This can be reset by passing
// a nil value to this method.
func (client *AdminClient) MockError(err error) {
	client.mockError = err
}

// MockGetConnectionStringError mocks an error that will be returned by GetConnectionString()
func (client *AdminClient) MockGetConnectionStringError(err error) {
	client.mockGetConnectionStringError = err
}

// SetLimitingDurabilityLag sets/mocks the limiting durability lag of any storage server in the cluster.
func (client *AdminClient) SetLimitingDurabilityLag(lagInfo *fdbv1beta2.FoundationDBStatusLagInfo) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.LagInfo["limitingDurabilityLag"] = *lagInfo
}

// GetLimitingDurabilityLag returns the limiting durability lag of any storage server in the cluster.
func (client *AdminClient) GetLimitingDurabilityLag() (fdbv1beta2.FoundationDBStatusLagInfo, bool) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	lagInfo, ok := client.LagInfo["limitingDurabilityLag"]
	return lagInfo, ok
}

// SetWorstDataLag sets/mocks the worst data lag of any storage server in the cluster.
func (client *AdminClient) SetWorstDataLag(lagInfo *fdbv1beta2.FoundationDBStatusLagInfo) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.LagInfo["worstDataLag"] = *lagInfo
}

// GetWorstDataLag returns the (mocked) worst data lag of any storage server in the cluster.
func (client *AdminClient) GetWorstDataLag() (fdbv1beta2.FoundationDBStatusLagInfo, bool) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	lagInfo, ok := client.LagInfo["worstDataLag"]
	return lagInfo, ok
}

// SetWorstDurabilityLag sets/mocks the worst durability lag of any storage server in the cluster.
func (client *AdminClient) SetWorstDurabilityLag(lagInfo *fdbv1beta2.FoundationDBStatusLagInfo) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.LagInfo["worstDurabilityLag"] = *lagInfo
}

// GetWorstDurabilityLag returns the (mocked) worst durability lag of any storage server in the cluster.
func (client *AdminClient) GetWorstDurabilityLag() (fdbv1beta2.FoundationDBStatusLagInfo, bool) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	lagInfo, ok := client.LagInfo["worstDurabilityLag"]
	return lagInfo, ok
}

// WithValues will update the logger used by the current AdminClient to contain the provided key value pairs. The provided
// arguments must be even.
func (client *AdminClient) WithValues(_ ...interface{}) {}

// SetTimeout will overwrite the default timeout for interacting the FDB cluster.
func (client *AdminClient) SetTimeout(_ time.Duration) {}

// GetProcessesUnderMaintenance will return all process groups that are currently stored to be under maintenance.
// The result is a map with the process group ID as key and the start of the maintenance as value.
func (client *AdminClient) GetProcessesUnderMaintenance() (map[fdbv1beta2.ProcessGroupID]int64, error) {
	// We have to create a copy here as the map will be a pointer.
	res := make(map[fdbv1beta2.ProcessGroupID]int64, len(client.processesUnderMaintenance))

	for processGroupID, timestamp := range client.processesUnderMaintenance {
		res[processGroupID] = timestamp
	}

	return res, nil
}

// RemoveProcessesUnderMaintenance will remove the provided process groups from the list of processes that
// are planned to be taken down for maintenance.
func (client *AdminClient) RemoveProcessesUnderMaintenance(ids []fdbv1beta2.ProcessGroupID) error {
	for _, id := range ids {
		delete(client.processesUnderMaintenance, id)
	}

	return nil
}

// SetProcessesUnderMaintenance will add the provided process groups to the list of processes that will be taken
// down for maintenance. The value will be the provided time stamp.
func (client *AdminClient) SetProcessesUnderMaintenance(ids []fdbv1beta2.ProcessGroupID, timestamp int64) error {
	for _, id := range ids {
		client.processesUnderMaintenance[id] = timestamp
	}

	return nil
}

// GetVersionFromReachableCoordinators will return the running version based on the reachable coordinators. This method
// can be used during version incompatible upgrades and based on the responses of the coordinators, this method will
// assume the current running version of the cluster. If the fdbcli calls for none of the provided version return
// a majority of reachable coordinators, the default version from the cluster.Status.RunningVersion will be returned.
func (client *AdminClient) GetVersionFromReachableCoordinators() string {
	// TODO (johscheuer): In the future we could allow to mock another running version.
	return client.Cluster.Status.RunningVersion
}
