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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// mockLockClient provides a mock client for managing operation locks.
type mockLockClient struct {
	// cluster stores the cluster this client is working with.
	cluster *fdbv1beta2.FoundationDBCluster

	// owner stores the deny list for lock acquisition.
	denyList []string

	// pendingUpgrades stores data about process groups that have a pending
	// upgrade.
	pendingUpgrades map[fdb.Version]map[string]bool
}

// TakeLock attempts to acquire a lock.
func (client *mockLockClient) TakeLock() (bool, error) {
	return true, nil
}

// Disabled determines if the client should automatically grant locks.
func (client *mockLockClient) Disabled() bool {
	return !client.cluster.ShouldUseLocks()
}

// AddPendingUpgrades registers information about which process groups are
// pending an upgrade to a new version.
func (client *mockLockClient) AddPendingUpgrades(version fdb.Version, processGroupIDs []string) error {
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
func (client *mockLockClient) GetPendingUpgrades(version fdb.Version) (map[string]bool, error) {
	upgrades := client.pendingUpgrades[version]
	if upgrades == nil {
		return make(map[string]bool), nil
	}
	return upgrades, nil
}

// GetDenyList retrieves the current deny list from the database.
func (client *mockLockClient) GetDenyList() ([]string, error) {
	return client.denyList, nil
}

// UpdateDenyList updates the deny list to match a list of entries.
// This will return the complete deny list after these changes are made.
func (client *mockLockClient) UpdateDenyList(locks []fdbv1beta2.LockDenyListEntry) error {
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
var lockClientCache = make(map[string]*mockLockClient)
var lockClientMutex sync.Mutex

// newMockLockClient creates a mock lock client.
func newMockLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return newMockLockClientUncast(cluster), nil
}

// NewMockLockClientUncast creates a mock lock client.
func newMockLockClientUncast(cluster *fdbv1beta2.FoundationDBCluster) *mockLockClient {
	lockClientMutex.Lock()
	defer lockClientMutex.Unlock()

	client := lockClientCache[cluster.Name]
	if client == nil {
		client = &mockLockClient{cluster: cluster, pendingUpgrades: make(map[fdb.Version]map[string]bool)}
		lockClientCache[cluster.Name] = client
	}
	return client
}

// ClearPendingUpgrades clears any stored information about pending
// upgrades.
func (client *mockLockClient) ClearPendingUpgrades() error {
	return nil
}

// clearMockLockClients clears the cache of mock lock clients
func clearMockLockClients() {
	lockClientCache = map[string]*mockLockClient{}
}
