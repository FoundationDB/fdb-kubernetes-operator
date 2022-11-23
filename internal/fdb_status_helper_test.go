/*
 * fdb_status_helper_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package internal

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("fdb_status_helper_test", func() {
	When("Removing warnings in JSON", func() {
		type testCase struct {
			input       string
			expected    []byte
			expectedErr error
		}

		DescribeTable("Test remove warnings in JSON string",
			func(tc testCase) {
				result, err := RemoveWarningsInJSON(tc.input)
				// We need the if statement to make ginkgo happy:
				//   Refusing to compare <nil> to <nil>.
				//   Be explicit and use BeNil() instead.
				//   This is to avoid mistakes where both sides of an assertion are erroneously uninitialized.
				// ¯\_(ツ)_/¯
				if tc.expectedErr == nil {
					Expect(err).To(BeNil())
				} else {
					Expect(err).To(Equal(tc.expectedErr))
				}
				Expect(result).To(Equal(tc.expected))
			},
			Entry("Valid JSON without warning",
				testCase{
					input:       "{}",
					expected:    []byte("{}"),
					expectedErr: nil,
				},
			),
			Entry("Valid JSON with warning",
				testCase{
					input: `
 # Warning Slow response

 {}`,
					expected:    []byte("{}"),
					expectedErr: nil,
				},
			),
			Entry("Invalid JSON",
				testCase{
					input:       "}",
					expected:    nil,
					expectedErr: fmt.Errorf("the JSON string doesn't contain a starting '{'"),
				},
			),
		)
	})
})
