package controllers

import (
	"context"
	"github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	descClusterDefaultLabels = []string{"namespace", "name"}

	descClusterCreated = prometheus.NewDesc(
		"fdb_cluster_created_time",
		"Creation time in unix timestamp for Fdb Cluster.",
		descClusterDefaultLabels,
		nil,
	)
	descClusterStatus = prometheus.NewDesc(
		"fdb_cluster_status",
		"status of the Fdb Cluster.",
		append(descClusterDefaultLabels, "status_type"),
		nil,
	)
)

type fdbClusterCollector struct {
	reconciler *FoundationDBClusterReconciler
}

func NewFDBClusterCollector(reconciler *FoundationDBClusterReconciler) *fdbClusterCollector {
	return &fdbClusterCollector{reconciler: reconciler}
}

// Describe implements the prometheus.Collector interface
func (c *fdbClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descClusterCreated
	ch <- descClusterStatus
}

// Collect implements the prometheus.Collector interface
func (c *fdbClusterCollector) Collect(ch chan<- prometheus.Metric) {
	clusters := &v1beta1.FoundationDBClusterList{}
	err := c.reconciler.List(context.Background(), clusters)
	if err != nil {
		return
	}
	for _, cluster := range clusters.Items {
		collectMetrics(ch, &cluster)
	}
}

func collectMetrics(ch chan<- prometheus.Metric, cluster *v1beta1.FoundationDBCluster) {

	addConstMetric := func(desc *prometheus.Desc, t prometheus.ValueType, v float64, lv ...string) {
		lv = append([]string{cluster.Namespace, cluster.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, t, v, lv...)
	}
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		addConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	addGauge(descClusterCreated, float64(cluster.CreationTimestamp.Unix()))
	addGauge(descClusterStatus, boolFloat64(cluster.Status.Health.Healthy == true), "health")
	addGauge(descClusterStatus, boolFloat64(cluster.Status.Health.Available == true), "available")
	addGauge(descClusterStatus, boolFloat64(cluster.Status.Health.FullReplication == true), "replication")
	addGauge(descClusterStatus, float64(cluster.Status.Health.DataMovementPriority), "datamovementpriority")
}

func InitCustomMetrics(reconciler *FoundationDBClusterReconciler) {
	metrics.Registry.MustRegister(
		NewFDBClusterCollector(reconciler),
	)
}

func boolFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
