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
	descClusterStatus_healthy = prometheus.NewDesc(
		"fdb_cluster_status_healthy",
		"Health of the Fdb Cluster.",
		append(descClusterDefaultLabels, "heath"),
		nil,
	)
	descClusterStatus_availability = prometheus.NewDesc(
		"fdb_cluster_status_available",
		"Availabilty of the Fdb Cluster for read/write operations",
		append(descClusterDefaultLabels, "available"),
		nil,
	)
	descClusterStatus_replication = prometheus.NewDesc(
		"fdb_cluster_status_replication",
		"Replication status as per defined replication policy for the Fdb Cluster",
		append(descClusterDefaultLabels, "replication"),
		nil,
	)
	descClusterStatus_dataMovementPri = prometheus.NewDesc(
		"fdb_cluster_status_data_movement_priority",
		"Data Movement Priority for the Fdb Cluster",
		append(descClusterDefaultLabels, "datamovementpriority"),
		nil,
	)
)

type fdbClusterCollector struct {
	reconciler *FoundationDBClusterReconciler
}

func NewFDBClusterCollector(reconciler *FoundationDBClusterReconciler) *fdbClusterCollector {
	return &fdbClusterCollector{reconciler:reconciler}
}

// Describe implements the prometheus.Collector interface
func (c *fdbClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descClusterCreated
	ch <- descClusterStatus_healthy
	ch <- descClusterStatus_availability
	ch <- descClusterStatus_replication
	ch <- descClusterStatus_dataMovementPri
}

// Collect implements the prometheus.Collector interface
func (c *fdbClusterCollector) Collect(ch chan<- prometheus.Metric) {
	clusters := &v1beta1.FoundationDBClusterList{}
	err := c.reconciler.List(context.Background(),clusters)
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
	addGauge(descClusterStatus_healthy, boolFloat64(cluster.Status.Health.Healthy==true))
	addGauge(descClusterStatus_availability, boolFloat64(cluster.Status.Health.Available==true))
	addGauge(descClusterStatus_replication, float64(cluster.Status.Health.DataMovementPriority))
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