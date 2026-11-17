/*
 * metrics_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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

package metrics

import (
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getVersionInfoLabels collects the metrics of the cluster and returns the labels of the version info metric.
func getVersionInfoLabels(cluster *fdbv1beta2.FoundationDBCluster) (map[string]string, float64) {
	ch := make(chan prometheus.Metric, 100)
	CollectMetrics(ch, cluster)
	close(ch)

	for metric := range ch {
		if metric.Desc().String() != descClusterVersionInfo.String() {
			continue
		}

		data := &dto.Metric{}
		Expect(metric.Write(data)).To(Succeed())
		labels := make(map[string]string, len(data.GetLabel()))
		for _, label := range data.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}

		return labels, data.GetGauge().GetValue()
	}

	return nil, 0
}

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

	Context("Collecting the version metrics", func() {
		BeforeEach(func() {
			cluster.Name = "test"
			cluster.Namespace = "default"
			cluster.Spec.Version = "7.4.5"
			cluster.Status.RunningVersion = "7.3.69"
		})

		It(
			"should use the version of the cluster as monitor version if the main container image is not pinned",
			func() {
				labels, value := getVersionInfoLabels(cluster)
				Expect(value).To(BeNumerically("==", 1))
				Expect(labels).To(Equal(map[string]string{
					"namespace":       "default",
					"name":            "test",
					"desired_version": "7.4.5",
					"running_version": "7.3.69",
					"monitor_version": "7.4.5",
				}))
			},
		)

		It(
			"should use the desired version as running version if the running version is not known",
			func() {
				cluster.Status.RunningVersion = ""
				labels, _ := getVersionInfoLabels(cluster)
				Expect(labels).To(HaveKeyWithValue("running_version", "7.4.5"))
			},
		)

		It("should use the pinned version as monitor version", func() {
			cluster.Spec.MainContainer.MonitorVersion = new("7.3.69")
			labels, _ := getVersionInfoLabels(cluster)
			Expect(labels).To(HaveKeyWithValue("monitor_version", "7.3.69"))
		})
	})

	Context("Collecting the processGroup metrics", func() {
		It("generate the process class metrics", func() {
			stats, removals, exclusions := getProcessGroupMetrics(cluster)
			Expect(len(stats)).To(BeNumerically("==", 3))
			Expect(
				len(stats[fdbv1beta2.ProcessClassStorage]),
			).To(BeNumerically("==", len(fdbv1beta2.AllProcessGroupConditionTypes())))
			Expect(
				len(stats[fdbv1beta2.ProcessClassStorage]),
			).To(BeNumerically("==", len(fdbv1beta2.AllProcessGroupConditionTypes())))
			Expect(
				stats[fdbv1beta2.ProcessClassStorage][fdbv1beta2.ReadyCondition],
			).To(BeNumerically("==", 2))
			Expect(
				stats[fdbv1beta2.ProcessClassLog][fdbv1beta2.ReadyCondition],
			).To(BeNumerically("==", 0))
			Expect(
				stats[fdbv1beta2.ProcessClassLog][fdbv1beta2.MissingProcesses],
			).To(BeNumerically("==", 1))
			Expect(
				stats[fdbv1beta2.ProcessClassStateless][fdbv1beta2.ReadyCondition],
			).To(BeNumerically("==", 1))
			Expect(removals[fdbv1beta2.ProcessClassStorage]).To(BeNumerically("==", 1))
			Expect(exclusions[fdbv1beta2.ProcessClassStorage]).To(BeNumerically("==", 0))
			Expect(removals[fdbv1beta2.ProcessClassStateless]).To(BeNumerically("==", 1))
			Expect(exclusions[fdbv1beta2.ProcessClassStateless]).To(BeNumerically("==", 1))
		})
	})
})
