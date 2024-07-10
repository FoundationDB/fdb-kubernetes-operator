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
	"fmt"
	"io/fs"
	"os"
	"path"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultCLITimeout is the default timeout for CLI commands.
var DefaultCLITimeout = 10 * time.Second

// MaxCliTimeout is the maximum CLI timeout that will be used for requests that might be slower to respond.
var MaxCliTimeout = 40 * time.Second

const (
	defaultTransactionTimeout = 5 * time.Second
)

func parseMachineReadableStatus(logger logr.Logger, contents []byte, checkForProcesses bool) (*fdbv1beta2.FoundationDBStatus, error) {
	status := &fdbv1beta2.FoundationDBStatus{}
	err := json.Unmarshal(contents, status)
	if err != nil {
		return nil, err
	}

	if len(status.Client.Messages) > 0 {
		logger.Info("found client message(s) in the machine-readable status", "messages", status.Client.Messages)
		// TODO: Check for client messages that should be validated here.
	}

	if !status.Client.DatabaseStatus.Available {
		logger.Info("database is unavailable", "status", status)
		return nil, fdbv1beta2.TimeoutError{Err: fmt.Errorf("database is unavailable")}
	}

	if len(status.Cluster.Messages) > 0 {
		logger.Info("found cluster message(s) in the machine-readable status", "messages", status.Cluster.Messages)
		logger.V(1).Info("current status with cluster messages", "status", status)

		// If the status is incomplete because of a timeout, return an error. This will force a new reconciliation.
		for _, message := range status.Cluster.Messages {
			if message.Name == "status_incomplete_timeout" {
				return nil, fdbv1beta2.TimeoutError{Err: fmt.Errorf("found \"status_incomplete_timeout\" in cluster messages")}
			}
		}
	}

	if len(status.Cluster.Processes) == 0 && checkForProcesses {
		logger.Info("machine-readable status is missing process information")
		return nil, fdbv1beta2.TimeoutError{Err: fmt.Errorf("machine-readable status is missing process information")}
	}

	return status, nil
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
func getConnectionStringFromDB(libClient fdbLibClient, timeout time.Duration) ([]byte, error) {
	return libClient.getValueFromDBUsingKey("\xff/coordinators", timeout)
}

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(libClient fdbLibClient, logger logr.Logger, timeout time.Duration) (*fdbv1beta2.FoundationDBStatus, error) {
	contents, err := libClient.getValueFromDBUsingKey("\xff\xff/status/json", timeout)
	if err != nil {
		return nil, err
	}

	return parseMachineReadableStatus(logger, contents, true)
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
