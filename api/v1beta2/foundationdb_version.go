/*
 * foundationdb_version.go
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
	"github.com/apple/foundationdb/fdbkubernetesmonitor/api"
)

// Version represents a version of FoundationDB.
//
// This provides convenience methods for checking features available in
// different versions.
type Version struct {
	api.Version
}

// ParseFdbVersion parses a version from its string representation.
func ParseFdbVersion(version string) (Version, error) {
	parsed, err := api.ParseFdbVersion(version)
	if err != nil {
		return Version{}, err
	}

	return Version{parsed}, nil
}

// IsSupported defines the minimum supported FDB version.
func (version Version) IsSupported() bool {
	return version.IsAtLeast(Versions.MinimumVersion)
}

// IsStorageEngineSupported return true if storage engine is supported by FDB version.
func (version Version) IsStorageEngineSupported(storageEngine StorageEngine) bool {
	switch storageEngine {
	case StorageEngineShardedRocksDB:
		return version.IsAtLeast(Versions.SupportsShardedRocksDB)
	case StorageEngineRedwood1:
		return version.IsAtLeast(Versions.SupportsRedwood1)
	}

	return true
}

// IsReleaseCandidate returns true if the version is a release candidate or not
func (version Version) IsReleaseCandidate() bool {
	return version.ReleaseCandidate > 0
}

// SupportsIsPresent returns true if the sidecar of this version supports the is_present endpoint
func (version Version) SupportsIsPresent() bool {
	return version.IsAtLeast(Versions.SupportsIsPresent)
}

// SupportsRecoveryState returns true if the version of FDB supports the recovered since field.
func (version Version) SupportsRecoveryState() bool {
	return version.IsAtLeast(Versions.SupportsRecoveryState)
}

// SupportsVersionChange returns true if the current version can be downgraded or upgraded to provided other version.
func (version Version) SupportsVersionChange(other Version) bool {
	return version.IsProtocolCompatible(other) || other.IsAtLeast(version)
}

// SupportsLocalityBasedExclusions returns true if the current version supports locality based exclusions.
func (version Version) SupportsLocalityBasedExclusions() bool {
	// If the version is 7.1.* we have to check if it supports locality based exclusions. For all newer versions
	// we will check against the 7.3 version.
	if version.IsProtocolCompatible(Version{api.Version{Major: 7, Minor: 1, Patch: 0}}) {
		return version.IsAtLeast(Versions.SupportsLocalityBasedExclusions71)
	}

	return version.IsAtLeast(Versions.SupportsLocalityBasedExclusions)
}

// SupportsBackupEncryption returns true if the current version supports encryption of backups.
func (version Version) SupportsBackupEncryption() bool {
	return version.IsAtLeast(Versions.SupportsBackupEncryption)
}

// AutomaticallyRemovesDeadTesterProcesses returns true if the FDB version automatically removes old tester processes
// from the list of processes.
func (version Version) AutomaticallyRemovesDeadTesterProcesses() bool {
	if version.IsProtocolCompatible(Version{api.Version{Major: 7, Minor: 3, Patch: 0}}) {
		return version.IsAtLeast(Version{api.Version{Major: 7, Minor: 3, Patch: 35}})
	}

	return version.IsAtLeast(Version{api.Version{Major: 7, Minor: 1, Patch: 55}})
}

// IsProtocolCompatible determines whether two versions of FDB are protocol
// compatible.
func (version Version) IsProtocolCompatible(other Version) bool {
	return version.Version.IsProtocolCompatible(other.Version)
}

// Equal checks if two Version are the same.
func (version Version) Equal(other Version) bool {
	return version.Version.Equal(other.Version)
}

// IsAtLeast determines if a version is greater than or equal to another version.
func (version Version) IsAtLeast(other Version) bool {
	return version.Version.IsAtLeast(other.Version)
}

// NextMajorVersion returns the next major version of FoundationDB.
func (version Version) NextMajorVersion() Version {
	return Version{version.Version.NextMajorVersion()}
}

// NextMinorVersion returns the next minor version of FoundationDB.
func (version Version) NextMinorVersion() Version {
	return Version{version.Version.NextMinorVersion()}
}

// NextPatchVersion returns the next patch version of FoundationDB.
func (version Version) NextPatchVersion() Version {
	return Version{version.Version.NextPatchVersion()}
}

// Versions provides a shorthand for known versions.
// This is only to be used in testing.
var Versions = struct {
	NextMajorVersion,
	NextPatchVersion,
	MinimumVersion,
	SupportsIsPresent,
	SupportsShardedRocksDB,
	SupportsRedwood1,
	SupportsBackupEncryption,
	IncompatibleVersion,
	PreviousPatchVersion,
	SupportsRecoveryState,
	SupportsLocalityBasedExclusions71,
	SupportsLocalityBasedExclusions,
	Default Version
}{
	Default:                           Version{api.Version{Major: 7, Minor: 1, Patch: 57}},
	IncompatibleVersion:               Version{api.Version{Major: 7, Minor: 0, Patch: 0}},
	PreviousPatchVersion:              Version{api.Version{Major: 7, Minor: 1, Patch: 56}},
	NextPatchVersion:                  Version{api.Version{Major: 7, Minor: 1, Patch: 68}},
	NextMajorVersion:                  Version{api.Version{Major: 8, Minor: 0, Patch: 0}},
	MinimumVersion:                    Version{api.Version{Major: 7, Minor: 1, Patch: 0}},
	SupportsIsPresent:                 Version{api.Version{Major: 7, Minor: 1, Patch: 4}},
	SupportsShardedRocksDB:            Version{api.Version{Major: 7, Minor: 2, Patch: 0}},
	SupportsRedwood1:                  Version{api.Version{Major: 7, Minor: 3, Patch: 0}},
	SupportsRecoveryState:             Version{api.Version{Major: 7, Minor: 1, Patch: 22}},
	SupportsLocalityBasedExclusions71: Version{api.Version{Major: 7, Minor: 1, Patch: 42}},
	SupportsLocalityBasedExclusions:   Version{api.Version{Major: 7, Minor: 3, Patch: 26}},
	SupportsBackupEncryption:          Version{api.Version{Major: 7, Minor: 3, Patch: 0}},
}
