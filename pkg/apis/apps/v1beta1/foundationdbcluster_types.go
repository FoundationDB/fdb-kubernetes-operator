/*
Copyright 2019 FoundationDB project authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"fmt"
	"math/rand"
	"reflect"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FoundationDBClusterSpec defines the desired state of FoundationDBCluster
type FoundationDBClusterSpec struct {
	Version               string `json:"version"`
	RoleCounts            `json:"roleCounts,omitempty"`
	ProcessCounts         `json:"processCounts,omitempty"`
	ConnectionString      string                         `json:"connectionString,omitempty"`
	NextInstanceID        int                            `json:"nextInstanceID,omitempty"`
	ReplicationMode       string                         `json:"replicationMode,omitempty"`
	StorageEngine         string                         `json:"storageEngine,omitempty"`
	Configured            bool                           `json:"configured,omitempty"`
	FaultDomain           FoundationDBClusterFaultDomain `json:"faultDomain,omitempty"`
	StorageClass          *string                        `json:"storageClass,omitempty"`
	VolumeSize            string                         `json:"volumeSize"`
	CustomParameters      []string                       `json:"customParameters,omitempty"`
	Resources             *corev1.ResourceRequirements   `json:"resources,omitempty"`
	PendingRemovals       map[string]string              `json:"pendingRemovals,omitempty"`
	InitContainers        []corev1.Container             `json:"initContainers,omitempty"`
	Containers            []corev1.Container             `json:"containers,omitempty"`
	Env                   []corev1.EnvVar                `json:"env,omitempty"`
	Volumes               []corev1.Volume                `json:"volumes,omitempty"`
	VolumeMounts          []corev1.VolumeMount           `json:"volumeMounts,omitempty"`
	EnableTLS             bool                           `json:"enableTls,omitempty"`
	TrustedCAs            []string                       `json:"trustedCAs,omitempty"`
	PeerVerificationRules []string                       `json:"peerVerificationRules,omitempty"`
	LogGroup              string                         `json:"logGroup,omitempty"`
}

// FoundationDBClusterStatus defines the observed state of FoundationDBCluster
type FoundationDBClusterStatus struct {
	FullyReconciled    bool `json:"fullyReconciled"`
	ProcessCounts      `json:"processCounts,omitempty"`
	IncorrectProcesses map[string]int64 `json:"incorrectProcesses,omitempty"`
	MissingProcesses   map[string]int64 `json:"missingProcesses,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FoundationDBCluster is the Schema for the foundationdbclusters API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type FoundationDBCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FoundationDBClusterSpec   `json:"spec,omitempty"`
	Status FoundationDBClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FoundationDBClusterList contains a list of FoundationDBCluster
type FoundationDBClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FoundationDBCluster `json:"items"`
}

// RoleCounts represents the roles whose counts can be customized.
type RoleCounts struct {
	Storage   int `json:"storage,omitempty"`
	Logs      int `json:"logs,omitempty"`
	Proxies   int `json:"proxies,omitempty"`
	Resolvers int `json:"resolvers,omitempty"`
}

// Map returns a map from process classes to the desired count for that role
func (counts RoleCounts) Map() map[string]int {
	countMap := make(map[string]int, len(roleIndices))
	countValue := reflect.ValueOf(counts)
	for role, index := range roleIndices {
		if role != "storage" {
			value := int(countValue.Field(index).Int())
			if value > 0 {
				countMap[role] = value
			}
		}
	}
	return countMap
}

// ProcessCounts represents the number of processes we have for each valid
// process class.
type ProcessCounts struct {
	Storage           int `json:"storage,omitempty"`
	Transaction       int `json:"transaction,omitempty"`
	Stateless         int `json:"stateless,omitempty"`
	Resolution        int `json:"resolution,omitempty"`
	Unset             int `json:"unset,omitempty"`
	Log               int `json:"log,omitempty"`
	Master            int `json:"master,omitempty"`
	ClusterController int `json:"cluster_controller,omitempty"`
	Proxy             int `json:"proxy,omitempty"`
	Resolver          int `json:"resolver,omitempty"`
	Router            int `json:"router,omitempty"`
}

// Map returns a map from process classes to the number of processes with that
// class
func (counts ProcessCounts) Map() map[string]int {
	countMap := make(map[string]int, len(processClassIndices))
	countValue := reflect.ValueOf(counts)
	for processClass, index := range processClassIndices {
		value := int(countValue.Field(index).Int())
		if value > 0 {
			countMap[processClass] = value
		}
	}
	return countMap
}

// IncreaseCount adds to one of the process counts based on the name
func (counts *ProcessCounts) IncreaseCount(name string, amount int) {
	index, present := processClassIndices[name]
	if present {
		countValue := reflect.ValueOf(counts)
		value := countValue.Elem().Field(index)
		value.SetInt(value.Int() + int64(amount))
	}
}

func fieldNames(value interface{}) []string {
	countType := reflect.TypeOf(ProcessCounts{})
	names := make([]string, 0, countType.NumField())
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		names = append(names, tag[0])
	}
	return names
}

func fieldIndices(value interface{}) map[string]int {
	countType := reflect.TypeOf(value)
	indices := make(map[string]int, countType.NumField())
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		indices[tag[0]] = index
	}
	return indices
}

// ProcessClasses provides a consistent ordered list of the supported process
// classes.
var ProcessClasses = fieldNames(ProcessCounts{})
var processClassIndices = fieldIndices(ProcessCounts{})
var roleIndices = fieldIndices(RoleCounts{})

// ApplyDefaultRoleCounts sets the default values for any role
// counts that are currently zero.
func (cluster *FoundationDBCluster) ApplyDefaultRoleCounts() bool {
	changed := false
	if cluster.Spec.RoleCounts.Storage == 0 {
		cluster.Spec.RoleCounts.Storage = 2*cluster.DesiredFaultTolerance() + 1
		changed = true
	}
	if cluster.Spec.RoleCounts.Logs == 0 {
		cluster.Spec.RoleCounts.Logs = 3
		changed = true
	}
	if cluster.Spec.RoleCounts.Proxies == 0 {
		cluster.Spec.RoleCounts.Proxies = 3
		changed = true
	}
	if cluster.Spec.RoleCounts.Resolvers == 0 {
		cluster.Spec.RoleCounts.Resolvers = 1
		changed = true
	}
	return changed
}

func (cluster *FoundationDBCluster) calculateProcessCountFromRole(count int, alternatives ...int) int {
	for _, value := range alternatives {
		if value > 0 {
			return 0
		}
	}
	if count < 0 {
		return 0
	}
	return count
}

func (cluster *FoundationDBCluster) calculateProcessCount(addFaultTolerance bool, counts ...int) int {
	var count = 0

	if cluster.Spec.FaultDomain.ZoneIndex < 0 {
		return -1
	}

	for _, possibleCount := range counts {
		if possibleCount > count {
			count = possibleCount
		}
	}
	if count > 0 {
		if addFaultTolerance {
			count += cluster.DesiredFaultTolerance()
		}
		if cluster.Spec.FaultDomain.Key == "foundationdb.org/kubernetes-cluster" {
			zoneCount := cluster.Spec.FaultDomain.ZoneCount
			if zoneCount < 1 {
				zoneCount = cluster.MinimumFaultDomains() + cluster.DesiredFaultTolerance()
			}
			overflow := count % zoneCount
			count = count / zoneCount
			if cluster.Spec.FaultDomain.ZoneIndex < overflow {
				count++
			}
		}
		return count
	}

	return -1
}

// ApplyDefaultProcessCounts sets the default values for any process
// counts that are currently zero.
func (cluster *FoundationDBCluster) ApplyDefaultProcessCounts() bool {
	changed := false

	if cluster.Spec.ProcessCounts.Storage == 0 {
		cluster.Spec.ProcessCounts.Storage = cluster.calculateProcessCount(false,
			cluster.Spec.RoleCounts.Storage)
		changed = true
	}
	if cluster.Spec.ProcessCounts.Transaction == 0 {
		cluster.Spec.ProcessCounts.Transaction = cluster.calculateProcessCount(true,
			cluster.calculateProcessCountFromRole(cluster.Spec.RoleCounts.Logs, cluster.Spec.ProcessCounts.Log),
		)
		changed = true
	}
	if cluster.Spec.ProcessCounts.Stateless == 0 {
		cluster.Spec.ProcessCounts.Stateless = cluster.calculateProcessCount(true,
			cluster.calculateProcessCountFromRole(1, cluster.Spec.ProcessCounts.Master)+
				cluster.calculateProcessCountFromRole(1, cluster.Spec.ProcessCounts.ClusterController)+
				cluster.calculateProcessCountFromRole(cluster.Spec.RoleCounts.Proxies, cluster.Spec.ProcessCounts.Proxy)+
				cluster.calculateProcessCountFromRole(cluster.Spec.RoleCounts.Resolvers, cluster.Spec.ProcessCounts.Resolution, cluster.Spec.ProcessCounts.Resolver),
		)
		changed = true
	}
	return changed
}

// DesiredFaultTolerance returns the number of replicas we should be able to
// lose when the cluster is at full replication health.
func (cluster *FoundationDBCluster) DesiredFaultTolerance() int {
	switch cluster.Spec.ReplicationMode {
	case "single":
		return 0
	case "double":
		return 1
	case "triple":
		return 2
	default:
		return 0
	}
}

// MinimumFaultDomains returns the number of fault domains the cluster needs
// to function.
func (cluster *FoundationDBCluster) MinimumFaultDomains() int {
	switch cluster.Spec.ReplicationMode {
	case "single":
		return 1
	case "double":
		return 2
	case "triple":
		return 3
	default:
		return 1
	}
}

// DesiredCoordinatorCount returns the number of coordinators to recruit for
// a cluster
func (cluster *FoundationDBCluster) DesiredCoordinatorCount() int {
	return cluster.MinimumFaultDomains() + cluster.DesiredFaultTolerance()
}

// CountsAreSatisfied checks whether the current counts of processes satisfy
// a desired set of counts
func (counts ProcessCounts) CountsAreSatisfied(currentCounts ProcessCounts) bool {
	desiredValue := reflect.ValueOf(counts)
	currentValue := reflect.ValueOf(currentCounts)
	for _, index := range processClassIndices {
		desired := desiredValue.Field(index).Int()
		current := currentValue.Field(index).Int()
		if (desired > 0 || current > 0) && desired != current {
			return false
		}
	}
	return true
}

// FoundationDBStatus describes the status of the cluster as provided by
// FoundationDB itself
type FoundationDBStatus struct {
	Client  FoundationDBStatusClientInfo  `json:"client,omitempty"`
	Cluster FoundationDBStatusClusterInfo `json:"cluster,omitempty"`
}

// FoundationDBStatusClientInfo contains information about the client connection
type FoundationDBStatusClientInfo struct {
	Coordinators FoundationDBStatusCoordinatorInfo `json:"coordinators,omitempty"`
}

// FoundationDBStatusCoordinatorInfo contains information about the clients
// connection to the coordinators
type FoundationDBStatusCoordinatorInfo struct {
	Coordinators []FoundationDBStatusCoordinator `json:"coordinators,omitempty"`
}

// FoundationDBStatusCoordinator contains information about one of the
// coordinators
type FoundationDBStatusCoordinator struct {
	Address   string `json:"address,omitempty"`
	Reachable bool   `json:"reachable,omitempty"`
}

// FoundationDBStatusClusterInfo describes the "cluster" portion of the
// cluster status
type FoundationDBStatusClusterInfo struct {
	Processes map[string]FoundationDBStatusProcessInfo `json:"processes,omitempty"`
}

// FoundationDBStatusProcessInfo describes the "processes" portion of the
// cluster status
type FoundationDBStatusProcessInfo struct {
	Address      string `json:"address,omitempty"`
	ProcessClass string `json:"class_type,omitempty"`
	CommandLine  string `json:"command_line,omitempty"`
	Excluded     bool   `json:"excluded,omitempty"`
}

var alphanum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
var connectionStringPattern = regexp.MustCompile("^([^:@]+):([^:@]+)@(.*)$")

// ConnectionString models the contents of a cluster file in a structured way
type ConnectionString struct {
	DatabaseName string
	GenerationID string
	Coordinators []string
}

// ParseConnectionString parses a connection string from its string
// representation
func ParseConnectionString(str string) (ConnectionString, error) {
	components := connectionStringPattern.FindStringSubmatch(str)
	if components == nil {
		return ConnectionString{}, fmt.Errorf("Invalid connection string %s", str)
	}
	return ConnectionString{
		components[1],
		components[2],
		strings.Split(components[3], ","),
	}, nil
}

// String formats a connection string as a string
func (str *ConnectionString) String() string {
	return fmt.Sprintf("%s:%s@%s", str.DatabaseName, str.GenerationID, strings.Join(str.Coordinators, ","))
}

// GenerateNewGenerationID builds a new generation ID
func (str *ConnectionString) GenerateNewGenerationID() error {
	id := strings.Builder{}
	for i := 0; i < 32; i++ {
		err := id.WriteByte(alphanum[rand.Intn(len(alphanum))])
		if err != nil {
			return err
		}
	}
	str.GenerationID = id.String()
	return nil
}

// GetFullAddress gets the full public address we should use for a process.
// This will include the IP address, the port, and any additional flags.
func (cluster *FoundationDBCluster) GetFullAddress(address string) string {
	var port int
	var suffix string

	if cluster.Spec.EnableTLS {
		port = 4500
		suffix = ":tls"
	} else {
		port = 4501
		suffix = ""
	}
	return fmt.Sprintf("%s:%d%s", address, port, suffix)
}

// HasCoordinators checks whether this connection string matches a set of
// coordinators
func (str *ConnectionString) HasCoordinators(coordinators []string) bool {
	matchedCoordinators := make(map[string]bool, len(str.Coordinators))
	for _, address := range str.Coordinators {
		matchedCoordinators[address] = false
	}
	for _, address := range coordinators {
		_, matched := matchedCoordinators[address]
		if matched {
			matchedCoordinators[address] = true
		} else {
			return false
		}
	}
	for _, matched := range matchedCoordinators {
		if !matched {
			return false
		}
	}
	return true
}

// FoundationDBClusterFaultDomain describes the fault domain that a cluster is
// replicated across
type FoundationDBClusterFaultDomain struct {
	Key       string `json:"key,omitempty"`
	Value     string `json:"value,omitempty"`
	ValueFrom string `json:"valueFrom,omitempty"`
	ZoneCount int    `json:"zoneCount,omitempty"`
	ZoneIndex int    `json:"zoneIndex,omitempty"`
}

func init() {
	SchemeBuilder.Register(&FoundationDBCluster{}, &FoundationDBClusterList{})
}
