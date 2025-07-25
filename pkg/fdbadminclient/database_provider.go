/*
 * database_provider.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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

package fdbadminclient

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DatabaseClientProvider provides an abstraction for creating clients that
// communicate with the database.
type DatabaseClientProvider interface {
	// GetLockClient generates a client for working with locks through the database.
	GetLockClient(cluster *fdbv1beta2.FoundationDBCluster) (LockClient, error)

	// GetLockClientWithLogger generates a client for working with locks through the database.
	// The provided logger will be used as logger for the LockClient.
	GetLockClientWithLogger(
		cluster *fdbv1beta2.FoundationDBCluster,
		logger logr.Logger,
	) (LockClient, error)

	// GetAdminClient generates a client for performing administrative actions
	// against the database.
	GetAdminClient(
		cluster *fdbv1beta2.FoundationDBCluster,
		kubernetesClient client.Client,
	) (AdminClient, error)

	// GetAdminClientWithLogger generates a client for performing administrative actions
	// against the database. The provided logger will be used as logger for the AdminClient.
	GetAdminClientWithLogger(
		cluster *fdbv1beta2.FoundationDBCluster,
		kubernetesClient client.Client,
		logger logr.Logger,
	) (AdminClient, error)
}
