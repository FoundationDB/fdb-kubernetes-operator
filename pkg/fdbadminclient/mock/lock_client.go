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

package mock

import (
	"sort"
	"sync"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// LockClient provides a mock client for managing operation locks.
type LockClient struct {
	// cluster stores the cluster this client is working with.
	cluster *fdbv1beta2.FoundationDBCluster

	// owner stores the deny list for lock acquisition.
	denyList []string

	// pendingUpgrades stores data about process groups that have a pending
	// upgrade.
	pendingUpgrades map[fdbv1beta2.Version]map[fdbv1beta2.ProcessGroupID]bool
}

// TakeLock attempts to acquire a lock.
func (client *LockClient) TakeLock() (bool, error) {
	return true, nil
}

// Disabled determines if the client should automatically grant locks.
func (client *LockClient) Disabled() bool {
	return !client.cluster.ShouldUseLocks()
}

// AddPendingUpgrades registers information about which process groups are
// pending an upgrade to a new version.
func (client *LockClient) AddPendingUpgrades(version fdbv1beta2.Version, processGroupIDs []fdbv1beta2.ProcessGroupID) error {
	if client.pendingUpgrades[version] == nil {
		client.pendingUpgrades[version] = make(map[fdbv1beta2.ProcessGroupID]bool)
	}
	for _, processGroupID := range processGroupIDs {
		client.pendingUpgrades[version][processGroupID] = true
	}
	return nil
}

// GetPendingUpgrades returns the stored information about which process
// groups are pending an upgrade to a new version.
func (client *LockClient) GetPendingUpgrades(version fdbv1beta2.Version) (map[fdbv1beta2.ProcessGroupID]bool, error) {
	upgrades := client.pendingUpgrades[version]
	if upgrades == nil {
		return make(map[fdbv1beta2.ProcessGroupID]bool), nil
	}
	return upgrades, nil
}

// GetDenyList retrieves the current deny list from the database.
func (client *LockClient) GetDenyList() ([]string, error) {
	return client.denyList, nil
}

// UpdateDenyList updates the deny list to match a list of entries.
// This will return the complete deny list after these changes are made.
func (client *LockClient) UpdateDenyList(locks []fdbv1beta2.LockDenyListEntry) error {
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
var lockClientCache = make(map[string]*LockClient)
var lockClientMutex sync.Mutex

// NewMockLockClient creates a mock lock client.
func NewMockLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return NewMockLockClientUncast(cluster), nil
}

// NewMockLockClientUncast creates a mock lock client.
func NewMockLockClientUncast(cluster *fdbv1beta2.FoundationDBCluster) *LockClient {
	lockClientMutex.Lock()
	defer lockClientMutex.Unlock()

	client := lockClientCache[cluster.Name]
	if client == nil {
		client = &LockClient{cluster: cluster, pendingUpgrades: make(map[fdbv1beta2.Version]map[fdbv1beta2.ProcessGroupID]bool)}
		lockClientCache[cluster.Name] = client
	}
	return client
}

// ClearPendingUpgrades clears any stored information about pending
// upgrades.
func (client *LockClient) ClearPendingUpgrades() error {
	return nil
}

// ClearMockLockClients clears the cache of mock lock clients
func ClearMockLockClients() {
	lockClientCache = map[string]*LockClient{}
}
