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

	// fdbLibClient is an interface to interact with a FDB cluster over the FDB client libraries. In the real fdb lib client
	// we will issue the actual requests against FDB. In the mock runner we will return predefined output.
	fdbLibClient fdbLibClient

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

	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		return nil, client.takeLockInTransaction(tr)
	})
	return err
}

// takeLockInTransaction attempts to acquire a lock using an open transaction.
func (client *realLockClient) takeLockInTransaction(transaction fdb.Transaction) error {
	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))
	lockValue := transaction.Get(lockKey).MustGet()

	if len(lockValue) == 0 {
		client.log.Info("Setting initial lock")
		client.updateLock(transaction, 0)
		return nil
	}

	currentLockOwnerID, currentLockStartTimestamp, currentLockEndTimestamp, err := unpackLockValue(
		lockKey,
		lockValue,
	)
	if err != nil {
		return err
	}

	// ownerID represents the current cluster ID. If a lock is present the currentLockOwnerID represents the operator
	// instance holding the lock.
	ownerID := client.cluster.GetLockID()

	endTime := time.Unix(currentLockEndTimestamp, 0)
	logger := client.log.WithValues(
		"currentLockOwnerID", currentLockOwnerID,
		"startTime", time.Unix(currentLockStartTimestamp, 0),
		"endTime", endTime)

	newOwnerDenied, err := transaction.Get(client.getDenyListKey(ownerID)).Get()
	if err != nil {
		return err
	}

	if newOwnerDenied != nil {
		logger.Info("Failed to get lock due to deny list")
		return fmt.Errorf(
			"failed to get lock due to deny list, owner ID: %s is on deny list",
			ownerID,
		)
	}

	oldOwnerDenied, err := transaction.Get(client.getDenyListKey(currentLockOwnerID)).Get()
	if err != nil {
		return err
	}

	if currentLockEndTimestamp < time.Now().Unix() || oldOwnerDenied != nil {
		logger.Info("Clearing expired lock")
		client.updateLock(transaction, currentLockStartTimestamp)
		return nil
	}

	if currentLockOwnerID == ownerID {
		logger.Info("Extending previous lock")
		client.updateLock(transaction, currentLockStartTimestamp)
		return nil
	}

	logger.Info("Failed to get lock")

	return fmt.Errorf(
		"failed to get the lock, current lock ower: %s, lock will expire at: %s",
		currentLockOwnerID,
		endTime,
	)
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
	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
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
	var upgrades map[fdbv1beta2.ProcessGroupID]bool
	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		keyPrefix := []byte(
			fmt.Sprintf("%s/upgrades/%s/", client.cluster.GetLockPrefix(), version.String()),
		)
		keyRange, err := fdb.PrefixRange(keyPrefix)
		if err != nil {
			return nil, err
		}
		results := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		upgrades = make(map[fdbv1beta2.ProcessGroupID]bool, len(results))
		for _, result := range results {
			upgrades[fdbv1beta2.ProcessGroupID(result.Value)] = true
		}

		return nil, nil
	})

	return upgrades, err
}

// ClearPendingUpgrades clears any stored information about pending
// upgrades.
func (client *realLockClient) ClearPendingUpgrades() error {
	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
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
	var list []string
	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		keyRange, err := client.getDenyListKeyRange()
		if err != nil {
			return nil, err
		}

		values := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		list = make([]string, len(values))
		for index, value := range values {
			list[index] = string(value.Value)
		}

		return nil, nil
	})

	return list, err
}

// UpdateDenyList updates the deny list to match a list of entries.
func (client *realLockClient) UpdateDenyList(locks []fdbv1beta2.LockDenyListEntry) error {
	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
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
	_, err := client.fdbLibClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		lockValue := tr.Get(lockKey).MustGet()
		// The lock value is not set, so no action is required.
		if len(lockValue) == 0 {
			return nil, nil
		}

		currentLockOwnerID, currentLockStartTimestamp, currentLockEndTimestamp, err := unpackLockValue(
			lockKey,
			lockValue,
		)
		if err != nil {
			return nil, err
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
			return nil, nil
		}

		// Check the timestamp of the logs and make sure to only release locks when the timestamps are valid.
		// If the lock is not valid anymore, other operator instances can take the lock in takeLockInTransaction.
		now := time.Now()
		if currentLockStartTimestamp > now.Unix() {
			logger.Info("cannot release lock that is taken in the future")
			return nil, nil
		}

		if currentLockEndTimestamp < now.Unix() {
			logger.Info("cannot release a lock that is expired")
			return nil, nil
		}

		logger.Info("releasing lock")
		tr.Clear(lockKey)

		return nil, nil
	})
	return err
}

// unpackLockValue will unpack the lock value and cast the tuple values into the expected types.
func unpackLockValue(lockKey fdb.Key, lockValue []byte) (string, int64, int64, error) {
	lockTuple, err := tuple.Unpack(lockValue)
	if err != nil {
		return "", 0, 0, err
	}

	if len(lockTuple) < 3 {
		return "", 0, 0, invalidLockValue{key: lockKey, value: lockValue}
	}

	currentLockOwnerID, valid := lockTuple[0].(string)
	if !valid {
		return "", 0, 0, invalidLockValue{key: lockKey, value: lockValue}
	}

	currentLockStartTimestamp, valid := lockTuple[1].(int64)
	if !valid {
		return "", 0, 0, invalidLockValue{key: lockKey, value: lockValue}
	}

	currentLockEndTimestamp, valid := lockTuple[2].(int64)
	if !valid {
		return "", 0, 0, invalidLockValue{key: lockKey, value: lockValue}
	}

	return currentLockOwnerID, currentLockStartTimestamp, currentLockEndTimestamp, nil
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

	logger := log.WithValues(
		"namespace", cluster.Namespace,
		"cluster", cluster.Name,
		"ownerID", cluster.GetLockID(),
	)

	return &realLockClient{
		cluster: cluster,
		fdbLibClient: &realFdbLibClient{
			cluster: cluster,
			logger:  logger,
		},
		log: logger,
	}, nil
}
