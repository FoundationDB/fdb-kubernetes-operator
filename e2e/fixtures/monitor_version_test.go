/*
 * monitor_version_test.go
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

package fixtures

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("monitor version", func() {
	DescribeTable(
		"when getting the monitor version for a cluster",
		func(options *FactoryOptions, config *ClusterConfig, expectedPinned bool, expected *string) {
			version, err := fdbv1beta2.ParseFdbVersion("7.3.69")
			Expect(err).NotTo(HaveOccurred())

			factory := &Factory{options: options, fdbVersion: version}
			Expect(factory.UsePinnedMonitorVersion()).To(Equal(expectedPinned))
			Expect(factory.GetMonitorVersion(config)).To(Equal(expected))
			Expect(factory.GetMainContainerOverrides(config).MonitorVersion).To(Equal(expected))
		},
		Entry("no option is set",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
			},
			&ClusterConfig{Version: new("7.3.67")},
			false,
			nil,
		),
		Entry("the monitor version should be kept",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
				pinMonitorVersion:           true,
			},
			&ClusterConfig{Version: new("7.3.67")},
			true,
			new("7.3.67"),
		),
		Entry("the monitor version should be kept and the cluster has no version",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
				pinMonitorVersion:           true,
			},
			&ClusterConfig{},
			true,
			new("7.3.69"),
		),
		Entry("the monitor version is set explicitly",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
				monitorVersion:              "7.4.10",
			},
			&ClusterConfig{Version: new("7.3.67")},
			true,
			new("7.4.10"),
		),
		Entry("the monitor version is set explicitly and should be kept",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
				pinMonitorVersion:           true,
				monitorVersion:              "7.4.10",
			},
			&ClusterConfig{Version: new("7.3.67")},
			true,
			new("7.4.10"),
		),
		Entry("the monitor version is set in the cluster config",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
				pinMonitorVersion:           true,
				monitorVersion:              "7.4.10",
			},
			&ClusterConfig{Version: new("7.3.67"), MonitorVersion: new("7.3.79")},
			true,
			new("7.3.79"),
		),
		Entry("the monitor version is set in the cluster config but no option is set",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
			},
			&ClusterConfig{Version: new("7.3.67"), MonitorVersion: new("7.3.79")},
			false,
			new("7.3.79"),
		),
		Entry("the unified image is not used by the test suite",
			&FactoryOptions{
				featureOperatorUnifiedImage: false,
				pinMonitorVersion:           true,
				monitorVersion:              "7.4.10",
			},
			&ClusterConfig{Version: new("7.3.67")},
			false,
			nil,
		),
		Entry("the unified image is not used for the cluster",
			&FactoryOptions{
				featureOperatorUnifiedImage: true,
				pinMonitorVersion:           true,
			},
			&ClusterConfig{Version: new("7.3.67"), UseUnifiedImage: new(false)},
			true,
			nil,
		),
	)

	DescribeTable(
		"when validating the monitor version flag",
		func(monitorVersion string, expectError bool) {
			options := &FactoryOptions{
				fdbImage:      "docker.io/foundationdb/foundationdb",
				sidecarImage:  "docker.io/foundationdb/foundationdb-kubernetes-sidecar",
				operatorImage: "docker.io/foundationdb/fdb-kubernetes-operator",
				fdbVersion:    "7.3.69",
			}
			options.monitorVersion = monitorVersion

			err := options.validateFlags()
			if expectError {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(err).NotTo(HaveOccurred())
		},
		Entry("not set", "", false),
		Entry("valid version", "7.4.10", false),
		Entry("invalid version", "not-a-version", true),
		Entry("incomplete version", "7.4", true),
	)
})
