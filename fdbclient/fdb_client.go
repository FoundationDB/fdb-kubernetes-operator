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
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/go-logr/logr"
)

// fdbLibClient is an interface to interact with FDB over the client libraries
type fdbLibClient interface {
	// updateGlobalCoordinationKeys will update the provided updates in FDB.
	updateGlobalCoordinationKeys(
		prefix string,
		updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction,
	) error
	// getGlobalCoordinationKeys will return the entries under the provided prefix.
	getGlobalCoordinationKeys(
		prefix string,
	) (map[fdbv1beta2.ProcessGroupID]time.Time, error)
	// clearGlobalCoordinationKeys will remove all the ready information for the provided prefix.
	clearGlobalCoordinationKeys(prefix string) error
	// updateProcessAddresses will update the provided addresses associated with the provided process group. If the address slice
	// is empty or nil, the entry will be removed.
	updateProcessAddresses(
		updates map[fdbv1beta2.ProcessGroupID][]string,
	) error
	// getProcessAddresses gets the process group IDs and their associated process addresses.
	getProcessAddresses(
		prefix string,
	) (map[fdbv1beta2.ProcessGroupID][]string, error)
	// executeTransactionForManagementAPI will run an operation for the management API. This method handles all the common
	// options.
	executeTransactionForManagementAPI(
		operation func(tr fdb.Transaction) error,
	) error
	// executeTransaction will run a transaction for the target cluster. This method will handle all the common options.
	// The operation may return a result, which will be passed through to the caller.
	executeTransaction(operation func(tr fdb.Transaction) (any, error)) (any, error)
}

var _ fdbLibClient = &realFdbLibClient{}

// realFdbLibClient represents the actual FDB client that will interact with FDB.
type realFdbLibClient struct {
	cluster *fdbv1beta2.FoundationDBCluster
	logger  logr.Logger
}

// updateGlobalCoordinationKeys will update the provided updates in FDB.
func (fdbClient *realFdbLibClient) updateGlobalCoordinationKeys(
	prefix string,
	updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction,
) error {
	if len(updates) == 0 {
		return nil
	}

	fdbClient.logger.V(1).
		Info("Updating global coordination keys in FDB", "prefix", prefix, "updates", len(updates))
	defer func() {
		fdbClient.logger.V(1).
			Info("Done updating global coordination keys in FDB", "prefix", prefix, "updates", len(updates))
	}()

	keyPrefix := path.Join(
		fdbClient.cluster.GetLockPrefix(),
		prefix,
		fdbClient.cluster.Spec.ProcessGroupIDPrefix,
	)

	_, err := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		timestampTuple := tuple.Tuple{time.Now().Unix()}
		timestampBytes := timestampTuple.Pack()

		for processGroupID, action := range updates {
			key := fdb.Key(path.Join(keyPrefix, string(processGroupID)))

			fdbClient.logger.V(1).
				Info("updateGlobalCoordinationKeys update process group", "processGroupID", processGroupID, "action", action, "key", key.String())
			// Ignore tester processes, those shouldn't be added as those are not managed by the global coordination system.
			if action == fdbv1beta2.UpdateActionAdd &&
				processGroupID.GetProcessClass() != fdbv1beta2.ProcessClassTest {
				tr.Set(key, timestampBytes)
				continue
			}

			if action == fdbv1beta2.UpdateActionDelete {
				tr.Clear(key)
				continue
			}
		}
		return nil, nil
	})

	return err
}

// getGlobalCoordinationKeys will return the entries under the provided prefix.
func (fdbClient *realFdbLibClient) getGlobalCoordinationKeys(
	prefix string,
) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	var resultProcessGroups map[fdbv1beta2.ProcessGroupID]time.Time
	fdbClient.logger.V(1).Info("Fetching global coordination keys in FDB", "prefix", prefix)
	defer func() {
		fdbClient.logger.V(1).
			Info("Done fetching global coordination keys in FDB", "prefix", prefix, "results", len(resultProcessGroups))
	}()

	_, trErr := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		keyPrefix := path.Join(fdbClient.cluster.GetLockPrefix(), prefix)
		keyRange, err := fdb.PrefixRange([]byte(keyPrefix))
		if err != nil {
			return nil, err
		}

		results := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		processGroups := make(map[fdbv1beta2.ProcessGroupID]time.Time, len(results))
		for _, result := range results {
			processGroupID := path.Base(result.Key.String())
			fdbClient.logger.V(1).
				Info("Found result", "processGroupID", processGroupID, "key", result.Key.String())

			timestampBytes, err := tuple.Unpack(result.Value)
			if err != nil {
				return nil, err
			}

			if len(timestampBytes) < 1 {
				return nil, fmt.Errorf(
					"expected that the tuple contains one element, got %d elements",
					len(timestampBytes),
				)
			}

			timestamp, valid := timestampBytes[0].(int64)
			if !valid {
				return nil, fmt.Errorf("could not cast timestamp bytes into timestamp")
			}

			processGroups[fdbv1beta2.ProcessGroupID(processGroupID)] = time.Unix(timestamp, 0)
		}

		resultProcessGroups = processGroups
		return nil, nil
	})

	return resultProcessGroups, trErr
}

// getGlobalCoordinationKeys will return the entries under the provided prefix.
func (fdbClient *realFdbLibClient) clearGlobalCoordinationKeys(
	prefix string,
) error {
	fdbClient.logger.V(1).Info("Clearing global coordination keys in FDB", "prefix", prefix)
	defer func() {
		fdbClient.logger.V(1).
			Info("Done clearing global coordination keys in FDB", "prefix", prefix)
	}()

	_, err := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		keyPrefix := path.Join(fdbClient.cluster.GetLockPrefix(), prefix)
		keyRange, err := fdb.PrefixRange([]byte(keyPrefix))
		if err != nil {
			return nil, err
		}

		tr.ClearRange(keyRange)
		return nil, nil
	})

	return err
}

func (fdbClient *realFdbLibClient) updateProcessAddresses(
	updates map[fdbv1beta2.ProcessGroupID][]string,
) error {
	if len(updates) == 0 {
		return nil
	}

	fdbClient.logger.V(1).
		Info("Updating process addresses for global coordination in FDB", "prefix", processAddresses, "updates", len(updates))
	defer func() {
		fdbClient.logger.V(1).
			Info("Done updating process addresses for global coordination in FDB", "prefix", processAddresses, "updates", len(updates))
	}()

	keyPrefix := path.Join(
		fdbClient.cluster.GetLockPrefix(),
		processAddresses,
		fdbClient.cluster.Spec.ProcessGroupIDPrefix,
	)

	_, err := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		for processGroupID, addresses := range updates {
			if processGroupID.GetProcessClass() == fdbv1beta2.ProcessClassTest {
				continue
			}

			key := fdb.Key(path.Join(keyPrefix, string(processGroupID)))
			fdbClient.logger.V(1).
				Info("updateProcessAddresses update process group", "processGroupID", processGroupID, "addresses", addresses, "key", key.String())
			if len(addresses) > 0 {
				addressesBytes, parseError := json.Marshal(addresses)
				// Should we continue here?
				if parseError != nil {
					return nil, parseError
				}

				tr.Set(key, addressesBytes)
				continue
			}

			if len(addresses) == 0 {
				tr.Clear(key)
				continue
			}
		}

		return nil, nil
	})

	return err
}

// getProcessAddresses gets the process group IDs and their associated process addresses.
func (fdbClient *realFdbLibClient) getProcessAddresses(
	prefix string,
) (map[fdbv1beta2.ProcessGroupID][]string, error) {
	var result map[fdbv1beta2.ProcessGroupID][]string
	fdbClient.logger.V(1).
		Info("Fetching process addresses for global coordination in FDB", "prefix", prefix)
	defer func() {
		fdbClient.logger.V(1).
			Info("Done fetching process addresses for global coordination in FDB", "prefix", prefix, "results", len(result))
	}()

	_, err := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		keyPrefix := path.Join(fdbClient.cluster.GetLockPrefix(), processAddresses, prefix)
		keyRange, err := fdb.PrefixRange([]byte(keyPrefix))
		if err != nil {
			return nil, err
		}

		slice := tr.GetRange(keyRange, fdb.RangeOptions{}).GetSliceOrPanic()
		processGroups := make(map[fdbv1beta2.ProcessGroupID][]string, len(slice))
		for _, sliceResult := range slice {
			processGroupID := path.Base(sliceResult.Key.String())
			fdbClient.logger.V(1).
				Info("Found result", "processGroupID", processGroupID, "key", sliceResult.Key.String())

			var addresses []string
			parseErr := json.Unmarshal(sliceResult.Value, &addresses)
			if parseErr != nil {
				return nil, parseErr
			}

			processGroups[fdbv1beta2.ProcessGroupID(processGroupID)] = addresses
		}

		result = processGroups
		return nil, nil
	})

	return result, err
}

func checkError(err error) error {
	if err != nil {
		var fdbError *fdb.Error
		if errors.As(err, &fdbError) {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out.
			if fdbError.Code == 1031 {
				return fdbv1beta2.TimeoutError{Err: err}
			}

			// Api call through special keys failed. For more information, read the 0xff0xff/error_message key.
			if fdbError.Code == 2117 {
				return fdbv1beta2.SpecialKeysAPIFailureError{Err: err}
			}
		}

		return err
	}

	return nil
}

// setCommonOptions sets the common FDB transaction options.
func setCommonOptions(
	tr *fdb.Transaction,
) error {
	err := tr.Options().SetAccessSystemKeys()
	if err != nil {
		return err
	}

	// TODO (https://github.com/FoundationDB/fdb-kubernetes-operator/issues/2445): We should add an option to allow read
	// and writes to a locked database. Right now only the management API calls will be allowed in a locked database.
	return nil
}

func (fdbClient *realFdbLibClient) executeTransaction(
	operation func(tr fdb.Transaction) (any, error),
) (any, error) {
	db, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return nil, err
	}

	result, err := db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		err = setCommonOptions(&tr)
		if err != nil {
			return nil, err
		}

		return operation(tr)
	})

	return result, checkError(err)
}

// executeTransactionForManagementAPI will run an operation for the management API. This method handles all the common
// options.
func (fdbClient *realFdbLibClient) executeTransactionForManagementAPI(
	operation func(tr fdb.Transaction) error,
) error {
	_, err := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
		err := tr.Options().SetSpecialKeySpaceEnableWrites()
		if err != nil {
			return nil, err
		}

		// Allow the operator to read and write from/to a locked database e.g. in cases where a restore is ongoing.
		// This allows the operator to read and write from/to the FDB cluster.
		err = tr.Options().SetLockAware()
		if err != nil {
			return nil, err
		}

		return nil, operation(tr)
	})

	// In case that the retuned error is SpecialKeysAPIFailureError we have to make another transaction to get the actual
	// error message from FDB.
	var specialKeysErr fdbv1beta2.SpecialKeysAPIFailureError
	if errors.As(err, &specialKeysErr) {
		message, trErr := fdbClient.executeTransaction(func(tr fdb.Transaction) (any, error) {
			// Allow the operator to read and write from/to a locked database e.g. in cases where a restore is ongoing.
			// This allows the operator to read and write from/to the FDB cluster.
			optErr := tr.Options().SetLockAware()
			if optErr != nil {
				return nil, optErr
			}

			// Api call through special keys failed. For more information, read the 0xff0xff/error_message key.
			return tr.Get(fdb.Key("\xff\xff/error_message")).Get()
		})

		// If this transaction fails too, print out the additional information that 0xff0xff/error_message could not
		// be read.
		if trErr != nil {
			return fdbv1beta2.SpecialKeysAPIFailureError{
				Err: fmt.Errorf(
					"could not read error from special key 0xff0xff/error_message got: %w, original error: %w",
					trErr,
					err,
				),
			}
		}

		fdbClient.logger.V(1).
			Info("error message from special key 0xff0xff/error_message", "message", message, "err", err)
		return fdbv1beta2.SpecialKeysAPIFailureError{
			Err: fmt.Errorf(
				"error from special key 0xff0xff/error_message is: %s, original error: %w",
				message,
				err,
			),
		}
	}

	return err
}

// mockFdbLibClient is a mock for unit testing.
type mockFdbLibClient struct {
	// mockedOutput is the output returned by getValueFromDBUsingKey.
	mockedOutput []byte
	// mockedError is the error returned by executeTransaction.
	mockedError error
	// coordinationState represents the current coordination state.
	coordinationState map[string]time.Time
	// executeTransactionCallCount tracks how many times executeTransaction was called.
	executeTransactionCallCount int
}

var _ fdbLibClient = (*mockFdbLibClient)(nil)

// updateGlobalCoordinationKeys will update the provided updates in FDB.
func (fdbClient *mockFdbLibClient) updateGlobalCoordinationKeys(
	keyPrefix string,
	updates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction,
) error {
	for processGroupID, action := range updates {
		key := path.Join(keyPrefix, string(processGroupID))

		if action == fdbv1beta2.UpdateActionAdd {
			if _, ok := fdbClient.coordinationState[key]; !ok {
				fdbClient.coordinationState[key] = time.Now()
			}
			continue
		}

		if action == fdbv1beta2.UpdateActionDelete {
			delete(fdbClient.coordinationState, key)
			continue
		}
	}

	return fdbClient.mockedError
}

// getGlobalCoordinationKeys will return the entries under the provided prefix.
func (fdbClient *mockFdbLibClient) getGlobalCoordinationKeys(
	keyPrefix string,
) (map[fdbv1beta2.ProcessGroupID]time.Time, error) {
	result := map[fdbv1beta2.ProcessGroupID]time.Time{}
	for key, timeStamp := range fdbClient.coordinationState {
		if !strings.HasPrefix(key, keyPrefix) {
			continue
		}

		result[fdbv1beta2.ProcessGroupID(path.Base(key))] = timeStamp
	}

	return nil, fdbClient.mockedError
}

func (fdbClient *mockFdbLibClient) clearGlobalCoordinationKeys(
	keyPrefix string,
) error {
	for key := range fdbClient.coordinationState {
		if !strings.HasPrefix(key, keyPrefix) {
			continue
		}

		delete(fdbClient.coordinationState, path.Base(key))
	}

	return fdbClient.mockedError
}

func (fdbClient *mockFdbLibClient) updateProcessAddresses(
	_ map[fdbv1beta2.ProcessGroupID][]string,
) error {
	// TODO (johscheuer)
	return fdbClient.mockedError
}

func (fdbClient *mockFdbLibClient) getProcessAddresses(
	_ string,
) (map[fdbv1beta2.ProcessGroupID][]string, error) {
	// TODO (johscheuer)
	return nil, fdbClient.mockedError
}

func (fdbClient *mockFdbLibClient) executeTransactionForManagementAPI(
	_ func(tr fdb.Transaction) error,
) error {
	// TODO implement and add unit tests.
	return fdbClient.mockedError
}

func (fdbClient *mockFdbLibClient) executeTransaction(
	_ func(tr fdb.Transaction) (any, error),
) (any, error) {
	fdbClient.executeTransactionCallCount++
	return fdbClient.mockedOutput, fdbClient.mockedError
}
