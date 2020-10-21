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
	"os"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
)

// LockDuration determines how long locks are valid for.
var LockDuration = 10 * time.Minute

// LockAggregationDelay determines how long we wait after getting a lock before
// moving ahead with an operation that uses aggregated values.
var LockAggregationDelay = 2 * time.Minute

// LockClient provides a client for getting locks on operations for a cluster.
type LockClient interface {
	// GetCluster provides the cluster this client is working on.
	GetCluster() *fdbtypes.FoundationDBCluster

	// TakeLock attempts to acquire a lock.
	TakeLock() (*Lock, error)

	// SubmitAggregatedOperation submits values that should be operated on
	// by whichever operator has the lock.
	SubmitAggregatedOperation(string, []string) error

	// RetrieveAggregatedOperation retrieves values that should be operated on
	// by whichever operator has the lock.
	RetrieveAggregatedOperation(string) ([]string, error)

	// ClearAggregatedOperation removes values that have been executed in an
	// aggregated operation.
	ClearAggregatedOperation(string, []string) error

	// Close cleans up any resources that the client needs to keep open.
	Close() error
}

// Lock provides data about a lock that has been acquired.
type Lock struct {
	// AcquisitionTime provides the time when the lock was acquired.
	AcquisitionTime time.Time
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

	// clusterFilePath provides the path this client is using for its cluster
	// file.
	clusterFilePath string

	// The connection to the database.
	database fdb.Database
}

// GetCluster provides the cluster this client is working on.
func (client *RealLockClient) GetCluster() *fdbtypes.FoundationDBCluster {
	return client.cluster
}

// TakeLock attempts to acquire a lock.
func (client *RealLockClient) TakeLock() (*Lock, error) {
	if client.disableLocks {
		return &Lock{AcquisitionTime: time.Now()}, nil
	}

	lock, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return false, err
		}

		lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))
		lockValue := transaction.Get(lockKey).MustGet()

		if len(lockValue) == 0 {
			log.Info("Setting initial lock")
			return client.takeLockDirect(transaction, nil)
		}

		lockTuple, err := tuple.Unpack(lockValue)
		if err != nil {
			return false, err
		}

		if len(lockTuple) < 3 {
			return false, InvalidLockValue{key: lockKey, value: lockValue}
		}

		startTime, valid := lockTuple[1].(int64)
		if !valid {
			return false, InvalidLockValue{key: lockKey, value: lockValue}
		}

		ownerID, valid := lockTuple[0].(string)
		if !valid {
			return false, InvalidLockValue{key: lockKey, value: lockValue}
		}

		endTime, valid := lockTuple[2].(int64)
		if !valid {
			return false, InvalidLockValue{key: lockKey, value: lockValue}
		}

		ownsLock := ownerID == client.cluster.GetLockID()

		if endTime < time.Now().Unix() {
			log.Info("Clearing expired lock", "previousLockValue", lockValue)
			var startTimeToReuse *int64
			if ownsLock {
				startTimeToReuse = &startTime
			} else {
				startTimeToReuse = nil
			}
			return client.takeLockDirect(transaction, startTimeToReuse)
		}

		if ownsLock {
			return &Lock{AcquisitionTime: time.Unix(startTime, 0)}, nil
		}
		return nil, nil
	})
	return lock.(*Lock), err
}

// SubmitAggregatedOperation submits values that should be operated on
// by whichever operator has the lock.
func (client *RealLockClient) SubmitAggregatedOperation(operation string, values []string) error {
	if client.disableLocks {
		return nil
	}

	_, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		for _, value := range values {
			key := fdb.Key(fmt.Sprintf("%s/operations/%s/%s",
				client.cluster.GetLockPrefix(),
				operation,
				value,
			))

			transaction.Set(key, []byte(value))
		}
		return nil, nil
	})
	return err
}

// RetrieveAggregatedOperation retrieves values that should be operated on
// by whichever operator has the lock.
func (client *RealLockClient) RetrieveAggregatedOperation(operation string) ([]string, error) {
	if client.disableLocks {
		return nil, nil
	}

	values, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		prefix := fmt.Sprintf("%s/operations/%s/",
			client.cluster.GetLockPrefix(),
			operation,
		)
		prefixRange, err := fdb.PrefixRange([]byte(prefix))
		if err != nil {
			return nil, err
		}

		resultHandle := transaction.GetRange(prefixRange, fdb.RangeOptions{})

		results, err := resultHandle.GetSliceWithError()
		if err != nil {
			return nil, err
		}

		values := make([]string, len(results))
		for index, entry := range results {
			values[index] = string(entry.Value)
		}

		return values, nil
	})
	if err != nil {
		return nil, err
	}
	return values.([]string), nil
}

// ClearAggregatedOperation removes values that have been executed in an
// aggregated operation.
func (client *RealLockClient) ClearAggregatedOperation(operation string, values []string) error {

	if client.disableLocks {
		return nil
	}

	_, err := client.database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		for _, value := range values {
			key := fdb.Key(fmt.Sprintf("%s/operations/%s/%s",
				client.cluster.GetLockPrefix(),
				operation,
				value,
			))

			transaction.Clear(key)
		}
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

// TakeLock attempts to acquire a lock.
func (client *RealLockClient) takeLockDirect(transaction fdb.Transaction, startTimeToReuse *int64) (interface{}, error) {
	lockKey := fdb.Key(fmt.Sprintf("%s/global", client.cluster.GetLockPrefix()))
	var start time.Time
	if startTimeToReuse == nil {
		start = time.Now()
	} else {
		start = time.Unix(*startTimeToReuse, 0)
	}

	end := start.Add(LockDuration)
	lockValue := tuple.Tuple{
		client.cluster.GetLockID(),
		start.Unix(),
		end.Unix(),
	}
	log.Info("Setting new lock", "lockValue", lockValue)
	transaction.Set(lockKey, lockValue.Pack())
	return true, nil
}

// Close cleans up any resources that the client needs to keep open.
func (client *RealLockClient) Close() error {
	if client.disableLocks {
		return nil
	}
	return os.Remove(client.clusterFilePath)
}

// NewRealLockClient creates a lock client.
func NewRealLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	if !cluster.ShouldUseLocks() {
		return &RealLockClient{disableLocks: true}, nil
	}

	clusterFile, err := ioutil.TempFile("", "")
	clusterFilePath := clusterFile.Name()
	if err != nil {
		return nil, err
	}

	defer clusterFile.Close()
	if err != nil {
		return nil, err
	}

	_, err = clusterFile.WriteString(cluster.Status.ConnectionString)
	if err != nil {
		return nil, err
	}
	err = clusterFile.Close()
	if err != nil {
		return nil, err
	}

	database, err := fdb.OpenDatabase(clusterFilePath)
	if err != nil {
		return nil, err
	}

	return &RealLockClient{cluster: cluster, clusterFilePath: clusterFilePath, database: database}, nil
}

// MockLockClient provides a mock client for managing operation locks.
type MockLockClient struct {
	// cluster stores the cluster this client is working with.
	cluster *fdbtypes.FoundationDBCluster

	// aggregations stores values that have been submitted for aggregated
	// operations.
	aggregations map[string][]string
}

// GetCluster provides the cluster this client is working on.
func (client *MockLockClient) GetCluster() *fdbtypes.FoundationDBCluster {
	return client.cluster
}

// TakeLock attempts to acquire a lock.
func (client *MockLockClient) TakeLock() (*Lock, error) {
	return &Lock{AcquisitionTime: time.Unix(0, 0)}, nil
}

// SubmitAggregatedOperation submits values that should be operated on
// by whichever operator has the lock.
func (client *MockLockClient) SubmitAggregatedOperation(operation string, values []string) error {
	client.aggregations[operation] = append(client.aggregations[operation], values...)
	return nil
}

// RetrieveAggregatedOperation retrieves values that should be operated on
// by whichever operator has the lock.
func (client *MockLockClient) RetrieveAggregatedOperation(operation string) ([]string, error) {
	values := client.aggregations[operation]
	if values == nil {
		return []string{}, nil
	}
	return values, nil
}

// ClearAggregatedOperation removes values that have been executed in an
// aggregated operation.
func (client *MockLockClient) ClearAggregatedOperation(operation string, values []string) error {
	newValues := make([]string, 0, len(client.aggregations[operation]))
	valuesToRemove := make(map[string]bool, len(values))
	for _, value := range values {
		valuesToRemove[value] = true
	}
	for _, value := range client.aggregations[operation] {
		if !valuesToRemove[value] {
			newValues = append(newValues, value)
		}
	}
	client.aggregations[operation] = newValues
	return nil
}

// Close cleans up any resources that the client needs to keep open.
func (client *MockLockClient) Close() error {
	return nil
}

// NewMockLockClient creates a mock lock client.
func NewMockLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	return &MockLockClient{cluster: cluster, aggregations: make(map[string][]string)}, nil
}

// TakeLockWithAggregationDelay attempts to acquire a lock, but adds an
// additional delay beyond the initial acquisition time to allow a window for
// other aggregations.
func TakeLockWithAggregationDelay(client LockClient) (*Lock, error) {
	lock, err := client.TakeLock()
	if lock == nil || err != nil {
		return lock, err
	}

	desiredAcquisitionTime := time.Now().Add(-1 * LockAggregationDelay)

	if lock.AcquisitionTime.After(desiredAcquisitionTime) {
		log.Info("Waiting for aggregated values for lock", "namespace", client.GetCluster().Namespace, "cluster", client.GetCluster().Name, "lockAcquisitionTime", lock.AcquisitionTime)
		return nil, nil
	}

	return lock, nil
}
