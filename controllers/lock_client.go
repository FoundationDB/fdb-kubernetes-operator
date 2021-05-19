/*
 * lock_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"sort"
	"sync"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// LockClient provides a client for getting locks on operations for a cluster.
type LockClient interface {
	// Disabled determines whether the locking is disabled.
	Disabled() bool

	// TakeLock attempts to acquire a lock.
	TakeLock() (bool, error)

	// AddPendingUpgrades registers information about which process groups are
	// pending an upgrade to a new version.
	AddPendingUpgrades(version fdbtypes.FdbVersion, processGroupIDs []string) error

	// GetPendingUpgrades returns the stored information about which process
	// groups are pending an upgrade to a new version.
	GetPendingUpgrades(version fdbtypes.FdbVersion) (map[string]bool, error)

	// ClearPendingUpgrades clears any stored information about pending
	// upgrades.
	ClearPendingUpgrades() error

	// GetDenyList retrieves the current deny list from the database.
	GetDenyList() ([]string, error)

	// UpdateDenyList updates the deny list to match a list of entries.
	UpdateDenyList(locks []fdbtypes.LockDenyListEntry) error
}

// LockClientProvider provides a dependency injection for creating a lock client.
// Deprecated: Use DatabaseClientProvider instead.
type LockClientProvider func(*fdbtypes.FoundationDBCluster) (LockClient, error)

// MockLockClient provides a mock client for managing operation locks.
type MockLockClient struct {
	// cluster stores the cluster this client is working with.
	cluster *fdbtypes.FoundationDBCluster

	// owner stores the deny list for lock acquisition.
	denyList []string

	// pendingUpgrades stores data about process groups that have a pending
	// upgrade.
	pendingUpgrades map[fdbtypes.FdbVersion]map[string]bool
}

// TakeLock attempts to acquire a lock.
func (client *MockLockClient) TakeLock() (bool, error) {
	return true, nil
}

// Disabled determines if the client should automatically grant locks.
func (client *MockLockClient) Disabled() bool {
	return !client.cluster.ShouldUseLocks()
}

// AddPendingUpgrades registers information about which process groups are
// pending an upgrade to a new version.
func (client *MockLockClient) AddPendingUpgrades(version fdbtypes.FdbVersion, processGroupIDs []string) error {
	if client.pendingUpgrades[version] == nil {
		client.pendingUpgrades[version] = make(map[string]bool)
	}
	for _, processGroupID := range processGroupIDs {
		client.pendingUpgrades[version][processGroupID] = true
	}
	return nil
}

// GetPendingUpgrades returns the stored information about which process
// groups are pending an upgrade to a new version.
func (client *MockLockClient) GetPendingUpgrades(version fdbtypes.FdbVersion) (map[string]bool, error) {
	upgrades := client.pendingUpgrades[version]
	if upgrades == nil {
		return make(map[string]bool), nil
	}
	return upgrades, nil
}

// GetDenyList retrieves the current deny list from the database.
func (client *MockLockClient) GetDenyList() ([]string, error) {
	return client.denyList, nil
}

// UpdateDenyList updates the deny list to match a list of entries.
// This will return the complete deny list after these changes are made.
func (client *MockLockClient) UpdateDenyList(locks []fdbtypes.LockDenyListEntry) error {
	newDenyList := make([]string, 0, len(client.denyList)+len(locks))
	newDenyMap := make(map[string]bool)
	for _, id := range client.denyList {
		allowed := false
		for _, entry := range locks {
			allowed = allowed || (entry.ID == id && entry.Allow)
		}
		if !allowed {
			newDenyList = append(newDenyList, id)
			newDenyMap[id] = true
		}
	}

	for _, entry := range locks {
		if !newDenyMap[entry.ID] && !entry.Allow {
			newDenyList = append(newDenyList, entry.ID)
			newDenyMap[entry.ID] = true
		}
	}

	sort.Strings(newDenyList)
	client.denyList = newDenyList
	return nil
}

// lockClientCache provides a cache of mock lock clients.
var lockClientCache = make(map[string]*MockLockClient)
var lockClientMutex sync.Mutex

// NewMockLockClient creates a mock lock client.
func NewMockLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	return newMockLockClientUncast(cluster), nil
}

// NewMockLockClientUncast creates a mock lock client.
func newMockLockClientUncast(cluster *fdbtypes.FoundationDBCluster) *MockLockClient {
	lockClientMutex.Lock()
	defer lockClientMutex.Unlock()

	client := lockClientCache[cluster.Name]
	if client == nil {
		client = &MockLockClient{cluster: cluster, pendingUpgrades: make(map[fdbtypes.FdbVersion]map[string]bool)}
		lockClientCache[cluster.Name] = client
	}
	return client
}

// ClearPendingUpgrades clears any stored information about pending
// upgrades.
func (client *MockLockClient) ClearPendingUpgrades() error {
	return nil
}

// ClearMockLockClients clears the cache of mock lock clients
func ClearMockLockClients() {
	lockClientCache = map[string]*MockLockClient{}
}
