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
	"time"

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
			zones, timestamp, err := GetZonedRemovals(status, []*fdbtypes.ProcessGroupStatus{
				{
					ProcessGroupID: "1",
				},
				{
					ProcessGroupID: "2",
				},
				{
					ProcessGroupID: "3",
				},
				{
					ProcessGroupID: "4",
				},
				{
					ProcessGroupID: "5",
					ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbtypes.ResourcesTerminating,
							Timestamp:                 1,
						},
					},
				},
				{
					ProcessGroupID: "6",
					ProcessGroupConditions: []*fdbtypes.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbtypes.ResourcesTerminating,
							Timestamp:                 42,
						},
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(len(zones)).To(BeNumerically("==", 4))

			Expect(len(zones["zone1"])).To(BeNumerically("==", 2))
			Expect(zones["zone1"]).To(ConsistOf("1", "2"))

			Expect(len(zones["zone3"])).To(BeNumerically("==", 1))
			Expect(zones["zone3"]).To(ConsistOf("3"))

			Expect(len(zones[UnknownZone])).To(BeNumerically("==", 1))
			Expect(zones[UnknownZone]).To(ConsistOf("4"))

			Expect(len(zones[TerminatingZone])).To(BeNumerically("==", 2))
			Expect(zones[TerminatingZone]).To(ConsistOf("5", "6"))

			Expect(timestamp).To(BeNumerically("==", 42))
		})

	})

	When("getting the process groups to remove", func() {
		zones := map[string][]string{
			"zone1":     {"1", "2"},
			"zone3":     {"3", "4"},
			UnknownZone: {"4", "5"},
		}

		DescribeTable("should delete the Pods based on the deletion mode",
			func(removalMode fdbtypes.PodUpdateMode, zones map[string][]string, expected int, expectedErr error) {
				_, removals, err := GetProcessGroupsToRemove(removalMode, zones)
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				}

				Expect(len(removals)).To(Equal(expected))
			},
			Entry("With the deletion mode Zone",
				fdbtypes.PodUpdateModeZone,
				zones,
				2,
				nil),
			Entry("With the deletion mode Zone and only terminating process groupse",
				fdbtypes.PodUpdateModeZone,
				map[string][]string{
					TerminatingZone: {"1", "2"},
				},
				0,
				nil),
			Entry("With the deletion mode Process Group",
				fdbtypes.PodUpdateModeProcessGroup,
				zones,
				1,
				nil),
			Entry("With the deletion mode All",
				fdbtypes.PodUpdateModeAll,
				zones,
				6,
				nil),
			Entry("With the deletion mode None",
				fdbtypes.PodUpdateModeNone,
				zones,
				0,
				nil),
			Entry("With the deletion mode All",
				fdbtypes.PodUpdateMode("banana"),
				zones,
				0,
				fmt.Errorf("unknown deletion mode: \"banana\"")),
		)
	})

	When("checking if a removal is allowed", func() {
		DescribeTable("should return if a removal is allowed and the wait time",
			func(lastDeletion int64, currentTimestamp int64, waitBetween time.Duration, expectedRes bool, expectedWaitTime int64) {
				waitTime, ok := RemovalAllowed(lastDeletion, currentTimestamp, waitBetween)
				Expect(ok).To(Equal(expectedRes))
				Expect(waitTime).To(Equal(expectedWaitTime))
			},
			Entry("No last deletion",
				int64(0),
				int64(120),
				1*time.Minute,
				true,
				int64(0)),
			Entry("With a recent deletion",
				int64(120),
				int64(121),
				1*time.Minute,
				false,
				int64(59)),
			Entry("With a recent deletion",
				int64(120),
				int64(179),
				1*time.Minute,
				false,
				int64(1)),
			Entry("With a recent deletion but enough wait time",
				int64(120),
				int64(181),
				1*time.Minute,
				true,
				int64(0)),
			Entry("With a recent deletion but a short wait time",
				int64(120),
				int64(121),
				1*time.Nanosecond,
				true,
				int64(0)),
			Entry("With a recent deletion and a long wait time",
				int64(120),
				int64(181),
				2*time.Minute,
				false,
				int64(59)),
		)
	})
})
