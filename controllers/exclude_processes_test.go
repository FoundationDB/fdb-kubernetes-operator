/*
 * exclude_processes_test.go
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

package controllers

import (
	"context"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("exclude_processes", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))
	})

	Describe("canExcludeNewProcesses", func() {
		Context("with a small cluster", func() {
			When("all processes are healthy", func() {
				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdb.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})

			When("one process group is missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 1, fdb.ProcessClassStorage)
				})

				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdb.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})

			When("two process groups are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 2, fdb.ProcessClassStorage)
				})

				It("should not allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdb.ProcessClassStorage)
					Expect(canExclude).To(BeFalse())
					Expect(missing).To(Equal([]string{"storage-1", "storage-2"}))
				})
			})

			When("two process groups of a different type are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 2, fdb.ProcessClassLog)
				})

				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdb.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})
		})

		Context("with a large cluster", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessCounts.Storage = 20
				err = clusterReconciler.Update(context.TODO(), cluster)
				Expect(err).NotTo(HaveOccurred())

				result, err := reconcileCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())

				_, err = reloadCluster(cluster)
				Expect(err).NotTo(HaveOccurred())
			})

			When("two process groups are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 2, fdb.ProcessClassStorage)
				})

				It("should allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdb.ProcessClassStorage)
					Expect(canExclude).To(BeTrue())
					Expect(missing).To(BeNil())
				})
			})

			When("five process groups are missing", func() {
				BeforeEach(func() {
					createMissingProcesses(cluster, 5, fdb.ProcessClassStorage)
				})

				It("should not allow the exclusion", func() {
					canExclude, missing := canExcludeNewProcesses(cluster, fdb.ProcessClassStorage)
					Expect(canExclude).To(BeFalse())
					Expect(missing).To(Equal([]string{"storage-1", "storage-10", "storage-11", "storage-12", "storage-13"}))
				})
			})
		})
	})
})

func createMissingProcesses(cluster *fdbtypes.FoundationDBCluster, count int, processClass fdb.ProcessClass) {
	missing := 0
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.ProcessClass == processClass {
			processGroup.UpdateCondition(fdbtypes.MissingProcesses, true, nil, "")
			missing++
			if missing == count {
				break
			}
		}
	}
	Expect(missing).To(Equal(count))
}
