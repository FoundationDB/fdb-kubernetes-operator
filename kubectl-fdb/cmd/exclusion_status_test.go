/*
 * exclusion_status_test.go
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

package cmd

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[plugin] exclustion stats command", func() {
	DescribeTable("pretty printing the stored bytes", func(storedByes int, expected string) {
		Expect(prettyPrintStoredBytes(storedByes)).To(Equal(expected))
	},
		Entry("a few bytes", 1023, "1023.00"),
		Entry("two KiB", 2*1024, "2.00Ki"),
		Entry("two and a half KiB", 2*1024+512, "2.50Ki"),
		Entry("three MiB", 3*1024*1024, "3.00Mi"),
		Entry("four GiB", 4*1024*1024*1024, "4.00Gi"),
		Entry("five TiB", 5*1024*1024*1024*1024, "5.00Ti"),
		Entry("six Pib", 6*1024*1024*1024*1024*1024, "6.00Pi"),
	)

})
