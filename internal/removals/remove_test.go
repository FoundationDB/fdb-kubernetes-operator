/*
 * remove.go
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

package removals

import (
	"fmt"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("remove", func() {
	When("getting the zoned removals", func() {
		var status *fdbtypes.FoundationDBStatus

		BeforeEach(func() {
			status = &fdbtypes.FoundationDBStatus{
				Cluster: fdbtypes.FoundationDBStatusClusterInfo{
					Processes: map[string]fdbtypes.FoundationDBStatusProcessInfo{
						"1": {
							Locality: map[string]string{
								fdbtypes.FDBLocalityInstanceIDKey: "1",
								fdbtypes.FDBLocalityZoneIDKey:     "zone1",
							},
						},
						"2": {
							Locality: map[string]string{
								fdbtypes.FDBLocalityInstanceIDKey: "2",
								fdbtypes.FDBLocalityZoneIDKey:     "zone1",
							},
						},
						"3": {
							Locality: map[string]string{
								fdbtypes.FDBLocalityInstanceIDKey: "3",
								fdbtypes.FDBLocalityZoneIDKey:     "zone3",
							},
						},
					},
				},
			}
		})

		It("should return the correct mapping", func() {
			zones, err := GetZonedRemovals(status, []string{"1", "2", "3", "4"})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(zones)).To(BeNumerically("==", 3))

			Expect(len(zones["zone1"])).To(BeNumerically("==", 2))
			Expect(zones["zone1"]).To(ConsistOf("1", "2"))

			Expect(len(zones["zone3"])).To(BeNumerically("==", 1))
			Expect(zones["zone3"]).To(ConsistOf("3"))

			Expect(len(zones["UNKNOWN"])).To(BeNumerically("==", 1))
			Expect(zones["UNKNOWN"]).To(ConsistOf("4"))
		})

	})

	When("getting the process groups to remove", func() {
		zones := map[string][]string{
			"zone1":   {"1", "2"},
			"zone3":   {"3", "4"},
			"UNKNOWN": {"4", "5"},
		}

		DescribeTable("should delete the Pods based on the deletion mode",
			func(removalMode fdbtypes.DeletionMode, zones map[string][]string, expected int, expectedErr error) {
				_, removals, err := GetProcessGroupsToRemove(removalMode, zones)
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				}

				Expect(len(removals)).To(Equal(expected))
			},
			Entry("With the deletion mode Zone",
				fdbtypes.DeletionModeZone,
				zones,
				2,
				nil),
			Entry("With the deletion mode Process Group",
				fdbtypes.DeletionModeProcessGroup,
				zones,
				1,
				nil),
			Entry("With the deletion mode All",
				fdbtypes.DeletionModeAll,
				zones,
				6,
				nil),
			Entry("With the deletion mode All",
				fdbtypes.DeletionMode("banana"),
				zones,
				0,
				fmt.Errorf("unknown deletion mode: \"banana\"")),
		)
	})
})
