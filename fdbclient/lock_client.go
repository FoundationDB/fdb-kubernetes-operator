/*
 * lock_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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

	"github.com/go-logr/logr"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
)

// RealLockClient provides a client for managing operation locks through the
// database.
type realLockClient struct {
	// The cluster we are managing locks for.
	cluster *fdbv1beta2.FoundationDBCluster

	// Whether we should disable locking completely.
	disableLocks bool

	// The connection to the database.
	database fdb.Database

	// log implementation for logging output
	log logr.Logger
}

// Disabled determines if the client should automatically grant locks.
func (client *realLockClient) Disabled() bool {
	return client.disableLocks
}

// TakeLock attempts to acquire a lock.
func (client *realLockClient) TakeLock() error {
	if client.disableLocks {
		return nil
	}

	_, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		lockErr := client.takeLockInTransaction(transaction)
		return nil, lockErr
	})

	return err
}

// takeLockInTransaction attempts to acquire a lock using an open transaction.
func (client *realLockClient) takeLockInTransaction(transaction fdb.Transaction) error {
	err := transaction.Options().SetAccessSystemKeys()
	if err != nil {
		return err
	}

	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))
	lockValue := transaction.Get(lockKey).MustGet()

	if len(lockValue) == 0 {
		client.log.Info("Setting initial lock")
		client.updateLock(transaction, 0)
		return nil
	}

	lockTuple, err := tuple.Unpack(lockValue)
	if err != nil {
		return err
	}

	if len(lockTuple) < 3 {
		return invalidLockValue{key: lockKey, value: lockValue}
	}

	currentLockOwnerID, valid := lockTuple[0].(string)
	if !valid {
		return invalidLockValue{key: lockKey, value: lockValue}
	}

	currentLockStartTime, valid := lockTuple[1].(int64)
	if !valid {
		return invalidLockValue{key: lockKey, value: lockValue}
	}

	currentLockEndTime, valid := lockTuple[2].(int64)
	if !valid {
		return invalidLockValue{key: lockKey, value: lockValue}
	}

	// ownerID represents the current cluster ID. If a lock is present the currentLockOwnerID represents the operator
	// instance holding the lock.
	ownerID := client.cluster.GetLockID()

	logger := client.log.WithValues(
		"currentLockOwnerID", currentLockOwnerID,
		"startTime", time.Unix(currentLockStartTime, 0),
		"endTime", time.Unix(currentLockEndTime, 0))

	newOwnerDenied := transaction.Get(client.getDenyListKey(ownerID)).MustGet() != nil
	if newOwnerDenied {
		logger.Info("Failed to get lock due to deny list")
		return fmt.Errorf(
			"failed to get lock due to deny list, owner ID: %s is on deny list",
			ownerID,
		)
	}

	oldOwnerDenied := transaction.Get(client.getDenyListKey(currentLockOwnerID)).MustGet() != nil
	shouldClear := currentLockEndTime < time.Now().Unix() || oldOwnerDenied

	if shouldClear {
		logger.Info("Clearing expired lock")
		client.updateLock(transaction, currentLockStartTime)
		return nil
	}

	if currentLockOwnerID == ownerID {
		logger.Info("Extending previous lock")
		client.updateLock(transaction, currentLockStartTime)
		return nil
	}

	logger.Info("Failed to get lock")

	return fmt.Errorf("failed to get the lock")
}

// updateLock sets the keys to acquire a lock.
func (client *realLockClient) updateLock(transaction fdb.Transaction, start int64) {
	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))

	if start == 0 {
		start = time.Now().Unix()
	}
	end := time.Now().Add(client.cluster.GetLockDuration()).Unix()
	ownerID := client.cluster.GetLockID()
	lockValue := tuple.Tuple{
		ownerID,
		start,
		end,
	}
	client.log.Info("Setting new lock",
		"lockValue", lockValue,
		"startTime", time.Unix(start, 0),
		"endTime", time.Unix(end, 0))
	transaction.Set(lockKey, lockValue.Pack())
}

// AddPendingUpgrades registers information about which process groups are
// pending an upgrade to a new version.
func (client *realLockClient) AddPendingUpgrades(
	version fdbv1beta2.Version,
	processGroupIDs []fdbv1beta2.ProcessGroupID,
) error {
	_, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		for _, processGroupID := range processGroupIDs {
			key := fdb.Key(
				fmt.Sprintf(
					"%s/upgrades/%s/%s",
					client.cluster.GetLockPrefix(),
					version.String(),
					processGroupID,
				),
			)
			tr.Set(key, []byte(processGroupID))
		}
		return nil, nil
	})
	return err
}

// GetPendingUpgrades returns the stored information about which process
// groups are pending an upgrade to a new version.
func (client *realLockClient) GetPendingUpgrades(
	version fdbv1beta2.Version,
) (map[fdbv1beta2.ProcessGroupID]bool, error) {
	upgrades, err := client.database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err := tr.Options().SetReadSystemKeys()
		if err != nil {
			return nil, err
		}

		keyPrefix := []byte(
			fmt.Sprintf("%s/upgrades/%s/", client.cluster.GetLockPrefix(), version.String()),
		)
		keyRange, err := fdb.PrefixRange(keyPrefix)
		if err != nil {
			return nil, err
		}
		results := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		upgrades := make(map[fdbv1beta2.ProcessGroupID]bool, len(results))
		for _, result := range results {
			upgrades[fdbv1beta2.ProcessGroupID(result.Value)] = true
		}

		return upgrades, nil
	})

	if err != nil {
		return nil, err
	}

	upgradeMap, isMap := upgrades.(map[fdbv1beta2.ProcessGroupID]bool)
	if !isMap {
		return nil, fmt.Errorf(
			"invalid return value from transaction in GetPendingUpgrades: %v",
			upgrades,
		)
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
func (client *realLockClient) UpdateDenyList(locks []fdbv1beta2.LockDenyListEntry) error {
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
	return fdb.PrefixRange([]byte(fmt.Sprintf("%s/denyList/", client.cluster.GetLockPrefix())))
}

// getDenyListKeyRange defines a key range containing a potential deny list
// entry for an owner ID.
func (client *realLockClient) getDenyListKey(id string) fdb.Key {
	return fdb.Key(fmt.Sprintf("%s/denyList/%s", client.cluster.GetLockPrefix(), id))
}

// ReleaseLock will release the current lock. The method will only release the lock if the current
// operator is the lock holder.
func (client *realLockClient) ReleaseLock() error {
	if client.disableLocks {
		return nil
	}

	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))
	_, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return false, err
		}

		lockValue := transaction.Get(lockKey).MustGet()
		// The lock value is not set, so no action is required.
		if len(lockValue) == 0 {
			return false, nil
		}

		lockTuple, err := tuple.Unpack(lockValue)
		if err != nil {
			return false, err
		}

		currentLockOwnerID, valid := lockTuple[0].(string)
		if !valid {
			return false, invalidLockValue{key: lockKey, value: lockValue}
		}

		currentLockStartTimestamp, valid := lockTuple[1].(int64)
		if !valid {
			return false, invalidLockValue{key: lockKey, value: lockValue}
		}

		currentLockEndTimestamp, valid := lockTuple[2].(int64)
		if !valid {
			return false, invalidLockValue{key: lockKey, value: lockValue}
		}

		ownerID := client.cluster.GetLockID()
		startTime := time.Unix(currentLockStartTimestamp, 0)
		logger := client.log.WithValues(
			"currentLockOwnerID", currentLockOwnerID,
			"startTime", startTime,
			"endTime", time.Unix(currentLockEndTimestamp, 0),
			"lockDuration", time.Since(startTime).String())

		if currentLockOwnerID != ownerID {
			logger.Info("cannot release lock from other owner")
			return false, nil
		}

		// Check the timestamp of the logs and make sure to only release locks when the timestamps are valid.
		// If the lock is not valid anymore, other operator instances can take the lock in takeLockInTransaction.
		now := time.Now()
		if currentLockStartTimestamp > now.Unix() {
			logger.Info("cannot release lock that is taken in the future")
			return false, nil
		}

		if currentLockEndTimestamp < now.Unix() {
			logger.Info("cannot release a lock that is expired")
			return false, nil
		}

		logger.Info("releasing lock")
		transaction.Clear(lockKey)

		return nil, nil
	})

	return err
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
func NewRealLockClient(
	cluster *fdbv1beta2.FoundationDBCluster,
	log logr.Logger,
) (fdbadminclient.LockClient, error) {
	if !cluster.ShouldUseLocks() {
		return &realLockClient{disableLocks: true}, nil
	}

	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}

	return &realLockClient{
		cluster:  cluster,
		database: database,
		log: log.WithValues(
			"namespace", cluster.Namespace,
			"cluster", cluster.Name,
			"ownerID", cluster.GetLockID(),
		),
	}, nil
}
