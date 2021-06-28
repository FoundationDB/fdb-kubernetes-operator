/*
 * remove_pods_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Remove Pods", func() {
	var cluster *fdbtypes.FoundationDBCluster

	BeforeEach(func() {
		cluster = createDefaultCluster()

		err := k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))
	})

	When("trying to remove a coordinator", func() {
		It("should not remove the coordinator", func() {
			remaining := map[string]bool{
				"1.1.1.1": false,
			}

			marked, processGroup := fdbtypes.MarkProcessGroupForRemoval(cluster.Status.ProcessGroups, "storage-1", fdbtypes.ProcessClassStorage, "1.1.1.1")
			Expect(marked).To(BeTrue())
			Expect(processGroup).To(BeNil())

			allExcluded, processes := clusterReconciler.getProcessGroupsToRemove(cluster, remaining)
			Expect(allExcluded).To(BeFalse())
			Expect(processes).To(BeEmpty())
		})
	})

	AfterEach(func() {
		k8sClient.Clear()
	})
})
