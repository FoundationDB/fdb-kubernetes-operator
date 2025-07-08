/*
 * images_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("image setup", func() {
	DescribeTable(
		"when generating the sidecar image configuration",
		func(factory *Factory, debugSymbols bool, expected fdbv1beta2.ContainerOverrides) {
			result := factory.GetSidecarContainerOverrides(debugSymbols)
			Expect(result.ImageConfigs).To(ConsistOf(expected.ImageConfigs))
		},
		Entry("no version mapping and no debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1",
					},
				},
			},
		),
		Entry("a tag is already defined",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar:mytag",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						Tag:       "mytag",
						TagSuffix: "",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1-debug",
					},
				},
			},
		),
		Entry("version mapping and no debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						Tag:       "7.3.63-mytag-1",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1",
					},
				},
			},
		),
		Entry("version mapping and debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						Tag:       "7.3.63-mytag-1-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1-debug",
					},
				},
			},
		),
		Entry("version mapping and debug symbols with defined tag",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar:7.3.63-1-debug",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						Tag:       "7.3.63-mytag-1-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1-debug",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols with defined tag",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar:7.3.63-1-debug",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						Tag:       "7.3.63-1-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1-debug",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols with defined tag without debug suffix",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar:7.3.63-mytag-1",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						Tag:       "7.3.63-mytag-1-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-sidecar",
						TagSuffix: "-1-debug",
					},
				},
			},
		),
	)

	DescribeTable(
		"when generating the main image configuration",
		func(factory *Factory, debugSymbols bool, unifiedImage bool, expected fdbv1beta2.ContainerOverrides) {
			result := factory.GetMainContainerOverrides(debugSymbols, unifiedImage)
			Expect(result.ImageConfigs).To(ConsistOf(expected.ImageConfigs))
		},
		Entry("no version mapping and no debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			false,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "",
					},
				},
			},
		),
		Entry("a tag is already defined",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb:mytag",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			false,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						Tag:       "mytag",
						TagSuffix: "",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("version mapping and no debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			false,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						Tag:       "7.3.63-mytag",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "",
					},
				},
			},
		),
		Entry("version mapping and debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			true,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						Tag:       "7.3.63-mytag-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("version mapping and debug symbols with defined tag",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb:7.3.63-debug",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			true,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						Tag:       "7.3.63-mytag-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols with defined tag",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb:7.3.63-debug",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						Tag:       "7.3.63-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols with defined tag without debug suffix",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb:7.3.63-mytag",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			false,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/foundationdb",
						Tag:       "7.3.63-mytag-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/foundationdb",
						TagSuffix: "-debug",
					},
				},
			},
		),
		// unified images tests
		Entry("no version mapping and no debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			false,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "",
					},
				},
			},
		),
		Entry("a tag is already defined",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor:mytag",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			false,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						Tag:       "mytag",
						TagSuffix: "",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("version mapping and no debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			false,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						Tag:       "7.3.63-mytag",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "",
					},
				},
			},
		),
		Entry("version mapping and debug symbols",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			true,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						Tag:       "7.3.63-mytag-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("version mapping and debug symbols with defined tag",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor:7.3.63-debug",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "7.3.63:7.3.63-mytag",
				},
			},
			true,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						Tag:       "7.3.63-mytag-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols with defined tag",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor:7.3.63-debug",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						Tag:       "7.3.63-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "-debug",
					},
				},
			},
		),
		Entry("no version mapping and debug symbols with defined tag without debug suffix",
			&Factory{
				options: &FactoryOptions{
					sidecarImage:         "docker.io/foundationdb/fdb-kubernetes-sidecar",
					fdbImage:             "docker.io/foundationdb/foundationdb",
					unifiedFDBImage:      "docker.io/foundationdb/fdb-kubernetes-monitor:7.3.63-mytag",
					fdbVersion:           "7.3.63",
					fdbVersionTagMapping: "",
				},
			},
			true,
			true,
			fdbv1beta2.ContainerOverrides{
				ImageConfigs: []fdbv1beta2.ImageConfig{
					{
						Version:   "7.3.63",
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						Tag:       "7.3.63-mytag-debug",
					},
					{
						BaseImage: "docker.io/foundationdb/fdb-kubernetes-monitor",
						TagSuffix: "-debug",
					},
				},
			},
		),
	)
})
