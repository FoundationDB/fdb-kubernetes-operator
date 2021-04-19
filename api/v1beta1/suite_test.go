/*
Copyright 2020 FoundationDB project authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var Versions = struct {
	NextMajorVersion,
	WithSidecarInstanceIDSubstitution, WithoutSidecarInstanceIDSubstitution,
	WithCommandLineVariablesForSidecar, WithEnvironmentVariablesForSidecar,
	WithBinariesFromMainContainer, WithoutBinariesFromMainContainer,
	WithRatekeeperRole, WithoutRatekeeperRole,
	WithSidecarCrashOnEmpty, WithoutSidecarCrashOnEmpty,
	Default FdbVersion
}{
	Default:                              FdbVersion{Major: 6, Minor: 2, Patch: 20},
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

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FDB API")
}
