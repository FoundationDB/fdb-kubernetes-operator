/*
 * lock_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("lock_client", func() {
	Describe("mock lock client", func() {
		var client *MockLockClient
		var cluster *fdbtypes.FoundationDBCluster
		var err error

		BeforeEach(func() {
			cluster = createDefaultCluster()

			var rawClient LockClient
			rawClient, err = NewMockLockClient(cluster)
			Expect(err).NotTo(HaveOccurred())

			var validCast bool
			client, validCast = rawClient.(*MockLockClient)
			Expect(validCast).To(BeTrue())

			client.aggregations = map[string][]string{
				"exclude": {"1.1.1.1:4500", "1.1.1.2:4500"},
				"bounce":  {"1.1.1.1:4500"},
			}
		})

		Describe("SubmitAggregatedOperation", func() {
			Context("with an existing operation", func() {
				It("should add them to the aggregations", func() {
					err = client.SubmitAggregatedOperation("exclude", []string{"1.1.1.3:4500"})
					Expect(err).NotTo(HaveOccurred())

					Expect(client.aggregations["exclude"]).To(Equal([]string{
						"1.1.1.1:4500", "1.1.1.2:4500", "1.1.1.3:4500",
					}))
					Expect(client.aggregations["bounce"]).To(Equal([]string{
						"1.1.1.1:4500",
					}))
				})
			})

			Context("with an undefined operation", func() {
				It("should add a new entry", func() {
					err = client.SubmitAggregatedOperation("kill", []string{"1.1.1.3:4500"})
					Expect(err).NotTo(HaveOccurred())

					Expect(client.aggregations["exclude"]).To(Equal([]string{
						"1.1.1.1:4500", "1.1.1.2:4500",
					}))
					Expect(client.aggregations["bounce"]).To(Equal([]string{
						"1.1.1.1:4500",
					}))
					Expect(client.aggregations["kill"]).To(Equal([]string{
						"1.1.1.3:4500",
					}))
				})
			})
		})

		Describe("RetrieveAggregatedOperation", func() {
			Context("with an existing operation", func() {
				It("should return the list", func() {
					values, err := client.RetrieveAggregatedOperation("exclude")
					Expect(err).NotTo(HaveOccurred())
					Expect(values).To(Equal([]string{
						"1.1.1.1:4500", "1.1.1.2:4500",
					}))
				})
			})

			Context("with an undefined operation", func() {
				It("should return an empty list", func() {
					values, err := client.RetrieveAggregatedOperation("kill")
					Expect(err).NotTo(HaveOccurred())
					Expect(values).NotTo(BeNil())
					Expect(values).To(Equal([]string{}))
				})
			})
		})

		Describe("ClearAggregatedOperation", func() {
			Context("with an existing operation", func() {
				It("should update the list", func() {
					err := client.ClearAggregatedOperation("exclude", []string{"1.1.1.1:4500", "1.1.1.4:4500"})
					Expect(err).NotTo(HaveOccurred())
					Expect(client.aggregations["exclude"]).To(Equal([]string{
						"1.1.1.2:4500",
					}))
					Expect(client.aggregations["bounce"]).To(Equal([]string{
						"1.1.1.1:4500",
					}))
				})
			})

			Context("with the full list in the operation", func() {
				It("should return the list", func() {
					err := client.ClearAggregatedOperation("exclude", []string{"1.1.1.1:4500", "1.1.1.2:4500"})
					Expect(err).NotTo(HaveOccurred())
					Expect(client.aggregations["exclude"]).To(Equal([]string{}))
					Expect(client.aggregations["bounce"]).To(Equal([]string{
						"1.1.1.1:4500",
					}))
				})
			})

			Context("with an undefined operation", func() {
				It("should not modify any list", func() {
					err := client.ClearAggregatedOperation("kill", []string{"1.1.1.1:4500", "1.1.1.4:4500"})
					Expect(err).NotTo(HaveOccurred())
					Expect(client.aggregations["exclude"]).To(Equal([]string{
						"1.1.1.1:4500", "1.1.1.2:4500",
					}))
					Expect(client.aggregations["bounce"]).To(Equal([]string{
						"1.1.1.1:4500",
					}))
				})
			})
		})
	})
})
