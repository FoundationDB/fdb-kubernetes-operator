/*
 * safeguards_test.go
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

package safeguards

import (
	"errors"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"time"
)

/*

// HasEnoughProcessesToUpgrade checks if enough processes are ready to be upgraded. The logic is currently a simple heuristic
// which checks if at least 90% of the desired processes are pending the upgrade. Processes that are stuck in a terminating
// state will be ignore for the calculation.
func HasEnoughProcessesToUpgrade(processGroupIDs []string, processGroups []*fdbv1beta2.ProcessGroupStatus, desiredProcesses fdbv1beta2.ProcessCounts) error {
	// TODO(johscheuer): Expose this fraction and make it configurable.
	desiredProcessCounts := int(math.Ceil(float64(desiredProcesses.Total()) * 0.9))

	var terminatingProcesses int
	for _, processGroup := range processGroups {
		if processGroup.GetConditionTime(fdbv1beta2.ResourcesTerminating) == nil {
			continue
		}

		terminatingProcesses++
	}

	processesForUpgrade := len(processGroupIDs) - terminatingProcesses
	if processesForUpgrade < desiredProcessCounts {
		return fmt.Errorf("expected to have %d process groups for performing the upgrade, currently only %d process groups are available", desiredProcessCounts, len(processGroupIDs))
	}

	return nil
}

*/

var _ = Describe("safeguards", func() {
	DescribeTable("when testing if enough processes are ready for upgrades", func(processGroupIDs []string, processGroups []*fdbv1beta2.ProcessGroupStatus, desiredProcesses fdbv1beta2.ProcessCounts, expected error) {
		err := HasEnoughProcessesToUpgrade(processGroupIDs, processGroups, desiredProcesses)
		if expected == nil {
			Expect(err).NotTo(HaveOccurred())
			return
		}

		Expect(err).To(Equal(expected))
	},
		Entry("All process groups are fine",
			[]string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
				"storage-6",
				"storage-7",
				"storage-8",
				"storage-9",
				"storage-10",
			},
			[]*fdbv1beta2.ProcessGroupStatus{},
			fdbv1beta2.ProcessCounts{
				Storage: 10,
			},
			nil,
		),
		Entry("To many processes are missing",
			[]string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
				"storage-6",
				"storage-7",
				"storage-8",
				"storage-9",
				"storage-10",
			},
			[]*fdbv1beta2.ProcessGroupStatus{},
			fdbv1beta2.ProcessCounts{
				Storage: 15,
			},
			errors.New("expected to have 14 process groups for performing the upgrade, currently only 10 process groups are available"),
		),
		Entry("when processes with terminating state exist",
			[]string{
				"storage-1",
				"storage-2",
				"storage-3",
				"storage-4",
				"storage-5",
				"storage-6",
				"storage-7",
				"storage-8",
				"storage-9",
				"storage-10",
				"storage-11",
				"storage-12",
				"storage-13",
			},
			[]*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: "storage-11",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 time.Now().Unix(),
						},
					},
				},
				{
					ProcessGroupID: "storage-12",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 time.Now().Unix(),
						},
					},
				},
				{
					ProcessGroupID: "storage-13",
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						{
							ProcessGroupConditionType: fdbv1beta2.ResourcesTerminating,
							Timestamp:                 time.Now().Unix(),
						},
					},
				},
			},
			fdbv1beta2.ProcessCounts{
				Storage: 10,
			},
			nil,
		),
	)
})
