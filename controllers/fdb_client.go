/*
 * fdb_client.go
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

package controllers

import (
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DatabaseClientProvider provides an abstraction for creating clients that
// communicate with the database.
type DatabaseClientProvider interface {
	// GetLockClient generates a client for working with locks through the database.
	GetLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error)

	// GetAdminClient generates a client for performing administrative actions
	// against the database.
	GetAdminClient(cluster *fdbtypes.FoundationDBCluster, kubernetesClient client.Client) (AdminClient, error)

	// CleanUpCache removes the cache entry for a cluster.
	CleanUpCache(namespace string, name string)
}

type legacyDatabaseClientProvider struct {
	AdminClientProvider func(cluster *fdbtypes.FoundationDBCluster, kubernetesClient client.Client) (AdminClient, error)
	LockClientProvider  LockClientProvider
}

// GetLockClient generates a client for working with locks through the database.
func (p legacyDatabaseClientProvider) GetLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	return p.LockClientProvider(cluster)
}

// GetAdminClient generates a client for performing administrative actions
// against the database.
func (p legacyDatabaseClientProvider) GetAdminClient(cluster *fdbtypes.FoundationDBCluster, kubernetesClient client.Client) (AdminClient, error) {
	return p.AdminClientProvider(cluster, kubernetesClient)
}

// CleanUpCache removes the cache entry for a cluster.
func (p legacyDatabaseClientProvider) CleanUpCache(namespace string, name string) {
}

type mockDatabaseClientProvider struct{}

// GetLockClient generates a client for working with locks through the database.
func (p mockDatabaseClientProvider) GetLockClient(cluster *fdbtypes.FoundationDBCluster) (LockClient, error) {
	return NewMockLockClient(cluster)
}

// GetAdminClient generates a client for performing administrative actions
// against the database.
func (p mockDatabaseClientProvider) GetAdminClient(cluster *fdbtypes.FoundationDBCluster, kubernetesClient client.Client) (AdminClient, error) {
	return NewMockAdminClient(cluster, kubernetesClient)
}

// CleanUpCache removes the cache entry for a cluster.
func (p mockDatabaseClientProvider) CleanUpCache(namespace string, name string) {

}
