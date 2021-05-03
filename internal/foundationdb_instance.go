/*
 * foundationdb_instance.go
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
	"fmt"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// FdbInstance represents an instance of FDB that has been configured in
// Kubernetes.
type FdbInstance struct {
	Metadata *metav1.ObjectMeta
	Pod      *corev1.Pod
}

// NewFdbInstance create a new FdbInstance based on the given Pod
func NewFdbInstance(pod corev1.Pod) FdbInstance {
	return FdbInstance{Metadata: &pod.ObjectMeta, Pod: &pod}
}

// NamespacedName gets the name of an instance along with its namespace
func (instance FdbInstance) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: instance.Metadata.Namespace, Name: instance.Metadata.Name}
}

// GetInstanceID fetches the instance ID from an instance's metadata.
func (instance FdbInstance) GetInstanceID() string {
	return GetInstanceIDFromMeta(*instance.Metadata)
}

// GetInstanceIDFromMeta fetches the instance ID from an object's metadata.
func GetInstanceIDFromMeta(metadata metav1.ObjectMeta) string {
	return metadata.Labels[fdbtypes.FDBInstanceIDLabel]
}

// GetProcessClass fetches the process class from an instance's metadata.
func (instance FdbInstance) GetProcessClass() fdbtypes.ProcessClass {
	return GetProcessClassFromMeta(*instance.Metadata)
}

// GetProcessClassFromMeta fetches the process class from an object's metadata.
func GetProcessClassFromMeta(metadata metav1.ObjectMeta) fdbtypes.ProcessClass {
	return ProcessClassFromLabels(metadata.Labels)
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
		// TODO for dual-stack support return PodIPs
		return []string{instance.Pod.Status.PodIP}
	}

	return []string{instance.Pod.ObjectMeta.Annotations[fdbtypes.PublicIPAnnotation]}
}

// GetProcessID fetches the instance ID from an instance's metadata.
func (instance FdbInstance) GetProcessID(processNumber int) string {
	return fmt.Sprintf("%s-%d", GetInstanceIDFromMeta(*instance.Metadata), processNumber)
}

// ProcessClassFromLabels extracts the ProcessClass label from the metav1.ObjectMeta.Labels map
func ProcessClassFromLabels(labels map[string]string) fdbtypes.ProcessClass {
	return fdbtypes.ProcessClass(labels[fdbtypes.FDBProcessClassLabel])
}
