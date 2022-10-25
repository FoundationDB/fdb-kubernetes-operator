package locality

/*
 * change_coordinators_test.go
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

import (
	"math"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Change coordinators", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		disabled := false
		cluster.Spec.LockOptions.DisableLocks = &disabled
		cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
			{
				ProcessClass: fdbv1beta2.ProcessClassStorage,
				Priority:     math.MaxInt32,
			},
			{
				ProcessClass: fdbv1beta2.ProcessClassLog,
				Priority:     0,
			},
		}
	})

	When("Sorting the localities", func() {
		var localities []Info

		BeforeEach(func() {
			localities = []Info{
				{
					ID:    "storage-1",
					Class: fdbv1beta2.ProcessClassStorage,
				},
				{
					ID:    "tlog-1",
					Class: fdbv1beta2.ProcessClassTransaction,
				},
				{
					ID:    "log-1",
					Class: fdbv1beta2.ProcessClassLog,
				},
				{
					ID:    "storage-51",
					Class: fdbv1beta2.ProcessClassStorage,
				},
			}
		})

		When("no other preferences are defined", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{}
			})

			It("should sort the localities based on the IDs", func() {
				sortLocalities(cluster, localities)

				Expect(localities[0].Class).To(Equal(fdbv1beta2.ProcessClassLog))
				Expect(localities[0].ID).To(Equal("log-1"))
				Expect(localities[1].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[1].ID).To(Equal("storage-1"))
				Expect(localities[2].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[2].ID).To(Equal("storage-51"))
				Expect(localities[3].Class).To(Equal(fdbv1beta2.ProcessClassTransaction))
				Expect(localities[3].ID).To(Equal("tlog-1"))
			})
		})

		When("when the storage class is preferred", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     0,
					},
				}
			})

			It("should sort the localities based on the provided config", func() {
				sortLocalities(cluster, localities)

				Expect(localities[0].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[0].ID).To(Equal("storage-1"))
				Expect(localities[1].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[1].ID).To(Equal("storage-51"))
				Expect(localities[2].Class).To(Equal(fdbv1beta2.ProcessClassLog))
				Expect(localities[2].ID).To(Equal("log-1"))
				Expect(localities[3].Class).To(Equal(fdbv1beta2.ProcessClassTransaction))
				Expect(localities[3].ID).To(Equal("tlog-1"))
			})
		})

		When("when the storage class is preferred over transaction class", func() {
			BeforeEach(func() {
				cluster.Spec.CoordinatorSelection = []fdbv1beta2.CoordinatorSelectionSetting{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
						Priority:     1,
					},
					{
						ProcessClass: fdbv1beta2.ProcessClassTransaction,
						Priority:     0,
					},
				}
			})

			It("should sort the localities based on the provided config", func() {
				sortLocalities(cluster, localities)

				Expect(localities[0].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[0].ID).To(Equal("storage-1"))
				Expect(localities[1].Class).To(Equal(fdbv1beta2.ProcessClassStorage))
				Expect(localities[1].ID).To(Equal("storage-51"))
				Expect(localities[2].Class).To(Equal(fdbv1beta2.ProcessClassTransaction))
				Expect(localities[2].ID).To(Equal("tlog-1"))
				Expect(localities[3].Class).To(Equal(fdbv1beta2.ProcessClassLog))
				Expect(localities[3].ID).To(Equal("log-1"))
			})
		})
	})

	Describe("chooseDistributedProcesses", func() {
		var candidates []Info
		var result []Info
		var err error

		Context("with a flat set of processes", func() {
			BeforeEach(func() {
				candidates = []Info{
					{ID: "p1", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p2", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p3", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p4", LocalityData: map[string]string{"zoneid": "z3"}},
					{ID: "p5", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p6", LocalityData: map[string]string{"zoneid": "z4"}},
					{ID: "p7", LocalityData: map[string]string{"zoneid": "z5"}},
				}
				result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should recruit the processes across multiple zones", func() {
				Expect(len(result)).To(Equal(5))
				Expect(result[0].ID).To(Equal("p1"))
				Expect(result[1].ID).To(Equal("p3"))
				Expect(result[2].ID).To(Equal("p4"))
				Expect(result[3].ID).To(Equal("p6"))
				Expect(result[4].ID).To(Equal("p7"))
			})
		})

		Context("with fewer zones than desired processes", func() {
			BeforeEach(func() {
				candidates = []Info{
					{ID: "p1", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p2", LocalityData: map[string]string{"zoneid": "z1"}},
					{ID: "p3", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p4", LocalityData: map[string]string{"zoneid": "z3"}},
					{ID: "p5", LocalityData: map[string]string{"zoneid": "z2"}},
					{ID: "p6", LocalityData: map[string]string{"zoneid": "z4"}},
				}
			})

			Context("with no hard limit", func() {
				It("should only re-use zones as necessary", func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{})
					Expect(err).NotTo(HaveOccurred())

					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p3"))
					Expect(result[2].ID).To(Equal("p4"))
					Expect(result[3].ID).To(Equal("p6"))
					Expect(result[4].ID).To(Equal("p2"))
				})
			})

			Context("with a hard limit", func() {
				It("should give an error", func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{
						HardLimits: map[string]int{"zoneid": 1},
					})
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("Could only select 4 processes, but 5 are required"))
				})
			})
		})

		Context("with multiple data centers", func() {
			BeforeEach(func() {
				candidates = []Info{
					{ID: "p1", LocalityData: map[string]string{"zoneid": "z1", "dcid": "dc1"}},
					{ID: "p2", LocalityData: map[string]string{"zoneid": "z1", "dcid": "dc1"}},
					{ID: "p3", LocalityData: map[string]string{"zoneid": "z2", "dcid": "dc1"}},
					{ID: "p4", LocalityData: map[string]string{"zoneid": "z3", "dcid": "dc1"}},
					{ID: "p5", LocalityData: map[string]string{"zoneid": "z2", "dcid": "dc1"}},
					{ID: "p6", LocalityData: map[string]string{"zoneid": "z4", "dcid": "dc1"}},
					{ID: "p7", LocalityData: map[string]string{"zoneid": "z5", "dcid": "dc1"}},
					{ID: "p8", LocalityData: map[string]string{"zoneid": "z6", "dcid": "dc2"}},
					{ID: "p9", LocalityData: map[string]string{"zoneid": "z7", "dcid": "dc2"}},
					{ID: "p10", LocalityData: map[string]string{"zoneid": "z8", "dcid": "dc2"}},
				}
			})

			Context("with the default constraints", func() {
				BeforeEach(func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should recruit the processes across multiple zones and data centers", func() {
					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p10"))
					Expect(result[2].ID).To(Equal("p3"))
					Expect(result[3].ID).To(Equal("p8"))
					Expect(result[4].ID).To(Equal("p4"))
				})
			})

			Context("when only distributing across data centers", func() {
				BeforeEach(func() {
					result, err = ChooseDistributedProcesses(cluster, candidates, 5, ProcessSelectionConstraint{
						Fields: []string{"dcid"},
					})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should recruit the processes across data centers", func() {
					Expect(len(result)).To(Equal(5))
					Expect(result[0].ID).To(Equal("p1"))
					Expect(result[1].ID).To(Equal("p10"))
					Expect(result[2].ID).To(Equal("p2"))
					Expect(result[3].ID).To(Equal("p8"))
					Expect(result[4].ID).To(Equal("p3"))
				})
			})
		})
	})
})
