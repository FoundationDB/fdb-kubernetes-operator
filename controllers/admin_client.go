/*
 * admin_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2021 Apple Inc. and the FoundationDB project authors
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
	"net"
	"strings"
	"sync"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AdminClient describes an interface for running administrative commands on a
// cluster
type AdminClient interface {
	// GetStatus gets the database's status
	GetStatus() (*fdbtypes.FoundationDBStatus, error)

	// ConfigureDatabase sets the database configuration
	ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error

	// ExcludeInstances starts evacuating processes so that they can be removed
	// from the database.
	ExcludeInstances(addresses []string) error

	// IncludeInstances removes processes from the exclusion list and allows
	// them to take on roles again.
	IncludeInstances(addresses []string) error

	// GetExclusions gets a list of the addresses currently excluded from the
	// database.
	GetExclusions() ([]string, error)

	// CanSafelyRemove checks whether it is safe to remove processes from the
	// cluster.
	//
	// The list returned by this method will be the addresses that are *not*
	// safe to remove.
	CanSafelyRemove(addresses []string) ([]string, error)

	// KillProcesses restarts processes
	KillInstances(addresses []string) error

	// ChangeCoordinators changes the coordinator set
	ChangeCoordinators(addresses []string) (string, error)

	// GetConnectionString fetches the latest connection string.
	GetConnectionString() (string, error)

	// VersionSupported reports whether we can support a cluster with a given
	// version.
	VersionSupported(version string) (bool, error)

	// GetProtocolVersion determines the protocol version that is used by a
	// version of FDB.
	GetProtocolVersion(version string) (string, error)

	// StartBackup starts a new backup.
	StartBackup(url string, snapshotPeriodSeconds int) error

	// StopBackup stops a backup.
	StopBackup(url string) error

	// PauseBackups pauses the backups.
	PauseBackups() error

	// ResumeBackups resumes the backups.
	ResumeBackups() error

	// ModifyBackup modifies the configuration of the backup.
	ModifyBackup(int) error

	// GetBackupStatus gets the status of the current backup.
	GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error)

	// StartRestore starts a new restore.
	StartRestore(url string, keyRanges []fdbtypes.FoundationDBKeyRange) error

	// GetRestoreStatus gets the status of the current restore.
	GetRestoreStatus() (string, error)

	// Close shuts down any resources for the client once it is no longer
	// needed.
	Close() error

	// GetCoordinatorSet returns a set of the current coordinators.
	GetCoordinatorSet() (map[string]internal.None, error)
}

// MockAdminClient provides a mock implementation of the cluster admin interface
type MockAdminClient struct {
	Cluster               *fdbtypes.FoundationDBCluster
	KubeClient            client.Client
	DatabaseConfiguration *fdbtypes.DatabaseConfiguration
	ExcludedAddresses     []string
	ReincludedAddresses   map[string]bool
	KilledAddresses       []string
	frozenStatus          *fdbtypes.FoundationDBStatus
	Backups               map[string]fdbtypes.FoundationDBBackupStatusBackupDetails
	restoreURL            string
	clientVersions        map[string][]string
	missingProcessGroups  map[string]bool
	additionalProcesses   []fdbtypes.ProcessGroupStatus
	localityInfo          map[string]map[string]string
	incorrectCommandLines map[string]bool
}

// adminClientCache provides a cache of mock admin clients.
var adminClientCache = make(map[string]*MockAdminClient)
var adminClientMutex sync.Mutex

// NewMockAdminClient creates an admin client for a cluster.
func NewMockAdminClient(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (AdminClient, error) {
	return newMockAdminClientUncast(cluster, kubeClient)
}

// newMockAdminClientUncast creates a mock admin client for a cluster.
// nolint:unparam
// is required because we always return a nil error
func newMockAdminClientUncast(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*MockAdminClient, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client := adminClientCache[cluster.Name]

	if client == nil {
		client = &MockAdminClient{
			Cluster:              cluster.DeepCopy(),
			KubeClient:           kubeClient,
			ReincludedAddresses:  make(map[string]bool),
			missingProcessGroups: make(map[string]bool),
			localityInfo:         make(map[string]map[string]string),
		}
		adminClientCache[cluster.Name] = client
		client.Backups = make(map[string]fdbtypes.FoundationDBBackupStatusBackupDetails)
	} else {
		client.Cluster = cluster.DeepCopy()
	}
	return client, nil
}

// ClearMockAdminClients clears the cache of mock Admin clients
func ClearMockAdminClients() {
	adminClientCache = map[string]*MockAdminClient{}
}

// GetStatus gets the database's status
func (client *MockAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
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
	status := &fdbtypes.FoundationDBStatus{
		Cluster: fdbtypes.FoundationDBStatusClusterInfo{
			Processes: make(map[string]fdbtypes.FoundationDBStatusProcessInfo, len(pods.Items)),
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
		podClient := &mockFdbPodClient{Cluster: client.Cluster, Pod: &pod}

		processCount, err := getStorageServersPerPodForPod(&pod)
		if err != nil {
			return nil, err
		}

		instance := newFdbInstance(pod)
		instanceID := instance.GetInstanceID()

		if client.missingProcessGroups[instanceID] {
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
			var fdbRoles []fdbtypes.FoundationDBStatusProcessRoleInfo

			fullAddress := client.Cluster.GetFullAddress(processIP, processIndex)
			_, ipExcluded := exclusionMap[pod.Status.PodIP]
			_, addressExcluded := exclusionMap[fullAddress]
			excluded := ipExcluded || addressExcluded
			_, isCoordinator := coordinators[fullAddress]
			if isCoordinator && !excluded {
				coordinators[fullAddress] = true
				fdbRoles = append(fdbRoles, fdbtypes.FoundationDBStatusProcessRoleInfo{Role: string(fdbtypes.ProcessRoleCoordinator)})
			}
			command, err := GetStartCommand(client.Cluster, instance, podClient, processIndex, processCount)
			if err != nil {
				return nil, err
			}
			if client.incorrectCommandLines != nil && client.incorrectCommandLines[instance.GetInstanceID()] {
				command += " --locality_incorrect=1"
			}

			locality := map[string]string{
				fdbtypes.FDBLocalityInstanceIDKey: instance.GetInstanceID(),
				fdbtypes.FDBLocalityZoneIDKey:     pod.Name,
				fdbtypes.FDBLocalityDCIDKey:       client.Cluster.Spec.DataCenter,
			}

			for key, value := range client.localityInfo[instance.GetInstanceID()] {
				locality[key] = value
			}

			if processCount > 1 {
				locality["process_id"] = fmt.Sprintf("%s-%d", instance.GetInstanceID(), processIndex)
			}

			status.Cluster.Processes[fmt.Sprintf("%s-%d", pod.Name, processIndex)] = fdbtypes.FoundationDBStatusProcessInfo{
				Address:       fullAddress,
				ProcessClass:  internal.GetProcessClassFromMeta(pod.ObjectMeta),
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
				fdbtypes.FDBLocalityInstanceIDKey: processGroup.ProcessGroupID,
				fdbtypes.FDBLocalityZoneIDKey:     processGroup.ProcessGroupID,
			}

			for key, value := range client.localityInfo[instance.GetInstanceID()] {
				locality[key] = value
			}

			fullAddress := client.Cluster.GetFullAddress(processGroup.Addresses[0], 1)

			status.Cluster.Processes[processGroup.ProcessGroupID] = fdbtypes.FoundationDBStatusProcessInfo{
				Address:       fullAddress,
				ProcessClass:  processGroup.ProcessClass,
				Locality:      locality,
				Version:       client.Cluster.Status.RunningVersion,
				UptimeSeconds: 60000,
			}

		}
	}

	if client.clientVersions != nil {
		supportedVersions := make([]fdbtypes.FoundationDBStatusSupportedVersion, 0, len(client.clientVersions))
		for version, addresses := range client.clientVersions {
			protocolVersion, err := client.GetProtocolVersion(version)
			if err != nil {
				return nil, err
			}

			protocolClients := make([]fdbtypes.FoundationDBStatusConnectedClient, 0, len(addresses))
			for _, address := range addresses {
				protocolClients = append(protocolClients, fdbtypes.FoundationDBStatusConnectedClient{
					Address: address,
				})
			}

			supportedVersions = append(supportedVersions, fdbtypes.FoundationDBStatusSupportedVersion{
				ClientVersion:      version,
				ProtocolVersion:    protocolVersion,
				MaxProtocolClients: protocolClients,
			})
		}
		status.Cluster.Clients.SupportedVersions = supportedVersions
	}

	for address, reachable := range coordinators {
		status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators, fdbtypes.FoundationDBStatusCoordinator{
			Address:   address,
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
		status.Cluster.Layers.Backup.Tags = make(map[string]fdbtypes.FoundationDBStatusBackupTag, len(client.Backups))
		for tag, tagStatus := range client.Backups {
			status.Cluster.Layers.Backup.Tags[tag] = fdbtypes.FoundationDBStatusBackupTag{
				CurrentContainer: tagStatus.URL,
				RunningBackup:    tagStatus.Running,
				Restorable:       true,
			}
			status.Cluster.Layers.Backup.Paused = tagStatus.Paused
		}
	}

	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *MockAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.DatabaseConfiguration = configuration.DeepCopy()
	return nil
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *MockAdminClient) ExcludeInstances(addresses []string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	count := len(addresses) + len(client.ExcludedAddresses)
	exclusionMap := make(map[string]bool, count)
	newExclusions := make([]string, 0, count)
	for _, address := range addresses {
		if !isValidAddress(address) {
			return fmt.Errorf("Invalid exclusion address %s", address)
		}

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

func isValidAddress(address string) bool {
	return net.ParseIP(address) != nil
}

// IncludeInstances removes instances from the exclusion list and allows
// them to take on roles again.
func (client *MockAdminClient) IncludeInstances(addresses []string) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	newExclusions := make([]string, 0, len(client.ExcludedAddresses))
	for _, address := range addresses {
		if !isValidAddress(address) {
			return fmt.Errorf("Invalid exclusion address %s", address)
		}
	}
	for _, excludedAddress := range client.ExcludedAddresses {
		included := false
		for _, address := range addresses {
			if address == excludedAddress {
				included = true
				client.ReincludedAddresses[address] = true
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

// CanSafelyRemove checks whether it is safe to remove instances from the
// cluster
//
// The list returned by this method will be the addresses that are *not*
// safe to remove.
func (client *MockAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	return nil, nil
}

// GetExclusions gets a list of the addresses currently excluded from the
// database.
func (client *MockAdminClient) GetExclusions() ([]string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return client.ExcludedAddresses, nil
}

// KillInstances restarts processes
func (client *MockAdminClient) KillInstances(addresses []string) error {
	adminClientMutex.Lock()
	client.KilledAddresses = append(client.KilledAddresses, addresses...)
	adminClientMutex.Unlock()

	client.UnfreezeStatus()
	return nil
}

// ChangeCoordinators changes the coordinator set
func (client *MockAdminClient) ChangeCoordinators(addresses []string) (string, error) {
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
	connectionString.Coordinators = addresses
	return connectionString.String(), err
}

// GetConnectionString fetches the latest connection string.
func (client *MockAdminClient) GetConnectionString() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return client.Cluster.Status.ConnectionString, nil
}

// VersionSupported reports whether we can support a cluster with a given
// version.
func (client *MockAdminClient) VersionSupported(versionString string) (bool, error) {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return false, err
	}

	if !version.IsAtLeast(MinimumFDBVersion()) {
		return false, nil
	}

	return true, nil
}

// GetProtocolVersion determines the protocol version that is used by a
// version of FDB.
func (client *MockAdminClient) GetProtocolVersion(version string) (string, error) {
	return version, nil
}

// StartBackup starts a new backup.
func (client *MockAdminClient) StartBackup(url string, snapshotPeriodSeconds int) error {
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
func (client *MockAdminClient) PauseBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		backup.Paused = true
		client.Backups[tag] = backup
	}
	return nil
}

// ResumeBackups resumes backups.
func (client *MockAdminClient) ResumeBackups() error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	for tag, backup := range client.Backups {
		backup.Paused = false
		client.Backups[tag] = backup
	}
	return nil
}

// ModifyBackup reconfigures the backup.
func (client *MockAdminClient) ModifyBackup(snapshotPeriodSeconds int) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	backup := client.Backups["default"]
	backup.SnapshotPeriodSeconds = snapshotPeriodSeconds
	client.Backups["default"] = backup
	return nil
}

// StopBackup stops a backup.
func (client *MockAdminClient) StopBackup(url string) error {
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
func (client *MockAdminClient) GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error) {
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
func (client *MockAdminClient) StartRestore(url string, keyRanges []fdbtypes.FoundationDBKeyRange) error {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.restoreURL = url
	return nil
}

// GetRestoreStatus gets the status of the current restore.
func (client *MockAdminClient) GetRestoreStatus() (string, error) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	return fmt.Sprintf("%s\n", client.restoreURL), nil
}

// MockClientVersion returns a mocked client version
func (client *MockAdminClient) MockClientVersion(version string, clients []string) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	if client.clientVersions == nil {
		client.clientVersions = make(map[string][]string)
	}
	client.clientVersions[version] = clients
}

// MockAdditionalProcesses adds additional processes to the cluster status.
func (client *MockAdminClient) MockAdditionalProcesses(processes []fdbtypes.ProcessGroupStatus) {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.additionalProcesses = append(client.additionalProcesses, processes...)
}

// MockMissingProcessGroup updates the mock for whether a process group should
// be missing from the cluster status.
func (client *MockAdminClient) MockMissingProcessGroup(instanceID string, missing bool) {
	client.missingProcessGroups[instanceID] = missing
}

// MockLocalityInfo sets mock locality information for a process.
func (client *MockAdminClient) MockLocalityInfo(processGroupID string, locality map[string]string) {
	client.localityInfo[processGroupID] = locality
}

// MockIncorrectCommandLine updates the mock for whether a process group should
// be have an incorrect command-line.
func (client *MockAdminClient) MockIncorrectCommandLine(instanceID string, incorrect bool) {
	if client.incorrectCommandLines == nil {
		client.incorrectCommandLines = make(map[string]bool)
	}
	client.incorrectCommandLines[instanceID] = incorrect
}

// Close shuts down any resources for the client once it is no longer
// needed.
func (client *MockAdminClient) Close() error {
	return nil
}

// FreezeStatus causes the GetStatus method to return its current value until
// UnfreezeStatus is called, or another method is called which would invalidate
// the status.
func (client *MockAdminClient) FreezeStatus() error {
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
func (client *MockAdminClient) UnfreezeStatus() {
	adminClientMutex.Lock()
	defer adminClientMutex.Unlock()

	client.frozenStatus = nil
}

// GetCoordinatorSet gets the current coordinators from the status
func (client *MockAdminClient) GetCoordinatorSet() (map[string]internal.None, error) {
	status, err := client.GetStatus()
	if err != nil {
		return nil, err
	}

	return internal.GetCoordinatorsFromStatus(status), nil
}
