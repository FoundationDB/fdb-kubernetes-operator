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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
	"time"
)

// fdbLibClient is an interface to interact with FDB over the client libraries
type fdbLibClient interface {
	// getValueFromDBUsingKey returns the value of the provided key.
	getValueFromDBUsingKey(fdbKey string, timeout time.Duration) ([]byte, error)
	// getInProgressExclusions returns a map that represents all addresses that are present in the \xff\xff/management/in_progress_exclusion key range
	getInProgressExclusions(timeout time.Duration) (map[string]fdbv1beta2.None, error)
}

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
		if len(rawResult) == 0 {
			return nil, err
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

// getInProgressExclusions returns a map that represents all addresses that are present in the \xff\xff/management/in_progress_exclusion key range.
// The key will represent the machine address without the port.
func (fdbClient *realFdbLibClient) getInProgressExclusions(timeout time.Duration) (map[string]fdbv1beta2.None, error) {
	fdbClient.logger.Info("Fetch in progress exclusions")
	defer func() {
		fdbClient.logger.Info("Done fetching in progress exclusions")
	}()
	database, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return nil, err
	}

	inProgressExclusions, err := database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		err = transaction.Options().SetTimeout(timeout.Milliseconds())
		if err != nil {
			return nil, err
		}

		prefix := "\\xff\\xff/management/in_progress_exclusion/"
		keyRange, err := fdb.PrefixRange([]byte(prefix))
		if err != nil {
			return nil, err
		}
		results := transaction.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		upgrades := make(map[string]fdbv1beta2.None, len(results))

		for _, result := range results {
			address := string(result.Key[len(prefix)+1:])

			// ...
			parsedAddr, err := fdbv1beta2.ParseProcessAddress(address)
			if err != nil {
				return nil, err
			}

			fdbClient.logger.V(1).Info("In progress exclusion contains following address", "address", address, "key", result.Key.String())
			upgrades[parsedAddr.MachineAddress()] = fdbv1beta2.None{}
		}

		return upgrades, nil
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

	if err != nil {
		return nil, err
	}

	inProgressExclusionsMap, isMap := inProgressExclusions.(map[string]fdbv1beta2.None)
	if !isMap {
		return nil, fmt.Errorf("invalid return value from transaction in getInProgressExclusions: %v", inProgressExclusionsMap)
	}

	return inProgressExclusionsMap, nil
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

func (fdbClient *mockFdbLibClient) getValueFromDBUsingKey(fdbKey string, _ time.Duration) ([]byte, error) {
	fdbClient.requestedKey = fdbKey

	return fdbClient.mockedOutput, fdbClient.mockedError
}

// getInProgressExclusions returns a map that represents all addresses that are present in the \xff\xff/management/in_progress_exclusion key range
func (fdbClient *mockFdbLibClient) getInProgressExclusions(timeout time.Duration) (map[string]fdbv1beta2.None, error) {
	// TODO(johscheuer) implement
	return nil, fdbClient.mockedError
}
