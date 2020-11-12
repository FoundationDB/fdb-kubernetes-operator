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
	"io/ioutil"
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
}

// TakeLock attempts to acquire a lock.
func (client *MockLockClient) TakeLock() (bool, error) {
	return true, nil
}

// Disabled determines if the client should automatically grant locks.
func (client *MockLockClient) Disabled() bool {
	return !client.cluster.ShouldUseLocks()
}

// NewMockLockClient creates a mock lock client.
func NewMockLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	return &MockLockClient{cluster: cluster}, nil
}

// fdbDatabaseCache provides a
var fdbDatabaseCache = map[string]fdb.Database{}

// getFDBDatabase opens an FDB database. The result will be cached for
// subsequent calls, based on the cluster namespace and name.
func getFDBDatabase(cluster *fdbtypes.FoundationDBCluster) (fdb.Database, error) {
	cacheKey := fmt.Sprintf("%s/%s", cluster.ObjectMeta.Namespace, cluster.ObjectMeta.Name)
	database, present := fdbDatabaseCache[cacheKey]
	if present {
		return database, nil
	}

	clusterFile, err := ioutil.TempFile("", "")
	clusterFilePath := clusterFile.Name()
	if err != nil {
		return fdb.Database{}, err
	}

	defer clusterFile.Close()
	if err != nil {
		return fdb.Database{}, err
	}

	_, err = clusterFile.WriteString(cluster.Status.ConnectionString)
	if err != nil {
		return fdb.Database{}, err
	}
	err = clusterFile.Close()
	if err != nil {
		return fdb.Database{}, err
	}

	database, err = fdb.OpenDatabase(clusterFilePath)
	if err != nil {
		return fdb.Database{}, err
	}

	fdbDatabaseCache[cacheKey] = database
	return database, nil
}
