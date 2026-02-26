/*
 * exclusion_status_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2026 Apple Inc. and the FoundationDB project authors
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

package cmd

import (
	"bytes"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("[plugin] exclusion status", func() {
	When("testing calculateProgressBarWidth", func() {
		type testCase struct {
			terminalWidth    int
			expectedBarWidth int
		}

		DescribeTable("return the correct progress bar width",
			func(tc testCase) {
				Expect(calculateProgressBarWidth(tc.terminalWidth)).To(Equal(tc.expectedBarWidth))
			},
			Entry("terminal is very narrow, bar width below minimum",
				testCase{
					// overhead=90, so terminalWidth=100 → barWidth=10, below minimum 20 → returns 20
					terminalWidth:    100,
					expectedBarWidth: 20,
				}),
			Entry("terminal width yields bar exactly at minimum",
				testCase{
					// terminalWidth - overhead = 20 → exactly minimum
					terminalWidth:    overhead + 20,
					expectedBarWidth: 20,
				}),
			Entry("terminal width yields bar in the normal range",
				testCase{
					// terminalWidth - overhead = 40 → within [20, 60]
					terminalWidth:    overhead + 40,
					expectedBarWidth: 40,
				}),
			Entry("terminal width yields bar exactly at upper boundary (not above), returns 60",
				testCase{
					// terminalWidth - overhead = 60 → not > 60, so returns 60 directly
					terminalWidth:    overhead + 60,
					expectedBarWidth: 60,
				}),
			Entry("terminal width yields bar one above boundary, returns 120",
				testCase{
					// terminalWidth - overhead = 61 → 61 > 60, returns 120
					terminalWidth:    overhead + 61,
					expectedBarWidth: 120,
				}),
			Entry("terminal width yields bar well above maximum boundary",
				testCase{
					// terminalWidth - overhead = 200 → above 60, returns 120
					terminalWidth:    overhead + 200,
					expectedBarWidth: 120,
				}),
		)
	})

	When("testing renderProgressBar", func() {
		type testCase struct {
			storedBytes    int
			maxBytes       int
			width          int
			expectedOutput string
		}

		DescribeTable("render the correct progress bar",
			func(tc testCase) {
				Expect(
					renderProgressBar(tc.storedBytes, tc.maxBytes, tc.width),
				).To(Equal(tc.expectedOutput))
			},
			Entry("maxBytes is zero, process is fully excluded",
				testCase{
					storedBytes:    0,
					maxBytes:       0,
					width:          10,
					expectedOutput: "[██████████] 100.0%",
				}),
			Entry("storedBytes is zero, all data moved out",
				testCase{
					storedBytes:    0,
					maxBytes:       1000,
					width:          10,
					expectedOutput: "[██████████] 100.0%",
				}),
			Entry("storedBytes equals maxBytes, no progress",
				testCase{
					storedBytes:    1000,
					maxBytes:       1000,
					width:          10,
					expectedOutput: "[░░░░░░░░░░] 0.0%",
				}),
			Entry("storedBytes is half of maxBytes",
				testCase{
					storedBytes:    500,
					maxBytes:       1000,
					width:          10,
					expectedOutput: "[█████░░░░░] 50.0%",
				}),
			Entry("storedBytes exceeds maxBytes, clamped to 0%",
				testCase{
					storedBytes:    1500,
					maxBytes:       1000,
					width:          10,
					expectedOutput: "[░░░░░░░░░░] 0.0%",
				}),
		)
	})

	When("testing trackInitialBytes", func() {
		type testCase struct {
			previousRun   map[string]exclusionResult
			instance      string
			storedBytes   int
			expectedBytes int
		}

		DescribeTable("return the correct initial bytes",
			func(tc testCase) {
				Expect(
					trackInitialBytes(tc.previousRun, tc.instance, tc.storedBytes),
				).To(Equal(tc.expectedBytes))
			},
			Entry("no previous run, returns current storedBytes",
				testCase{
					previousRun:   map[string]exclusionResult{},
					instance:      "storage-1",
					storedBytes:   1000,
					expectedBytes: 1000,
				}),
			Entry("previous run exists with initialBytes > 0, returns previous initialBytes",
				testCase{
					previousRun: map[string]exclusionResult{
						"storage-1": {initialBytes: 5000, storedBytes: 3000},
					},
					instance:      "storage-1",
					storedBytes:   1000,
					expectedBytes: 5000,
				}),
			Entry("previous run exists but initialBytes is 0, returns current storedBytes",
				testCase{
					previousRun: map[string]exclusionResult{
						"storage-1": {initialBytes: 0, storedBytes: 3000},
					},
					instance:      "storage-1",
					storedBytes:   1000,
					expectedBytes: 1000,
				}),
			Entry("previous run exists for different instance, returns current storedBytes",
				testCase{
					previousRun: map[string]exclusionResult{
						"storage-2": {initialBytes: 5000, storedBytes: 3000},
					},
					instance:      "storage-1",
					storedBytes:   1000,
					expectedBytes: 1000,
				}),
		)
	})

	When("testing getOngoingExclusions", func() {
		var (
			baseTimestamp time.Time
			laterTime     time.Time
		)

		BeforeEach(func() {
			baseTimestamp = time.Now()
			laterTime = baseTimestamp.Add(1 * time.Minute)
		})

		type testCase struct {
			status               *fdbv1beta2.FoundationDBStatus
			ignoreFullyExcluded  bool
			previousRun          map[string]exclusionResult
			expectedExclusionIDs []string
			expectedExcludedCnt  int
		}

		DescribeTable(
			"return the correct ongoing exclusions",
			func(tc testCase) {
				results, totalExcluded := getOngoingExclusions(
					tc.status,
					tc.ignoreFullyExcluded,
					tc.previousRun,
					baseTimestamp,
				)

				actualIDs := make([]string, 0, len(results))
				for _, r := range results {
					actualIDs = append(actualIDs, r.id)
				}

				Expect(actualIDs).To(ConsistOf(tc.expectedExclusionIDs))
				Expect(totalExcluded).To(Equal(tc.expectedExcludedCnt))
			},
			Entry("no processes in status",
				testCase{
					status:               &fdbv1beta2.FoundationDBStatus{},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: nil,
					expectedExcludedCnt:  0,
				}),
			Entry("process is not excluded",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: false,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 500,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: nil,
					expectedExcludedCnt:  0,
				}),
			Entry("excluded process with no locality ID is skipped",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 500,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: nil,
					expectedExcludedCnt:  0,
				}),
			Entry("excluded process with no roles and ignoreFullyExcluded=true is skipped",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: nil,
					// Fully excluded processes are counted even when ignored
					expectedExcludedCnt: 1,
				}),
			Entry(
				"excluded process with no roles and ignoreFullyExcluded=false is included in count but not in exclusion list",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{},
								},
							},
						},
					},
					ignoreFullyExcluded:  false,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: nil,
					expectedExcludedCnt:  0,
				},
			),
			Entry("excluded process with stateless role produces no ongoing exclusion entry",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"stateless-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "stateless-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStateless),
											StoredBytes: 0,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: nil,
					expectedExcludedCnt:  1,
				}),
			Entry("excluded storage process with no previous run has N/A estimate",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 1000,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: []string{"storage-1"},
					expectedExcludedCnt:  1,
				}),
			Entry(
				"excluded storage process uses instance_id locality as fallback when process_id is absent",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityInstanceIDKey: "storage-1-instance",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 1000,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: []string{"storage-1-instance"},
					expectedExcludedCnt:  1,
				},
			),
			Entry("excluded storage process with previous run and no bytes change has N/A estimate",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 1000,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded: true,
					previousRun: map[string]exclusionResult{
						"storage-1": {
							id:           "storage-1",
							storedBytes:  1000,
							initialBytes: 5000,
							timestamp:    baseTimestamp.Add(-1 * time.Minute),
						},
					},
					expectedExclusionIDs: []string{"storage-1"},
					expectedExcludedCnt:  1,
				}),
			Entry("multiple excluded processes",
				testCase{
					status: &fdbv1beta2.FoundationDBStatus{
						Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
							Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
								"storage-1": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 2000,
										},
									},
								},
								"storage-2": {
									Excluded: true,
									Locality: map[string]string{
										fdbv1beta2.FDBLocalityProcessIDKey: "storage-2",
									},
									Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
										{
											Role:        string(fdbv1beta2.ProcessClassStorage),
											StoredBytes: 1000,
										},
									},
								},
							},
						},
					},
					ignoreFullyExcluded:  true,
					previousRun:          map[string]exclusionResult{},
					expectedExclusionIDs: []string{"storage-1", "storage-2"},
					expectedExcludedCnt:  2,
				}),
		)

		It("should calculate a non-N/A estimate when bytes decrease between runs", func() {
			status := &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Processes: map[fdbv1beta2.ProcessGroupID]fdbv1beta2.FoundationDBStatusProcessInfo{
						"storage-1": {
							Excluded: true,
							Locality: map[string]string{
								fdbv1beta2.FDBLocalityProcessIDKey: "storage-1",
							},
							Roles: []fdbv1beta2.FoundationDBStatusProcessRoleInfo{
								{Role: string(fdbv1beta2.ProcessClassStorage), StoredBytes: 500},
							},
						},
					},
				},
			}
			previousRun := map[string]exclusionResult{
				"storage-1": {
					id:           "storage-1",
					storedBytes:  1000,
					initialBytes: 2000,
					timestamp:    baseTimestamp.Add(-1 * time.Minute),
				},
			}

			results, _ := getOngoingExclusions(status, true, previousRun, laterTime)
			Expect(results).To(HaveLen(1))
			Expect(results[0].estimate).NotTo(Equal("N/A"))
			Expect(results[0].initialBytes).To(Equal(2000))
		})
	})

	When("testing barChartPrinter", func() {
		var (
			outBuffer bytes.Buffer
			printer   barChartPrinter
		)

		BeforeEach(func() {
			outBuffer.Reset()
			printer = barChartPrinter{
				output:           &outBuffer,
				separator:        strings.Repeat("=", 10),
				progressBarWidth: 10,
				interval:         1 * time.Minute,
			}
		})

		It("printerHeader should print the timestamp and separator", func() {
			ts := time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC)
			printer.printerHeader(ts)
			output := outBuffer.String()
			Expect(output).To(ContainSubstring("14:30:00"))
			Expect(output).To(ContainSubstring(strings.Repeat("=", 10)))
		})

		It("printExclusionResult with zero storedBytes should print 'fully excluded'", func() {
			result := exclusionResult{
				id:           "storage-1",
				storedBytes:  0,
				initialBytes: 0,
				estimate:     "N/A",
			}
			printer.printExclusionResult(result)
			output := outBuffer.String()
			Expect(output).To(ContainSubstring("storage-1"))
			Expect(output).To(ContainSubstring("fully excluded"))
			Expect(output).To(ContainSubstring("ETA: N/A"))
		})

		It("printExclusionResult with non-zero storedBytes should print byte count", func() {
			result := exclusionResult{
				id:           "storage-2",
				storedBytes:  1024 * 1024,
				initialBytes: 2 * 1024 * 1024,
				estimate:     "30s",
			}
			printer.printExclusionResult(result)
			output := outBuffer.String()
			Expect(output).To(ContainSubstring("storage-2"))
			Expect(output).NotTo(ContainSubstring("fully excluded"))
			Expect(output).To(ContainSubstring("ETA: 30s"))
		})

		It("printerSummary should print counts and team tracker info", func() {
			printer.printerSummary(
				[]string{"region: primary, unhealthy servers 0, min replicas remaining 3"},
				2,
				1,
			)
			output := outBuffer.String()
			Expect(output).To(ContainSubstring("Total processes being excluded: 2"))
			Expect(output).To(ContainSubstring("ongoing exclusions: 1"))
			Expect(output).To(ContainSubstring("primary"))
			Expect(output).To(ContainSubstring("1m0s"))
		})
	})

	When("testing textPrinter", func() {
		var (
			outBuffer bytes.Buffer
			printer   textPrinter
		)

		BeforeEach(func() {
			outBuffer.Reset()
			printer = textPrinter{
				output:    &outBuffer,
				separator: strings.Repeat("=", 10),
				interval:  5 * time.Minute,
			}
		})

		It("printerHeader should print the timestamp and separator", func() {
			ts := time.Date(2024, 1, 15, 9, 5, 3, 0, time.UTC)
			printer.printerHeader(ts)
			output := outBuffer.String()
			Expect(output).To(ContainSubstring("09:05:03"))
			Expect(output).To(ContainSubstring(strings.Repeat("=", 10)))
		})

		It("printExclusionResult should print the id, byte count, and estimate", func() {
			result := exclusionResult{
				id:          "storage-3",
				storedBytes: 512,
				estimate:    "2m",
			}
			printer.printExclusionResult(result)
			output := outBuffer.String()
			Expect(output).To(ContainSubstring("storage-3"))
			Expect(output).To(ContainSubstring("estimate: 2m"))
		})

		It(
			"printerSummary should print counts and multiple team tracker entries joined by dash",
			func() {
				printer.printerSummary(
					[]string{
						"region: primary, unhealthy servers 0, min replicas remaining 3",
						"region: remote, unhealthy servers 1, min replicas remaining 2",
					},
					3,
					2,
				)
				output := outBuffer.String()
				Expect(output).To(ContainSubstring("Total processes being excluded: 3"))
				Expect(output).To(ContainSubstring("ongoing exclusions: 2"))
				Expect(output).To(ContainSubstring("primary"))
				Expect(output).To(ContainSubstring("remote"))
				Expect(output).To(ContainSubstring("-"))
				Expect(output).To(ContainSubstring("5m0s"))
			},
		)
	})

	When("testing printSummaryOngoingExclusion", func() {
		var (
			outBuffer bytes.Buffer
			printer   *textPrinter
			timestamp time.Time
		)

		BeforeEach(func() {
			outBuffer.Reset()
			timestamp = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
			printer = &textPrinter{
				output:    &outBuffer,
				separator: strings.Repeat("=", 20),
				interval:  1 * time.Minute,
			}
		})

		It("should sort ongoingExclusions by storedBytes ascending", func() {
			ongoingExclusions := []exclusionResult{
				{id: "storage-3", storedBytes: 3000, estimate: "N/A"},
				{id: "storage-1", storedBytes: 1000, estimate: "N/A"},
				{id: "storage-2", storedBytes: 2000, estimate: "N/A"},
			}
			status := &fdbv1beta2.FoundationDBStatus{}

			printSummaryOngoingExclusion(printer, status, ongoingExclusions, timestamp, 3)

			output := outBuffer.String()
			pos1 := strings.Index(output, "storage-1")
			pos2 := strings.Index(output, "storage-2")
			pos3 := strings.Index(output, "storage-3")
			Expect(pos1).To(BeNumerically("<", pos2))
			Expect(pos2).To(BeNumerically("<", pos3))
		})

		It("should include team tracker info for primary and remote regions", func() {
			unhealthyServers := ptr.To(int64(1))
			status := &fdbv1beta2.FoundationDBStatus{
				Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
					Data: fdbv1beta2.FoundationDBStatusDataStatistics{
						TeamTrackers: []fdbv1beta2.FoundationDBStatusTeamTracker{
							{
								Primary: true,
								State: fdbv1beta2.FoundationDBStatusDataState{
									MinReplicasRemaining: 3,
								},
								UnhealthyServers: unhealthyServers,
							},
							{
								Primary: false,
								State: fdbv1beta2.FoundationDBStatusDataState{
									MinReplicasRemaining: 2,
								},
							},
						},
					},
				},
			}
			ongoingExclusions := []exclusionResult{
				{id: "storage-1", storedBytes: 500, estimate: "N/A"},
			}

			printSummaryOngoingExclusion(printer, status, ongoingExclusions, timestamp, 1)

			output := outBuffer.String()
			Expect(output).To(ContainSubstring("region: primary"))
			Expect(output).To(ContainSubstring("region: remote"))
			Expect(output).To(ContainSubstring("unhealthy servers 1"))
			Expect(output).To(ContainSubstring("min replicas remaining 3"))
			Expect(output).To(ContainSubstring("min replicas remaining 2"))
		})
	})
})
