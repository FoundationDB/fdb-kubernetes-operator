/*
 * fdb_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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
	"errors"
	"fmt"
	"path"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/go-logr/logr"
)

// fdbLibClient is an interface to interact with FDB over the client libraries
type fdbLibClient interface {
	// getValueFromDBUsingKey returns the value of the provided key.
	getValueFromDBUsingKey(fdbKey string, timeout time.Duration) ([]byte, error)
	// updateGlobalCoordinationKeys will update the provided updates in FDB.
	updateGlobalCoordinationKeys(prefix string, updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error
	// getGlobalCoordinationKeys will return the entries under the provided prefix.
	getGlobalCoordinationKeys(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error)
	// clearGlobalCoordinationKeys will remove all the ready information for the provided prefix.
	clearGlobalCoordinationKeys(prefix string) error
}

var _ fdbLibClient = (*realFdbLibClient)(nil)

// realFdbLibClient represents the actual FDB client that will interact with FDB.
type realFdbLibClient struct {
	cluster *fdbv1beta2.FoundationDBCluster
	logger  logr.Logger
}

func (fdbClient *realFdbLibClient) getValueFromDBUsingKey(fdbKey string, timeout time.Duration) ([]byte, error) {
	fdbClient.logger.Info("Fetch values from FDB", "key", fdbKey)
	defer func() {
		fdbClient.logger.Info("Done fetching values from FDB", "key", fdbKey)
	}()
	database, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return nil, err
	}

	result, err := database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		err = transaction.Options().SetTimeout(timeout.Milliseconds())
		if err != nil {
			return nil, err
		}

		rawResult := transaction.Get(fdb.Key(fdbKey)).MustGet()
		// If the value is empty return an empty byte slice. Otherwise, an error will be thrown.
		if len(rawResult) == 0 {
			return []byte{}, err
		}

		return rawResult, err
	})

	if err != nil {
		var fdbError *fdb.Error
		if errors.As(err, &fdbError) {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out
			if fdbError.Code == 1031 {
				return nil, fdbv1beta2.TimeoutError{Err: err}
			}
		}

		return nil, err
	}

	byteResult, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("could not cast result into byte slice")
	}

	return byteResult, nil
}

// updateGlobalCoordinationKeys will update the provided updates in FDB.
func (fdbClient *realFdbLibClient) updateGlobalCoordinationKeys(prefix string, updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	if len(updates) == 0 {
		return nil
	}

	fdbClient.logger.Info("Updating global coordination keys in FDB", "prefix", prefix, "updates", len(updates))
	defer func() {
		fdbClient.logger.Info("Done updating global coordination keys in FDB", "prefix", prefix, "updates", len(updates))
	}()

	database, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return err
	}

	keyPrefix := path.Join(fdbClient.cluster.GetLockPrefix(), prefix, fdbClient.cluster.Spec.ProcessGroupIDPrefix)
	_, err = database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		timestampTuple := tuple.Tuple{time.Now().Unix()}
		timestampBytes := timestampTuple.Pack()

		for processGroupID, action := range updates {
			key := fdb.Key(path.Join(keyPrefix, string(processGroupID)))

			fdbClient.logger.V(0).Info("updateGlobalCoordinationKeys update process group", "processGroupID", processGroupID, "action", action, "key", key.String())
			if action == fdbv1beta2.UpdateActionAdd {
				transaction.Set(key, timestampBytes)
				continue
			}

			if action == fdbv1beta2.UpdateActionDelete {
				transaction.Clear(key)
				continue
			}
		}

		return nil, nil
	})

	return checkError(err)
}

// getGlobalCoordinationKeys will return the entries under the provided prefix.
func (fdbClient *realFdbLibClient) getGlobalCoordinationKeys(prefix string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	var mapResult map[fdbv1beta2.ProcessGroupID]time.Time
	fdbClient.logger.Info("Fetching global coordination keys in FDB", "prefix", prefix)
	defer func() {
		fdbClient.logger.Info("Done fetching global coordination keys in FDB", "prefix", prefix, "results", len(mapResult))
	}()

	database, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return nil, err
	}

	result, err := database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetReadSystemKeys()
		if err != nil {
			return nil, err
		}

		keyPrefix := path.Join(fdbClient.cluster.GetLockPrefix(), prefix)
		keyRange, err := fdb.PrefixRange([]byte(keyPrefix))
		if err != nil {
			return nil, err
		}

		results := transaction.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		processGroups := make(map[fdbv1beta2.ProcessGroupID]time.Time, len(results))
		for _, result := range results {
			processGroupID := path.Base(result.Key.String())
			fdbClient.logger.V(0).Info("Found result", "processGroupID", processGroupID, "key", result.Key.String())

			timestampBytes, err := tuple.Unpack(result.Value)
			if err != nil {
				return nil, err
			}

			if len(timestampBytes) < 1 {
				return false, fmt.Errorf("expected that the tuple contains one element, got %d elements", len(timestampBytes))
			}

			timestamp, valid := timestampBytes[0].(int64)
			if !valid {
				return false, fmt.Errorf("could not cast timestamp bytes into timestamp")
			}

			processGroups[fdbv1beta2.ProcessGroupID(processGroupID)] = time.Unix(timestamp, 0)
		}

		return processGroups, nil
	})

	if err != nil {
		var fdbError *fdb.Error
		if errors.As(err, &fdbError) {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out
			if fdbError.Code == 1031 {
				return nil, fdbv1beta2.TimeoutError{Err: err}
			}
		}

		return nil, err
	}

	var ok bool
	mapResult, ok = result.(map[fdbv1beta2.ProcessGroupID]time.Time)
	if !ok {
		return nil, fmt.Errorf("could not cast result into map")
	}

	return mapResult, nil
}

// getGlobalCoordinationKeys will return the entries under the provided prefix.
func (fdbClient *realFdbLibClient) clearGlobalCoordinationKeys(prefix string) error {
	fdbClient.logger.Info("Clearing global coordination keys in FDB", "prefix", prefix)
	defer func() {
		fdbClient.logger.Info("Done clearing global coordination keys in FDB", "prefix", prefix)
	}()

	database, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return err
	}

	_, err = database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		keyPrefix := path.Join(fdbClient.cluster.GetLockPrefix(), prefix)
		keyRange, err := fdb.PrefixRange([]byte(keyPrefix))
		if err != nil {
			return nil, err
		}

		transaction.ClearRange(keyRange)
		return nil, nil
	})

	return checkError(err)
}

func checkError(err error) error {
	if err != nil {
		var fdbError *fdb.Error
		if errors.As(err, &fdbError) {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out
			if fdbError.Code == 1031 {
				return fdbv1beta2.TimeoutError{Err: err}
			}
		}

		return err
	}

	return nil
}

// mockFdbLibClient is a mock for unit testing.
type mockFdbLibClient struct {
	// mockedOutput is the output returned by getValueFromDBUsingKey.
	mockedOutput []byte
	// mockedError is the error returned by getValueFromDBUsingKey.
	mockedError error
	// requestedKey will be the key that was used to call getValueFromDBUsingKey.
	requestedKey string
}

var _ fdbLibClient = (*mockFdbLibClient)(nil)

func (fdbClient *mockFdbLibClient) getValueFromDBUsingKey(fdbKey string, _ time.Duration) ([]byte, error) {
	fdbClient.requestedKey = fdbKey

	return fdbClient.mockedOutput, fdbClient.mockedError
}

// updateGlobalCoordinationKeys will update the provided updates in FDB.
func (fdbClient *mockFdbLibClient) updateGlobalCoordinationKeys(_ string, _ map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) error {
	// TODO(johscheuer) implement for unit testing.
	return nil
}

// TODO(johscheuer) implement.
// getGlobalCoordinationKeys will return the entries under the provided prefix.
func (fdbClient *mockFdbLibClient) getGlobalCoordinationKeys(_ string) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	// TODO(johscheuer) implement for unit testing.
	return nil, nil
}

func (fdbClient *mockFdbLibClient) clearGlobalCoordinationKeys(_ string) error {
	//TODO implement me
	return nil
}
