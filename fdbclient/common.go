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
	"strings"
	"sync"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
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

func parseMachineReadableStatus(
	logger logr.Logger,
	contents []byte,
	checkForProcesses bool,
) (*fdbv1beta2.FoundationDBStatus, error) {
	status := &fdbv1beta2.FoundationDBStatus{}
	err := json.Unmarshal(contents, status)
	if err != nil {
		return nil, err
	}

	if len(status.Client.Messages) > 0 {
		logger.Info(
			"found client message(s) in the machine-readable status",
			"messages",
			status.Client.Messages,
		)
		// TODO: Check for client messages that should be validated here.
	}

	if !status.Client.DatabaseStatus.Available {
		logger.Info("database is unavailable", "status", status)
		return nil, fdbv1beta2.TimeoutError{Err: fmt.Errorf("database is unavailable")}
	}

	if len(status.Cluster.Messages) > 0 {
		logger.Info(
			"found cluster message(s) in the machine-readable status",
			"messages",
			status.Cluster.Messages,
		)
		logger.V(1).Info("current status with cluster messages", "status", status)

		// If the status is incomplete because of a timeout, return an error. This will force a new reconciliation.
		for _, message := range status.Cluster.Messages {
			if message.Name == "status_incomplete_timeout" {
				return nil, fdbv1beta2.TimeoutError{
					Err: fmt.Errorf("found \"status_incomplete_timeout\" in cluster messages"),
				}
			}
		}
	}

	if len(status.Cluster.Processes) == 0 && checkForProcesses {
		logger.Info("machine-readable status is missing process information")
		return nil, fdbv1beta2.TimeoutError{
			Err: fmt.Errorf("machine-readable status is missing process information"),
		}
	}

	return status, nil
}

// getFDBDatabase opens an FDB database.
func getFDBDatabase(cluster *fdbv1beta2.FoundationDBCluster) (fdb.Database, error) {
	clusterFile, err := ensureClusterFileIsPresent(
		path.Join(os.TempDir(), string(cluster.UID)),
		cluster.Status.ConnectionString,
	)
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

// clusterFileMutex is used to synchronize the creation of the initial cluster file.
var clusterFileMutex sync.Mutex

// ensureClusterFileIsPresent will ensure that the cluster file with the specified connection string is present. If the cluster file
// is already present, the cluster file will not be updated.
func ensureClusterFileIsPresent(clusterFileName string, connectionString string) (string, error) {
	// Check if the file already exists, if so, just return the cluster file name and don't modify the existing cluster file.
	// Since we only check if the file exist, we don't need a mutex here.
	_, err := os.Stat(clusterFileName)
	if err == nil {
		return clusterFileName, nil
	}

	// If the file doesn't exist we have to create it
	if errors.Is(err, fs.ErrNotExist) {
		// Ensure that only a single go routine creates the initial cluster file.
		clusterFileMutex.Lock()
		defer clusterFileMutex.Unlock()

		// Check a second time here if the file now exists, in theory there could be a race where one go routine checked
		// that the file doesn't exist and before taking the mutex, the file was now created in parallel.
		_, err = os.Stat(clusterFileName)
		if err == nil {
			return clusterFileName, nil
		}

		createDirErr := os.MkdirAll(path.Dir(clusterFileName), 0777)
		if createDirErr != nil {
			return "", createDirErr
		}

		return clusterFileName, os.WriteFile(clusterFileName, []byte(connectionString), 0777)
	}

	// If we end up here there was a different error which might indicate some permission errors or I/O errors.
	return "", err
}

// createClusterFileForCommandLine will create a cluster file that can be used by the fdb cli tooling, e.g. fdbcli or
// fdbbackup. The file should be deleted after use.
func createClusterFileForCommandLine(cluster *fdbv1beta2.FoundationDBCluster) (*os.File, error) {
	tmpDir := path.Join(os.TempDir(), fmt.Sprintf("%s-cli", cluster.UID))
	err := os.MkdirAll(tmpDir, 0777)
	if err != nil {
		return nil, err
	}

	tempClusterFile, err := os.CreateTemp(tmpDir, "")
	if err != nil {
		return nil, err
	}

	return tempClusterFile, os.WriteFile(
		tempClusterFile.Name(),
		[]byte(cluster.Status.ConnectionString),
		0777,
	)
}

// getConnectionStringFromDB gets the database's connection string directly from the system key
func getConnectionStringFromDB(libClient fdbLibClient, timeout time.Duration) (string, error) {
	outputBytes, err := libClient.getValueFromDBUsingKey("\xff/coordinators", timeout)
	if err != nil {
		return "", err
	}

	var connectionString fdbv1beta2.ConnectionString
	connectionString, err = fdbv1beta2.ParseConnectionString(
		cleanConnectionStringOutput(string(outputBytes)),
	)
	if err != nil {
		return "", err
	}

	return connectionString.String(), nil
}

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(
	libClient fdbLibClient,
	logger logr.Logger,
	timeout time.Duration,
) (*fdbv1beta2.FoundationDBStatus, error) {
	contents, err := libClient.getValueFromDBUsingKey("\xff\xff/status/json", timeout)
	if err != nil {
		return nil, err
	}

	return parseMachineReadableStatus(logger, contents, true)
}

// cleanConnectionStringOutput is a helper method to remove unrelated output from the get command in the connection string
// output.
func cleanConnectionStringOutput(input string) string {
	startIdx := strings.LastIndex(input, "`")
	endIdx := strings.LastIndex(input, "'")
	if startIdx == -1 && endIdx == -1 {
		return input
	}

	return input[startIdx+1 : endIdx]
}

type realDatabaseClientProvider struct {
	// log implementation for logging output
	log logr.Logger
}

func (p *realDatabaseClientProvider) GetAdminClientWithLogger(
	cluster *fdbv1beta2.FoundationDBCluster,
	kubernetesClient client.Client,
	logger logr.Logger,
) (fdbadminclient.AdminClient, error) {
	return NewCliAdminClient(cluster, kubernetesClient, logger.WithName("fdbclient"))
}

// GetLockClient generates a client for working with locks through the database.
func (p *realDatabaseClientProvider) GetLockClient(
	cluster *fdbv1beta2.FoundationDBCluster,
) (fdbadminclient.LockClient, error) {
	return NewRealLockClient(cluster, p.log)
}

// GetLockClientWithLogger generates a client for working with locks through the database.
// The provided logger will be used as logger for the LockClient.
func (p *realDatabaseClientProvider) GetLockClientWithLogger(
	cluster *fdbv1beta2.FoundationDBCluster,
	logger logr.Logger,
) (fdbadminclient.LockClient, error) {
	return NewRealLockClient(cluster, logger.WithName("fdbclient"))
}

// GetAdminClient generates a client for performing administrative actions
// against the database.
func (p *realDatabaseClientProvider) GetAdminClient(
	cluster *fdbv1beta2.FoundationDBCluster,
	kubernetesClient client.Client,
) (fdbadminclient.AdminClient, error) {
	return NewCliAdminClient(cluster, kubernetesClient, p.log)
}

// NewDatabaseClientProvider generates a client provider for talking to real
// databases.
func NewDatabaseClientProvider(log logr.Logger) fdbadminclient.DatabaseClientProvider {
	return &realDatabaseClientProvider{
		log: log.WithName("fdbclient"),
	}
}
