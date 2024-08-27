/*
 * database_provider.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DatabaseClientProvider is a DatabaseClientProvider thar provides mocked clients for testing.
type DatabaseClientProvider struct{}

// GetLockClient generates a client for working with locks through the database.
func (p DatabaseClientProvider) GetLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return NewMockLockClient(cluster)
}

// GetLockClientWithLogger generates a client for working with locks through the database.
// The provided logger will be used as logger for the LockClient.
func (p DatabaseClientProvider) GetLockClientWithLogger(cluster *fdbv1beta2.FoundationDBCluster, _ logr.Logger) (fdbadminclient.LockClient, error) {
	return NewMockLockClient(cluster)
}

// GetAdminClient generates a client for performing administrative actions
// against the database.
func (p DatabaseClientProvider) GetAdminClient(cluster *fdbv1beta2.FoundationDBCluster, kubernetesClient client.Client) (fdbadminclient.AdminClient, error) {
	return NewMockAdminClient(cluster, kubernetesClient)
}

// GetAdminClientWithLogger generates a client for performing administrative actions
// against the database.
func (p DatabaseClientProvider) GetAdminClientWithLogger(cluster *fdbv1beta2.FoundationDBCluster, kubernetesClient client.Client, _ logr.Logger) (fdbadminclient.AdminClient, error) {
	return NewMockAdminClient(cluster, kubernetesClient)
}
