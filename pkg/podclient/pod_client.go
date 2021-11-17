/*
 * pod_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2021 Apple Inc. and the FoundationDB project authors
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

package podclient

// FdbPodClient provides methods for working with a FoundationDB pod
type FdbPodClient interface {
	// IsPresent checks whether a file is present.
	IsPresent(path string) (bool, error)

	// UpdateFile checks if a file is up-to-date and tries to update it.
	UpdateFile(name string, contents string) (bool, error)

	// GetVariableSubstitutions gets the current keys and values that this
	// process group will substitute into its monitor conf.
	GetVariableSubstitutions() (map[string]string, error)
}
