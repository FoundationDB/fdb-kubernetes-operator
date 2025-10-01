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

package controllers

import (
	"context"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	internalMetrics "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type fdbClusterCollector struct {
	reconciler *FoundationDBClusterReconciler
}

func newFDBClusterCollector(reconciler *FoundationDBClusterReconciler) *fdbClusterCollector {
	return &fdbClusterCollector{reconciler: reconciler}
}

// Describe implements the prometheus.Collector interface
func (c *fdbClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- internalMetrics.DescClusterCreated
	ch <- internalMetrics.DescClusterStatus
}

// Collect implements the prometheus.Collector interface
func (c *fdbClusterCollector) Collect(ch chan<- prometheus.Metric) {
	clusters := &fdbv1beta2.FoundationDBClusterList{}
	err := c.reconciler.List(context.Background(), clusters)
	if err != nil {
		return
	}
	for _, cluster := range clusters.Items {
		internalMetrics.CollectMetrics(ch, &cluster)
	}
}

// InitCustomMetrics initializes the metrics collectors for the operator.
func InitCustomMetrics(reconciler *FoundationDBClusterReconciler) {
	metrics.Registry.MustRegister(
		newFDBClusterCollector(reconciler),
		internalMetrics.CoordinatorChangesCounter,
	)
}
