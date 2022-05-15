/*
 * metrics.go
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
	"time"

	"github.com/prometheus/client_golang/prometheus"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("metrics", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = &fdbv1beta2.FoundationDBCluster{
			Status: fdbv1beta2.FoundationDBClusterStatus{
				ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
					{
						ProcessClass: fdbv1beta2.ProcessClassStorage,
					},
					{
						ProcessClass: fdbv1beta2.ProcessClassLog,
						ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
							fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
						},
					},
					{
						ProcessClass:     fdbv1beta2.ProcessClassStorage,
						RemovalTimestamp: &metav1.Time{Time: time.Now()},
					},
					{
						ProcessClass:       fdbv1beta2.ProcessClassStateless,
						RemovalTimestamp:   &metav1.Time{Time: time.Now()},
						ExclusionTimestamp: &metav1.Time{Time: time.Now()},
					},
				},
			},
		}
	})

	Context("Collecting the processGroup metrics", func() {
		It("generate the process class metrics", func() {
			stats, removals, exclusions := getProcessGroupMetrics(cluster)
			Expect(len(stats)).To(BeNumerically("==", 3))
			Expect(len(stats[fdbv1beta2.ProcessClassStorage])).To(BeNumerically("==", len(fdbv1beta2.AllProcessGroupConditionTypes())))
			Expect(len(stats[fdbv1beta2.ProcessClassStorage])).To(BeNumerically("==", len(fdbv1beta2.AllProcessGroupConditionTypes())))
			Expect(stats[fdbv1beta2.ProcessClassStorage][fdbv1beta2.ReadyCondition]).To(BeNumerically("==", 2))
			Expect(stats[fdbv1beta2.ProcessClassLog][fdbv1beta2.ReadyCondition]).To(BeNumerically("==", 0))
			Expect(stats[fdbv1beta2.ProcessClassLog][fdbv1beta2.MissingProcesses]).To(BeNumerically("==", 1))
			Expect(stats[fdbv1beta2.ProcessClassStateless][fdbv1beta2.ReadyCondition]).To(BeNumerically("==", 1))
			Expect(removals[fdbv1beta2.ProcessClassStorage]).To(BeNumerically("==", 1))
			Expect(exclusions[fdbv1beta2.ProcessClassStorage]).To(BeNumerically("==", 0))
			Expect(removals[fdbv1beta2.ProcessClassStateless]).To(BeNumerically("==", 1))
			Expect(exclusions[fdbv1beta2.ProcessClassStateless]).To(BeNumerically("==", 1))
		})
	})

	When("collecting the prometheus metrics", func() {
		It("", func() {
			result := make(chan prometheus.Metric)

			visitedMetricsCnt := 0
			go func() {
				for range result {
					visitedMetricsCnt++
				}
			}()

			collectMetrics(result, cluster)

			Expect(len(result)).To(BeNumerically("==", 0))
			Expect(visitedMetricsCnt).To(BeNumerically("==", 48))
		})
	})
})
