/*
 * admin_client_mock.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"
	"net"
	"strings"
	"sync"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockAdminClient provides a mock implementation of the cluster admin interface
type mockAdminClient struct {
	Cluster                                  *fdbtypes.FoundationDBCluster
	KubeClient                               client.Client
	DatabaseConfiguration                    *fdb.DatabaseConfiguration
	ExcludedAddresses                        []string
	ReincludedAddresses                      map[string]bool
	KilledAddresses                          []string
	frozenStatus                             *fdb.FoundationDBStatus
	Backups                                  map[string]fdbtypes.FoundationDBBackupStatusBackupDetails
	restoreURL                               string
	clientVersions                           map[string][]string
	missingProcessGroups                     map[string]bool
	additionalProcesses                      []fdbtypes.ProcessGroupStatus
	localityInfo                             map[string]map[string]string
	incorrectCommandLines                    map[string]bool
	maxZoneFailuresWithoutLosingData         *int
	maxZoneFailuresWithoutLosingAvailability *int
	knobs                                    []string
}

// adminClientCache provides a cache of mock admin clients.
var adminClientCache = make(map[string]*mockAdminClient)
var adminClientMutex sync.Mutex

// newMockAdminClient creates an admin client for a cluster.
func newMockAdminClient(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (fdbadminclient.AdminClient, error) {
	return newMockAdminClientUncast(cluster, kubeClient)
}

// newMockAdminClientUncast creates a mock admin client for a cluster.
// nolint:unparam
// is required because we always return a nil error
func newMockAdminClientUncast(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*mockAdminClient, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	cachedClient := adminClientCache[cluster.Name]

	if cachedClient == nil {
		cachedClient = &mockAdminClient{
			Cluster:              cluster.DeepCopy(),
			KubeClient:           kubeClient,
			ReincludedAddresses:  make(map[string]bool),
			missingProcessGroups: make(map[string]bool),
			localityInfo:         make(map[string]map[string]string),
		}
		adminClientCache[cluster.Name] = cachedClient
		cachedClient.Backups = make(map[string]fdbtypes.FoundationDBBackupStatusBackupDetails)
	} else {
		cachedClient.Cluster = cluster.DeepCopy()
	}

	return cachedClient, nil
}

// clearMockAdminClients clears the cache of mock Admin clients
func clearMockAdminClients() {
	adminClientCache = map[string]*mockAdminClient{}
}

// GetStatus gets the database's status
func (client *mockAdminClient) GetStatus() (*fdb.FoundationDBStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.frozenStatus != nil {
		return client.frozenStatus, nil
	}
	pods := &corev1.PodList{}
	err := client.KubeClient.List(context.TODO(), pods)
	if err != nil {
		return nil, err
	}
	status := &fdb.FoundationDBStatus{
		Cluster: fdb.FoundationDBStatusClusterInfo{
			Processes: make(map[string]fdb.FoundationDBStatusProcessInfo, len(pods.Items)),
		},
	}

	coordinators := make(map[string]bool)
	var coordinatorAddresses []string
	if strings.Contains(client.Cluster.Status.ConnectionString, "@") {
		coordinatorAddresses = strings.Split(strings.Split(client.Cluster.Status.ConnectionString, "@")[1], ",")
	} else {
		coordinatorAddresses = []string{}
	}

	for _, address := range coordinatorAddresses {
		coordinators[address] = false
	}

	exclusionMap := make(map[string]bool, len(client.ExcludedAddresses))
	for _, address := range client.ExcludedAddresses {
		exclusionMap[address] = true
	}

	for _, pod := range pods.Items {
		podClient, _ := internal.NewMockFdbPodClient(client.Cluster, &pod)

		processCount, err := internal.GetStorageServersPerPodForPod(&pod)
		if err != nil {
			return nil, err
		}

		processGroupID := podmanager.GetProcessGroupID(client.Cluster, &pod)

		if client.missingProcessGroups[processGroupID] {
			continue
		}

		subs, err := podClient.GetVariableSubstitutions()
		if err != nil {
			return nil, err
		}

		processIP, ok := subs["FDB_PUBLIC_IP"]
		if !ok {
			processIP = pod.Status.PodIP
		}

		for processIndex := 1; processIndex <= processCount; processIndex++ {
			var fdbRoles []fdb.FoundationDBStatusProcessRoleInfo

			fullAddress := client.Cluster.GetFullAddress(processIP, processIndex)
			_, ipExcluded := exclusionMap[processIP]
			_, addressExcluded := exclusionMap[fullAddress.String()]
			excluded := ipExcluded || addressExcluded
			_, isCoordinator := coordinators[fullAddress.String()]
			if isCoordinator && !excluded {
				coordinators[fullAddress.String()] = true
				fdbRoles = append(fdbRoles, fdb.FoundationDBStatusProcessRoleInfo{Role: string(fdb.ProcessRoleCoordinator)})
			}

			pClass, err := podmanager.GetProcessClass(client.Cluster, &pod)
			if err != nil {
				return nil, err
			}

			command, err := internal.GetStartCommand(client.Cluster, pClass, podClient, processIndex, processCount)
			if err != nil {
				return nil, err
			}
			if client.incorrectCommandLines != nil && client.incorrectCommandLines[processGroupID] {
				command += " --locality_incorrect=1"
			}

			locality := map[string]string{
				fdb.FDBLocalityInstanceIDKey: processGroupID,
				fdb.FDBLocalityZoneIDKey:     pod.Name,
				fdb.FDBLocalityDCIDKey:       client.Cluster.Spec.DataCenter,
			}

			for key, value := range client.localityInfo[processGroupID] {
				locality[key] = value
			}

			for _, container := range pod.Spec.Containers {
				for _, envVar := range container.Env {
					if envVar.Name == "FDB_DNS_NAME" {
						locality[fdb.FDBLocalityDNSNameKey] = envVar.Value
					}
				}
			}

			if processCount > 1 {
				locality["process_id"] = fmt.Sprintf("%s-%d", processGroupID, processIndex)
			}

			status.Cluster.Processes[fmt.Sprintf("%s-%d", pod.Name, processIndex)] = fdb.FoundationDBStatusProcessInfo{
				Address:       fullAddress,
				ProcessClass:  internal.GetProcessClassFromMeta(client.Cluster, pod.ObjectMeta),
				CommandLine:   command,
				Excluded:      excluded,
				Locality:      locality,
				Version:       client.Cluster.Status.RunningVersion,
				UptimeSeconds: 60000,
				Roles:         fdbRoles,
			}
		}

		for _, processGroup := range client.additionalProcesses {
			locality := map[string]string{
				fdb.FDBLocalityInstanceIDKey: processGroup.ProcessGroupID,
				fdb.FDBLocalityZoneIDKey:     processGroup.ProcessGroupID,
			}

			for key, value := range client.localityInfo[processGroupID] {
				locality[key] = value
			}

			fullAddress := client.Cluster.GetFullAddress(processGroup.Addresses[0], 1)
			status.Cluster.Processes[processGroup.ProcessGroupID] = fdb.FoundationDBStatusProcessInfo{
				Address:       fullAddress,
				ProcessClass:  processGroup.ProcessClass,
				Locality:      locality,
				Version:       client.Cluster.Status.RunningVersion,
				UptimeSeconds: 60000,
			}

		}
	}

	if client.clientVersions != nil {
		supportedVersions := make([]fdb.FoundationDBStatusSupportedVersion, 0, len(client.clientVersions))
		for version, addresses := range client.clientVersions {
			protocolVersion, err := client.GetProtocolVersion(version)
			if err != nil {
				return nil, err
			}

			protocolClients := make([]fdb.FoundationDBStatusConnectedClient, 0, len(addresses))
			for _, address := range addresses {
				protocolClients = append(protocolClients, fdb.FoundationDBStatusConnectedClient{
					Address: address,
				})
			}

			supportedVersions = append(supportedVersions, fdb.FoundationDBStatusSupportedVersion{
				ClientVersion:      version,
				ProtocolVersion:    protocolVersion,
				MaxProtocolClients: protocolClients,
			})
		}
		status.Cluster.Clients.SupportedVersions = supportedVersions
	}

	for address, reachable := range coordinators {
		pAddr, err := fdb.ParseProcessAddress(address)
		if err != nil {
			return nil, err
		}

		status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators, fdb.FoundationDBStatusCoordinator{
			Address:   pAddr,
			Reachable: reachable,
		})
	}

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

	status.Cluster.FullReplication = true
	status.Cluster.Data.State.Healthy = true
	status.Cluster.Data.State.Name = "healthy"

	if len(client.Backups) > 0 {
		status.Cluster.Layers.Backup.Tags = make(map[string]fdb.FoundationDBStatusBackupTag, len(client.Backups))
		for tag, tagStatus := range client.Backups {
			status.Cluster.Layers.Backup.Tags[tag] = fdb.FoundationDBStatusBackupTag{
				CurrentContainer: tagStatus.URL,
				RunningBackup:    tagStatus.Running,
				Restorable:       true,
			}
			status.Cluster.Layers.Backup.Paused = tagStatus.Paused
		}
	}

	if client.maxZoneFailuresWithoutLosingData == nil {
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingData = client.Cluster.DesiredFaultTolerance()
	}

	if client.maxZoneFailuresWithoutLosingAvailability == nil {
		status.Cluster.FaultTolerance.MaxZoneFailuresWithoutLosingAvailability = client.Cluster.DesiredFaultTolerance()
	}

	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *mockAdminClient) ConfigureDatabase(configuration fdb.DatabaseConfiguration, newDatabase bool) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.DatabaseConfiguration = configuration.DeepCopy()
	return nil
}

// ExcludeProcesses starts evacuating processes so that they can be removed
// from the database.
func (client *mockAdminClient) ExcludeProcesses(addresses []fdb.ProcessAddress) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	count := len(addresses) + len(client.ExcludedAddresses)
	exclusionMap := make(map[string]bool, count)
	newExclusions := make([]string, 0, count)
	for _, pAddr := range addresses {
		address := pAddr.String()
		if !exclusionMap[address] {
			exclusionMap[address] = true
			newExclusions = append(newExclusions, address)
		}
	}
	for _, address := range client.ExcludedAddresses {
		if !exclusionMap[address] {
			exclusionMap[address] = true
			newExclusions = append(newExclusions, address)
		}
	}
	if len(newExclusions) == 0 {
		newExclusions = nil
	}
	client.ExcludedAddresses = newExclusions
	return nil
}

// IncludeProcesses removes processes from the exclusion list and allows
// them to take on roles again.
func (client *mockAdminClient) IncludeProcesses(addresses []fdb.ProcessAddress) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	newExclusions := make([]string, 0, len(client.ExcludedAddresses))
	for _, excludedAddress := range client.ExcludedAddresses {
		included := false
		for _, address := range addresses {
			if address.String() == excludedAddress {
				included = true
				client.ReincludedAddresses[address.String()] = true
				break
			}
		}
		if !included {
			newExclusions = append(newExclusions, excludedAddress)
		}
	}
	if len(newExclusions) == 0 {
		newExclusions = nil
	}
	client.ExcludedAddresses = newExclusions
	return nil
}

// CanSafelyRemove checks whether it is safe to remove the process group from the
// cluster
//
// The list returned by this method will be the addresses that are *not*
// safe to remove.
func (client *mockAdminClient) CanSafelyRemove(addresses []fdb.ProcessAddress) ([]fdb.ProcessAddress, error) {
	skipExclude := map[string]fdb.None{}

	// Check which process groups have the skip exclusion flag or are already
	// excluded
	for _, pg := range client.Cluster.Status.ProcessGroups {
		if !(pg.ExclusionSkipped || pg.Excluded) {
			continue
		}

		for _, addr := range pg.Addresses {
			skipExclude[addr] = fdb.None{}
		}
	}

	// Add all process groups that are excluded in the client
	for _, addr := range client.ExcludedAddresses {
		skipExclude[addr] = fdb.None{}
	}

	// Filter out all excluded process groups and also all process groups
	// that skip exclusion
	remaining := make([]fdb.ProcessAddress, 0, len(addresses))

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
func (client *mockAdminClient) GetExclusions() ([]fdb.ProcessAddress, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	pAddrs := make([]fdb.ProcessAddress, len(client.ExcludedAddresses))
	for _, addr := range client.ExcludedAddresses {
		pAddrs = append(pAddrs, fdb.ProcessAddress{
			IPAddress: net.ParseIP(addr),
			Port:      0,
			Flags:     nil,
		})
	}

	return pAddrs, nil
}

// KillProcesses restarts processes
func (client *mockAdminClient) KillProcesses(addresses []fdb.ProcessAddress) error {
	adminClientMutex.Lock()
	for _, addr := range addresses {
		client.KilledAddresses = append(client.KilledAddresses, addr.String())
	}
	adminClientMutex.Unlock()

	client.UnfreezeStatus()
	return nil
}

// ChangeCoordinators changes the coordinator set
func (client *mockAdminClient) ChangeCoordinators(addresses []fdb.ProcessAddress) (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	connectionString, err := fdbtypes.ParseConnectionString(client.Cluster.Status.ConnectionString)
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
func (client *mockAdminClient) GetConnectionString() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return client.Cluster.Status.ConnectionString, nil
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *mockAdminClient) VersionSupported(versionString string) (bool, error) {
	version, err := fdb.ParseFdbVersion(versionString)
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
func (client *mockAdminClient) GetProtocolVersion(version string) (string, error) {
	return version, nil
}

// StartBackup starts a new backup.
func (client *mockAdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.Backups["default"] = fdbtypes.FoundationDBBackupStatusBackupDetails{
		URL:                   url,
		Running:               true,
		SnapshotPeriodSeconds: snapshotPeriodSeconds,
	}
	return nil
}

// PauseBackups pauses backups.
func (client *mockAdminClient) PauseBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		backup.Paused = true
		client.Backups[tag] = backup
	}
	return nil
}

// ResumeBackups resumes backups.
func (client *mockAdminClient) ResumeBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		backup.Paused = false
		client.Backups[tag] = backup
	}
	return nil
}

// ModifyBackup reconfigures the backup.
func (client *mockAdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	backup := client.Backups["default"]
	backup.SnapshotPeriodSeconds = snapshotPeriodSeconds
	client.Backups["default"] = backup
	return nil
}

// StopBackup stops a backup.
func (client *mockAdminClient) StopBackup(url string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		if backup.URL == url {
			backup.Running = false
			client.Backups[tag] = backup
			return nil
		}
	}
	return fmt.Errorf("No backup found for URL %s", url)
}

// GetBackupStatus gets the status of the current backup.
func (client *mockAdminClient) GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	status := &fdbtypes.FoundationDBLiveBackupStatus{}

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
func (client *mockAdminClient) StartRestore(url string, keyRanges []fdbtypes.FoundationDBKeyRange) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.restoreURL = url
	return nil
}

// GetRestoreStatus gets the status of the current restore.
func (client *mockAdminClient) GetRestoreStatus() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return fmt.Sprintf("%s\n", client.restoreURL), nil
}

// MockClientVersion returns a mocked client version
func (client *mockAdminClient) MockClientVersion(version string, clients []string) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.clientVersions == nil {
		client.clientVersions = make(map[string][]string)
	}
	client.clientVersions[version] = clients
}

// MockAdditionalProcesses adds additional processes to the cluster status.
func (client *mockAdminClient) MockAdditionalProcesses(processes []fdbtypes.ProcessGroupStatus) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.additionalProcesses = append(client.additionalProcesses, processes...)
}

// MockMissingProcessGroup updates the mock for whether a process group should
// be missing from the cluster status.
func (client *mockAdminClient) MockMissingProcessGroup(processGroupID string, missing bool) {
	client.missingProcessGroups[processGroupID] = missing
}

// MockLocalityInfo sets mock locality information for a process.
func (client *mockAdminClient) MockLocalityInfo(processGroupID string, locality map[string]string) {
	client.localityInfo[processGroupID] = locality
}

// MockIncorrectCommandLine updates the mock for whether a process group should
// be have an incorrect command-line.
func (client *mockAdminClient) MockIncorrectCommandLine(processGroupID string, incorrect bool) {
	if client.incorrectCommandLines == nil {
		client.incorrectCommandLines = make(map[string]bool)
	}
	client.incorrectCommandLines[processGroupID] = incorrect
}

// Close shuts down any resources for the client once it is no longer
// needed.
func (client *mockAdminClient) Close() error {
	return nil
}

// FreezeStatus causes the GetStatus method to return its current value until
// UnfreezeStatus is called, or another method is called which would invalidate
// the status.
func (client *mockAdminClient) FreezeStatus() error {
	status, err := client.GetStatus()
	if err != nil {
		return err
	}

	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.frozenStatus = status
	return nil
}

// UnfreezeStatus causes the admin client to start recalculating the status
// on every call to GetStatus
func (client *mockAdminClient) UnfreezeStatus() {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.frozenStatus = nil
}

// GetCoordinatorSet gets the current coordinators from the status
func (client *mockAdminClient) GetCoordinatorSet() (map[string]struct{}, error) {
	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	return internal.GetCoordinatorsFromStatus(status), nil
}

// SetKnobs sets the knobs that should be used for the commandline call.
func (client *mockAdminClient) SetKnobs(knobs []string) {
	client.knobs = knobs
}
