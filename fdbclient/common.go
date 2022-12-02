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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultTransactionTimeout = 5 * time.Second
)

// DefaultCLITimeout is the default timeout for CLI commands.
var DefaultCLITimeout = 10 * time.Second

// createClusterFile will create or update the cluster file for the specified cluster.
func createClusterFile(cluster *fdbv1beta2.FoundationDBCluster) (string, error) {
	return ensureClusterFileIsPresent(os.TempDir(), string(cluster.UID), cluster.Status.ConnectionString)
}

// ensureClusterFileIsPresent will ensure that the cluster file with the specified connection string is present.
func ensureClusterFileIsPresent(dir string, uid string, connectionString string) (string, error) {
	clusterFileName := path.Join(dir, uid)

	// Try to read the file to check if the file already exists and if so, if the content matches
	content, err := os.ReadFile(clusterFileName)

	// If the file doesn't exist we have to create it
	if errors.Is(err, fs.ErrNotExist) {
		return clusterFileName, os.WriteFile(clusterFileName, []byte(connectionString), 0777)
	}

	// The content of the cluster file is already correct.
	if string(content) == connectionString {
		return clusterFileName, nil
	}

	// The content doesn't match, so we have to write the new content to the cluster file.
	return clusterFileName, os.WriteFile(clusterFileName, []byte(connectionString), 0777)
}

// getFDBDatabase opens an FDB database.
func getFDBDatabase(cluster *fdbv1beta2.FoundationDBCluster) (fdb.Database, error) {
	clusterFile, err := createClusterFile(cluster)
	if err != nil {
		return fdb.Database{}, err
	}

	database, err := fdb.OpenDatabase(clusterFile)
	if err != nil {
		return fdb.Database{}, err
	}

	err = database.Options().SetTransactionTimeout(defaultTransactionTimeout.Milliseconds())
	if err != nil {
		return fdb.Database{}, err
	}

	return database, nil
}

func getValueFromDBUsingKey(cluster *fdbv1beta2.FoundationDBCluster, log logr.Logger, fdbKey string, timeout time.Duration) ([]byte, error) {
	log.Info("Fetch values from FDB", "key", fdbKey)
	defer func() {
		log.Info("Done fetching values from FDB", "key", fdbKey)
	}()
	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}

	result, err := database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}
		err = transaction.Options().SetTimeout(timeout.Milliseconds())
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
		var fdbError *fdb.Error
		if errors.As(err, &fdbError) {
			// See: https://apple.github.io/foundationdb/api-error-codes.html
			// 1031: Operation aborted because the transaction timed out
			if fdbError.Code == 1031 {
				return nil, fdbv1beta2.TimeoutError{Err: err}
			}
		}

		return nil, err
	}

	byteResult, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("could not cast result into byte slice")
	}
	return byteResult, nil
}

// getConnectionStringFromDB gets the database's connection string directly from the system key
func getConnectionStringFromDB(cluster *fdbv1beta2.FoundationDBCluster, log logr.Logger) ([]byte, error) {
	return getValueFromDBUsingKey(cluster, log, "\xff/coordinators", DefaultCLITimeout)
}

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(cluster *fdbv1beta2.FoundationDBCluster, log logr.Logger) ([]byte, error) {
	return getValueFromDBUsingKey(cluster, log, "\xff\xff/status/json", DefaultCLITimeout)
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
