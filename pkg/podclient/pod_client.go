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
	// IsPresent checks whether a file in the sidecar is present
	IsPresent(filename string) (bool, error)

	// CheckHash checks whether a file in the sidecar has the expected contents.
	CheckHash(filename string, contents string) (bool, error)

	// GenerateMonitorConf updates the monitor conf file for a pod
	GenerateMonitorConf() error

	// CopyFiles copies the files from the config map to the shared dynamic conf
	// volume
	CopyFiles() error

	// GetVariableSubstitutions gets the current keys and values that this
	// process group will substitute into its monitor conf.
	GetVariableSubstitutions() (map[string]string, error)
}
