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
}

// FDBVersionRegex describes the format of a FoundationDB version.
var FDBVersionRegex = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

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

	return FdbVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// String gets the string representation of an FDB version.
func (version FdbVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
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
	return true
}

// IsProtocolCompatible determines whether two versions of FDB are protocol
// compatible.
func (version FdbVersion) IsProtocolCompatible(other FdbVersion) bool {
	return version.Major == other.Major && version.Minor == other.Minor
}

// HasInstanceIDInSidecarSubstitutions determines if a version has
// FDB_INSTANCE_ID supported natively in the variable substitutions in the
// sidecar.
func (version FdbVersion) HasInstanceIDInSidecarSubstitutions() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 15})
}

// PrefersCommandLineArgumentsInSidecar determines if a version has
// support for configuring the sidecar exclusively through command-line
// arguments.
func (version FdbVersion) PrefersCommandLineArgumentsInSidecar() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 15})
}

// SupportsUsingBinariesFromMainContainer determines if a version has
// support for having the sidecar dynamically switch between using binaries
// from the main container and binaries provided by the sidecar.
func (version FdbVersion) SupportsUsingBinariesFromMainContainer() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 15})
}

// HasRatekeeperRole determines if a version has a dedicated role for
// ratekeeper.
func (version FdbVersion) HasRatekeeperRole() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 0})
}

// HasMaxProtocolClientsInStatus determines if a version has the
// max_protocol_clients field in the cluster status.
func (version FdbVersion) HasMaxProtocolClientsInStatus() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 0})
}

// HasSidecarCrashOnEmpty determines if a version has the flag to have the
// sidecar crash on a file being empty.
func (version FdbVersion) HasSidecarCrashOnEmpty() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 20})
}

// HasNonBlockingExcludes determines if a version has support for non-blocking
// exclude commands.
//
// This is currently set to false across the board, pending investigation into
// potential bugs with non-blocking excludes.
func (version FdbVersion) HasNonBlockingExcludes() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 3, Patch: 5})
}

// Versions provides a shorthand for known versions.
// This is only to be used in testing.
var Versions = struct {
	NextMajorVersion, NextPatchVersion,
	WithSidecarInstanceIDSubstitution, WithoutSidecarInstanceIDSubstitution,
	WithCommandLineVariablesForSidecar, WithEnvironmentVariablesForSidecar,
	WithBinariesFromMainContainer, WithoutBinariesFromMainContainer,
	WithRatekeeperRole, WithoutRatekeeperRole,
	WithSidecarCrashOnEmpty, WithoutSidecarCrashOnEmpty,
	Default FdbVersion
}{
	Default:                              FdbVersion{Major: 6, Minor: 2, Patch: 20},
	NextPatchVersion:                     FdbVersion{Major: 6, Minor: 2, Patch: 21},
	NextMajorVersion:                     FdbVersion{Major: 7, Minor: 0, Patch: 0},
	WithSidecarInstanceIDSubstitution:    FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithoutSidecarInstanceIDSubstitution: FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithCommandLineVariablesForSidecar:   FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithEnvironmentVariablesForSidecar:   FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithBinariesFromMainContainer:        FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithoutBinariesFromMainContainer:     FdbVersion{Major: 6, Minor: 2, Patch: 11},
	WithRatekeeperRole:                   FdbVersion{Major: 6, Minor: 2, Patch: 15},
	WithoutRatekeeperRole:                FdbVersion{Major: 6, Minor: 1, Patch: 12},
	WithSidecarCrashOnEmpty:              FdbVersion{Major: 6, Minor: 2, Patch: 20},
	WithoutSidecarCrashOnEmpty:           FdbVersion{Major: 6, Minor: 2, Patch: 15},
}
