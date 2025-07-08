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
