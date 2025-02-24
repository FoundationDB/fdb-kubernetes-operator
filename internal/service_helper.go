/*
 * service_helper.go
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

package internal

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// GetHeadlessService builds a headless service for a FoundationDB cluster.
func GetHeadlessService(cluster *fdbv1beta2.FoundationDBCluster) *corev1.Service {
	if !cluster.NeedsHeadlessService() {
		return nil
	}

	service := &corev1.Service{
		ObjectMeta: GetObjectMetadata(cluster, nil, "", ""),
	}
	service.ObjectMeta.Name = cluster.ObjectMeta.Name
	service.Spec.ClusterIP = "None"
	service.Spec.Selector = cluster.GetMatchLabels()

	if cluster.Spec.Routing.PodIPFamily != nil && *cluster.Spec.Routing.PodIPFamily == 6 {
		service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv6Protocol}
	}

	return service
}
