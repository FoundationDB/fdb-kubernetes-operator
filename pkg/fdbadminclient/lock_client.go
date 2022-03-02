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
	"github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"
)

// LockClient provides a client for getting locks on operations for a cluster.
type LockClient interface {
	// Disabled determines whether the locking is disabled.
	Disabled() bool

	// TakeLock attempts to acquire a lock.
	TakeLock() (bool, error)

	// AddPendingUpgrades registers information about which process groups are
	// pending an upgrade to a new version.
	AddPendingUpgrades(version fdb.FdbVersion, processGroupIDs []string) error

	// GetPendingUpgrades returns the stored information about which process
	// groups are pending an upgrade to a new version.
	GetPendingUpgrades(version fdb.FdbVersion) (map[string]bool, error)

	// ClearPendingUpgrades clears any stored information about pending
	// upgrades.
	ClearPendingUpgrades() error

	// GetDenyList retrieves the current deny list from the database.
	GetDenyList() ([]string, error)

	// UpdateDenyList updates the deny list to match a list of entries.
	UpdateDenyList(locks []v1beta1.LockDenyListEntry) error
}
