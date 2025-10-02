/*
 * metrics.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2025 Apple Inc. and the FoundationDB project authors
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	descClusterDefaultLabels = []string{"namespace", "name"}

	// CoordinatorChangesCounter counts the total number of coordinator changes.
	CoordinatorChangesCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fdb_operator_coordinator_changes_total",
			Help: "Total number of coordinator changes performed on the cluster",
		},
		descClusterDefaultLabels,
	)

	// DescClusterCreated stores the creation time of the cluster.
	DescClusterCreated = prometheus.NewDesc(
		"fdb_operator_cluster_created_time",
		"Creation time in unix timestamp for Fdb Cluster.",
		descClusterDefaultLabels,
		nil,
	)

	// DescClusterStatus stores status metrics.
	DescClusterStatus = prometheus.NewDesc(
		"fdb_operator_cluster_status",
		"status of the Fdb Cluster.",
		append(descClusterDefaultLabels, "status_type"),
		nil,
	)

	descClusterLastReconciled = prometheus.NewDesc(
		"fdb_operator_cluster_latest_reconciled_status",
		"the latest generation that was reconciled.",
		descClusterDefaultLabels,
		nil,
	)

	descProcessGroupsToRemove = prometheus.NewDesc(
		"fdb_operator_process_groups_to_remove_total",
		"the count of process groups that should be removed from the cluster.",
		descClusterDefaultLabels,
		nil,
	)

	descProcessGroupsToRemoveWithoutExclusion = prometheus.NewDesc(
		"fdb_operator_process_group_to_remove_without_exclusion_total",
		"the count of process groups that should be removed from the cluster without excluding.",
		descClusterDefaultLabels,
		nil,
	)

	descClusterReconciled = prometheus.NewDesc(
		"fdb_operator_cluster_reconciled_status",
		"status if the Fdb Cluster is reconciled.",
		descClusterDefaultLabels,
		nil,
	)

	descProcessGroupStatus = prometheus.NewDesc(
		"fdb_operator_process_group_total",
		"the count of Fdb process groups in a specific condition.",
		append(descClusterDefaultLabels, "process_class", "condition"),
		nil,
	)

	descProcessGroupMarkedRemoval = prometheus.NewDesc(
		"fdb_operator_process_group_marked_removal",
		"the count of Fdb process groups that are marked for removal.",
		append(descClusterDefaultLabels, "process_class"),
		nil,
	)

	descProcessGroupMarkedExcluded = prometheus.NewDesc(
		"fdb_operator_process_group_marked_excluded",
		"the count of Fdb process groups that are marked as excluded.",
		append(descClusterDefaultLabels, "process_class"),
		nil,
	)

	desDesiredProcessGroups = prometheus.NewDesc(
		"fdb_operator_desired_process_group_total",
		"the count of the desired Fdb process groups",
		append(descClusterDefaultLabels, "process_class"),
		nil,
	)
)

// CollectMetrics will collect the metrics for the provided fdbv1beta2.FoundationDBCluster and update all related
// metrics.
func CollectMetrics(ch chan<- prometheus.Metric, cluster *fdbv1beta2.FoundationDBCluster) {
	addConstMetric := func(desc *prometheus.Desc, t prometheus.ValueType, v float64, lv ...string) {
		lv = append([]string{cluster.Namespace, cluster.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, t, v, lv...)
	}
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		addConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	// These are the correct metrics with the prefix "fdb_operator"
	addGauge(DescClusterCreated, float64(cluster.CreationTimestamp.Unix()))
	addGauge(DescClusterStatus, boolFloat64(cluster.Status.Health.Healthy), "health")
	addGauge(DescClusterStatus, boolFloat64(cluster.Status.Health.Available), "available")
	addGauge(DescClusterStatus, boolFloat64(cluster.Status.Health.FullReplication), "replication")
	addGauge(
		DescClusterStatus,
		float64(cluster.Status.Health.DataMovementPriority),
		"datamovementpriority",
	)
	addGauge(descClusterLastReconciled, float64(cluster.Status.Generations.Reconciled))
	addGauge(
		descClusterReconciled,
		boolFloat64(cluster.ObjectMeta.Generation == cluster.Status.Generations.Reconciled),
	)
	addGauge(descProcessGroupsToRemove, float64(len(cluster.Spec.ProcessGroupsToRemove)))
	addGauge(
		descProcessGroupsToRemoveWithoutExclusion,
		float64(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)),
	)

	// Calculate the process group metrics
	conditionMap, removals, exclusions := getProcessGroupMetrics(cluster)

	for pClass, conditionMap := range conditionMap {
		for condition, count := range conditionMap {
			addGauge(descProcessGroupStatus, float64(count), string(pClass), string(condition))
		}

		addGauge(descProcessGroupMarkedRemoval, float64(removals[pClass]), string(pClass))
		addGauge(descProcessGroupMarkedExcluded, float64(exclusions[pClass]), string(pClass))
	}

	counts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return
	}

	for processCount, count := range counts.Map() {
		addGauge(desDesiredProcessGroups, float64(count), string(processCount))
	}
}

func getProcessGroupMetrics(
	cluster *fdbv1beta2.FoundationDBCluster,
) (map[fdbv1beta2.ProcessClass]map[fdbv1beta2.ProcessGroupConditionType]int, map[fdbv1beta2.ProcessClass]int, map[fdbv1beta2.ProcessClass]int) {
	metricMap := map[fdbv1beta2.ProcessClass]map[fdbv1beta2.ProcessGroupConditionType]int{}
	removals := map[fdbv1beta2.ProcessClass]int{}
	exclusions := map[fdbv1beta2.ProcessClass]int{}

	for _, processGroup := range cluster.Status.ProcessGroups {
		if _, exits := metricMap[processGroup.ProcessClass]; !exits {
			metricMap[processGroup.ProcessClass] = map[fdbv1beta2.ProcessGroupConditionType]int{}
			removals[processGroup.ProcessClass] = 0
			exclusions[processGroup.ProcessClass] = 0
		}

		if processGroup.IsMarkedForRemoval() {
			removals[processGroup.ProcessClass]++
		}

		if processGroup.IsExcluded() {
			exclusions[processGroup.ProcessClass]++
		}

		if len(processGroup.ProcessGroupConditions) == 0 {
			metricMap[processGroup.ProcessClass][fdbv1beta2.ReadyCondition]++
		}

		for _, condition := range processGroup.ProcessGroupConditions {
			metricMap[processGroup.ProcessClass][condition.ProcessGroupConditionType]++
		}
	}

	// Ensure that all conditions are present
	for pClass := range metricMap {
		if _, exits := metricMap[pClass]; !exits {
			metricMap[pClass] = map[fdbv1beta2.ProcessGroupConditionType]int{}
		}

		for _, condition := range fdbv1beta2.AllProcessGroupConditionTypes() {
			if _, exists := metricMap[pClass][condition]; !exists {
				metricMap[pClass][condition] = 0
			}
		}
	}

	return metricMap, removals, exclusions
}

func boolFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
