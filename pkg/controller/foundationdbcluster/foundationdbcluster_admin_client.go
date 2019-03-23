package foundationdbcluster

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/google/uuid"
	fdbtypes "github.com/brownleej/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var configurationProtocolVersion = []byte("\x01\x00\x04Q\xa5\x00\xdb\x0f")

// AdminClient describes an interface for running administrative commands on a
// cluster
type AdminClient interface {
	// GetStatus gets the database's status
	GetStatus() (*fdbtypes.FoundationDBStatus, error)

	// ConfigureDatabase sets the database configuration
	ConfigureDatabase(configuration DatabaseConfiguration, newDatabase bool) error

	// ExcludeInstances starts evacuating processes so that they can be removed
	// from the database.
	ExcludeInstances(addresses []string) error

	// IncludeInstances removes processes from the exclusion list and allows
	// them to take on roles again.
	IncludeInstances(addresses []string) error

	// CanSafelyRemove checks whether it is safe to remove processes from the
	// cluster
	CanSafelyRemove(addresses []string) ([]string, error)

	// KillProcesses restarts processes
	KillInstances(addresses []string) error

	// Close shuts down any resources for the client once it is no longer
	// needed.
	Close() error
}

// DatabaseConfiguration represents the desired
type DatabaseConfiguration struct {
	ReplicationMode string
	StorageEngine   string
}

func (configuration DatabaseConfiguration) getConfigurationKeys() ([]fdb.KeyValue, error) {
	keys := make([]fdb.KeyValue, 0)
	var policy localityPolicy
	var replicas []byte

	switch configuration.ReplicationMode {
	case "single":
		policy = &singletonPolicy{}
		replicas = []byte("1")
	case "double":
		policy = &acrossPolicy{
			Count:     2,
			Field:     "zoneid",
			Subpolicy: &singletonPolicy{},
		}
		replicas = []byte("2")
	case "triple":
		policy = &acrossPolicy{
			Count:     3,
			Field:     "zoneid",
			Subpolicy: &singletonPolicy{},
		}
		replicas = []byte("3")
	default:
		return nil, fmt.Errorf("Unknown replication mode %s", configuration.ReplicationMode)
	}

	policyBytes := bytes.Join([][]byte{configurationProtocolVersion, policy.BinaryRepresentation()}, nil)
	keys = append(keys,
		fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_replicas"), Value: replicas},
		fdb.KeyValue{Key: fdb.Key("\xff/conf/log_replicas"), Value: replicas},
		fdb.KeyValue{Key: fdb.Key("\xff/conf/log_anti_quorum"), Value: []byte("0")},
		fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_replication_policy"), Value: policyBytes},
		fdb.KeyValue{Key: fdb.Key("\xff/conf/log_replication_policy"), Value: policyBytes},
	)

	var engine []byte
	switch configuration.StorageEngine {
	case "ssd":
		engine = []byte("1")
	case "memory":
		engine = []byte("2")
	default:
		return nil, fmt.Errorf("Unknown storage engine %s", configuration.StorageEngine)
	}

	keys = append(keys,
		fdb.KeyValue{Key: fdb.Key("\xff/conf/storage_engine"), Value: engine},
		fdb.KeyValue{Key: fdb.Key("\xff/conf/log_engine"), Value: engine},
	)
	return keys, nil
}

// RealAdminClient provides an implementation of the admin interface using the
// FDB client library
type RealAdminClient struct {
	Cluster  *fdbtypes.FoundationDBCluster
	Database fdb.Database
}

// NewAdminClient generates an Admin client for a cluster
func NewAdminClient(cluster *fdbtypes.FoundationDBCluster, _ client.Client) (AdminClient, error) {
	err := os.MkdirAll("/tmp/fdb", os.ModePerm)
	if err != nil {
		return nil, err
	}
	clusterFilePath := fmt.Sprintf("/tmp/fdb/%s.cluster", cluster.Name)

	clusterFile, err := os.OpenFile(clusterFilePath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	defer clusterFile.Close()
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

// GetStatus gets the database's status
func (client *RealAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	data, err := client.Database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		return tr.Get(fdb.Key("\xff\xff/status/json")).Get()
	})
	if err != nil {
		return nil, err
	}
	status := &fdbtypes.FoundationDBStatus{}
	err = json.Unmarshal(data.([]byte), &status)
	return status, err
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
	}

	for _, keyValue := range keys {
		var match bool
		if !newDatabase {
			currentValue, err := tr.Get(keyValue.Key).Get()
			if err != nil {
				return err
			}
			match = reflect.DeepEqual(currentValue, keyValue.Value)
		}
		if !match {
			tr.Set(keyValue.Key, keyValue.Value)
		}
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

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *RealAdminClient) ExcludeInstances(addresses []string) error {
	_, err := client.Database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		exclusionID, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		err = tr.Options().SetPrioritySystemImmediate()
		if err != nil {
			return nil, err
		}

		err = tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		err = tr.Options().SetLockAware()
		if err != nil {
			return nil, err
		}

		tr.AddReadConflictKey(fdb.Key("\xff/conf/excluded"))
		tr.Set(fdb.Key("\xff/conf/excluded"), exclusionID[:])
		for _, address := range addresses {
			tr.Set(
				fdb.Key(bytes.Join([][]byte{
					[]byte("\xff/conf/excluded/"),
					[]byte(address),
				}, nil)),
				nil,
			)
		}
		return nil, nil
	})
	return err
}

// IncludeInstances removes processes from the exclusion list and allows
// them to take on roles again.
func (client *RealAdminClient) IncludeInstances(addresses []string) error {
	_, err := client.Database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		exclusionID, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		err = tr.Options().SetPrioritySystemImmediate()
		if err != nil {
			return nil, err
		}

		err = tr.Options().SetAccessSystemKeys()
		if err != nil {
			return nil, err
		}

		err = tr.Options().SetLockAware()
		if err != nil {
			return nil, err
		}

		tr.AddReadConflictKey(fdb.Key("\xff/conf/excluded"))
		tr.Set(fdb.Key("\xff/conf/excluded"), exclusionID[:])
		for _, address := range addresses {
			// Clear an exclusion on this address
			key := bytes.Join([][]byte{
				[]byte("\xff/conf/excluded/"),
				[]byte(address),
			}, nil)
			tr.Clear(fdb.Key(key))

			// Clear an exclusion on any address that starts with this address,
			// followed by a colon
			key = append(key, 58)
			keyRange, err := fdb.PrefixRange(key)
			if err != nil {
				return nil, err
			}
			tr.ClearRange(keyRange)
		}
		return nil, nil
	})
	return err
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
func (client *RealAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	allServers, err := client.Database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		tr.Options().SetReadSystemKeys()
		tr.Options().SetLockAware()
		storageRange, err := fdb.PrefixRange([]byte("\xff/serverList/"))
		if err != nil {
			return nil, err
		}
		storageServers, err := tr.GetRange(storageRange, fdb.RangeOptions{}).GetSliceWithError()
		if err != nil {
			return nil, err
		}

		results := make([]string, 0, len(storageServers))
		for _, server := range storageServers {
			address, err := decodeStorageServerAddress(server.Value)
			if err != nil {
				return nil, err
			}
			results = append(results, address)
		}

		logServers, err := tr.Get(fdb.Key("\xff/logs")).Get()
		currentLogs, offset := decodeLogList(logServers[8:])
		oldLogs, _ := decodeLogList(logServers[offset+8:])

		results = append(results, currentLogs...)
		results = append(results, oldLogs...)

		return results, nil
	})

	if err != nil {
		return nil, err
	}

	remainingServers := make([]string, 0, len(addresses))
	for _, address := range addresses {
		prefix := address + ":"
		for _, activeAddress := range allServers.([]string) {
			if activeAddress == address || strings.HasPrefix(activeAddress, prefix) {
				remainingServers = append(remainingServers, address)
				break
			}
		}
	}

	return remainingServers, nil
}

// KillInstances restarts processes
func (client *RealAdminClient) KillInstances(addresses []string) error {
	_, err := client.Database.Transact(func(tr fdb.Transaction) (interface{}, error) {
		workerRange := fdb.KeyRange{Begin: fdb.Key("\xff\xff/worker_interfaces"), End: fdb.Key("\xff\xff/worker_interfaces")}
		workers, err := tr.GetRange(workerRange, fdb.RangeOptions{}).GetSliceWithError()
		if err != nil {
			return nil, err
		}
		for _, worker := range workers {
			workerAddress := string(worker.Key)
			for _, address := range addresses {
				prefix := address + ":"
				if workerAddress == address || strings.HasPrefix(workerAddress, prefix) {
					tr.Set(fdb.Key("\xff\xff/reboot_worker"), worker.Value)
				}
			}
		}
		return nil, nil
	})
	return err
}

// Close shuts down any resources for the client once it is no longer
// needed.
func (client *RealAdminClient) Close() error {
	return nil
}

func decodeStorageServerAddress(encoded []byte) (string, error) {
	localityStart := uint32(24)
	localityCount := binary.LittleEndian.Uint64(encoded[localityStart : localityStart+8])
	currentIndex := localityStart + 8
	for indexOfLocality := uint64(0); indexOfLocality < localityCount; indexOfLocality++ {
		nameLength := binary.LittleEndian.Uint32(encoded[currentIndex : currentIndex+4])
		currentIndex += nameLength + 4
		if encoded[currentIndex] > 0 {
			valueLength := binary.LittleEndian.Uint32(encoded[currentIndex+1 : currentIndex+5])
			currentIndex += valueLength + 5
		} else {
			currentIndex++
		}
	}

	address := decodeAddress(encoded[currentIndex:])

	return address, nil
}

func decodeLogList(encoded []byte) ([]string, int) {
	addressCount := binary.LittleEndian.Uint32(encoded)
	offset := 4
	addresses := make([]string, 0, addressCount)
	for indexOfAddress := uint32(0); indexOfAddress < addressCount; indexOfAddress++ {
		offset += 16
		addresses = append(addresses, decodeAddress(encoded[offset:]))
		offset += 8
	}
	return addresses, offset
}

func decodeAddress(encoded []byte) string {
	return fmt.Sprintf(
		"%d.%d.%d.%d:%d",
		encoded[3],
		encoded[2],
		encoded[1],
		encoded[0],
		binary.LittleEndian.Uint16(encoded[4:]),
	)
}

// MockAdminClient provides a mock implementation of the cluster admin interface
type MockAdminClient struct {
	Cluster    *fdbtypes.FoundationDBCluster
	KubeClient client.Client
	DatabaseConfiguration
	ExcludedAddresses   []string
	ReincludedAddresses []string
	KilledAddresses     []string
	frozenStatus        *fdbtypes.FoundationDBStatus
}

var adminClientCache = make(map[string]*MockAdminClient)

// NewMockAdminClient creates an admin client for a cluster.
func NewMockAdminClient(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (AdminClient, error) {
	return newMockAdminClientUncast(cluster, kubeClient)
}

func newMockAdminClientUncast(cluster *fdbtypes.FoundationDBCluster, kubeClient client.Client) (*MockAdminClient, error) {
	client := adminClientCache[cluster.Name]
	if client == nil {
		client = &MockAdminClient{Cluster: cluster, KubeClient: kubeClient}
		adminClientCache[cluster.Name] = client
	} else {
		client.Cluster = cluster
	}
	return client, nil
}

// ClearMockAdminClients clears the cache of mock Admin clients
func ClearMockAdminClients() {
	adminClientCache = map[string]*MockAdminClient{}
}

// GetStatus gets the database's status
func (client *MockAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	if client.frozenStatus != nil {
		return client.frozenStatus, nil
	}
	pods := &corev1.PodList{}
	err := client.KubeClient.List(context.TODO(), nil, pods)
	if err != nil {
		return nil, err
	}
	status := &fdbtypes.FoundationDBStatus{
		Cluster: fdbtypes.FoundationDBStatusClusterInfo{
			Processes: make(map[string]fdbtypes.FoundationDBStatusProcessInfo, len(pods.Items)),
		},
	}
	for _, pod := range pods.Items {
		ip := mockPodIP(&pod)
		status.Cluster.Processes[pod.Name] = fdbtypes.FoundationDBStatusProcessInfo{
			Address:     fmt.Sprintf("%s:4500", ip),
			CommandLine: GetStartCommand(client.Cluster, &pod),
		}
	}
	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *MockAdminClient) ConfigureDatabase(configuration DatabaseConfiguration, newDatabase bool) error {
	client.DatabaseConfiguration = configuration
	return nil
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *MockAdminClient) ExcludeInstances(addresses []string) error {
	client.ExcludedAddresses = append(client.ExcludedAddresses, addresses...)
	return nil
}

// IncludeInstances removes processes from the exclusion list and allows
// them to take on roles again.
func (client *MockAdminClient) IncludeInstances(addresses []string) error {
	newExclusions := make([]string, 0, len(client.ExcludedAddresses))
	for _, excludedAddress := range client.ExcludedAddresses {
		included := false
		for _, address := range addresses {
			if address == excludedAddress {
				included = true
				client.ReincludedAddresses = append(client.ReincludedAddresses, address)
				break
			}
		}
		if !included {
			newExclusions = append(newExclusions, excludedAddress)
		}
	}
	client.ExcludedAddresses = newExclusions
	return nil
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
func (client *MockAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	return nil, nil
}

// KillInstances restarts processes
func (client *MockAdminClient) KillInstances(addresses []string) error {
	client.KilledAddresses = append(client.KilledAddresses, addresses...)
	client.UnfreezeStatus()
	return nil
}

// Close shuts down any resources for the client once it is no longer
// needed.
func (client *MockAdminClient) Close() error {
	return nil
}

// FreezeStatus causes the GetStatus method to return its current value until
// UnfreezeStatus is called, or another method is called which would invalidate
// the status.
func (client *MockAdminClient) FreezeStatus() error {
	status, err := client.GetStatus()
	if err != nil {
		return err
	}
	client.frozenStatus = status
	return nil
}

// UnfreezeStatus causes the admin client to start recalculating the status
// on every call to GetStatus
func (client *MockAdminClient) UnfreezeStatus() {
	client.frozenStatus = nil
}

// localityPolicy describes a policy for how data is replicated.
type localityPolicy interface {
	// BinaryRepresentation gets the encoded policy for use in database
	// configuration
	BinaryRepresentation() []byte
}

// singletonPolicy provides a policy that keeps a single replica of data
type singletonPolicy struct {
}

// BinaryRepresentation gets the encoded policy for use in database
// configuration
func (policy *singletonPolicy) BinaryRepresentation() []byte {
	return []byte("\x03\x00\x00\x00One")
}

// acrossPolicy provides a policy that replicates across fault domains
type acrossPolicy struct {
	Count     uint32
	Field     string
	Subpolicy localityPolicy
}

// BinaryRepresentation gets the encoded policy for use in database
// configuration
func (policy *acrossPolicy) BinaryRepresentation() []byte {
	intBuffer := [4]byte{}
	buffer := bytes.NewBuffer(nil)
	binary.LittleEndian.PutUint32(intBuffer[:], 6)
	buffer.Write(intBuffer[:])
	buffer.WriteString("Across")
	binary.LittleEndian.PutUint32(intBuffer[:], uint32(len(policy.Field)))
	buffer.Write(intBuffer[:])
	buffer.WriteString(policy.Field)
	binary.LittleEndian.PutUint32(intBuffer[:], policy.Count)
	buffer.Write(intBuffer[:])
	buffer.Write(policy.Subpolicy.BinaryRepresentation())
	return buffer.Bytes()
}
