/*
 * foundationdb_custom_parameters_test.go
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
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("FoundationDBCustomParameters", func() {
	When("getting the custom parameters for the CLI", func() {
		var customParameters FoundationDBCustomParameters
		BeforeEach(func() {
			customParameters = []FoundationDBCustomParameter{
				"knob_http_verbose_level=3",
			}
		})

		It("", func() {
			expected := []string{
				"--knob_http_verbose_level=3",
			}

			result := customParameters.GetKnobsForCLI()
			Expect(result).To(ContainElements(expected))
			Expect(len(result)).To(Equal(len(expected)))
		})
	})

	When("Validating the custom parameters", func() {
		DescribeTable("should print the correct string",
			func(customParameters FoundationDBCustomParameters, expected error) {
				err := customParameters.ValidateCustomParameters()

				if expected == nil {
					Expect(err).NotTo(HaveOccurred())
				} else {
					Expect(err).To(Equal(expected))
				}

			},
			Entry("empty custom parameters",
				FoundationDBCustomParameters{},
				nil),
			Entry("valid custom parameters",
				FoundationDBCustomParameters{
					"test=test",
				},
				nil),
			Entry("custom parameters that sets protected knob",
				FoundationDBCustomParameters{
					"datadir=test",
				},
				errors.New("found the following customParameters violations:\nfound protected customParameter: datadir, please remove this parameter from the customParameters list")),
			Entry("duplicate custom parameters",
				FoundationDBCustomParameters{
					"test=test",
					"test=test",
				},
				errors.New("found the following customParameters violations:\nfound duplicated customParameter: test")),
			Entry("duplicate custom parameters",
				FoundationDBCustomParameters{
					"test=test",
					"datadir=test",
					"test=test",
				},
				errors.New("found the following customParameters violations:\nfound protected customParameter: datadir, please remove this parameter from the customParameters list\nfound duplicated customParameter: test")),
		)
	})
})
