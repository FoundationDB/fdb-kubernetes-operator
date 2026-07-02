/*
 * fdb_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	fdberrors "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/errors"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
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

	result, err := db.Transact(func(tr fdb.Transaction) (any, error) {
		err = setCommonOptions(&tr)
		if err != nil {
			return nil, err
		}

		return operation(tr)
	})

	return result, checkError(err)
}

// executeTransactionForManagementAPI will run an operation for the management API. This method handles all the common
// options. In case that an error is reported during commit time the \xff\xff/error_message key is read and the additional
// information is reported.
//
// We are not able to use db.Transact here because this would cause a double commit which is not supported and would
// result in a "FoundationDB error code 2017 (Operation issued while a commit was outstanding)" error. The commit inside
// the transaction is required because at this time the special key error in \xff\xff/error_message will be propagated
// if an error occurred.
func (fdbClient *realFdbLibClient) executeTransactionForManagementAPI(
	operation func(tr fdb.Transaction) error,
) error {
	db, err := getFDBDatabase(fdbClient.cluster)
	if err != nil {
		return err
	}

	tr, err := db.CreateTransaction()
	if err != nil {
		return err
	}

	for {
		if err = setCommonOptions(&tr); err != nil {
			return err
		}
		if err = tr.Options().SetSpecialKeySpaceEnableWrites(); err != nil {
			return err
		}
		if err = tr.Options().SetLockAware(); err != nil {
			return err
		}

		opErr := operation(tr)
		if opErr == nil {
			opErr = tr.Commit().Get()
		}

		if opErr == nil {
			// Committed successfully, done
			return nil
		}

		// commit/operation failed — tr is still valid here, read the special key before reset.
		opErr = fetchSpecialKeyErrorMessage(fdbClient.logger, tr, opErr)
		if opErr == nil {
			// fetchSpecialKeyErrorMessage determined this wasn't actually a failure (e.g. the coordinators
			// no-op case), so the commit above already succeeded.
			return nil
		}

		// A special_keys_api_failure reports its own retriable flag via the error_message special key. FDB's
		// own onError() does not know about this — the underlying fdb.Error is always special_keys_api_failure,
		// which is not one of the codes FDB's client considers automatically retryable — so we must honor the
		// retriable flag ourselves and reset the transaction manually before trying again.
		var specialKeysErr fdbv1beta2.SpecialKeysAPIFailureError
		if errors.As(opErr, &specialKeysErr) {
			if !specialKeysErr.Retriable {
				return opErr
			}

			tr.Reset()
			continue
		}

		// Otherwise fall back to letting FDB's client decide if the error is retryable (e.g. conflicts, timeouts).
		var fdbErr fdb.Error
		if errors.As(opErr, &fdbErr) {
			if retryErr := tr.OnError(fdbErr).Get(); retryErr != nil {
				return checkError(opErr) // not retryable, give up
			}

			// retryable: OnError already reset tr, loop and try again
			continue
		}

		return opErr
	}
}

// fetchSpecialKeyErrorMessage will attempt to fetch additional information from \xff\xff/error_message if an error was
// provided. If the error is nil it will return nil and if the Get request for \xff\xff/error_message doesn't return any
// additional information it returns the same error.
func fetchSpecialKeyErrorMessage(logger logr.Logger, tr fdb.Transaction, err error) error {
	if fdberrors.IsSpecialKeysAPIFailureError(err) {
		// To get more information on why the Api call through special keys failed we need to read the 0xff0xff/error_message key.
		errMessage, trErr := tr.Get(fdb.Key("\xff\xff/error_message")).Get()

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

		logger.V(1).
			Info("error message from special key 0xff0xff/error_message", "message", errMessage, "err", err)

		mgmtAPIError := &fdbv1beta2.ManagementAPIError{}
		jsonErr := json.Unmarshal(errMessage, mgmtAPIError)
		if jsonErr != nil {
			logger.Info("could not parse special key error_message", "error", jsonErr.Error())
			return fdbv1beta2.SpecialKeysAPIFailureError{
				Err: fmt.Errorf(
					"error from special key 0xff0xff/error_message is: %s, original error: %w",
					errMessage,
					err,
				),
			}
		}

		// fdbcli does the same, this error means the current coordinators where already the coordinators of the cluster
		// https://github.com/apple/foundationdb/blob/main/fdbcli/CoordinatorsCommand.cpp#L163-L169
		// NOTE(johscheuer): There might be more special cases and handling in fdbcli.
		if ptr.Deref(mgmtAPIError.Command, "") == "coordinators" &&
			ptr.Deref(
				mgmtAPIError.Message,
				"",
			) == "No change (existing configuration satisfies request)" {
			logger.V(1).
				Info("coordinator command didn't change nay coordinators. The provided coordinators were already recruited.")
			return nil
		}

		return fdbv1beta2.SpecialKeysAPIFailureError{
			Err: fmt.Errorf(
				"error from special key 0xff0xff/error_message is: %s, original error: %w",
				ptr.Deref(mgmtAPIError.Message, ""),
				err,
			),
			Retriable: ptr.Deref(mgmtAPIError.Retriable, false),
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
