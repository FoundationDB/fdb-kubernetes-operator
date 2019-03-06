package foundationdbcluster

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/google/uuid"
	fdbtypes "github.com/brownleej/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

// AdminClient describes an interface for running administrative commands on a
// cluster
type AdminClient interface {
	ConfigureDatabase(configuration DatabaseConfiguration, newDatabase bool) error
}

// DatabaseConfiguration represents the desired
type DatabaseConfiguration struct {
	ReplicationMode string
	StorageEngine   string
}

func (configuration DatabaseConfiguration) getConfigurationKeys() ([]fdb.KeyValue, error) {
	keys := make([]fdb.KeyValue, 0)

	switch configuration.ReplicationMode {
	case "single":
		keys = append(keys,
			fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_replicas"), Value: []byte("1")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_replicas"), Value: []byte("1")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_anti_quorum"), Value: []byte("0")},
		)
	case "double":
		keys = append(keys,
			fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_replicas"), Value: []byte("2")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_replicas"), Value: []byte("2")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_anti_quorum"), Value: []byte("0")},
		)
	case "triple":
		keys = append(keys,
			fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_replicas"), Value: []byte("3")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_replicas"), Value: []byte("3")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_anti_quorum"), Value: []byte("0")},
		)
	default:
		return nil, fmt.Errorf("Unknown replication mode %s", configuration.ReplicationMode)
	}

	switch configuration.StorageEngine {
	case "ssd":
		keys = append(keys,
			fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_engine"), Value: []byte("2")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_engine"), Value: []byte("2")},
		)
	case "memory":
		keys = append(keys,
			fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_engine"), Value: []byte("1")},
			fdb.KeyValue{Key: fdb.Key("\xff/conf/log_engine"), Value: []byte("1")},
		)
	default:
		return nil, fmt.Errorf("Unknown storage engine %s", configuration.StorageEngine)
	}
	return keys, nil
}

// RealAdminClient provides an implementation of the admin interface using the
// FDB client library
type RealAdminClient struct {
	Cluster  *fdbtypes.FoundationDBCluster
	Database fdb.Database
}

// NewAdminClient generates an Admin client for a cluster
func NewAdminClient(cluster *fdbtypes.FoundationDBCluster) (AdminClient, error) {
	err := os.MkdirAll("/tmp/fdb", os.ModePerm)
	if err != nil {
		return nil, err
	}
	clusterFilePath := fmt.Sprintf("/tmp/fdb/%s.cluster", cluster.Name)

	clusterFile, err := os.OpenFile(clusterFilePath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return nil, err
	}
	_, err = clusterFile.WriteString(cluster.Spec.ConnectionString)
	if err != nil {
		return nil, err
	}
	err = clusterFile.Close()
	if err != nil {
		return nil, err
	}

	db, err := fdb.Open(clusterFilePath, []byte("DB"))
	if err != nil {
		return nil, err
	}

	return &RealAdminClient{Cluster: cluster, Database: db}, nil
}

// ConfigureDatabase sets the database configuration
func (client *RealAdminClient) ConfigureDatabase(configuration DatabaseConfiguration, newDatabase bool) error {

	tr, err := client.Database.CreateTransaction()
	if err != nil {
		return err
	}

	initID, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	for {
		err = configureDatabaseInTransaction(configuration, newDatabase, tr, initID)
		if err == nil {
			return err
		}

		fdbErr, isFdb := err.(fdb.Error)
		if !isFdb {
			return err
		}
		if newDatabase && (fdbErr.Code == 1020 || fdbErr.Code == 1007) {
			tr.Reset()
			for {
				err := checkConfigurationInitID(tr, initID)
				if err == nil {
					return err
				}
				fdbErr, isFdb = err.(fdb.Error)
				if !isFdb {
					return fdbErr
				}
				err = tr.OnError(fdbErr).Get()
				if err != nil {
					return err
				}
			}
		} else {
			err = tr.OnError(fdbErr).Get()
			if err != nil {
				return err
			}
		}
	}
}

/**
configureDatabaseInTransaction runs the logic to change database
configuration within a transactional block.
*/
func configureDatabaseInTransaction(configuration DatabaseConfiguration, newDatabase bool, tr fdb.Transaction, initID uuid.UUID) error {
	err := tr.Options().SetAccessSystemKeys()
	if err != nil {
		return err
	}
	err = tr.Options().SetLockAware()
	if err != nil {
		return err
	}
	err = tr.Options().SetPrioritySystemImmediate()
	if err != nil {
		return err
	}
	keys, err := configuration.getConfigurationKeys()
	if err != nil {
		return err
	}
	if newDatabase {
		err = tr.Options().SetInitializeNewDatabase()
		if err != nil {
			return err
		}
		initIDKey := fdb.Key("\xff/init_id")
		err = tr.AddReadConflictKey(initIDKey)
		if err != nil {
			return err
		}

		tr.Set(fdb.Key(initIDKey), initID[:])
		tr.Set(fdb.Key("\xff/conf/initialized"), []byte("1"))
	} else {
		err = tr.Options().SetCausalWriteRisky()
		if err != nil {
			return err
		}
		err = tr.AddReadConflictKey(keys[0].Key)
		if err != nil {
			return err
		}
	}

	for _, keyValue := range keys {
		tr.Set(keyValue.Key, keyValue.Value)
	}

	return tr.Commit().Get()
}

/**
checkConfigurationInitID is run after a transaction to create a new database
fails. It checks to see if the initial ID for the configuration is set to the
value that this transaction was trying to set.
*/
func checkConfigurationInitID(tr fdb.Transaction, initID uuid.UUID) error {
	err := tr.Options().SetPrioritySystemImmediate()
	if err != nil {
		return err
	}
	err = tr.Options().SetLockAware()
	if err != nil {
		return err
	}
	err = tr.Options().SetReadSystemKeys()
	if err != nil {
		return err
	}
	currentID, err := tr.Get(fdb.Key("\xff/init_id")).Get()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(currentID, initID[:]) {
		return errors.New("Database has already been created")
	}

	return nil
}

// MockAdminClient provides a mock implementation of the cluster admin interface
type MockAdminClient struct {
	Cluster *fdbtypes.FoundationDBCluster
	DatabaseConfiguration
}

var adminClientCache = make(map[string]*MockAdminClient)

// NewMockAdminClient creates an admin client for a cluster.
func NewMockAdminClient(cluster *fdbtypes.FoundationDBCluster) (AdminClient, error) {
	return newMockAdminClientUncast(cluster)
}

func newMockAdminClientUncast(cluster *fdbtypes.FoundationDBCluster) (*MockAdminClient, error) {
	client := adminClientCache[cluster.Name]
	if client == nil {
		client = &MockAdminClient{Cluster: cluster}
		adminClientCache[cluster.Name] = client
	}
	return client, nil
}

// ClearMockAdminClients clears the cache of mock Admin clients
func ClearMockAdminClients() {
	adminClientCache = map[string]*MockAdminClient{}
}

// ConfigureDatabase changes the database configuration
func (client *MockAdminClient) ConfigureDatabase(configuration DatabaseConfiguration, newDatabase bool) error {
	if client.DatabaseConfiguration.ReplicationMode == "" && !newDatabase {
		return errors.New("Database not configured yet")
	} else if client.DatabaseConfiguration.ReplicationMode != "" {
		return errors.New("Database already configured")
	}
	client.DatabaseConfiguration = configuration
	return nil
}
