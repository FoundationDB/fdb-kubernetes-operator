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
	"k8s.io/apimachinery/pkg/types"
	"math/rand"
	"os"
	"path"
	"sync"
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

// Keeps a singleton databases. The key is the cluster UID. Guarded by the mutex below for read and write access.
var databaseSingleton map[types.UID]*fdb.Database
var databaseSingletonMutex = &sync.Mutex{}

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

// getFDBDatabase returns the singleton FDB database. May return an error if initializing the singleton failed.
func getFDBDatabase(cluster *fdbv1beta2.FoundationDBCluster) (fdb.Database, error) {
	databaseSingletonMutex.Lock()
	defer databaseSingletonMutex.Unlock()
	if databaseSingleton[cluster.UID] == nil {
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
		databaseSingleton[cluster.UID] = &database
	}
	// This is a copy, but fdb.Database is just a pointer-to-implementation and cheap to copy.
	return *databaseSingleton[cluster.UID], nil
}

// createClusterFile will create or update the cluster file for the specified cluster.
func createClusterFile(cluster *fdbv1beta2.FoundationDBCluster) (string, error) {
	return ensureClusterFileIsPresent(os.TempDir(), string(cluster.UID), cluster.Status.ConnectionString)
}

// ensureClusterFileIsPresent will ensure that the cluster file with the specified connection string is present.
func ensureClusterFileIsPresent(dir string, uid string, connectionString string) (string, error) {
	for {
		clusterFileName := path.Join(dir, fmt.Sprintf("%s-%d", uid, rand.Uint64()))

		f, err := os.OpenFile(clusterFileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0777)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		_, err = f.Write([]byte(connectionString))
		if err1 := f.Close(); err1 != nil && err == nil {
			err = err1
		}
		return clusterFileName, err
	}
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
