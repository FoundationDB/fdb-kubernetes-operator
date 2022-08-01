/*
Copyright 2020-2022 FoundationDB project authors.

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

package v1beta2

import "fmt"

// ImageConfig provides a policy for customizing an image.
//
// When multiple image configs are provided, they will be merged into a single
// config that will be used to define the final image. For each field, we select
// the value from the first entry in the config list that defines a value for
// that field, and matches the version of FoundationDB the image is for. Any
// config that specifies a different version than the one under consideration
// will be ignored for the purposes of defining that image.
type ImageConfig struct {
	// Version is the version of FoundationDB this policy applies to. If this is
	// blank, the policy applies to all FDB versions.
	// +kubebuilder:validation:MaxLength=20
	Version string `json:"version,omitempty"`

	// BaseImage specifies the part of the image before the tag.
	// +kubebuilder:validation:MaxLength=200
	BaseImage string `json:"baseImage,omitempty"`

	// Tag specifies a full image tag.
	// +kubebuilder:validation:MaxLength=100
	Tag string `json:"tag,omitempty"`

	// TagSuffix specifies a suffix that will be added after the version to form
	// the full tag.
	// +kubebuilder:validation:MaxLength=50
	TagSuffix string `json:"tagSuffix,omitempty"`
}

// SelectImageConfig selects image configs that apply to a version of FDB and
// merges them into a single config.
func SelectImageConfig(allConfigs []ImageConfig, versionString string) ImageConfig {
	config := ImageConfig{Version: versionString}
	for _, nextConfig := range allConfigs {
		if nextConfig.Version != "" && nextConfig.Version != versionString {
			continue
		}
		if config.BaseImage == "" {
			config.BaseImage = nextConfig.BaseImage
		}
		if config.Tag == "" {
			config.Tag = nextConfig.Tag
		}
		if config.TagSuffix == "" {
			config.TagSuffix = nextConfig.TagSuffix
		}
	}
	return config
}

// Image generates an image using a config.
func (config ImageConfig) Image() string {
	if config.Tag == "" {
		return fmt.Sprintf("%s:%s%s", config.BaseImage, config.Version, config.TagSuffix)
	}
	return fmt.Sprintf("%s:%s", config.BaseImage, config.Tag)
}
