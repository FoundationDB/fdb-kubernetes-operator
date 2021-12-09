/*
 * pod_lifecycle_manager_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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

package podmanager

import (
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("pod_lifecycle_manager", func() {
	var manager StandardPodLifecycleManager

	DescribeTable("getting the deletion mode of the cluster",
		func(cluster *fdbtypes.FoundationDBCluster, expected fdbtypes.PodUpdateMode) {
			Expect(manager.GetDeletionMode(cluster)).To(Equal(expected))
		},
		Entry("Without a deletion mode defined",
			&fdbtypes.FoundationDBCluster{},
			fdbtypes.PodUpdateModeZone,
		),
		Entry("With deletion mode Zone",
			&fdbtypes.FoundationDBCluster{
				Spec: fdbtypes.FoundationDBClusterSpec{
					AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
						DeletionMode: fdbtypes.PodUpdateModeZone,
					},
				},
			},
			fdbtypes.PodUpdateModeZone,
		),
		Entry("With deletion mode All",
			&fdbtypes.FoundationDBCluster{
				Spec: fdbtypes.FoundationDBClusterSpec{
					AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
						DeletionMode: fdbtypes.PodUpdateModeAll,
					},
				},
			},
			fdbtypes.PodUpdateModeAll,
		),
		Entry("With deletion mode Process Group",
			&fdbtypes.FoundationDBCluster{
				Spec: fdbtypes.FoundationDBClusterSpec{
					AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
						DeletionMode: fdbtypes.PodUpdateModeProcessGroup,
					},
				},
			},
			fdbtypes.PodUpdateModeProcessGroup,
		),
	)
})
