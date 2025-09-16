/*
 * foundationdb_errors.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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
