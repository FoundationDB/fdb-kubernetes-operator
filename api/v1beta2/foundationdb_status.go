/*
 * foundationdb_status.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2022 Apple Inc. and the FoundationDB project authors
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

import (
	"fmt"
)

// FoundationDBStatus describes the status of the cluster as provided by
// FoundationDB itself.
type FoundationDBStatus struct {
	// Client provides the client section of the status.
	Client FoundationDBStatusLocalClientInfo `json:"client,omitempty"`

	// Cluster provides the cluster section of the status.
	Cluster FoundationDBStatusClusterInfo `json:"cluster,omitempty"`
}

// FoundationDBStatusLocalClientInfo contains information about the
// client connection from the process getting the status.
type FoundationDBStatusLocalClientInfo struct {
	// Coordinators provides information about the cluster's coordinators.
	Coordinators FoundationDBStatusCoordinatorInfo `json:"coordinators,omitempty"`

	// DatabaseStatus provides a summary of the database's health.
	DatabaseStatus FoundationDBStatusClientDBStatus `json:"database_status,omitempty"`
}

// FoundationDBStatusCoordinatorInfo contains information about the client's
// connection to the coordinators.
type FoundationDBStatusCoordinatorInfo struct {
	// Coordinators provides a list with coordinator details.
	Coordinators []FoundationDBStatusCoordinator `json:"coordinators,omitempty"`

	// QuorumReachable provides a summary if a quorum of the coordinators are reachable
	QuorumReachable bool `json:"quorum_reachable,omitempty"`
}

// FoundationDBStatusCoordinator contains information about one of the
// coordinators.
type FoundationDBStatusCoordinator struct {
	// Address provides the coordinator's address.
	Address ProcessAddress `json:"address,omitempty"`

	// Reachable indicates whether the coordinator is reachable.
	Reachable bool `json:"reachable,omitempty"`
}

// FoundationDBStatusClusterInfo describes the "cluster" portion of the
// cluster status
type FoundationDBStatusClusterInfo struct {
	// DatabaseConfiguration describes the current configuration of the
	// database.
	DatabaseConfiguration DatabaseConfiguration `json:"configuration,omitempty"`

	// Processes provides details on the processes that are reporting to the
	// cluster.
	Processes map[ProcessGroupID]FoundationDBStatusProcessInfo `json:"processes,omitempty"`

	// Data provides information about the data in the database.
	Data FoundationDBStatusDataStatistics `json:"data,omitempty"`

	// FullReplication indicates whether the database is fully replicated.
	FullReplication bool `json:"full_replication,omitempty"`

	// Generation indicates the current generation of this database.
	Generation int `json:"generation,omitempty"`

	// MaintenanceZone contains current zone under maintenance, if any.
	MaintenanceZone string `json:"maintenance_zone,omitempty"`

	// Clients provides information about clients that are connected to the
	// database.
	Clients FoundationDBStatusClusterClientInfo `json:"clients,omitempty"`

	// Layers provides information about layers that are running against the
	// cluster.
	Layers FoundationDBStatusLayerInfo `json:"layers,omitempty"`

	// FaultTolerance provides information about the fault tolerance status
	// of the cluster.
	FaultTolerance FaultTolerance `json:"fault_tolerance,omitempty"`

	// IncompatibleConnections provides information about processes that try to connect to the cluster with an
	// incompatible version.
	IncompatibleConnections []string `json:"incompatible_connections,omitempty"`

	// RecoveryState represents the recovery state.
	RecoveryState RecoveryState `json:"recovery_state,omitempty"`

	// ConnectionString represents the connection string in the cluster status json output.
	ConnectionString string `json:"connection_string,omitempty"`
}

// FaultTolerance provides information about the fault tolerance status
// of the cluster.
type FaultTolerance struct {
	// MaxZoneFailuresWithoutLosingData defines the maximum number of zones that can fail before losing data.
	MaxZoneFailuresWithoutLosingData int `json:"max_zone_failures_without_losing_data,omitempty"`
	// MaxZoneFailuresWithoutLosingAvailability defines the maximum number of zones that can fail before losing availability.
	MaxZoneFailuresWithoutLosingAvailability int `json:"max_zone_failures_without_losing_availability,omitempty"`
}

// FoundationDBStatusProcessInfo describes the "processes" portion of the
// cluster status
type FoundationDBStatusProcessInfo struct {
	// Address provides the address of the process.
	Address ProcessAddress `json:"address,omitempty"`

	// ProcessClass provides the process class the process has been given.
	ProcessClass ProcessClass `json:"class_type,omitempty"`

	// CommandLine provides the command-line invocation for the process.
	CommandLine string `json:"command_line,omitempty"`

	// Excluded indicates whether the process has been excluded.
	Excluded bool `json:"excluded,omitempty"`

	// The locality information for the process.
	Locality map[string]string `json:"locality,omitempty"`

	// The version of FoundationDB the process is running.
	Version string `json:"version,omitempty"`

	// The time that the process has been up for.
	UptimeSeconds float64 `json:"uptime_seconds,omitempty"`

	// Roles contains a slice of all roles of the process
	Roles []FoundationDBStatusProcessRoleInfo `json:"roles,omitempty"`

	// Messages contains error messages from that fdbserver process instance
	Messages []FoundationDBStatusProcessMessage `json:"messages,omitempty"`
}

// FoundationDBStatusProcessMessage represents an error message in the status json
type FoundationDBStatusProcessMessage struct {
	// Time when the error was observed
	Time float64 `json:"time,omitempty"`
	// The name of the error
	Name string `json:"name,omitempty"`
	// The type of the error
	Type string `json:"type,omitempty"`
}

// FoundationDBStatusProcessRoleInfo contains the minimal information from the process status
// roles.
type FoundationDBStatusProcessRoleInfo struct {
	// Role defines the role a process currently has
	Role string `json:"role,omitempty"`
	// StoredBytes defines the number of bytes that are currently stored for this process.
	StoredBytes int `json:"stored_bytes,omitempty"`
	// ID represent the role ID.
	ID string `json:"id,omitempty"`
}

// FoundationDBStatusDataStatistics provides information about the data in
// the database
type FoundationDBStatusDataStatistics struct {
	// KVBytes provides the total Key Value Bytes in the database.
	KVBytes int `json:"total_kv_size_bytes,omitempty"`

	// MovingData provides information about the current data movement.
	MovingData FoundationDBStatusMovingData `json:"moving_data,omitempty"`

	// State provides a summary of the state of data distribution.
	State FoundationDBStatusDataState `json:"state,omitempty"`
}

// FoundationDBStatusDataState provides information about the state of data
// distribution.
type FoundationDBStatusDataState struct {
	// Description provides a human-readable description of the data
	// distribution.
	Description string `json:"description,omitempty"`

	// Healthy determines if the data distribution is healthy.
	Healthy bool `json:"healthy,omitempty"`

	// Name provides a machine-readable identifier for the data distribution
	// state.
	Name string `json:"name,omitempty"`
}

// FoundationDBStatusMovingData provides information about the current data
// movement
type FoundationDBStatusMovingData struct {
	// HighestPriority provides the priority of the highest-priority data
	// movement.
	HighestPriority int `json:"highest_priority,omitempty"`

	// InFlightBytes provides how many bytes are being actively moved.
	InFlightBytes int `json:"in_flight_bytes,omitempty"`

	// InQueueBytes provides how many bytes are pending data movement.
	InQueueBytes int `json:"in_queue_bytes,omitempty"`
}

// FoundationDBStatusClientDBStatus represents the databaseStatus field in the
// JSON database status
type FoundationDBStatusClientDBStatus struct {
	// Available indicates whether the database is accepting traffic.
	Available bool `json:"available,omitempty"`

	// Healthy indicates whether the database is fully healthy.
	Healthy bool `json:"healthy,omitempty"`
}

// FoundationDBStatusClusterClientInfo represents the connected client details in the
// cluster status.
type FoundationDBStatusClusterClientInfo struct {
	// Count provides the number of clients connected to the database.
	Count int `json:"count,omitempty"`

	// SupportedVersions provides information about the versions supported by
	// the connected clients.
	SupportedVersions []FoundationDBStatusSupportedVersion `json:"supported_versions,omitempty"`
}

// FoundationDBStatusSupportedVersion provides information about a version of
// FDB supported by the connected clients.
type FoundationDBStatusSupportedVersion struct {
	// ClientVersion provides the version of FDB the client is connecting
	// through.
	ClientVersion string `json:"client_version,omitempty"`

	// ConnectedClient provides the clients that are using this version.
	ConnectedClients []FoundationDBStatusConnectedClient `json:"connected_clients"`

	// MaxProtocolClients provides the clients that are using this version as
	// their highest supported protocol version.
	MaxProtocolClients []FoundationDBStatusConnectedClient `json:"max_protocol_clients"`

	// ProtocolVersion is the version of the wire protocol the client is using.
	ProtocolVersion string `json:"protocol_version,omitempty"`

	// SourceVersion is the version of the source code that the client library
	// was built from.
	SourceVersion string `json:"source_version,omitempty"`
}

// FoundationDBStatusConnectedClient provides information about a client that
// is connected to the database.
type FoundationDBStatusConnectedClient struct {
	// Address provides the address the client is connecting from.
	Address string `json:"address,omitempty"`

	// LogGroup provides the trace log group the client has set.
	LogGroup string `json:"log_group,omitempty"`
}

// Description returns a string description of the a connected client.
func (client FoundationDBStatusConnectedClient) Description() string {
	if client.LogGroup == "default" || client.LogGroup == "" {
		return client.Address
	}
	return fmt.Sprintf("%s (%s)", client.Address, client.LogGroup)
}

// FoundationDBStatusLayerInfo provides information about layers that are
// running against the cluster.
type FoundationDBStatusLayerInfo struct {
	// Backup provides information about backups that have been started.
	Backup FoundationDBStatusBackupInfo `json:"backup,omitempty"`

	// The error from the layer status.
	Error string `json:"_error,omitempty"`
}

// FoundationDBStatusBackupInfo provides information about backups that have been started.
type FoundationDBStatusBackupInfo struct {
	// Paused tells whether the backups are paused.
	Paused bool `json:"paused,omitempty"`

	// Tags provides information about specific backups.
	Tags map[string]FoundationDBStatusBackupTag `json:"tags,omitempty"`
}

// FoundationDBStatusBackupTag provides information about a backup under a tag
// in the cluster status.
type FoundationDBStatusBackupTag struct {
	CurrentContainer string `json:"current_container,omitempty"`
	RunningBackup    bool   `json:"running_backup,omitempty"`
	Restorable       bool   `json:"running_backup_is_restorable,omitempty"`
}

// ProcessRole models the role of a pod.
type ProcessRole string

const (
	// ProcessRoleCoordinator model for FDB coordinator role.
	ProcessRoleCoordinator ProcessRole = "coordinator"
	// ProcessRoleClusterController model for FDB cluster_controller role
	ProcessRoleClusterController ProcessRole = "cluster_controller"
	// ProcessRoleStorage model for FDB storage role
	ProcessRoleStorage ProcessRole = "storage"
	// ProcessRoleLog model for FDB log role
	ProcessRoleLog ProcessRole = "log"
	// ProcessRoleSequencer model for FDB sequencer role
	ProcessRoleSequencer ProcessRole = "sequencer"
	// ProcessRoleMaster model for FDB master role
	ProcessRoleMaster ProcessRole = "master"
	// ProcessRoleProxy model for FDB proxy role
	ProcessRoleProxy ProcessRole = "proxy"
	// ProcessRoleGrvProxy model for FDB grv_proxy role
	ProcessRoleGrvProxy ProcessRole = "grv_proxy"
	// ProcessRoleCommitProxy model for FDB commit_proxy role
	ProcessRoleCommitProxy ProcessRole = "commit_proxy"
	// ProcessRoleResolver model for FDB resolver role
	ProcessRoleResolver ProcessRole = "resolver"
	// ProcessRoleDataDistributor model for FDB data_distributor role
	ProcessRoleDataDistributor ProcessRole = "data_distributor"
	// ProcessRoleRatekeeper model for FDB ratekeeper role
	ProcessRoleRatekeeper ProcessRole = "ratekeeper"
)

// RecoveryState represents the recovery state from the FDB cluster json.
type RecoveryState struct {
	// ActiveGenerations represent the current active generations.
	ActiveGenerations int `json:"active_generations,omitempty"`
	// Name represent the name of the current recovery state.
	Name string `json:"name,omitempty"`
	// SecondsSinceLastRecovered represents the seconds since the last recovery.
	SecondsSinceLastRecovered float64 `json:"seconds_since_last_recovered,omitempty"`
}
