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
	"errors"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
	"io/fs"
	"os"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

// DefaultCLITimeout is the default timeout for CLI commands.
var DefaultCLITimeout = 10 * time.Second

const (
	defaultTransactionTimeout = 5 * time.Second
)

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

// getConnectionStringFromDB gets the database's connection string directly from the system key
func getConnectionStringFromDB(libClient fdbLibClient) ([]byte, error) {
	return libClient.getValueFromDBUsingKey("\xff/coordinators", DefaultCLITimeout)
}

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(libClient fdbLibClient) (*fdbv1beta2.FoundationDBStatus, error) {
	contents, err := libClient.getValueFromDBUsingKey("\xff\xff/status/json", DefaultCLITimeout)
	if err != nil {
		return nil, err
	}

	status := &fdbv1beta2.FoundationDBStatus{}
	err = json.Unmarshal(contents, status)
	if err != nil {
		return nil, err
	}

	return status, nil
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
func NewDatabaseClientProvider(log logr.Logger) fdbadminclient.DatabaseClientProvider {
	return &realDatabaseClientProvider{
		log: log.WithName("fdbclient"),
	}
}
