/*
 * change_coordinators_test.go
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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Reconcile.SelectCandidates", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var adminClient *MockAdminClient

	BeforeEach(func() {
		cluster = createDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		err := setupClusterForTest(cluster)
		Expect(err).NotTo(HaveOccurred())

		adminClient, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())

	})

	Context("with status", func() {
		var status *fdbtypes.FoundationDBStatus
		JustBeforeEach(func() {
			var err error
			status, err = adminClient.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		BeforeEach(func() {
			// Put the bad data in place
			// storage - are there symbols for these ips and ids?
			adminClient.ExcludedAddresses = append(adminClient.ExcludedAddresses, "1.1.0.1")
			// log
			adminClient.ExcludedAddresses = append(adminClient.ExcludedAddresses, "1.1.5.1")
			cluster.Status.PendingRemovals = map[string]fdbtypes.PendingRemovalState{
				"operator-test-1-log-2":     {},
				"operator-test-1-storage-2": {},
			}
			Expect(cluster.InstanceIsBeingRemoved("operator-test-1-log-2")).To(BeTrue())
		})

		Context("with candidates", func() {
			var candidates []localityInfo
			JustBeforeEach(func() {
				candidates = make([]localityInfo, 0, len(status.Cluster.Processes))
			})

			for _, class := range []fdbtypes.ProcessClass{fdbtypes.ProcessClassStorage, fdbtypes.ProcessClassLog} {
				Context("with class "+string(class), func() {
					var result []localityInfo

					getProcess := func(info localityInfo) *fdbtypes.FoundationDBStatusProcessInfo {
						for _, process := range status.Cluster.Processes {
							if info.ID == process.Locality["instance_id"] {
								return &process
							}
						}
						return nil
					}

					JustBeforeEach(func() {
						result = selectCandidates(cluster, status, candidates, class)
					})

					It("should find results", func() {
						Expect(len(result)).To(BeNumerically(">", 0))
					})

					It("should only select the desired class", func() {
						for _, info := range result {
							Expect(getProcess(info).ProcessClass).To(Equal(class))
						}
					})

					It("should not select excluded processes", func() {
						for _, info := range result {
							Expect(getProcess(info).Excluded).To(BeFalse())
						}
					})

					It("should not select instances being removed", func() {
						for _, info := range result {
							Expect(cluster.InstanceIsBeingRemoved(info.ID)).To(BeFalse())
						}
					})

				})
			}

		})
	})

})
