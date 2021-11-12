/*
 * admin_client.go
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

package fdbadminclient

import (
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// AdminClient describes an interface for running administrative commands on a
// cluster
type AdminClient interface {
	// GetStatus gets the database's status
	GetStatus() (*fdbtypes.FoundationDBStatus, error)

	// ConfigureDatabase sets the database configuration
	ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error

	// ExcludeProcesses starts evacuating processes so that they can be removed
	// from the database.
	ExcludeProcesses(addresses []fdbtypes.ProcessAddress) error

	// IncludeProcesses removes processes from the exclusion list and allows
	// them to take on roles again.
	IncludeProcesses(addresses []fdbtypes.ProcessAddress) error

	// GetExclusions gets a list of the addresses currently excluded from the
	// database.
	GetExclusions() ([]fdbtypes.ProcessAddress, error)

	// CanSafelyRemove checks whether it is safe to remove processes from the
	// cluster.
	//
	// The list returned by this method will be the addresses that are *not*
	// safe to remove.
	CanSafelyRemove(addresses []fdbtypes.ProcessAddress) ([]fdbtypes.ProcessAddress, error)

	// KillProcesses restarts processes
	KillProcesses(addresses []fdbtypes.ProcessAddress) error

	// ChangeCoordinators changes the coordinator set
	ChangeCoordinators(addresses []fdbtypes.ProcessAddress) (string, error)

	// GetConnectionString fetches the latest connection string.
	GetConnectionString() (string, error)

	// VersionSupported reports whether we can support a cluster with a given
	// version.
	VersionSupported(version string) (bool, error)

	// GetProtocolVersion determines the protocol version that is used by a
	// version of FDB.
	GetProtocolVersion(version string) (string, error)

	// StartBackup starts a new backup.
	StartBackup(url string, snapshotPeriodSeconds int) error

	// StopBackup stops a backup.
	StopBackup(url string) error

	// PauseBackups pauses the backups.
	PauseBackups() error

	// ResumeBackups resumes the backups.
	ResumeBackups() error

	// ModifyBackup modifies the configuration of the backup.
	ModifyBackup(int) error

	// GetBackupStatus gets the status of the current backup.
	GetBackupStatus() (*fdbtypes.FoundationDBLiveBackupStatus, error)

	// StartRestore starts a new restore.
	StartRestore(url string, keyRanges []fdbtypes.FoundationDBKeyRange) error

	// GetRestoreStatus gets the status of the current restore.
	GetRestoreStatus() (string, error)

	// Close shuts down any resources for the client once it is no longer
	// needed.
	Close() error

	// GetCoordinatorSet returns a set of the current coordinators.
	GetCoordinatorSet() (map[string]struct{}, error)
}
