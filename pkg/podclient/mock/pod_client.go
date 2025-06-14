/*
 * pod_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package mock

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podclient"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// FdbPodClient provides a mock connection to a pod
type FdbPodClient struct {
	Cluster *fdbv1beta2.FoundationDBCluster
	Pod     *corev1.Pod
	logger  logr.Logger
}

// NewMockFdbPodClient builds a mock client for working with an FDB pod
func NewMockFdbPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (podclient.FdbPodClient, error) {
	return &FdbPodClient{Cluster: cluster, Pod: pod, logger: logr.New(log.NullLogSink{})}, nil
}

// UpdateFile checks if a file is up-to-date and tries to update it.
func (client *FdbPodClient) UpdateFile(_ string, _ string) (bool, error) {
	return true, nil
}

// IsPresent checks whether a file in the sidecar is present.
func (client *FdbPodClient) IsPresent(_ string) (bool, error) {
	return true, nil
}

// GetVariableSubstitutions gets the current keys and values that this
// process group will substitute into its monitor conf.
func (client *FdbPodClient) GetVariableSubstitutions() (map[string]string, error) {
	return internal.GetSubstitutionsFromClusterAndPod(client.logger, client.Cluster, client.Pod)
}
