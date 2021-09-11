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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var instanceIDRegex = regexp.MustCompile(`^([\w-]+)-(\d+)`)
var processIDRegex = regexp.MustCompile(`^([\w-]+-\d)-\d$`)

// FdbInstance represents an instance of FDB that has been configured in
// Kubernetes.
type FdbInstance struct {
	Metadata *metav1.ObjectMeta
	Pod      *corev1.Pod
}

func newFdbInstance(pod corev1.Pod) FdbInstance {
	return FdbInstance{Metadata: &pod.ObjectMeta, Pod: &pod}
}

// NamespacedName gets the name of an instance along with its namespace
func (instance FdbInstance) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: instance.Metadata.Namespace, Name: instance.Metadata.Name}
}

// GetInstanceID fetches the instance ID from an instance's metadata.
func (instance FdbInstance) GetInstanceID() string {
	return internal.GetInstanceIDFromMeta(*instance.Metadata)
}

// GetProcessClass fetches the process class from an instance's metadata.
func (instance FdbInstance) GetProcessClass() fdbtypes.ProcessClass {
	return internal.GetProcessClassFromMeta(*instance.Metadata)
}

// GetPublicIPSource determines how an instance has gotten its public IP.
func (instance FdbInstance) GetPublicIPSource() fdbtypes.PublicIPSource {
	source := instance.Metadata.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" {
		return fdbtypes.PublicIPSourcePod
	}
	return fdbtypes.PublicIPSource(source)
}

// GetPublicIPs returns the public IP of an instance.
func (instance FdbInstance) GetPublicIPs() []string {
	if instance.Pod == nil {
		return []string{}
	}

	source := instance.Metadata.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" || source == string(fdbtypes.PublicIPSourcePod) {
		return internal.GetPublicIPsForPod(instance.Pod)
	}

	return []string{instance.Pod.ObjectMeta.Annotations[fdbtypes.PublicIPAnnotation]}
}

// GetProcessID fetches the instance ID from an instance's metadata.
func (instance FdbInstance) GetProcessID(processNumber int) string {
	return fmt.Sprintf("%s-%d", internal.GetInstanceIDFromMeta(*instance.Metadata), processNumber)
}

// ParseInstanceID extracts the components of an instance ID.
func ParseInstanceID(id string) (fdbtypes.ProcessClass, int, error) {
	result := instanceIDRegex.FindStringSubmatch(id)
	if result == nil {
		return "", 0, fmt.Errorf("could not parse instance ID %s", id)
	}
	prefix := result[1]
	number, err := strconv.Atoi(result[2])
	if err != nil {
		return "", 0, err
	}
	return fdbtypes.ProcessClass(prefix), number, nil
}

// GetInstanceIDFromProcessID returns the instance ID for the process ID
func GetInstanceIDFromProcessID(id string) string {
	result := processIDRegex.FindStringSubmatch(id)
	if result == nil {
		// In this case we assume that instance ID == process ID
		return id
	}

	return result[1]
}

// GetInstanceID returns the instance ID from the Pods metadata
func GetInstanceID(pod *corev1.Pod) string {
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
