/*
 * common.go
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

package fdbclient

import (
	"fmt"
	"os"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultTransactionTimeout int64 = 5000
)

// DefaultCLITimeout is the default timeout for CLI commands.
var DefaultCLITimeout = 10

// getFDBDatabase opens an FDB database. The result will be cached for
// subsequent calls, based on the cluster namespace and name.
func getFDBDatabase(cluster *fdbv1beta2.FoundationDBCluster) (fdb.Database, error) {
	clusterFile, err := os.CreateTemp("", "")
	if err != nil {
		return fdb.Database{}, err
	}

	defer clusterFile.Close()
	clusterFilePath := clusterFile.Name()

	_, err = clusterFile.WriteString(cluster.Status.ConnectionString)
	if err != nil {
		return fdb.Database{}, err
	}
	err = clusterFile.Close()
	if err != nil {
		return fdb.Database{}, err
	}

	database, err := fdb.OpenDatabase(clusterFilePath)
	if err != nil {
		return fdb.Database{}, err
	}

	err = database.Options().SetTransactionTimeout(defaultTransactionTimeout)
	if err != nil {
		return fdb.Database{}, err
	}

	return database, nil
}

func getValueFromDBUsingKey(cluster *fdbv1beta2.FoundationDBCluster, fdbKey string, extraTimeout int64) ([]byte, error) {
	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}
	result, err := database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		err = transaction.Options().SetTimeout(int64(DefaultCLITimeout) * extraTimeout)
		if err != nil {
			return nil, err
		}

		rawResult := transaction.Get(fdb.Key(fdbKey)).MustGet()
		if len(rawResult) == 0 {
			return nil, err
		}

		return rawResult, err
	})

	if err != nil {
		return nil, err
	}

	byteResult, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("could not cast result into byte slice")
	}
	return byteResult, nil
}

// getConnectionStringFromDB gets the database's connection string directly from the system key
func getConnectionStringFromDB(cluster *fdbv1beta2.FoundationDBCluster) ([]byte, error) {
	return getValueFromDBUsingKey(cluster, "\xff\xff/connection_string", 1)
}

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(cluster *fdbv1beta2.FoundationDBCluster, log logr.Logger) ([]byte, error) {
	log.Info("Fetch status from FDB", "namespace", cluster.Namespace, "cluster", cluster.Name)
	return getValueFromDBUsingKey(cluster, "\xff\xff/status/json", 1000)
}

type realDatabaseClientProvider struct {
	// log implementation for logging output
	log logr.Logger
}

// GetLockClient generates a client for working with locks through the database.
func (p *realDatabaseClientProvider) GetLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return NewRealLockClient(cluster, p.log)
}

// GetAdminClient generates a client for performing administrative actions
// against the database.
func (p *realDatabaseClientProvider) GetAdminClient(cluster *fdbv1beta2.FoundationDBCluster, kubernetesClient client.Client) (fdbadminclient.AdminClient, error) {
	return NewCliAdminClient(cluster, kubernetesClient, p.log)
}

// NewDatabaseClientProvider generates a client provider for talking to real
// databases.
func NewDatabaseClientProvider(log logr.Logger) controllers.DatabaseClientProvider {
	return &realDatabaseClientProvider{
		log: log.WithName("fdbclient"),
	}
}
