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
	"context"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	descClusterDefaultLabels = []string{"namespace", "name"}

	descClusterCreated = prometheus.NewDesc(
		"fdb_operator_cluster_created_time",
		"Creation time in unix timestamp for Fdb Cluster.",
		descClusterDefaultLabels,
		nil,
	)

	descClusterStatus = prometheus.NewDesc(
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

	descInstancesToRemove = prometheus.NewDesc(
		"fdb_operator_instances_to_remove_total",
		"the count of instances that should be removed from the cluster. Deprecated, use fdb_operator_process_groups_to_remove_total instead",
		descClusterDefaultLabels,
		nil,
	)

	descInstancesToRemoveWithoutExclusion = prometheus.NewDesc(
		"fdb_operator_instances_to_remove_without_exclusion_total",
		"the count of instances that should be removed from the cluster without excluding. Deprecated use fdb_operator_process_groups_to_remove_total instead",
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
		"the count of rocess groups that should be removed from the cluster without excluding.",
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
)

type fdbClusterCollector struct {
	reconciler *FoundationDBClusterReconciler
}

func newFDBClusterCollector(reconciler *FoundationDBClusterReconciler) *fdbClusterCollector {
	return &fdbClusterCollector{reconciler: reconciler}
}

// Describe implements the prometheus.Collector interface
func (c *fdbClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descClusterCreated
	ch <- descClusterStatus
}

// Collect implements the prometheus.Collector interface
func (c *fdbClusterCollector) Collect(ch chan<- prometheus.Metric) {
	clusters := &fdbtypes.FoundationDBClusterList{}
	err := c.reconciler.List(context.Background(), clusters)
	if err != nil {
		return
	}
	for _, cluster := range clusters.Items {
		collectMetrics(ch, &cluster)
	}
}

func collectMetrics(ch chan<- prometheus.Metric, cluster *fdbtypes.FoundationDBCluster) {
	addConstMetric := func(desc *prometheus.Desc, t prometheus.ValueType, v float64, lv ...string) {
		lv = append([]string{cluster.Namespace, cluster.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, t, v, lv...)
	}
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		addConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	// These are the correct metrics with the prefix "fdb_operator"
	addGauge(descClusterCreated, float64(cluster.CreationTimestamp.Unix()))
	addGauge(descClusterStatus, boolFloat64(cluster.Status.Health.Healthy), "health")
	addGauge(descClusterStatus, boolFloat64(cluster.Status.Health.Available), "available")
	addGauge(descClusterStatus, boolFloat64(cluster.Status.Health.FullReplication), "replication")
	addGauge(descClusterStatus, float64(cluster.Status.Health.DataMovementPriority), "datamovementpriority")
	addGauge(descClusterLastReconciled, float64(cluster.Status.Generations.Reconciled))
	addGauge(descClusterReconciled, boolFloat64(cluster.ObjectMeta.Generation == cluster.Status.Generations.Reconciled))
	addGauge(descInstancesToRemove, float64(len(cluster.Spec.InstancesToRemove)))
	addGauge(descInstancesToRemoveWithoutExclusion, float64(len(cluster.Spec.InstancesToRemoveWithoutExclusion)))
	addGauge(descProcessGroupsToRemove, float64(len(cluster.Spec.ProcessGroupsToRemove)))
	addGauge(descProcessGroupsToRemoveWithoutExclusion, float64(len(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion)))

	// Calculate the process group metrics
	conditionMap, removals, exclusions := getProcessGroupMetrics(cluster)

	for pclass, conditionMap := range conditionMap {
		for condition, count := range conditionMap {
			addGauge(descProcessGroupStatus, float64(count), string(pclass), string(condition))
		}

		addGauge(descProcessGroupMarkedRemoval, float64(removals[pclass]), string(pclass))
		addGauge(descProcessGroupMarkedExcluded, float64(exclusions[pclass]), string(pclass))
	}
}

func getProcessGroupMetrics(cluster *fdbtypes.FoundationDBCluster) (map[fdb.ProcessClass]map[fdbtypes.ProcessGroupConditionType]int, map[fdb.ProcessClass]int, map[fdb.ProcessClass]int) {
	metricMap := map[fdb.ProcessClass]map[fdbtypes.ProcessGroupConditionType]int{}
	removals := map[fdb.ProcessClass]int{}
	exclusions := map[fdb.ProcessClass]int{}

	for _, processGroup := range cluster.Status.ProcessGroups {
		if _, exits := metricMap[processGroup.ProcessClass]; !exits {
			metricMap[processGroup.ProcessClass] = map[fdbtypes.ProcessGroupConditionType]int{}
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
			metricMap[processGroup.ProcessClass][fdbtypes.ReadyCondition]++
		}

		for _, condition := range processGroup.ProcessGroupConditions {
			metricMap[processGroup.ProcessClass][condition.ProcessGroupConditionType]++
		}
	}

	// Ensure that all conditions are present
	for pClass := range metricMap {
		if _, exits := metricMap[pClass]; !exits {
			metricMap[pClass] = map[fdbtypes.ProcessGroupConditionType]int{}
		}

		for _, condition := range fdbtypes.AllProcessGroupConditionTypes() {
			if _, exists := metricMap[pClass][condition]; !exists {
				metricMap[pClass][condition] = 0
			}
		}
	}

	return metricMap, removals, exclusions
}

// InitCustomMetrics initializes the metrics collectors for the operator.
func InitCustomMetrics(reconciler *FoundationDBClusterReconciler) {
	metrics.Registry.MustRegister(
		newFDBClusterCollector(reconciler),
	)
}

func boolFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
