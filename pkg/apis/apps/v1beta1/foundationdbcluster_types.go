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
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FoundationDBClusterSpec defines the desired state of FoundationDBCluster
type FoundationDBClusterSpec struct {
	Version          string `json:"version"`
	RoleCounts       `json:"roleCounts,omitempty"`
	ProcessCounts    `json:"processCounts,omitempty"`
	ProcessCountsMap map[string]int               `json:"processCountsMap,omitempty"`
	ConnectionString string                       `json:"connectionString,omitempty"`
	NextInstanceID   int                          `json:"nextInstanceID,omitempty"`
	ReplicationMode  string                       `json:"replicationMode,omitempty"`
	StorageEngine    string                       `json:"storageEngine,omitempty"`
	StorageClass     *string                      `json:"storageClass,omitempty"`
	Configured       bool                         `json:"configured,omitempty"`
	PendingRemovals  map[string]string            `json:"pendingRemovals,omitempty"`
	VolumeSize       string                       `json:"volumeSize"`
	CustomParameters []string                     `json:"customParameters,omitempty"`
	Resources        *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// FoundationDBClusterStatus defines the observed state of FoundationDBCluster
type FoundationDBClusterStatus struct {
	FullyReconciled         bool `json:"fullyReconciled"`
	ProcessCounts           `json:"processCounts,omitempty"`
	ProcessCountsMap        map[string]int   `json:"processCountsMap,omitempty"`
	DesiredProcessCountsMap map[string]int   `json:"desiredProcessCountsMap,omitempty"`
	IncorrectProcesses      map[string]int64 `json:"incorrectProcesses,omitempty"`
	MissingProcesses        map[string]int64 `json:"missingProcesses,omitempty"`
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
	countMap := make(map[string]int, 11)
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

// ProcessClasses provides a consistent ordered list of the supported process
// classes.
var ProcessClasses = func() []string {
	countType := reflect.TypeOf(ProcessCounts{})
	classes := make([]string, 0, countType.NumField())
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		classes = append(classes, tag[0])
	}
	return classes
}()

var processClassIndices = func() map[string]int {
	countType := reflect.TypeOf(ProcessCounts{})
	classes := make(map[string]int, countType.NumField())
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		classes[tag[0]] = index
	}
	return classes
}()

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

func (cluster *FoundationDBCluster) calculateProcessCount(counts ...int) int {
	var final = 0
	for _, count := range counts {
		if count > final {
			final = count
		}
	}
	if final > 0 {
		return final + cluster.DesiredFaultTolerance()
	}
	return -1
}

// ApplyDefaultProcessCounts sets the default values for any process
// counts that are currently zero.
func (cluster *FoundationDBCluster) ApplyDefaultProcessCounts() bool {
	changed := false
	if cluster.Spec.ProcessCounts.Storage == 0 {
		cluster.Spec.ProcessCounts.Storage = cluster.Spec.RoleCounts.Storage
		changed = true
	}
	if cluster.Spec.ProcessCounts.Transaction == 0 {
		cluster.Spec.ProcessCounts.Transaction = cluster.calculateProcessCount(
			cluster.calculateProcessCountFromRole(cluster.Spec.RoleCounts.Logs, cluster.Spec.ProcessCounts.Log),
		)
		changed = true
	}
	if cluster.Spec.ProcessCounts.Stateless == 0 {
		cluster.Spec.ProcessCounts.Stateless = cluster.calculateProcessCount(
			cluster.calculateProcessCountFromRole(1, cluster.Spec.ProcessCounts.Master) +
				cluster.calculateProcessCountFromRole(1, cluster.Spec.ProcessCounts.ClusterController) +
				cluster.calculateProcessCountFromRole(cluster.Spec.RoleCounts.Proxies, cluster.Spec.ProcessCounts.Proxy) +
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

func (cluster *FoundationDBCluster) DesiredProcessCount(processClass string) int {
	count := cluster.Spec.ProcessCountsMap[processClass]
	var minimum int
	if processClass == "storage" {
		switch cluster.Spec.ReplicationMode {
		case "single":
			minimum = 1
		case "double":
			minimum = 3
		default:
			minimum = 1
		}
	}

	if minimum > count {
		return minimum
	}
	return count
}

// DesiredCoordinatorCount returns the number of coordinators to recruit for
// a cluster
func (cluster *FoundationDBCluster) DesiredCoordinatorCount() int {
	switch cluster.Spec.ReplicationMode {
	case "single":
		return 1
	case "double":
		return 3
	default:
		return 1
	}
}

// FoundationDBStatus describes the status of the cluster as provided by
// FoundationDB itself
type FoundationDBStatus struct {
	Cluster FoundationDBStatusClusterInfo `json:"cluster,omitempty"`
}

// FoundationDBStatusClusterInfo describes the "cluster" portion of the
// cluster status
type FoundationDBStatusClusterInfo struct {
	Processes map[string]FoundationDBStatusProcessInfo `json:"processes,omitempty"`
}

// FoundationDBStatusProcessInfo describes the "processes" portion of the
// cluster status
type FoundationDBStatusProcessInfo struct {
	Address     string `json:"address,omitempty"`
	CommandLine string `json:"command_line,omitempty"`
}

func init() {
	SchemeBuilder.Register(&FoundationDBCluster{}, &FoundationDBClusterList{})
}
