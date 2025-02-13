/*
 * foundationdb_version_test.go
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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] FDBVersion", func() {
	When("checking if the protocol and the version are compatible", func() {
		It("should return the correct compatibility", func() {
			version := Versions.Default
			compareVersion := Versions.Default
			compareVersion.Patch++
			Expect(version.IsProtocolCompatible(version)).To(BeTrue())
			Expect(version.IsProtocolCompatible(compareVersion)).To(BeTrue())
			compareVersion.Minor++
			Expect(version.IsProtocolCompatible(compareVersion)).To(BeFalse())
			compareVersion.Major++
			Expect(version.IsProtocolCompatible(compareVersion)).To(BeFalse())
		})

		When("release candidates differ", func() {
			It("should be incompatible", func() {
				version := Versions.Default
				version.ReleaseCandidate = 1
				compareVersion := Versions.Default
				compareVersion.ReleaseCandidate = 2
				Expect(version.IsProtocolCompatible(compareVersion)).To(BeFalse())
			})
		})
	})

	Context("Using the fdb version", func() {
		It("should return the fdb version struct", func() {
			version, err := ParseFdbVersion(Versions.Default.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Versions.Default))

			version, err = ParseFdbVersion("prerelease-" + Versions.Default.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Versions.Default))

			version, err = ParseFdbVersion("test-" + Versions.Default.String() + "-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Versions.Default))

			version, err = ParseFdbVersion(Versions.Default.String() + "-rc1")
			Expect(err).NotTo(HaveOccurred())
			expectedVersion := Versions.Default
			expectedVersion.ReleaseCandidate = 1
			Expect(version).To(Equal(expectedVersion))

			version, err = ParseFdbVersion(Versions.Default.String() + "-rc39")
			Expect(err).NotTo(HaveOccurred())
			expectedVersion = Versions.Default
			expectedVersion.ReleaseCandidate = 39
			Expect(version).To(Equal(expectedVersion))

			_, err = ParseFdbVersion(Versions.Default.Compact())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(HavePrefix("could not parse FDB version from"))
		})

		It("should format the version correctly", func() {
			version := Versions.Default
			Expect(version.String()).To(Equal("7.1.57"))
			version.ReleaseCandidate = 1
			Expect(version.String()).To(Equal("7.1.57-rc1"))
		})
	})

	When("getting the next version of the current FDBVersion", func() {
		It("should return the correct next version", func() {
			version := Versions.Default
			Expect(version.NextMajorVersion()).To(Equal(Version{api.Version{Major: 8, Minor: 0, Patch: 0}}))
			Expect(version.NextMinorVersion()).To(Equal(Version{api.Version{Major: version.Major, Minor: 2, Patch: 0}}))
			Expect(version.NextPatchVersion()).To(Equal(Version{api.Version{Major: version.Major, Minor: version.Minor, Patch: 58}}))
		})
	})

	When("comparing two FDBVersions", func() {
		It("should return if they are equal", func() {
			version := Version{api.Version{Major: 6, Minor: 2, Patch: 20}}
			Expect(version.Equal(version)).To(BeTrue())
			Expect(version.Equal(Version{api.Version{Major: 7, Minor: 0, Patch: 0}})).To(BeFalse())
			Expect(version.Equal(Version{api.Version{Major: 7, Minor: 0, Patch: 0}})).To(BeFalse())
			Expect(version.Equal(Version{api.Version{Major: 6, Minor: 3, Patch: 20}})).To(BeFalse())
			Expect(version.Equal(Version{api.Version{Major: 6, Minor: 2, Patch: 21}})).To(BeFalse())
		})

		It("should return correct result for IsAtleast", func() {
			version := Version{api.Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 2}}
			Expect(version.IsAtLeast(Version{api.Version{Major: 7, Minor: 1, Patch: 0}})).To(BeFalse())
			Expect(version.IsAtLeast(Version{api.Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 1}})).To(BeTrue())
			Expect(version.IsAtLeast(Version{api.Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 3}})).To(BeFalse())

			version = Version{api.Version{Major: 7, Minor: 1, Patch: 0}}
			Expect(version.IsAtLeast(Version{api.Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 1}})).To(BeTrue())
		})
	})

	DescribeTable("validating in a version check is allowed", func(version Version, targetVersion Version, expected bool) {
		Expect(version.SupportsVersionChange(targetVersion)).To(Equal(expected))
	},
		Entry("Same version",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			}},
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			}},
			true,
		),
		Entry("Patch upgrade",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			}},
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			}},
			true,
		),
		Entry("Minor upgrade",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			}},
			Version{api.Version{
				Major: 7,
				Minor: 2,
				Patch: 0,
			}},
			true,
		),
		Entry("Major upgrade",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			}},
			Version{api.Version{
				Major: 8,
				Minor: 1,
				Patch: 0,
			}},
			true,
		),
		Entry("Patch downgrade",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			}},
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			}},
			true,
		),
		Entry("Minor downgrade",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			}},
			Version{api.Version{
				Major: 7,
				Minor: 0,
				Patch: 0,
			}},
			false,
		),
		Entry("Major downgrade",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			}},
			Version{api.Version{
				Major: 6,
				Minor: 1,
				Patch: 0,
			}},
			false,
		),
	)

	DescribeTable("validating if the provided version supports locality based exclusions", func(version Version, expected bool) {
		Expect(version.SupportsLocalityBasedExclusions()).To(Equal(expected))
	},
		Entry(
			"Version is 6.3",
			Version{api.Version{
				Major: 6,
				Minor: 3,
				Patch: 0,
			}},
			false,
		),
		Entry(
			"Version is 7.1.41",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 41,
			}},
			false,
		),
		Entry(
			"Version is 7.1.42",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 42,
			}},
			true,
		),
		Entry(
			"Version is 7.3.25",
			Version{api.Version{
				Major: 7,
				Minor: 3,
				Patch: 25,
			}},
			false,
		),
		Entry(
			"Version is 7.3.26",
			Version{api.Version{
				Major: 7,
				Minor: 3,
				Patch: 26,
			}},
			true,
		),
	)

	DescribeTable("validating if the provided version automatically removes dead tester processes", func(version Version, expected bool) {
		Expect(version.AutomaticallyRemovesDeadTesterProcesses()).To(Equal(expected))
	},
		Entry(
			"Version is 6.3",
			Version{api.Version{
				Major: 6,
				Minor: 3,
				Patch: 0,
			}},
			false,
		),
		Entry(
			"Version is 7.1.41",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 41,
			}},
			false,
		),
		Entry(
			"Version is 7.1.55",
			Version{api.Version{
				Major: 7,
				Minor: 1,
				Patch: 55,
			}},
			true,
		),
		Entry(
			"Version is 7.3.25",
			Version{api.Version{
				Major: 7,
				Minor: 3,
				Patch: 25,
			}},
			false,
		),
		Entry(
			"Version is 7.3.35",
			Version{api.Version{
				Major: 7,
				Minor: 3,
				Patch: 35,
			}},
			true,
		),
	)
})
