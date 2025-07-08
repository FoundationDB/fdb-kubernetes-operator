/*
 * remove_test.go
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
	"net"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("remove", func() {
	When("getting the zoned removals", func() {
		It("should return the correct mapping", func() {
			zones, timestamp, err := GetZonedRemovals([]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "1",
					FaultDomain:    "zone1",
				},
				{
					ProcessGroupID: "2",
					FaultDomain:    "zone1",
				},
				{
					ProcessGroupID: "3",
					FaultDomain:    "zone3",
				},
				{
					ProcessGroupID: "4",
				},
				{
					ProcessGroupID: "5",
					FaultDomain:    "zone5",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 1,
						},
					},
				},
				{
					ProcessGroupID: "6",
					FaultDomain:    "zone6",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 42,
						},
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(len(zones)).To(BeNumerically("==", 4))

			Expect(len(zones["zone1"])).To(BeNumerically("==", 2))
			Expect(zones["zone1"]).To(ConsistOf(
				&fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: "1",
					FaultDomain:    "zone1",
				},
				&fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: "2",
					FaultDomain:    "zone1",
				}))

			Expect(len(zones["zone3"])).To(BeNumerically("==", 1))
			Expect(zones["zone3"]).To(ConsistOf(
				&fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: "3",
					FaultDomain:    "zone3",
				},
			))

			Expect(len(zones[UnknownZone])).To(BeNumerically("==", 1))
			Expect(zones[UnknownZone]).To(ConsistOf(
				&fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: "4",
					FaultDomain:    "",
				},
			))

			Expect(len(zones[TerminatingZone])).To(BeNumerically("==", 2))
			Expect(zones[TerminatingZone]).To(ConsistOf(
				&fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: "5",
					FaultDomain:    "zone5",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 1,
						},
					},
				},
				&fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: "6",
					FaultDomain:    "zone6",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 42,
						},
					},
				},
			))

			Expect(timestamp).To(BeNumerically("==", 42))
		})
	})

	When("getting the process groups to remove", func() {
		zones := map[fdbv1beta2.FaultDomain][]*fdbv1beta2.ProcessGroupStatus{
			"zone1": {
				{
					ProcessGroupID: "1",
					FaultDomain:    "zone1",
				},
				{
					ProcessGroupID: "2",
					FaultDomain:    "zone1",
				},
			},
			"zone3": {
				{
					ProcessGroupID: "3",
					FaultDomain:    "zone3",
				},
				{
					ProcessGroupID: "4",
					FaultDomain:    "zone3",
				},
			},
			UnknownZone: {
				{
					ProcessGroupID: "5",
					FaultDomain:    UnknownZone,
				},
			},
		}

		DescribeTable(
			"should delete the Pods based on the deletion mode",
			func(removalMode fdbv1beta2.PodUpdateMode, zones map[fdbv1beta2.FaultDomain][]*fdbv1beta2.ProcessGroupStatus, expected int, expectedErr error) {
				_, removals, err := GetProcessGroupsToRemove(removalMode, zones)
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				}

				Expect(len(removals)).To(Equal(expected))
			},
			Entry("With the deletion mode Zone",
				fdbv1beta2.PodUpdateModeZone,
				zones,
				2,
				nil),
			Entry("With the deletion mode Zone and only terminating process groups",
				fdbv1beta2.PodUpdateModeZone,
				map[fdbv1beta2.FaultDomain][]*fdbv1beta2.ProcessGroupStatus{
					TerminatingZone: {
						{
							ProcessGroupID: "1",
							FaultDomain:    "zone1",
						},
						{
							ProcessGroupID: "2",
							FaultDomain:    "zone1",
						},
					},
				},
				0,
				nil),
			Entry("With the deletion mode Process Group",
				fdbv1beta2.PodUpdateModeProcessGroup,
				zones,
				1,
				nil),
			Entry("With the deletion mode All",
				fdbv1beta2.PodUpdateModeAll,
				zones,
				5,
				nil),
			Entry("With the deletion mode None",
				fdbv1beta2.PodUpdateModeNone,
				zones,
				0,
				nil),
			Entry("With an invalid deletion mode",
				fdbv1beta2.PodUpdateMode("banana"),
				zones,
				0,
				fmt.Errorf("unknown deletion mode: \"banana\"")),
		)
	})

	When("checking if a removal is allowed", func() {
		DescribeTable(
			"should return if a removal is allowed and the wait time",
			func(lastDeletion int64, currentTimestamp int64, waitBetween int, expectedRes bool, expectedWaitTime int64) {
				waitTime, ok := RemovalAllowed(lastDeletion, currentTimestamp, waitBetween)
				Expect(ok).To(Equal(expectedRes))
				Expect(waitTime).To(Equal(expectedWaitTime))
			},
			Entry("No last deletion",
				int64(0),
				int64(120),
				60,
				true,
				int64(0)),
			Entry("With a recent deletion",
				int64(120),
				int64(121),
				60,
				false,
				int64(59)),
			Entry("With a recent deletion",
				int64(120),
				int64(179),
				60,
				false,
				int64(1)),
			Entry("With a recent deletion but enough wait time",
				int64(120),
				int64(181),
				60,
				true,
				int64(0)),
			Entry("With a recent deletion but a short wait time",
				int64(120),
				int64(121),
				0,
				true,
				int64(0)),
			Entry("With a recent deletion and a long wait time",
				int64(120),
				int64(181),
				120,
				false,
				int64(59)),
		)
	})

	DescribeTable(
		"when getting the addresses to validate before removal",
		func(cluster *fdbv1beta2.FoundationDBCluster, expected []fdbv1beta2.ProcessAddress) {
			remainingMap, addresses := getAddressesToValidateBeforeRemoval(logr.Discard(), cluster)
			Expect(addresses).To(ConsistOf(expected))
			for _, address := range addresses {
				Expect(remainingMap).To(HaveKeyWithValue(address.String(), false))
			}
			Expect(remainingMap).To(HaveLen(len(expected)))
		},
		Entry("when no process groups must be removed",
			&fdbv1beta2.FoundationDBCluster{},
			nil,
		),
		Entry("when one storage process group must be removed",
			&fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
						},
					},
				},
			},
			[]fdbv1beta2.ProcessAddress{
				{
					IPAddress: net.ParseIP("192.0.0.1"),
				},
			},
		),
		Entry("when one storage process group must be removed but is already excluded",
			&fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
							RemovalTimestamp:   &metav1.Time{Time: time.Now()},
							ExclusionTimestamp: &metav1.Time{Time: time.Now()},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
						},
					},
				},
			},
			nil,
		),
		Entry("when one storage process group must be removed with locality based exclusions",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						UseLocalitiesForExclusion: pointer.Bool(true),
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					RunningVersion: fdbv1beta2.Versions.SupportsLocalityBasedExclusions.String(),
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
						},
					},
				},
			},
			[]fdbv1beta2.ProcessAddress{
				{
					StringAddress: fdbv1beta2.FDBLocalityExclusionPrefix + ":" + "storage-1",
				},
			},
		),
		Entry("when one log process group must be removed",
			&fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
						},
					},
				},
			},
			[]fdbv1beta2.ProcessAddress{
				{
					IPAddress: net.ParseIP("192.0.0.4"),
				},
			},
		),
		Entry("when one log process group must be removed but is already excluded",
			&fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},

							RemovalTimestamp:   &metav1.Time{Time: time.Now()},
							ExclusionTimestamp: &metav1.Time{Time: time.Now()},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
						},
					},
				},
			},
			nil,
		),
		Entry("when one log process group must be removed with locality based exclusions",
			&fdbv1beta2.FoundationDBCluster{
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						UseLocalitiesForExclusion: pointer.Bool(true),
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					RunningVersion: fdbv1beta2.Versions.SupportsLocalityBasedExclusions.String(),
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
						},
					},
				},
			},
			[]fdbv1beta2.ProcessAddress{
				{
					StringAddress: fdbv1beta2.FDBLocalityExclusionPrefix + ":" + "log-1",
				},
				{
					IPAddress: net.ParseIP("192.0.0.4"),
				},
			},
		),
		Entry("when one process group with multiple addresses must be removed",
			&fdbv1beta2.FoundationDBCluster{
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
						{
							ProcessGroupID: "storage-1",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.1",
							},
						},
						{
							ProcessGroupID: "storage-2",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.2",
							},
						},
						{
							ProcessGroupID: "storage-3",
							ProcessClass:   fdbv1beta2.ProcessClassStorage,
							Addresses: []string{
								"192.0.0.3",
							},
						},
						{
							ProcessGroupID: "log-1",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.4",
							},
						},
						{
							ProcessGroupID: "log-2",
							ProcessClass:   fdbv1beta2.ProcessClassLog,
							Addresses: []string{
								"192.0.0.5",
								"192.0.0.6",
							},
							RemovalTimestamp: &metav1.Time{Time: time.Now()},
						},
					},
				},
			},
			[]fdbv1beta2.ProcessAddress{
				{
					IPAddress: net.ParseIP("192.0.0.5"),
				},
				{
					IPAddress: net.ParseIP("192.0.0.6"),
				},
			},
		),
	)
})
