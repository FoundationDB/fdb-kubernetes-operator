/*
 * foundationdb_env_variables.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022-2024 Apple Inc. and the FoundationDB project authors
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
	// EnvNameDNSName specifies the DNS locality (identifies the pod when using DNS in lieu of IP)
	EnvNameDNSName = "FDB_DNS_NAME"

	// EnvNameMachineId specifies the Machine ID locality. Also defines fault domain along with EnvNameZoneId
	EnvNameMachineId = "FDB_MACHINE_ID"

	// EnvNameZoneId specifies Zone ID locality. Also defines fault domain along with EnvNameMachineId.
	// The current default value of EnvNameZoneId is the hostname
	EnvNameZoneId = "FDB_ZONE_ID"

	// EnvNameClusterFile specifies the path to the cluster file / file containing the connection string.
	EnvNameClusterFile = "FDB_CLUSTER_FILE"

	// EnvNameTLSCaFile specifies the path to the certificate authority file for TLS connections
	EnvNameTLSCaFile = "FDB_TLS_CA_FILE"

	// EnvNameTLSCert specifies the path to the certificate file for TLS connections
	EnvNameTLSCert = "FDB_TLS_CERTIFICATE_FILE"

	// EnvNameTLSKeyFile specifies the path to the key file for TLS connections
	EnvNameTLSKeyFile = "FDB_TLS_KEY_FILE"

	// EnvNameTLSVerifiyPeers specifies the peer verification rules for incoming TLS connections to the split-image sidecar.
	// See https://apple.github.io/foundationdb/tls.html#peer-verification for the format
	EnvNameTLSVerifiyPeers = "FDB_TLS_VERIFY_PEERS"

	// EnvNameFDBNetworkSunsetThing specifies whether to ignore the failure to initialize some of the external clients
	// TODO FDB 7.3 adds a check for loading external client library, which doesn't work with 6.3.
	//  Consider remove this option once 6.3 is no longer being used.
	EnvNameFDBNetworkSunsetThing = "FDB_NETWORK_OPTION_IGNORE_EXTERNAL_CLIENT_FAILURES"

	// EnvNameFDBTraceLogGroup sets the 'LogGroup' attribute with the specified value for all events in the trace output files; default value is 'default'
	EnvNameFDBTraceLogGroup = "FDB_NETWORK_OPTION_TRACE_LOG_GROUP"

	// EnvNameFDBTraceLogDirPath enables trace logs output to a file in the given directory
	EnvNameFDBTraceLogDirPath = "FDB_NETWORK_OPTION_TRACE_ENABLE"

	// EnvNameFDBExternalClientDir specifies path to search for dynamic libraries and adds them to the list of client
	// libraries for use by the multi-version client API. Must be set before setting up the network.
	EnvNameFDBExternalClientDir = "FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY"

	// EnvNameClientThreadsPerVersion specifies the number of client threads to be spawned.  Each cluster will be
	// serviced by a single client thread. Spawns multiple worker threads for each version of the client that is loaded.
	// Setting this to a number greater than one implies disable_local_client.
	EnvNameClientThreadsPerVersion = "FDB_NETWORK_OPTION_CLIENT_THREADS_PER_VERSION"

	// EnvNameLDLibraryPath tells FoundationDB to load its primary client library from this directory instead of the directory provided by the image
	EnvNameLDLibraryPath = "LD_LIBRARY_PATH"

	// EnvNamePublicIP defines the public IP for the split-image-sidecar or unified FDB kubernetes monitor
	EnvNamePublicIP = "FDB_PUBLIC_IP"

	// EnvNamePodIP specifies the listen address for the split-image-sidecar or unified FDB kubernetes monitor
	EnvNamePodIP = "FDB_POD_IP"

	// EnvNamePodName tells the split-image-sidecar or unified FDB kubernetes monitor the name of its pod
	EnvNamePodName = "FDB_POD_NAME"

	// EnvNamePodNamespace tells the split-image-sidecar or unified FDB kubernetes monitor the K8s namespace it is running in
	EnvNamePodNamespace = "FDB_POD_NAMESPACE"

	// EnvNameNodeName tells the split-image-sidecar or unified FDB kubernetes monitor the K8s node it is running on
	EnvNameNodeName = "FDB_NODE_NAME"

	// EnvNameInstanceId specifies the instance ID to the split-image-sidecar or unified FDB kubernetes monitor
	EnvNameInstanceId = "FDB_INSTANCE_ID"
)
