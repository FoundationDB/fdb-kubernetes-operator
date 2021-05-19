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
	"encoding/json"
	"fmt"
	"io/ioutil"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	controllers "github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("fdbclient")

const (
	defaultTransactionTimeout int64 = 5000
)

// DefaultCLITimeout is the default timeout for CLI commands.
var DefaultCLITimeout = 10

// fdbDatabaseCache provides a cache for FDB databases
var fdbDatabaseCache = map[string]fdb.Database{}

// getFDBDatabase opens an FDB database. The result will be cached for
// subsequent calls, based on the cluster namespace and name.
func getFDBDatabase(cluster *fdbtypes.FoundationDBCluster) (fdb.Database, error) {
	cacheKey := fmt.Sprintf("%s/%s", cluster.ObjectMeta.Namespace, cluster.ObjectMeta.Name)
	database, present := fdbDatabaseCache[cacheKey]
	if present {
		return database, nil
	}

	clusterFile, err := ioutil.TempFile("", "")
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

	database, err = fdb.OpenDatabase(clusterFilePath)
	if err != nil {
		return fdb.Database{}, err
	}

	err = database.Options().SetTransactionTimeout(defaultTransactionTimeout)
	if err != nil {
		return fdb.Database{}, err
	}

	fdbDatabaseCache[cacheKey] = database
	return database, nil
}

// cleanUpDBCache removes the FDB DB connection for the deleted cluster.
// Otherwise the controller will connect to an old cluster.
func cleanUpDBCache(namespace string, name string) {
	delete(fdbDatabaseCache, fmt.Sprintf("%s/%s", namespace, name))
}

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(cluster *fdbtypes.FoundationDBCluster) (*fdbtypes.FoundationDBStatus, error) {
	log.Info("Fetch status from FDB", "namespace", cluster.Namespace, "cluster", cluster.Name)
	statusKey := "\xff\xff/status/json"

	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}

	result, err := database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		// Wait default timeout seconds to receive status for larger clusters.
		err = transaction.Options().SetTimeout(int64(DefaultCLITimeout * 1000))
		if err != nil {
			return nil, err
		}

		statusBytes := transaction.Get(fdb.Key(statusKey)).MustGet()
		if len(statusBytes) == 0 {
			return nil, err
		}

		return statusBytes, err
	})

	if err != nil {
		return nil, err
	}

	statusBytes, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("could not cast result into byte slice")
	}

	status := &fdbtypes.FoundationDBStatus{}
	err = json.Unmarshal(statusBytes, &status)
	if err == nil {
		log.Info("Successfully fetched status from FDB", "namespace", cluster.Namespace, "cluster", cluster.Name)
	}

	return status, err
}

type realDatabaseClientProvider struct{}

// GetLockClient generates a client for working with locks through the database.
func (p *realDatabaseClientProvider) GetLockClient(cluster *fdbtypes.FoundationDBCluster) (controllers.LockClient, error) {
	return NewRealLockClient(cluster)
}

// GetAdminClient generates a client for performing administrative actions
// against the database.
func (p *realDatabaseClientProvider) GetAdminClient(cluster *fdbtypes.FoundationDBCluster, kubernetesClient client.Client) (controllers.AdminClient, error) {
	return NewCliAdminClient(cluster, kubernetesClient)
}

// CleanUpCache removes the cache entry for a cluster.
func (p *realDatabaseClientProvider) CleanUpCache(namespace string, name string) {
	cleanUpDBCache(namespace, name)
}

// NewDatabaseClientProvider generates a client provider for talking to real
// databases.
func NewDatabaseClientProvider() controllers.DatabaseClientProvider {
	return &realDatabaseClientProvider{}
}
