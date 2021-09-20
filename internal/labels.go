/*
 * labels.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package internal

const (
	// OldFDBInstanceIDLabel represents the label that is used to represent a instance ID
	// Deprecated: This label will not be applied by default in the future.
	OldFDBInstanceIDLabel = "fdb-instance-id"

	// OldFDBProcessClassLabel represents the label that is used to represent the process class
	// Deprecated: This label will not be applied by default in the future.
	OldFDBProcessClassLabel = "fdb-process-class"

	// OldFDBClusterLabel represents the label that is used to represent the cluster of an instance
	// Deprecated: This label will not be applied by default in the future.
	OldFDBClusterLabel = "fdb-cluster-name"
)
