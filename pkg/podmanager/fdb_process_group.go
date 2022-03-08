/*
 * fdb_process_group.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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

package podmanager

import (
	"fmt"
	"regexp"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	corev1 "k8s.io/api/core/v1"
)

var processIDRegex = regexp.MustCompile(`^([\w-]+-\d)-\d$`)

// ParseProcessGroupID extracts the components of an process group ID.
func ParseProcessGroupID(id string) (fdbv1beta2.ProcessClass, int, error) {
	return internal.ParseProcessGroupID(id)
}

// GetProcessGroupIDFromProcessID returns the process group ID for the process ID
func GetProcessGroupIDFromProcessID(id string) string {
	result := processIDRegex.FindStringSubmatch(id)
	if result == nil {
		// In this case we assume that process group ID == process ID
		return id
	}

	return result[1]
}

// GetProcessGroupID returns the process group ID from the Pods metadata
func GetProcessGroupID(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}

	return internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta)
}

// GetProcessClass fetches the process class from a Pod's metadata.
func GetProcessClass(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (fdbv1beta2.ProcessClass, error) {
	if pod == nil {
		return "", fmt.Errorf("failed to fetch process class from nil Pod")
	}

	return internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta), nil
}

// GetPublicIPSource determines how a Pod has gotten its public IP.
func GetPublicIPSource(pod *corev1.Pod) (fdbv1beta2.PublicIPSource, error) {
	return internal.GetPublicIPSource(pod)
}

// GetPublicIPs returns the public IP of a pod.
func GetPublicIPs(pod *corev1.Pod) []string {
	if pod == nil {
		return []string{}
	}

	source := pod.ObjectMeta.Annotations[fdbv1beta2.PublicIPSourceAnnotation]
	if source == "" || source == string(fdbv1beta2.PublicIPSourcePod) {
		return internal.GetPublicIPsForPod(pod)
	}

	return []string{pod.ObjectMeta.Annotations[fdbv1beta2.PublicIPAnnotation]}
}
