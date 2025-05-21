/*
 * maintenance_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2024 Apple Inc. and the FoundationDB project authors
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

package maintenance

import (
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("maintenance", func() {
	type maintenanceTest struct {
		cluster                     *fdbv1beta2.FoundationDBCluster
		status                      *fdbv1beta2.FoundationDBStatus
		processesUnderMaintenance   map[fdbv1beta2.ProcessGroupID]int64
		staleDuration               time.Duration
		differentZoneWaitDuration   time.Duration
		finishedMaintenance         []fdbv1beta2.ProcessGroupID
		staleMaintenanceInformation []fdbv1beta2.ProcessGroupID
		processesToUpdate           []fdbv1beta2.ProcessGroupID
	}

	DescribeTable("when getting the maintenance information", func(input maintenanceTest) {
		finishedMaintenance, staleMaintenanceInformation, processesToUpdate := GetMaintenanceInformation(logr.Discard(), input.cluster, input.status, input.processesUnderMaintenance, input.staleDuration, input.differentZoneWaitDuration)
		Expect(finishedMaintenance).To(ConsistOf(input.finishedMaintenance))
		Expect(staleMaintenanceInformation).To(ConsistOf(input.staleMaintenanceInformation))
		Expect(processesToUpdate).To(ConsistOf(input.processesToUpdate))
	},
		Entry("when no process are under maintenance",
			maintenanceTest{
				cluster:                     &fdbv1beta2.FoundationDBCluster{},
				status:                      &fdbv1beta2.FoundationDBStatus{},
				processesUnderMaintenance:   nil,
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         nil,
				staleMaintenanceInformation: nil,
				processesToUpdate:           nil,
			},
		),
		Entry("when one process is done with the maintenance",
			maintenanceTest{
				cluster: &fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
						},
					},
				},
				status: &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						MaintenanceZone: "rack-1",
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"storage-1": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 15.0,
							},
						},
					},
				},
				processesUnderMaintenance: map[fdbv1beta2.ProcessGroupID]int64{
					"storage-1": time.Now().Add(-1 * time.Minute).Unix(),
				},
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         []fdbv1beta2.ProcessGroupID{"storage-1"},
				staleMaintenanceInformation: nil,
				processesToUpdate:           nil,
			},
		),
		Entry("when one process is done with the maintenance but another one is pending",
			maintenanceTest{
				cluster: &fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
						},
					},
				},
				status: &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						MaintenanceZone: "rack-1",
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"storage-1": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 15.0,
							},
							"storage-2": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-2",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 900000.0,
							},
						},
					},
				},
				processesUnderMaintenance: map[fdbv1beta2.ProcessGroupID]int64{
					"storage-1": time.Now().Add(-1 * time.Minute).Unix(),
					"storage-2": time.Now().Add(-1 * time.Minute).Unix(),
				},
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         []fdbv1beta2.ProcessGroupID{"storage-1"},
				staleMaintenanceInformation: nil,
				processesToUpdate:           []fdbv1beta2.ProcessGroupID{"storage-2"},
			},
		),
		Entry("when one process is done with the maintenance but another one is missing in the status",
			maintenanceTest{
				cluster: &fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
						},
					},
				},
				status: &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						MaintenanceZone: "rack-1",
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"storage-1": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 15.0,
							},
						},
					},
				},
				processesUnderMaintenance: map[fdbv1beta2.ProcessGroupID]int64{
					"storage-1": time.Now().Add(-1 * time.Minute).Unix(),
					"storage-2": time.Now().Add(-1 * time.Minute).Unix(),
				},
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         []fdbv1beta2.ProcessGroupID{"storage-1"},
				staleMaintenanceInformation: nil,
				processesToUpdate:           []fdbv1beta2.ProcessGroupID{"storage-2"},
			},
		),
		Entry("when one process is done with the maintenance but another one is missing in the status and is removed from the process group list",
			maintenanceTest{
				cluster: &fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
						},
					},
				},
				status: &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						MaintenanceZone: "rack-1",
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"storage-1": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 15.0,
							},
						},
					},
				},
				processesUnderMaintenance: map[fdbv1beta2.ProcessGroupID]int64{
					"storage-1": time.Now().Add(-1 * time.Minute).Unix(),
					"storage-2": time.Now().Add(-1 * time.Minute).Unix(),
				},
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         []fdbv1beta2.ProcessGroupID{"storage-1"},
				staleMaintenanceInformation: []fdbv1beta2.ProcessGroupID{"storage-2"},
				processesToUpdate:           nil,
			},
		),
		Entry("when one process is done with the maintenance and another one from a different fault domain is present",
			maintenanceTest{
				cluster: &fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
						},
					},
				},
				status: &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						MaintenanceZone: "rack-1",
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"storage-1": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 15.0,
							},
							"storage-2": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-2",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-2",
								},
								UptimeSeconds: 900000.0,
							},
						},
					},
				},
				processesUnderMaintenance: map[fdbv1beta2.ProcessGroupID]int64{
					"storage-1": time.Now().Add(-1 * time.Minute).Unix(),
					"storage-2": time.Now().Add(-1 * time.Minute).Unix(),
				},
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         []fdbv1beta2.ProcessGroupID{"storage-1"},
				staleMaintenanceInformation: nil,
				processesToUpdate:           nil,
			},
		),
		Entry("when one process is done with the maintenance and another one from a different fault domain is present with an old entry",
			maintenanceTest{
				cluster: &fdbv1beta2.FoundationDBCluster{
					Status: fdbv1beta2.FoundationDBClusterStatus{
						ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
							{
								ProcessGroupID: "storage-1",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
							{
								ProcessGroupID: "storage-2",
								ProcessClass:   fdbv1beta2.ProcessClassStorage,
							},
						},
					},
				},
				status: &fdbv1beta2.FoundationDBStatus{
					Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
						MaintenanceZone: "rack-1",
						Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
							"storage-1": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-1",
								},
								UptimeSeconds: 15.0,
							},
							"storage-2": {
								ProcessClass: fdbv1beta2.ProcessClassStorage,
								Locality: map[string]string{
									fdbv1beta2.FDBLocalityInstanceIDKey: "storage-2",
									fdbv1beta2.FDBLocalityZoneIDKey:     "rack-2",
								},
								UptimeSeconds: 900000.0,
							},
						},
					},
				},
				processesUnderMaintenance: map[fdbv1beta2.ProcessGroupID]int64{
					"storage-1": time.Now().Add(-1 * time.Minute).Unix(),
					"storage-2": time.Now().Add(-2 * time.Hour).Unix(),
				},
				staleDuration:               1 * time.Hour,
				finishedMaintenance:         []fdbv1beta2.ProcessGroupID{"storage-1"},
				staleMaintenanceInformation: []fdbv1beta2.ProcessGroupID{"storage-2"},
				processesToUpdate:           nil,
			},
		),
	)

	// TODO test case for different zone -> differentZoneWaitDuration
})
