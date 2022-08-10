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
	"fmt"
	"regexp"
	"strconv"
)

// Version represents a version of FoundationDB.
//
// This provides convenience methods for checking features available in
// different versions.
type Version struct {
	// Major is the major version
	Major int

	// Minor is the minor version
	Minor int

	// Patch is the patch version
	Patch int

	// ReleaseCandidate is the number from the `-rc\d+` suffix version
	// of the version if it exists
	ReleaseCandidate int
}

// VersionRegex describes the format of a FoundationDB version.
var VersionRegex = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)(-rc(\d+))?`)

// ParseFdbVersion parses a version from its string representation.
func ParseFdbVersion(version string) (Version, error) {
	matches := VersionRegex.FindStringSubmatch(version)
	if matches == nil {
		return Version{}, fmt.Errorf("could not parse FDB version from %s", version)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return Version{}, err
	}

	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return Version{}, err
	}

	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return Version{}, err
	}

	rc, err := strconv.Atoi(matches[5])
	if err != nil {
		rc = 0
	}

	return Version{Major: major, Minor: minor, Patch: patch, ReleaseCandidate: rc}, nil
}

// String gets the string representation of an FDB version.
func (version Version) String() string {
	if version.ReleaseCandidate == 0 {
		return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
	}
	return fmt.Sprintf("%d.%d.%d-rc%d", version.Major, version.Minor, version.Patch, version.ReleaseCandidate)
}

// Compact prints the version in the major.minor format.
func (version Version) Compact() string {
	return fmt.Sprintf("%d.%d", version.Major, version.Minor)
}

// IsAtLeast determines if a version is greater than or equal to another version.
func (version Version) IsAtLeast(other Version) bool {
	if version.Major < other.Major {
		return false
	}
	if version.Major > other.Major {
		return true
	}
	if version.Minor < other.Minor {
		return false
	}
	if version.Minor > other.Minor {
		return true
	}
	if version.Patch < other.Patch {
		return false
	}
	if version.Patch > other.Patch {
		return true
	}
	if version.ReleaseCandidate == 0 {
		return true
	}
	if other.ReleaseCandidate == 0 {
		return false
	}
	if version.ReleaseCandidate < other.ReleaseCandidate {
		return false
	}
	if version.ReleaseCandidate > other.ReleaseCandidate {
		return true
	}
	return true
}

// GetBinaryVersion Returns a version string compatible with the log implemented in the sidecars
func (version Version) GetBinaryVersion() string {
	if version.ReleaseCandidate > 0 {
		return version.String()
	}
	return version.Compact()
}

// IsProtocolCompatible determines whether two versions of FDB are protocol
// compatible.
func (version Version) IsProtocolCompatible(other Version) bool {
	return version.Major == other.Major && version.Minor == other.Minor && version.ReleaseCandidate == other.ReleaseCandidate
}

// HasNonBlockingExcludes determines if a version has support for non-blocking
// exclude commands.
func (version Version) HasNonBlockingExcludes(useNonBlockingExcludes bool) bool {
	return version.IsAtLeast(Version{Major: 6, Minor: 3, Patch: 5}) && useNonBlockingExcludes
}

// HasSeparatedProxies determines if a version has support for separate
// grv/commit proxies
func (version Version) HasSeparatedProxies() bool {
	return version.IsAtLeast(Version{Major: 7, Minor: 0, Patch: 0})
}

// NextMajorVersion returns the next major version of FoundationDB.
func (version Version) NextMajorVersion() Version {
	return Version{Major: version.Major + 1, Minor: 0, Patch: 0}
}

// NextMinorVersion returns the next minor version of FoundationDB.
func (version Version) NextMinorVersion() Version {
	return Version{Major: version.Major, Minor: version.Minor + 1, Patch: 0}
}

// NextPatchVersion returns the next patch version of FoundationDB.
func (version Version) NextPatchVersion() Version {
	return Version{Major: version.Major, Minor: version.Minor, Patch: version.Patch + 1}
}

// Equal checks if two Version are the same.
func (version Version) Equal(other Version) bool {
	return version.Major == other.Major &&
		version.Minor == other.Minor &&
		version.Patch == other.Patch &&
		version.ReleaseCandidate == other.ReleaseCandidate
}

// IsSupported defines the minimum supported FDB version.
func (version Version) IsSupported() bool {
	return version.IsAtLeast(Versions.MinimumVersion)
}

// IsStorageEngineSupported return true if storage engine is supported by FDB version.
func (version Version) IsStorageEngineSupported(storageEngine StorageEngine) bool {
	if storageEngine == StorageEngineRocksDbV1 {
		return version.IsAtLeast(Versions.SupportsRocksDBV1)
	} else if storageEngine == StorageEngineRocksDbExperimental {
		return !version.IsAtLeast(Versions.SupportsRocksDBV1)
	} else if storageEngine == StorageEngineShardedRocksDB {
		return version.IsAtLeast(Versions.SupportsShardedRocksDB)
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

// Versions provides a shorthand for known versions.
// This is only to be used in testing.
var Versions = struct {
	NextMajorVersion,
	NextPatchVersion,
	MinimumVersion,
	SupportsRocksDBV1,
	SupportsIsPresent,
	SupportsShardedRocksDB,
	Default Version
}{
	Default:                Version{Major: 6, Minor: 2, Patch: 20},
	NextPatchVersion:       Version{Major: 6, Minor: 2, Patch: 21},
	NextMajorVersion:       Version{Major: 7, Minor: 0, Patch: 0},
	MinimumVersion:         Version{Major: 6, Minor: 2, Patch: 20},
	SupportsRocksDBV1:      Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 4},
	SupportsIsPresent:      Version{Major: 7, Minor: 1, Patch: 4},
	SupportsShardedRocksDB: Version{Major: 7, Minor: 2, Patch: 0},
}
