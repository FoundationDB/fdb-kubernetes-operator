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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] FDBVersion", func() {
	When("checking if the protocol and the version are compatible", func() {
		It("should return the correct compatibility", func() {
			version := Version{Major: 6, Minor: 2, Patch: 20}
			Expect(version.IsProtocolCompatible(Version{Major: 6, Minor: 2, Patch: 20})).To(BeTrue())
			Expect(version.IsProtocolCompatible(Version{Major: 6, Minor: 2, Patch: 22})).To(BeTrue())
			Expect(version.IsProtocolCompatible(Version{Major: 6, Minor: 3, Patch: 0})).To(BeFalse())
			Expect(version.IsProtocolCompatible(Version{Major: 6, Minor: 3, Patch: 20})).To(BeFalse())
			Expect(version.IsProtocolCompatible(Version{Major: 7, Minor: 2, Patch: 20})).To(BeFalse())
		})

		When("release candidates differ", func() {
			It("should be incompatible", func() {
				version := Version{Major: 7, Minor: 0, Patch: 0, ReleaseCandidate: 1}
				Expect(version.IsProtocolCompatible(Version{Major: 7, Minor: 0, Patch: 0, ReleaseCandidate: 2})).To(BeFalse())
			})
		})
	})

	Context("Using the fdb version", func() {
		It("should return the fdb version struct", func() {
			version, err := ParseFdbVersion("6.2.11")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Version{Major: 6, Minor: 2, Patch: 11}))
			Expect(version.HasSeparatedProxies()).To(BeFalse())

			version, err = ParseFdbVersion("prerelease-6.2.11")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Version{Major: 6, Minor: 2, Patch: 11}))

			version, err = ParseFdbVersion("test-6.2.11-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Version{Major: 6, Minor: 2, Patch: 11, ReleaseCandidate: 0}))

			version, err = ParseFdbVersion("7.0.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Version{Major: 7, Minor: 0, Patch: 0, ReleaseCandidate: 0}))
			Expect(version.HasSeparatedProxies()).To(BeTrue())

			version, err = ParseFdbVersion("7.0.0-rc1")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Version{Major: 7, Minor: 0, Patch: 0, ReleaseCandidate: 1}))

			version, err = ParseFdbVersion("7.1.0-rc39")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 39}))
			Expect(version.HasSeparatedProxies()).To(BeTrue())

			_, err = ParseFdbVersion("6.2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("could not parse FDB version from 6.2"))
		})

		It("should format the version correctly", func() {
			version := Version{Major: 6, Minor: 2, Patch: 11}
			Expect(version.String()).To(Equal("6.2.11"))
			version = Version{Major: 6, Minor: 2, Patch: 11, ReleaseCandidate: 0}
			Expect(version.String()).To(Equal("6.2.11"))
			version = Version{Major: 6, Minor: 2, Patch: 11, ReleaseCandidate: 1}
			Expect(version.String()).To(Equal("6.2.11-rc1"))
		})
	})

	When("getting the next version of the current FDBVersion", func() {
		It("should return the correct next version", func() {
			version := Version{Major: 6, Minor: 2, Patch: 20}
			Expect(version.NextMajorVersion()).To(Equal(Version{Major: 7, Minor: 0, Patch: 0}))
			Expect(version.NextMinorVersion()).To(Equal(Version{Major: version.Major, Minor: 3, Patch: 0}))
			Expect(version.NextPatchVersion()).To(Equal(Version{Major: version.Major, Minor: version.Minor, Patch: 21}))
		})
	})

	When("comparing two FDBVersions", func() {
		It("should return if they are equal", func() {
			version := Version{Major: 6, Minor: 2, Patch: 20}
			Expect(version.Equal(version)).To(BeTrue())
			Expect(version.Equal(Version{Major: 7, Minor: 0, Patch: 0})).To(BeFalse())
			Expect(version.Equal(Version{Major: 7, Minor: 0, Patch: 0})).To(BeFalse())
			Expect(version.Equal(Version{Major: 6, Minor: 3, Patch: 20})).To(BeFalse())
			Expect(version.Equal(Version{Major: 6, Minor: 2, Patch: 21})).To(BeFalse())
		})

		It("should return correct result for IsAtleast", func() {
			version := Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 2}
			Expect(version.IsAtLeast(Version{Major: 7, Minor: 1, Patch: 0})).To(BeFalse())
			Expect(version.IsAtLeast(Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 1})).To(BeTrue())
			Expect(version.IsAtLeast(Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 3})).To(BeFalse())

			version = Version{Major: 7, Minor: 1, Patch: 0}
			Expect(version.IsAtLeast(Version{Major: 7, Minor: 1, Patch: 0, ReleaseCandidate: 1})).To(BeTrue())
		})
	})

	When("checking if the version has support for non-blocking exclude commands", func() {
		type testCase struct {
			version                Version
			useNonBlockingExcludes bool
			expectedResult         bool
		}

		DescribeTable("should return if non-blocking excludes are enabled",
			func(tc testCase) {
				Expect(tc.version.HasNonBlockingExcludes(tc.useNonBlockingExcludes)).To(Equal(tc.expectedResult))
			},
			Entry("When version is below 6.3.5 and useNonBlockingExcludes is false",
				testCase{
					version:                Version{Major: 6, Minor: 3, Patch: 0},
					useNonBlockingExcludes: false,
					expectedResult:         false,
				}),
			Entry("When version is below 6.3.5 and useNonBlockingExcludes is true",
				testCase{
					version:                Version{Major: 6, Minor: 3, Patch: 0},
					useNonBlockingExcludes: false,
					expectedResult:         false,
				}),
			Entry("When version is atleast 6.3.5 and useNonBlockingExcludes is false",
				testCase{
					version:                Version{Major: 6, Minor: 3, Patch: 5},
					useNonBlockingExcludes: false,
					expectedResult:         false,
				}),
			Entry("When version is atleast 6.3.5 and useNonBlockingExcludes is true",
				testCase{
					version:                Version{Major: 6, Minor: 3, Patch: 6},
					useNonBlockingExcludes: true,
					expectedResult:         true,
				}),
		)
	})

	DescribeTable("validating in a version check is allowed", func(version Version, targetVersion Version, expected bool) {
		Expect(version.SupportsVersionChange(targetVersion)).To(Equal(expected))
	},
		Entry("Same version",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			},
			Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			},
			true,
		),
		Entry("Patch upgrade",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			},
			Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			},
			true,
		),
		Entry("Minor upgrade",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			},
			Version{
				Major: 7,
				Minor: 2,
				Patch: 0,
			},
			true,
		),
		Entry("Major upgrade",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			},
			Version{
				Major: 8,
				Minor: 1,
				Patch: 0,
			},
			true,
		),
		Entry("Patch downgrade",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			},
			Version{
				Major: 7,
				Minor: 1,
				Patch: 0,
			},
			true,
		),
		Entry("Minor downgrade",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			},
			Version{
				Major: 7,
				Minor: 0,
				Patch: 0,
			},
			false,
		),
		Entry("Major downgrade",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 1,
			},
			Version{
				Major: 6,
				Minor: 1,
				Patch: 0,
			},
			false,
		),
	)

	DescribeTable("validating if the provided version supports locality based exclusions", func(version Version, expected bool) {
		Expect(version.SupportsLocalityBasedExclusions()).To(Equal(expected))
	},
		Entry(
			"Version is 6.3",
			Version{
				Major: 6,
				Minor: 3,
				Patch: 0,
			},
			false,
		),
		Entry(
			"Version is 7.1.41",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 41,
			},
			false,
		),
		Entry(
			"Version is 7.1.42",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 42,
			},
			true,
		),
		Entry(
			"Version is 7.3.25",
			Version{
				Major: 7,
				Minor: 3,
				Patch: 25,
			},
			false,
		),
		Entry(
			"Version is 7.3.26",
			Version{
				Major: 7,
				Minor: 3,
				Patch: 26,
			},
			true,
		),
	)

	DescribeTable("validating if the provided version automatically removes dead tester processes", func(version Version, expected bool) {
		Expect(version.AutomaticallyRemovesDeadTesterProcesses()).To(Equal(expected))
	},
		Entry(
			"Version is 6.3",
			Version{
				Major: 6,
				Minor: 3,
				Patch: 0,
			},
			false,
		),
		Entry(
			"Version is 7.1.41",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 41,
			},
			false,
		),
		Entry(
			"Version is 7.1.55",
			Version{
				Major: 7,
				Minor: 1,
				Patch: 55,
			},
			true,
		),
		Entry(
			"Version is 7.3.25",
			Version{
				Major: 7,
				Minor: 3,
				Patch: 25,
			},
			false,
		),
		Entry(
			"Version is 7.3.35",
			Version{
				Major: 7,
				Minor: 3,
				Patch: 35,
			},
			true,
		),
	)
})
