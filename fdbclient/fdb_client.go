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
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
)

// fdbLibClient is an interface to interact with FDB over the client libraries
type fdbLibClient interface {
	// getValueFromDBUsingKey returns the value of the provided key.
	getValueFromDBUsingKey(fdbKey string, timeout time.Duration) ([]byte, error)
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

// mockFdbLibClient is a mock for unit testing.
type mockFdbLibClient struct {
	// mockedKeyValue indicates outputs to return for specific keys in getValueFromDBUsingKey. If no entry matches,
	// the default mockedOutput below is returned.
	mockedKeyValue map[string][]byte
	// mockedOutput is the default output returned by getValueFromDBUsingKey.
	mockedOutput []byte
	// mockedError is the error returned by getValueFromDBUsingKey.
	mockedError error
	// requestedKey will contain all the key that were used to call getValueFromDBUsingKey.
	requestedKeys []string
}

func (fdbClient *mockFdbLibClient) getValueFromDBUsingKey(fdbKey string, _ time.Duration) ([]byte, error) {
	fdbClient.requestedKeys = append(fdbClient.requestedKeys, fdbKey)
	output, ok := fdbClient.mockedKeyValue[fdbKey]
	if ok {
		return output, nil
	}
	return fdbClient.mockedOutput, fdbClient.mockedError
}
