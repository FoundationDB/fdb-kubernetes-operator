/*
 * foundationdb_constants.go
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

const (
	// MainContainerName represents the container name of the main FoundationDB container.
	MainContainerName = "foundationdb"

	// SidecarContainerName represents the container name of the sidecar container.
	SidecarContainerName = "foundationdb-kubernetes-sidecar"

	// InitContainerName represents the container name of the init container.
	InitContainerName = "foundationdb-kubernetes-init"

	// NoneFaultDomainKey represents the none fault domain, where every Pod is a fault domain.
	NoneFaultDomainKey = "foundationdb.org/none"

	/*
		Config map constants
	*/

	// ClusterFileKey defines the key name in the ConfigMap whose value is the content of the cluster file, the connection string.
	ClusterFileKey = "cluster-file"

	// CaFileKey defines the key name in the ConfigMap whose value contains the trusted certificate authority PEM
	CaFileKey = "ca-file"

	// SidecarConfKey defines the key name in the ConfigMap whose value contains the configuration for the sidecar
	SidecarConfKey = "sidecar-conf"

	// RunningVersionKey defines the key name in the ConfigMap whose value is the FDB version that the cluster is currently running.
	RunningVersionKey = "running-version"
)
