package foundationdbcluster

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var maxCommandOutput int = 20

// AdminClient describes an interface for running administrative commands on a
// cluster
type AdminClient interface {
	// GetStatus gets the database's status
	GetStatus() (*fdbtypes.FoundationDBStatus, error)

	// ConfigureDatabase sets the database configuration
	ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error

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

	// ChangeCoordinators changes the coordinator set
	ChangeCoordinators(addresses []string) (string, error)

	// Close shuts down any resources for the client once it is no longer
	// needed.
	Close() error
}

// CliAdminClient provides an implementation of the admin interface using the
// FDB CLI.
type CliAdminClient struct {
	Cluster         *fdbtypes.FoundationDBCluster
	clusterFilePath string
}

// NewCliAdminClient generates an Admin client for a cluster
func NewCliAdminClient(cluster *fdbtypes.FoundationDBCluster, _ client.Client) (AdminClient, error) {
	clusterFile, err := ioutil.TempFile("", "")
	clusterFilePath := clusterFile.Name()
	if err != nil {
		return nil, err
	}

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

	return &CliAdminClient{Cluster: cluster, clusterFilePath: clusterFilePath}, nil
}

type cliCommand struct {
	command string
	timeout int
}

func (client *CliAdminClient) runCommand(command cliCommand) (string, error) {
	version := client.Cluster.Spec.RunningVersion
	version = version[:strings.LastIndex(version, ".")]
	timeout := command.timeout
	if timeout == 0 {
		timeout = 10
	}
	execCommand := exec.Command(
		fmt.Sprintf("%s/%s/fdbcli", os.Getenv("FDB_BINARY_DIR"), version),
		"-C", client.clusterFilePath, "--exec", command.command,
		"--timeout", fmt.Sprintf("%d", timeout),
		"--log", "--log-dir", os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE"),
	)

	log.Info("Running command", "path", execCommand.Path, "args", execCommand.Args)

	output, err := execCommand.Output()
	if err != nil {
		exitError := err.(*exec.ExitError)
		if exitError != nil {
			log.Error(exitError, "Error from FDB command", "code", exitError.ProcessState.ExitCode(), "stderr", string(exitError.Stderr))
		}
		return "", err
	}

	outputString := string(output)
	debugOutput := outputString
	if len(debugOutput) > maxCommandOutput {
		debugOutput = debugOutput[0:maxCommandOutput] + "..."
	}
	log.Info("Command completed", "output", debugOutput)
	return outputString, nil
}

// GetStatus gets the database's status
func (client *CliAdminClient) GetStatus() (*fdbtypes.FoundationDBStatus, error) {
	statusString, err := client.runCommand(cliCommand{command: "status json"})
	if err != nil {
		return nil, err
	}
	status := &fdbtypes.FoundationDBStatus{}
	err = json.Unmarshal([]byte(statusString), &status)
	if err != nil {
		return nil, err
	}
	return status, nil
}

// ConfigureDatabase sets the database configuration
func (client *CliAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
	configurationString, err := configuration.GetConfigurationString()
	if err != nil {
		return err
	}

	if newDatabase {
		configurationString = "new " + configurationString
	}

	_, err = client.runCommand(cliCommand{command: fmt.Sprintf("configure %s", configurationString)})
	return err
}

func removeAddressFlags(addresses []string) []string {
	results := make([]string, len(addresses))
	for _, address := range addresses {
		components := strings.Split(address, ":")
		results = append(results, fmt.Sprintf("%s:%s", components[0], components[1]))
	}
	return results
}

// ExcludeInstances starts evacuating processes so that they can be removed
// from the database.
func (client *CliAdminClient) ExcludeInstances(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}

	_, err := client.runCommand(cliCommand{
		command: fmt.Sprintf(
			"exclude %s",
			strings.Join(removeAddressFlags(addresses), " "),
		)})
	return err
}

// IncludeInstances removes processes from the exclusion list and allows
// them to take on roles again.
func (client *CliAdminClient) IncludeInstances(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"include %s",
		strings.Join(removeAddressFlags(addresses), " "),
	)})
	return err
}

// CanSafelyRemove checks whether it is safe to remove processes from the
// cluster
func (client *CliAdminClient) CanSafelyRemove(addresses []string) ([]string, error) {
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"exclude %s",
		strings.Join(removeAddressFlags(addresses), " "),
	)})
	return nil, err
}

// KillProcesses restarts processes
func (client *CliAdminClient) KillInstances(addresses []string) error {

	if len(addresses) == 0 {
		return nil
	}
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"kill; kill %s; status",
		strings.Join(removeAddressFlags(addresses), " "),
	)})
	return err
}

// SetConnectionString changes the coordinator set
func (client *CliAdminClient) ChangeCoordinators(addresses []string) (string, error) {
	_, err := client.runCommand(cliCommand{command: fmt.Sprintf(
		"coordinators %s",
		strings.Join(addresses, " "),
	)})
	if err != nil {
		return "", err
	}

	connectionStringBytes, err := ioutil.ReadFile(client.clusterFilePath)
	if err != nil {
		return "", err
	}
	return string(connectionStringBytes), nil
}

func (client *CliAdminClient) Close() error {
	err := os.Remove(client.clusterFilePath)
	if err != nil {
		return err
	}
	return nil
}

// MockAdminClient provides a mock implementation of the cluster admin interface
type MockAdminClient struct {
	Cluster               *fdbtypes.FoundationDBCluster
	KubeClient            client.Client
	DatabaseConfiguration *fdbtypes.DatabaseConfiguration
	ExcludedAddresses     []string
	ReincludedAddresses   []string
	KilledAddresses       []string
	frozenStatus          *fdbtypes.FoundationDBStatus
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

	coordinators := make(map[string]bool)
	coordinatorAddresses := strings.Split(strings.Split(client.Cluster.Spec.ConnectionString, "@")[1], ",")
	for _, address := range coordinatorAddresses {
		coordinators[address] = false
	}

	exclusionMap := make(map[string]bool, len(client.ExcludedAddresses))
	for _, address := range client.ExcludedAddresses {
		exclusionMap[address] = true
	}

	for _, pod := range pods.Items {
		ip := mockPodIP(&pod)
		fullAddress := client.Cluster.GetFullAddress(ip)
		_, ipExcluded := exclusionMap[ip]
		_, addressExcluded := exclusionMap[fullAddress]
		excluded := ipExcluded || addressExcluded
		_, isCoordinator := coordinators[fullAddress]
		if isCoordinator && !excluded {
			coordinators[fullAddress] = true
		}
		instance := newFdbInstance(pod)
		command, err := GetStartCommand(client.Cluster, instance)
		if err != nil {
			return nil, err
		}
		status.Cluster.Processes[pod.Name] = fdbtypes.FoundationDBStatusProcessInfo{
			Address:      fullAddress,
			ProcessClass: pod.Labels["fdb-process-class"],
			CommandLine:  command,
			Excluded:     ipExcluded || addressExcluded,
		}
	}

	for address, reachable := range coordinators {
		status.Client.Coordinators.Coordinators = append(status.Client.Coordinators.Coordinators, fdbtypes.FoundationDBStatusCoordinator{
			Address:   address,
			Reachable: reachable,
		})
	}

	status.Client.DatabaseStatus.Available = true
	status.Client.DatabaseStatus.Healthy = true

	if client.DatabaseConfiguration != nil {
		status.Cluster.DatabaseConfiguration = *client.DatabaseConfiguration
	} else {
		status.Cluster.DatabaseConfiguration = client.Cluster.DesiredDatabaseConfiguration()
	}

	status.Cluster.FullReplication = true

	return status, nil
}

// ConfigureDatabase changes the database configuration
func (client *MockAdminClient) ConfigureDatabase(configuration fdbtypes.DatabaseConfiguration, newDatabase bool) error {
	client.DatabaseConfiguration = configuration.DeepCopy()
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

// ChangeCoordinators changes the coordinator set
func (client *MockAdminClient) ChangeCoordinators(addresses []string) (string, error) {
	connectionString, err := fdbtypes.ParseConnectionString(client.Cluster.Spec.ConnectionString)
	if err != nil {
		return "", err
	}
	connectionString.GenerateNewGenerationID()
	connectionString.Coordinators = addresses
	return connectionString.String(), err
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
