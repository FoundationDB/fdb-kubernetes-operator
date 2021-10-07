/*
 * foundationdb_version_test.go
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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] FDBVersion", func() {
	When("checking if the protocol and the version are compatible", func() {
		It("should return the correct compatibility", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 20}
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 2, Patch: 20})).To(BeTrue())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 2, Patch: 22})).To(BeTrue())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 3, Patch: 0})).To(BeFalse())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 6, Minor: 3, Patch: 20})).To(BeFalse())
			Expect(version.IsProtocolCompatible(FdbVersion{Major: 7, Minor: 2, Patch: 20})).To(BeFalse())
		})
	})

	Context("Using the fdb version", func() {
		It("should return the fdb version struct", func() {
			version, err := ParseFdbVersion("6.2.11")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

			version, err = ParseFdbVersion("prerelease-6.2.11")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

			version, err = ParseFdbVersion("test-6.2.11-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(FdbVersion{Major: 6, Minor: 2, Patch: 11}))

			_, err = ParseFdbVersion("6.2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("could not parse FDB version from 6.2"))
		})

		It("should format the version correctly", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 11}
			Expect(version.String()).To(Equal("6.2.11"))
		})

		It("should validate the flags for the version correct", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 0}
			Expect(version.HasInstanceIDInSidecarSubstitutions()).To(BeFalse())
			Expect(version.PrefersCommandLineArgumentsInSidecar()).To(BeFalse())

			version = FdbVersion{Major: 7, Minor: 0, Patch: 0}
			Expect(version.HasInstanceIDInSidecarSubstitutions()).To(BeTrue())
			Expect(version.PrefersCommandLineArgumentsInSidecar()).To(BeTrue())
		})
	})

	When("getting the next version of the current FDBVersion", func() {
		It("should return the correct next version", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 20}
			Expect(version.NextMajorVersion()).To(Equal(FdbVersion{Major: 7, Minor: 0, Patch: 0}))
			Expect(version.NextMinorVersion()).To(Equal(FdbVersion{Major: version.Major, Minor: 3, Patch: 0}))
			Expect(version.NextPatchVersion()).To(Equal(FdbVersion{Major: version.Major, Minor: version.Minor, Patch: 21}))
		})
	})

	When("comparing two FDBVersions", func() {
		It("should return if they are euqal", func() {
			version := FdbVersion{Major: 6, Minor: 2, Patch: 20}
			Expect(version.Equal(version)).To(BeTrue())
			Expect(version.Equal(FdbVersion{Major: 7, Minor: 0, Patch: 0})).To(BeFalse())
			Expect(version.Equal(FdbVersion{Major: 7, Minor: 0, Patch: 0})).To(BeFalse())
			Expect(version.Equal(FdbVersion{Major: 6, Minor: 3, Patch: 20})).To(BeFalse())
			Expect(version.Equal(FdbVersion{Major: 6, Minor: 2, Patch: 21})).To(BeFalse())
		})
	})

	When("checking if the version has support for non-blocking exclude commands", func() {
		type testCase struct {
			version                FdbVersion
			useNonBlockingExcludes bool
			expectedResult         bool
		}

		DescribeTable("should return if non-blocking excludes are enabled",
			func(tc testCase) {
				Expect(tc.version.HasNonBlockingExcludes(tc.useNonBlockingExcludes)).To(Equal(tc.expectedResult))
			},
			Entry("When version is below 6.3.5 and useNonBlockingExcludes is false",
				testCase{
					version:                FdbVersion{Major: 6, Minor: 3, Patch: 0},
					useNonBlockingExcludes: false,
					expectedResult:         false,
				}),
			Entry("When version is below 6.3.5 and useNonBlockingExcludes is true",
				testCase{
					version:                FdbVersion{Major: 6, Minor: 3, Patch: 0},
					useNonBlockingExcludes: false,
					expectedResult:         false,
				}),
			Entry("When version is atleast 6.3.5 and useNonBlockingExcludes is false",
				testCase{
					version:                FdbVersion{Major: 6, Minor: 3, Patch: 5},
					useNonBlockingExcludes: false,
					expectedResult:         false,
				}),
			Entry("When version is atleast 6.3.5 and useNonBlockingExcludes is true",
				testCase{
					version:                FdbVersion{Major: 6, Minor: 3, Patch: 6},
					useNonBlockingExcludes: true,
					expectedResult:         true,
				}),
		)

	})
})
