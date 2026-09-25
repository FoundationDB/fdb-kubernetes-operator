/*
 * service_helper.go
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

package internal

import (
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	if cluster.IsPodIPFamily6() {
		service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv6Protocol}
	}

	return service
}

// GetGRPCService builds a service for a GRPC interface for a cluster.
func GetGRPCService(
	cluster *fdbv1beta2.FoundationDBCluster,
) (*corev1.Service, error) {
	service := &corev1.Service{}
	if cluster.Spec.Routing.GRPCService != nil {
		*service = *cluster.Spec.Routing.GRPCService
	}
	service.ObjectMeta = GetObjectMetadata(
		cluster,
		&service.ObjectMeta,
		"",
		"",
	)
	if service.ObjectMeta.Name == "" {
		service.ObjectMeta.Name = fmt.Sprintf("%s-grpc", cluster.Name)
	}
	service.ObjectMeta.OwnerReferences = append(
		service.ObjectMeta.OwnerReferences,
		BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)...)

	if cluster.IsPodIPFamily6() && service.Spec.IPFamilies == nil {
		service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv6Protocol}
	}
	if service.Spec.Type == "" {
		service.Spec.Type = corev1.ServiceTypeClusterIP
	}
	if service.Spec.Ports == nil {
		service.Spec.Ports = []corev1.ServicePort{
			{Port: cluster.GetGRPCPort(), TargetPort: intstr.FromInt32(cluster.GetGRPCPort())},
		}
	}
	service.Spec.PublishNotReadyAddresses = true
	if service.Spec.Selector == nil {
		service.Spec.Selector = GetPodMatchLabels(
			cluster,
			"",
			"",
		)
	}
	return service, nil
}
