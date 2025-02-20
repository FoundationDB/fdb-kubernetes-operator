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
	"k8s.io/utils/pointer"
	"os"
	"path"
	"sync"
	"sync/atomic"
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

// clientMutex is used to coordinate the creation and close (deletion) of clients.
var clientMutex sync.Mutex

// clientRefCounter is used to count the open references for a specific cluster file, if the counter is 0 when the Close()
// method is called, all resources will be cleaned up.
var clientRefCounter sync.Map

// incrementClientRefCounter will be called for every new client that is created. The clientRefCounter with the provided
// cluster will be used to track the active clients. The key will be the generation ID of the connection string and the
// cluster UID.
func incrementClientRefCounter(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) (string, error) {
	key, err := clusterFileKey(cluster)
	if err != nil {
		return "", err
	}

	cnt := updateClientRefCounter(key, 1)
	logger.V(1).Info("incrementing client ref counter", "key", key, "refCounter", cnt)
	return key, nil
}

// closeClient will take a lock and then decrease the clientRefCounter for the provided key. If the clientRefCounter
// is equal to or less than 0, the cluster file at the clusterFilePath will be deleted.
func closeClient(logger logr.Logger, key string, clusterFilePath string) error {
	clientMutex.Lock()
	defer clientMutex.Unlock()
	databaseMutex.Lock()
	defer databaseMutex.Unlock()

	if key == "" {
		return nil
	}

	cnt := updateClientRefCounter(key, -1)
	logger.V(1).Info("decrementing client ref counter", "key", key, "refCounter", cnt)

	if cnt <= 0 {
		// Make sure to first close the database if a database is opened with this key. Otherwise,
		// we have a race condition where we deleted the cluster file but the database is updating the cluster file
		// before it is closed.
		loadedDatabase, ok := databaseMap[key]
		if ok {
			logger.V(1).Info("closing database", "key", key, "refCounter", cnt, "clusterFilePath", clusterFilePath, "databaseMap", databaseMap)
			loadedDatabase.Close()
			delete(databaseMap, key)
		}

		logger.V(1).Info("clean up cluster file", "key", key, "refCounter", cnt, "clusterFilePath", clusterFilePath)
		err := os.Remove(clusterFilePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}

			return err
		}

		// Delete the entry from the sync.Map as the reference count is 0.
		clientRefCounter.Delete(key)
	}

	return nil
}

// updateClientRefCounter will update the clientRefCounter with the provided key. If the key doesn't exist, a new entry
// will be created. The value should be 1 or -1, to either increment or decrement the value.
func updateClientRefCounter(key string, value int64) int64 {
	val, _ := clientRefCounter.LoadOrStore(key, new(int64))
	ptr := val.(*int64)
	atomic.AddInt64(ptr, value)

	return pointer.Int64Deref(ptr, 1)
}

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

// databaseMutex is used to synchronize the creation/loading of the fdb.Database resource for this cluster.
var databaseMutex sync.Mutex

// databaseMap keeps track of the fdb.Database that was opened for a specific cluster file (generation ID and cluster
// UID).
var databaseMap = make(map[string]fdb.Database)

// getFDBDatabase opens an FDB database.
func getFDBDatabase(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) (fdb.Database, string, error) {
	databaseMutex.Lock()
	defer databaseMutex.Unlock()

	key, err := clusterFileKey(cluster)
	if err != nil {
		return fdb.Database{}, "", err
	}

	clusterFile, err := createClusterFile(logger, cluster)
	if err != nil {
		return fdb.Database{}, "", err
	}

	// If the database was already opened, just return it.
	loadedDatabase, ok := databaseMap[key]
	if ok {
		return loadedDatabase, clusterFile, nil
	}

	database, err := fdb.OpenDatabase(clusterFile)
	if err != nil {
		return fdb.Database{}, clusterFile, err
	}

	err = database.Options().SetTransactionTimeout(defaultTransactionTimeout.Milliseconds())
	if err != nil {
		return fdb.Database{}, clusterFile, err
	}

	databaseMap[key] = database

	return database, clusterFile, nil
}

func clusterFileKey(cluster *fdbv1beta2.FoundationDBCluster) (string, error) {
	parsedConnectionString, err := fdbv1beta2.ParseConnectionString(cluster.Status.ConnectionString)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s@%s", parsedConnectionString.GenerationID, cluster.UID), nil
}

// createClusterFile will create or update the cluster file for the specified cluster.
func createClusterFile(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster) (string, error) {
	clusterFile, err := clusterFileKey(cluster)
	if err != nil {
		return "", err
	}

	return ensureClusterFileIsPresent(logger, path.Join(os.TempDir(), cluster.Name), clusterFile, cluster.Status.ConnectionString)
}

// ensureClusterFileIsPresent will ensure that the cluster file with the specified connection string is present.
func ensureClusterFileIsPresent(logger logr.Logger, dir string, fileName string, connectionString string) (string, error) {
	clusterFileName := path.Join(dir, fileName)

	logger.V(1).Info("creating cluster file", "dir", dir, "fileName", fileName, "clusterFileName", clusterFileName)
	// Try to read the file to check if the file already exists.
	_, err := os.Stat(clusterFileName)
	if err != nil {
		// If the file doesn't exist we have to create it
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		// Ensure the directory exists.
		err = os.MkdirAll(dir, 0777)
		if err != nil {
			return "", err
		}

		// Create the cluster file with the expected connection string.
		err = os.WriteFile(clusterFileName, []byte(connectionString), 0777)
		if err != nil {
			return "", err
		}
	}

	// The cluster file already exists, so we are not overwriting it.
	return clusterFileName, nil
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

func (p *realDatabaseClientProvider) GetAdminClientWithLogger(cluster *fdbv1beta2.FoundationDBCluster, kubernetesClient client.Client, logger logr.Logger) (fdbadminclient.AdminClient, error) {
	return NewCliAdminClient(cluster, kubernetesClient, logger.WithName("fdbclient"))
}

// GetLockClient generates a client for working with locks through the database.
func (p *realDatabaseClientProvider) GetLockClient(cluster *fdbv1beta2.FoundationDBCluster) (fdbadminclient.LockClient, error) {
	return NewRealLockClient(cluster, p.log)
}

// GetLockClientWithLogger generates a client for working with locks through the database.
// The provided logger will be used as logger for the LockClient.
func (p *realDatabaseClientProvider) GetLockClientWithLogger(cluster *fdbv1beta2.FoundationDBCluster, logger logr.Logger) (fdbadminclient.LockClient, error) {
	return NewRealLockClient(cluster, logger.WithName("fdbclient"))
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
