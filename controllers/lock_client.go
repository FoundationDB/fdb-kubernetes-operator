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
	"fmt"
	"sync"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
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
}

// LockClientProvider provides a dependency injection for creating a lock client.
type LockClientProvider func(*fdbtypes.FoundationDBCluster) (LockClient, error)

// RealLockClient provides a client for managing operation locks through the
// database.
type RealLockClient struct {
	// The cluster we are managing locks for.
	cluster *fdbtypes.FoundationDBCluster

	// Whether we should disable locking completely.
	disableLocks bool

	// The connection to the database.
	database fdb.Database
}

// Disabled determines if the client should automatically grant locks.
func (client *RealLockClient) Disabled() bool {
	return client.disableLocks
}

// TakeLock attempts to acquire a lock.
func (client *RealLockClient) TakeLock() (bool, error) {
	if client.disableLocks {
		return true, nil
	}

	hasLock, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		return client.takeLockInTransaction(transaction)
	})

	if hasLock == nil {
		return false, err
	}

	return hasLock.(bool), err
}

// takeLockInTransaction attempts to acquire a lock using an open transaction.
func (client *RealLockClient) takeLockInTransaction(transaction fdb.Transaction) (bool, error) {
	err := transaction.Options().SetAccessSystemKeys()
	if err != nil {
		return false, err
	}

	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))
	lockValue := transaction.Get(lockKey).MustGet()

	if len(lockValue) == 0 {
		log.Info("Setting initial lock")
		client.updateLock(transaction, 0)
		return true, nil
	}

	lockTuple, err := tuple.Unpack(lockValue)
	if err != nil {
		return false, err
	}

	if len(lockTuple) < 3 {
		return false, InvalidLockValue{key: lockKey, value: lockValue}
	}

	ownerID, valid := lockTuple[0].(string)
	if !valid {
		return false, InvalidLockValue{key: lockKey, value: lockValue}
	}

	startTime, valid := lockTuple[1].(int64)
	if !valid {
		return false, InvalidLockValue{key: lockKey, value: lockValue}
	}

	endTime, valid := lockTuple[2].(int64)
	if !valid {
		return false, InvalidLockValue{key: lockKey, value: lockValue}
	}

	cluster := client.cluster

	if endTime < time.Now().Unix() {
		log.Info("Clearing expired lock", "namespace", cluster.Namespace, "cluster", cluster.Name, "owner", ownerID, "startTime", time.Unix(startTime, 0), "endTime", time.Unix(endTime, 0))
		client.updateLock(transaction, startTime)
		return true, nil
	}

	if ownerID == client.cluster.GetLockID() {
		log.Info("Extending previous lock", "namespace", cluster.Namespace, "cluster", cluster.Name, "owner", ownerID, "startTime", time.Unix(startTime, 0), "endTime", time.Unix(endTime, 0))
		client.updateLock(transaction, startTime)
		return true, nil
	}

	log.Info("Failed to get lock", "namespace", cluster.Namespace, "cluster", cluster.Name, "owner", ownerID, "startTime", time.Unix(startTime, 0), "endTime", time.Unix(endTime, 0))
	return false, nil
}

// updateLock sets the keys to acquire a lock.
func (client *RealLockClient) updateLock(transaction fdb.Transaction, start int64) {
	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))

	if start == 0 {
		start = time.Now().Unix()
	}
	end := time.Now().Add(client.cluster.GetLockDuration()).Unix()
	lockValue := tuple.Tuple{
		client.cluster.GetLockID(),
		start,
		end,
	}
	log.Info("Setting new lock", "namespace", client.cluster.Namespace, "cluster", client.cluster.Name, "lockValue", lockValue)
	transaction.Set(lockKey, lockValue.Pack())
}

// AddPendingUpgrades registers information about which process groups are
// pending an upgrade to a new version.
func (client *RealLockClient) AddPendingUpgrades(version fdbtypes.FdbVersion, processGroupIDs []string) error {
	_, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		for _, processGroupID := range processGroupIDs {
			key := fdb.Key(fmt.Sprintf("%s/upgrades/%s/%s", client.cluster.GetLockPrefix(), version.String(), processGroupID))
			tr.Set(key, []byte(processGroupID))
		}
		return nil, nil
	})
	return err
}

// GetPendingUpgrades returns the stored information about which process
// groups are pending an upgrade to a new version.
func (client *RealLockClient) GetPendingUpgrades(version fdbtypes.FdbVersion) (map[string]bool, error) {
	upgrades, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetReadSystemKeys()
		if err != nil {
			return nil, err
		}

		keyPrefix := []byte(fmt.Sprintf("%s/upgrades/%s/", client.cluster.GetLockPrefix(), version.String()))
		keyRange, err := fdb.PrefixRange(keyPrefix)
		if err != nil {
			return nil, err
		}
		results := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		upgrades := make(map[string]bool, len(results))
		for _, result := range results {
			upgrades[string(result.Value)] = true
		}
		return upgrades, nil
	})
	if err != nil {
		return nil, err
	}
	upgradeMap, isMap := upgrades.(map[string]bool)
	if !isMap {
		return nil, fmt.Errorf("Invalid return value from transaction in GetPendingUpgrades: %v", upgrades)
	}
	return upgradeMap, nil
}

// ClearPendingUpgrades clears any stored information about pending
// upgrades.
func (client *RealLockClient) ClearPendingUpgrades() error {
	_, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		keyPrefix := []byte(fmt.Sprintf("%s/upgrades/", client.cluster.GetLockPrefix()))
		keyRange, err := fdb.PrefixRange(keyPrefix)
		if err != nil {
			return nil, err
		}

		tr.ClearRange(keyRange)
		return nil, nil
	})
	return err
}

// InvalidLockValue is an error we can return when we cannot parse the existing
// values in the locking system.
type InvalidLockValue struct {
	key   fdb.Key
	value []byte
}

// Error formats the error message.
func (err InvalidLockValue) Error() string {
	return fmt.Sprintf("Could not decode value %s for key %s", err.value, err.key)
}

// NewRealLockClient creates a lock client.
func NewRealLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	if !cluster.ShouldUseLocks() {
		return &RealLockClient{disableLocks: true}, nil
	}

	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}

	return &RealLockClient{cluster: cluster, database: database}, nil
}

// MockLockClient provides a mock client for managing operation locks.
type MockLockClient struct {
	// cluster stores the cluster this client is working with.
	cluster *fdbtypes.FoundationDBCluster

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
