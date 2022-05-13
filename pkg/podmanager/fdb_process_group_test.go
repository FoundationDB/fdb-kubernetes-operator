/*
 * fdb_process_group_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2022 Apple Inc. and the FoundationDB project authors
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

package podmanager

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("fdb_process_group", func() {
	When("parsing the process group ID", func() {
		Context("with a storage ID", func() {
			It("can parse the ID", func() {
				_, id, err := ParseProcessGroupID("storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(12))
			})
		})

		Context("with a cluster controller ID", func() {
			It("can parse the ID", func() {
				_, id, err := ParseProcessGroupID("cluster_controller-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(3))
			})
		})

		Context("with a custom prefix", func() {
			It("parses the prefix", func() {
				_, id, err := ParseProcessGroupID("dc1-storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(12))
			})
		})

		Context("with no prefix", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseProcessGroupID("6")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID 6"))
			})
		})

		Context("with no numbers", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseProcessGroupID("storage")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID storage"))
			})
		})

		Context("with a text suffix", func() {
			It("gives a parsing error", func() {
				_, _, err := ParseProcessGroupID("storage-bad")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID storage-bad"))
			})
		})
	})

	When("parsing the process group ID number", func() {
		Context("with a storage ID", func() {
			It("can parse the ID", func() {
				id, err := GetProcessGroupIDNumber("storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(12))
			})
		})

		Context("with a cluster controller ID", func() {
			It("can parse the ID", func() {
				id, err := GetProcessGroupIDNumber("cluster_controller-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(3))
			})
		})

		Context("with a custom prefix", func() {
			It("parses the prefix", func() {
				id, err := GetProcessGroupIDNumber("dc1-storage-12")
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(12))
			})
		})

		Context("with no prefix", func() {
			It("gives a parsing error", func() {
				_, err := GetProcessGroupIDNumber("6")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID 6"))
			})
		})

		Context("with no numbers", func() {
			It("gives a parsing error", func() {
				_, err := GetProcessGroupIDNumber("storage")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID storage"))
			})
		})

		Context("with a text suffix", func() {
			It("gives a parsing error", func() {
				_, err := GetProcessGroupIDNumber("storage-bad")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("could not parse process group ID storage-bad"))
			})
		})
	})

	When("getting the process ID from the process group ID", func() {
		It("can parse a process ID", func() {
			Expect(GetProcessGroupIDFromProcessID("storage-1-1")).To(Equal("storage-1"))
		})
		It("can parse a process ID with a prefix", func() {
			Expect(GetProcessGroupIDFromProcessID("dc1-storage-1-1")).To(Equal("dc1-storage-1"))
		})

		It("can handle a process group ID with no process number", func() {
			Expect(GetProcessGroupIDFromProcessID("storage-2")).To(Equal("storage-2"))
		})
	})
})
