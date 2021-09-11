/*
 * fdb_instance.go
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

package controllers

import (
	"fmt"
	"regexp"
	"strconv"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	corev1 "k8s.io/api/core/v1"
)

var processGroupIDRegex = regexp.MustCompile(`^([\w-]+)-(\d+)`)
var processIDRegex = regexp.MustCompile(`^([\w-]+-\d)-\d$`)

// ParseProcessGroupID extracts the components of an process group ID.
func ParseProcessGroupID(id string) (fdbtypes.ProcessClass, int, error) {
	result := processGroupIDRegex.FindStringSubmatch(id)
	if result == nil {
		return "", 0, fmt.Errorf("could not parse process group ID %s", id)
	}
	prefix := result[1]
	number, err := strconv.Atoi(result[2])
	if err != nil {
		return "", 0, err
	}
	return fdbtypes.ProcessClass(prefix), number, nil
}

// GetInstanceIDFromProcessID returns the process group ID for the process ID
func GetInstanceIDFromProcessID(id string) string {
	result := processIDRegex.FindStringSubmatch(id)
	if result == nil {
		// In this case we assume that instance ID == process ID
		return id
	}

	return result[1]
}

// GetProcessGroupID returns the process group ID from the Pods metadata
func GetProcessGroupID(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}

	return internal.GetInstanceIDFromMeta(pod.ObjectMeta)
}

// GetProcessClass fetches the process class from a Pod's metadata.
func GetProcessClass(pod *corev1.Pod) (fdbtypes.ProcessClass, error) {
	if pod == nil {
		return "", fmt.Errorf("failed to fetch process class from nil Pod")
	}

	return internal.GetProcessClassFromMeta(pod.ObjectMeta), nil
}

// GetPublicIPSource determines how a Pod has gotten its public IP.
func GetPublicIPSource(pod *corev1.Pod) (fdbtypes.PublicIPSource, error) {
	if pod == nil {
		return "", fmt.Errorf("failed to fetch public IP source from nil Pod")
	}

	source := pod.ObjectMeta.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" {
		return fdbtypes.PublicIPSourcePod, nil
	}
	return fdbtypes.PublicIPSource(source), nil
}

// GetPublicIPs returns the public IP of a pod.
func GetPublicIPs(pod *corev1.Pod) []string {
	if pod == nil {
		return []string{}
	}

	source := pod.ObjectMeta.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" || source == string(fdbtypes.PublicIPSourcePod) {
		return internal.GetPublicIPsForPod(pod)
	}

	return []string{pod.ObjectMeta.Annotations[fdbtypes.PublicIPAnnotation]}
}
