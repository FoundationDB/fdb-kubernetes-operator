/*
 * buggify_test.go
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

package buggify

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Buggify", func() {
	DescribeTable("filtering blocked removals", func(cluster *fdbv1beta2.FoundationDBCluster, initialProcessGroupsToRemove []*fdbv1beta2.ProcessGroupStatus, expected []*fdbv1beta2.ProcessGroupStatus) {
		Expect(FilterBlockedRemovals(cluster, initialProcessGroupsToRemove)).To(ConsistOf(expected))
	},
		Entry("No process group is defined to be filtered",
			&fdbv1beta2.FoundationDBCluster{},
			nil,
			nil,
		),
		Entry("One process group is defined to be filtered",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Buggify: fdbv1beta2.BuggifyConfig{
						BlockRemoval: []fdbv1beta2.ProcessGroupID{
							"storage-1",
						},
					},
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-1",
				},
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
		),
		Entry("No matching process group is defined to be filtered",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Buggify: fdbv1beta2.BuggifyConfig{
						BlockRemoval: []fdbv1beta2.ProcessGroupID{
							"storage-42",
						},
					},
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-1",
				},
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-1",
				},
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
		),
		Entry("Multiple process groups are defined to be filtered",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Buggify: fdbv1beta2.BuggifyConfig{
						BlockRemoval: []fdbv1beta2.ProcessGroupID{
							"storage-1",
							"storage-3",
						},
					},
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-1",
				},
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-2",
				},
			},
		),
		Entry("One matching process group and one non-matching process group is defined to be filtered",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Buggify: fdbv1beta2.BuggifyConfig{
						BlockRemoval: []fdbv1beta2.ProcessGroupID{
							"storage-1",
							"storage-42",
						},
					},
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-1",
				},
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-2",
				},
				{
					ProcessGroupID: "storage-3",
				},
			},
		),
	)
})
