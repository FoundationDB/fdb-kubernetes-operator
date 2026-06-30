/*
 * foundationdb_errors.go
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

package v1beta2

import "fmt"

// TimeoutError represents a timeout for either the fdb client library or fdbcli.
// See: https://github.com/apple/foundationdb/blob/main/flow/include/flow/error_definitions.h
// +k8s:deepcopy-gen=false
type TimeoutError struct {
	Err error
}

// BackupDoesNotExist represents an error for the backup command when the targeted backup doesn't exist.
// +k8s:deepcopy-gen=false
type BackupDoesNotExist struct {
	Err error
}

// BackupNotRunning represents an error for the backup command when the targeted backup is not running.
// +k8s:deepcopy-gen=false
type BackupNotRunning struct {
	Err error
}

// RestoreDoesNotExist represents an error for the restore command when the targeted restore does not exist.
// +k8s:deepcopy-gen=false
type RestoreDoesNotExist struct {
	Err error
}

// SpecialKeysAPIFailureError represents the error when an Api call through special keys failed.
// For more information, call get on special key 0xff0xff/error_message to get a json string of the error message.
// See: https://github.com/apple/foundationdb/blob/main/flow/include/flow/error_definitions.h
// +k8s:deepcopy-gen=false
type SpecialKeysAPIFailureError struct {
	Err error
}

// Error returns the error message of the internal timeout error.
func (err TimeoutError) Error() string {
	return fmt.Sprintf("fdb timeout: %s", err.Err.Error())
}

// Error returns the error message of the internal backup does not exist error.
func (err BackupDoesNotExist) Error() string {
	return fmt.Sprintf("fdb backup does not exist: %s", err.Err.Error())
}

// Error returns the error message of the internal backup not running error.
func (err BackupNotRunning) Error() string {
	return fmt.Sprintf("fdb backup does not exist: %s", err.Err.Error())
}

// Error returns the error message from a failed Api call through special keys.
func (err SpecialKeysAPIFailureError) Error() string {
	return fmt.Sprintf("fdb special_keys_api_failure: %s", err.Err.Error())
}

// Error returns the error message of the fdbrestore command if no restore is present.
func (err RestoreDoesNotExist) Error() string {
	return fmt.Sprintf("fdb restore does not exist: %s", err.Err.Error())
}

// ManagementAPIError represents the ManagementAPIError from the FDB management API.
// +k8s:deepcopy-gen=false
type ManagementAPIError struct {
	// Retriable true if the error can be retried.
	Retriable *bool `json:"retriable,omitempty"`
	// Command the command that was tried to be executed.
	Command *string `json:"command,omitempty"`
	// Message the error message.
	Message *string `json:"message,omitempty"`
}
