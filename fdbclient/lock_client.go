/*
 * lock_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package fdbclient

import (
	"fmt"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	controllers "github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
)

// RealLockClient provides a client for managing operation locks through the
// database.
type realLockClient struct {
	// The cluster we are managing locks for.
	cluster *fdbtypes.FoundationDBCluster

	// Whether we should disable locking completely.
	disableLocks bool

	// The connection to the database.
	database fdb.Database
}

// Disabled determines if the client should automatically grant locks.
func (client *realLockClient) Disabled() bool {
	return client.disableLocks
}

// TakeLock attempts to acquire a lock.
func (client *realLockClient) TakeLock() (bool, error) {
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
func (client *realLockClient) takeLockInTransaction(transaction fdb.Transaction) (bool, error) {
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
		return false, invalidLockValue{key: lockKey, value: lockValue}
	}

	ownerID, valid := lockTuple[0].(string)
	if !valid {
		return false, invalidLockValue{key: lockKey, value: lockValue}
	}

	startTime, valid := lockTuple[1].(int64)
	if !valid {
		return false, invalidLockValue{key: lockKey, value: lockValue}
	}

	endTime, valid := lockTuple[2].(int64)
	if !valid {
		return false, invalidLockValue{key: lockKey, value: lockValue}
	}

	cluster := client.cluster
	newOwnerDenied := transaction.Get(client.getDenyListKey(cluster.GetLockID())).MustGet() != nil
	if newOwnerDenied {
		log.Info("Failed to get lock due to deny list", "namespace", cluster.Namespace, "cluster", cluster.Name)
		return false, nil
	}

	oldOwnerDenied := transaction.Get(client.getDenyListKey(ownerID)).MustGet() != nil
	shouldClear := endTime < time.Now().Unix() || oldOwnerDenied

	if shouldClear {
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
func (client *realLockClient) updateLock(transaction fdb.Transaction, start int64) {
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
func (client *realLockClient) AddPendingUpgrades(version fdbtypes.FdbVersion, processGroupIDs []string) error {
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
func (client *realLockClient) GetPendingUpgrades(version fdbtypes.FdbVersion) (map[string]bool, error) {
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
		return nil, fmt.Errorf("invalid return value from transaction in GetPendingUpgrades: %v", upgrades)
	}
	return upgradeMap, nil
}

// ClearPendingUpgrades clears any stored information about pending
// upgrades.
func (client *realLockClient) ClearPendingUpgrades() error {
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

// GetDenyList retrieves the current deny list from the database.
func (client *realLockClient) GetDenyList() ([]string, error) {
	list, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetReadSystemKeys()
		if err != nil {
			return nil, err
		}

		keyRange, err := client.getDenyListKeyRange()
		if err != nil {
			return nil, err
		}

		values := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		list := make([]string, len(values))
		for index, value := range values {
			list[index] = string(value.Value)
		}
		return list, nil
	})

	if err != nil {
		return nil, err
	}

	return list.([]string), nil
}

// UpdateDenyList updates the deny list to match a list of entries.
func (client *realLockClient) UpdateDenyList(locks []fdbtypes.LockDenyListEntry) error {
	_, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		keyRange, err := client.getDenyListKeyRange()
		if err != nil {
			return nil, err
		}

		values := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		denyListMap := make(map[string]bool, len(values))
		for _, value := range values {
			denyListMap[string(value.Value)] = true
		}

		for _, entry := range locks {
			if entry.Allow && denyListMap[entry.ID] {
				tr.Clear(client.getDenyListKey(entry.ID))
			} else if !entry.Allow && !denyListMap[entry.ID] {
				tr.Set(client.getDenyListKey(entry.ID), []byte(entry.ID))
			}
		}
		return nil, nil
	})

	return err
}

// getDenyListKeyRange defines a key range containing the full deny list.
func (client *realLockClient) getDenyListKeyRange() (fdb.KeyRange, error) {
	keyPrefix := []byte(fmt.Sprintf("%s/denyList/", client.cluster.GetLockPrefix()))
	return fdb.PrefixRange(keyPrefix)
}

// getDenyListKeyRange defines a key range containing a potential deny list
// entry for an owner ID.
func (client *realLockClient) getDenyListKey(id string) fdb.Key {
	return fdb.Key(fmt.Sprintf("%s/denyList/%s", client.cluster.GetLockPrefix(), id))
}

// invalidLockValue is an error we can return when we cannot parse the existing
// values in the locking system.
type invalidLockValue struct {
	key   fdb.Key
	value []byte
}

// Error formats the error message.
func (err invalidLockValue) Error() string {
	return fmt.Sprintf("Could not decode value %s for key %s", err.value, err.key)
}

// NewRealLockClient creates a lock client.
func NewRealLockClient(cluster *fdbtypes.FoundationDBCluster) (controllers.LockClient, error) {
	if !cluster.ShouldUseLocks() {
		return &realLockClient{disableLocks: true}, nil
	}

	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}

	return &realLockClient{cluster: cluster, database: database}, nil
}
