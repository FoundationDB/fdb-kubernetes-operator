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

// getStatusFromDB gets the database's status directly from the system key
func getStatusFromDB(cluster *fdbtypes.FoundationDBCluster) (*fdbtypes.FoundationDBStatus, error) {
	statusKey := "\xff\xff/status/json"

	database, err := getFDBDatabase(cluster)
	if err != nil {
		return nil, err
	}

	status := &fdbtypes.FoundationDBStatus{}
	_, err = database.Transact(func(transaction fdb.Transaction) (interface{}, error) {
		err := transaction.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		statusBytes := transaction.Get(fdb.Key(statusKey)).MustGet()
		if len(statusBytes) == 0 {
			return nil, fmt.Errorf("get for FDB status returned an empty result")
		}

		err = json.Unmarshal(statusBytes, &status)
		return status, err
	})

	if err != nil {
		return nil, err
	}

	return status, nil
}
