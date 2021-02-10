package controllers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
)

const (
	defaultTransactionTimeout int64 = 5000
)

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
