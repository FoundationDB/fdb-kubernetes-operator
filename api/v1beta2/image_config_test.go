/*
 * image_config_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[api] ImageConfig", func() {
	When("merging image configs", func() {
		It("applies chooses the first value for each field", func() {
			configs := []ImageConfig{
				{
					BaseImage: FoundationDBBaseImage,
					Version:   Versions.Default.String(),
				},
				{
					BaseImage: "foundationdb/foundationdb-slim",
					Version:   Versions.Default.String(),
					Tag:       "abcdef",
					TagSuffix: "-1",
				},
			}

			finalConfig := SelectImageConfig(configs, Versions.Default.String())
			Expect(finalConfig).To(Equal(ImageConfig{
				BaseImage: FoundationDBBaseImage,
				Version:   Versions.Default.String(),
				Tag:       "abcdef",
				TagSuffix: "-1",
			}))
		})

		It("ignores configs that are for different versions", func() {
			configs := []ImageConfig{
				{
					BaseImage: FoundationDBBaseImage,
					Version:   Versions.Default.String(),
				},
				{
					Version: Versions.NextMajorVersion.String(),
					Tag:     "abcdef",
				},
				{
					TagSuffix: "-1",
				},
			}

			finalConfig := SelectImageConfig(configs, Versions.Default.String())
			Expect(finalConfig).To(Equal(ImageConfig{
				BaseImage: FoundationDBBaseImage,
				Version:   Versions.Default.String(),
				TagSuffix: "-1",
			}))
		})
	})

	When("building image names", func() {
		It("applies the fields", func() {
			config := ImageConfig{
				BaseImage: FoundationDBSidecarBaseImage,
				Version:   Versions.Default.String(),
				TagSuffix: "-2",
			}
			image := config.Image()
			Expect(
				image,
			).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-2", Versions.Default)))
		})

		It("uses the tag to override the version and tag suffix", func() {
			config := ImageConfig{
				BaseImage: FoundationDBSidecarBaseImage,
				Version:   Versions.Default.String(),
				Tag:       "abcdef",
				TagSuffix: "-2",
			}
			image := config.Image()
			Expect(image).To(Equal("foundationdb/foundationdb-kubernetes-sidecar:abcdef"))
		})
	})
})
