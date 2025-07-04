/*
 * restarts.go
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

package restarts

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("restarts", func() {
	DescribeTable("when getting the filter conditions for a cluster", func(cluster *fdbv1beta2.FoundationDBCluster, expected map[fdbv1beta2.ProcessGroupConditionType]bool) {
		Expect(GetFilterConditions(cluster)).To(Equal(expected))
	},
		Entry("when no upgrade is performed",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.25",
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					RunningVersion: "7.1.25",
				},
			},
			map[fdbv1beta2.ProcessGroupConditionType]bool{
				fdbv1beta2.IncorrectCommandLine: true,
				fdbv1beta2.IncorrectPodSpec:     false,
				fdbv1beta2.SidecarUnreachable:   false,
				fdbv1beta2.IncorrectConfigMap:   false,
			}),
		Entry("when the running version is missing",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.25",
				},
			},
			map[fdbv1beta2.ProcessGroupConditionType]bool{
				fdbv1beta2.IncorrectCommandLine: true,
				fdbv1beta2.IncorrectPodSpec:     false,
				fdbv1beta2.SidecarUnreachable:   false,
				fdbv1beta2.IncorrectConfigMap:   false,
			}),
		Entry("when a version incompatible upgrade is performed",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: "7.1.25",
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					RunningVersion: "6.3.25",
				},
			},
			map[fdbv1beta2.ProcessGroupConditionType]bool{
				fdbv1beta2.IncorrectCommandLine:  true,
				fdbv1beta2.IncorrectSidecarImage: false,
				fdbv1beta2.IncorrectConfigMap:    false,
			}),
	)
})
