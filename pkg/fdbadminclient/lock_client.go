/*
 * lock_client.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// LockClient provides a client for getting locks on operations for a cluster.
type LockClient interface {
	// Disabled determines whether the locking is disabled.
	Disabled() bool

	// TakeLock attempts to acquire a lock.
	TakeLock() (bool, error)

	// ReleaseLock will release the current lock. The method will only release the lock if the current
	// operator is the lock holder.
	ReleaseLock() error

	// AddPendingUpgrades registers information about which process groups are
	// pending an upgrade to a new version.
	AddPendingUpgrades(version fdbv1beta2.Version, processGroupIDs []fdbv1beta2.ProcessGroupID) error

	// GetPendingUpgrades returns the stored information about which process
	// groups are pending an upgrade to a new version.
	GetPendingUpgrades(version fdbv1beta2.Version) (map[fdbv1beta2.ProcessGroupID]bool, error)

	// ClearPendingUpgrades clears any stored information about pending
	// upgrades.
	ClearPendingUpgrades() error

	// GetDenyList retrieves the current deny list from the database.
	GetDenyList() ([]string, error)

	// UpdateDenyList updates the deny list to match a list of entries.
	UpdateDenyList(locks []fdbv1beta2.LockDenyListEntry) error
}
