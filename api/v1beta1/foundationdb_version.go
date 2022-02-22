/*
 * foundationdb_version.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package v1beta1

import (
	"fmt"
	"regexp"
	"strconv"
)

// FdbVersion represents a version of FoundationDB.
//
// This provides convenience methods for checking features available in
// different versions.
type FdbVersion struct {
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

// FDBVersionRegex describes the format of a FoundationDB version.
var FDBVersionRegex = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)(-rc(\d+))?`)

// ParseFdbVersion parses a version from its string representation.
func ParseFdbVersion(version string) (FdbVersion, error) {
	matches := FDBVersionRegex.FindStringSubmatch(version)
	if matches == nil {
		return FdbVersion{}, fmt.Errorf("could not parse FDB version from %s", version)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return FdbVersion{}, err
	}

	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return FdbVersion{}, err
	}

	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return FdbVersion{}, err
	}

	rc, err := strconv.Atoi(matches[5])
	if err != nil {
		rc = 0
	}

	return FdbVersion{Major: major, Minor: minor, Patch: patch, ReleaseCandidate: rc}, nil
}

// String gets the string representation of an FDB version.
func (version FdbVersion) String() string {
	if version.ReleaseCandidate == 0 {
		return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
	}
	return fmt.Sprintf("%d.%d.%d-rc%d", version.Major, version.Minor, version.Patch, version.ReleaseCandidate)
}

// Compact prints the version in the major.minor format.
func (version FdbVersion) Compact() string {
	return fmt.Sprintf("%d.%d", version.Major, version.Minor)
}

// IsAtLeast determines if a version is greater than or equal to another version.
func (version FdbVersion) IsAtLeast(other FdbVersion) bool {
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
	if version.ReleaseCandidate < other.ReleaseCandidate {
		return false
	}
	if version.ReleaseCandidate > other.ReleaseCandidate {
		return true
	}
	return true
}

// IsProtocolCompatible determines whether two versions of FDB are protocol
// compatible.
func (version FdbVersion) IsProtocolCompatible(other FdbVersion) bool {
	return version.Major == other.Major && version.Minor == other.Minor && version.ReleaseCandidate == other.ReleaseCandidate
}

// HasNonBlockingExcludes determines if a version has support for non-blocking
// exclude commands.
func (version FdbVersion) HasNonBlockingExcludes(useNonBlockingExcludes bool) bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 3, Patch: 5}) && useNonBlockingExcludes
}

// NextMajorVersion returns the next major version of FoundationDB.
func (version FdbVersion) NextMajorVersion() FdbVersion {
	return FdbVersion{Major: version.Major + 1, Minor: 0, Patch: 0}
}

// NextMinorVersion returns the next minor version of FoundationDB.
func (version FdbVersion) NextMinorVersion() FdbVersion {
	return FdbVersion{Major: version.Major, Minor: version.Minor + 1, Patch: 0}
}

// NextPatchVersion returns the next patch version of FoundationDB.
func (version FdbVersion) NextPatchVersion() FdbVersion {
	return FdbVersion{Major: version.Major, Minor: version.Minor, Patch: version.Patch + 1}
}

// Equal checks if two FdbVersion are the same.
func (version FdbVersion) Equal(other FdbVersion) bool {
	return version.Major == other.Major &&
		version.Minor == other.Minor &&
		version.Patch == other.Patch &&
		version.ReleaseCandidate == other.ReleaseCandidate
}

// IsSupported defines the minimum supported FDB version.
func (version FdbVersion) IsSupported() bool {
	return version.IsAtLeast(Versions.MinimumVersion)
}

// Versions provides a shorthand for known versions.
// This is only to be used in testing.
var Versions = struct {
	NextMajorVersion,
	NextPatchVersion,
	MinimumVersion,
	Default FdbVersion
}{
	Default:          FdbVersion{Major: 6, Minor: 2, Patch: 20},
	NextPatchVersion: FdbVersion{Major: 6, Minor: 2, Patch: 21},
	NextMajorVersion: FdbVersion{Major: 7, Minor: 0, Patch: 0},
	MinimumVersion:   FdbVersion{Major: 6, Minor: 2, Patch: 20},
}
