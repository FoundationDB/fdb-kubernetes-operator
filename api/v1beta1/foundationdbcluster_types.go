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
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=fdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Generation",type="integer",JSONPath=".metadata.generation",description="Latest generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Reconciled",type="integer",JSONPath=".status.generations.reconciled",description="Last reconciled generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Healthy",type="boolean",JSONPath=".status.health.healthy",description="Database health",priority=0

// FoundationDBCluster is the Schema for the foundationdbclusters API
type FoundationDBCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FoundationDBClusterSpec   `json:"spec,omitempty"`
	Status FoundationDBClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FoundationDBClusterList contains a list of FoundationDBCluster
type FoundationDBClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FoundationDBCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&FoundationDBCluster{}, &FoundationDBClusterList{},
		&FoundationDBBackup{}, &FoundationDBBackupList{},
		&FoundationDBRestore{}, &FoundationDBRestoreList{},
	)
}

// FoundationDBClusterSpec defines the desired state of a cluster.
type FoundationDBClusterSpec struct {
	// Version defines the version of FoundationDB the cluster should run.
	Version string `json:"version"`

	// SidecarVersions defines the build version of the sidecar to run. This
	// maps an FDB version to the corresponding sidecar build version.
	SidecarVersions map[string]int `json:"sidecarVersions,omitempty"`

	// DatabaseConfiguration defines the database configuration.
	DatabaseConfiguration `json:"databaseConfiguration,omitempty"`

	// Processes defines process-level settings.
	Processes map[string]ProcessSettings `json:"processes,omitempty"`

	// ProcessCounts defines the number of processes to configure for each
	// process class. You can generally omit this, to allow the operator to
	// infer the process counts based on the database configuration.
	ProcessCounts `json:"processCounts,omitempty"`

	// SeedConnectionString provides a connection string for the initial
	// reconciliation.
	//
	// After the initial reconciliation, this will not be used.
	SeedConnectionString string `json:"seedConnectionString,omitempty"`

	// FaultDomain defines the rules for what fault domain to replicate across.
	FaultDomain FoundationDBClusterFaultDomain `json:"faultDomain,omitempty"`

	// InstancesToRemove defines the instances that we should remove from the
	// cluster. This list contains the instance IDs.
	InstancesToRemove []string `json:"instancesToRemove,omitempty"`

	// InstancesToRemoveWithoutExclusion defines the instances that we should
	// remove from the cluster without excluding them. This list contains the
	// instance IDs.
	//
	// This should be used for cases where a pod does not have an IP address and
	// you want to remove it and destroy its volume without confirming the data
	// is fully replicated.
	InstancesToRemoveWithoutExclusion []string `json:"instancesToRemoveWithoutExclusion,omitempty"`

	// ConfigMap allows customizing the config map the operator creates.
	ConfigMap *corev1.ConfigMap `json:"configMap,omitempty"`

	// MainContainer defines customization for the foundationdb container.
	MainContainer ContainerOverrides `json:"mainContainer,omitempty"`

	// SidecarContainer defines customization for the
	// foundationdb-kubernetes-sidecar container.
	SidecarContainer ContainerOverrides `json:"sidecarContainer,omitempty"`

	// TrustedCAs defines a list of root CAs the cluster should trust, in PEM
	// format.
	TrustedCAs []string `json:"trustedCAs,omitempty"`

	// SidecarVariables defines Custom variables that the sidecar should make
	// available for substitution in the monitor conf file.
	SidecarVariables []string `json:"sidecarVariables,omitempty"`

	// LogGroup defines the log group to use for the trace logs for the cluster.
	LogGroup string `json:"logGroup,omitempty"`

	// DataCenter defines the data center where these processes are running.
	DataCenter string `json:"dataCenter,omitempty"`

	// DataHall defines the data hall where these processes are running.
	DataHall string `json:"dataHall,omitempty"`

	// AutomationOptions defines customization for enabling or disabling certain
	// operations in the operator.
	AutomationOptions FoundationDBClusterAutomationOptions `json:"automationOptions,omitempty"`

	// InstanceIDPrefix defines a prefix to append to the instance IDs in the
	// locality fields.
	InstanceIDPrefix string `json:"instanceIDPrefix,omitempty"`

	// UpdatePodsByReplacement determines whether we should update pod config
	// by replacing the pods rather than deleting them.
	UpdatePodsByReplacement bool `json:"updatePodsByReplacement,omitempty"`

	// LockOptions allows customizing how we manage locks for global operations.
	LockOptions LockOptions `json:"lockOptions,omitempty"`

	// Services defines the configuration for services that sit in front of our
	// pods.
	Services ServiceConfig `json:"services,omitempty"`

	// IgnoreUpgradabilityChecks determines whether we should skip the check for
	// client compatibility when performing an upgrade.
	IgnoreUpgradabilityChecks bool `json:"ignoreUpgradabilityChecks,omitempty"`

	// SidecarVersion defines the build version of the sidecar to use.
	//
	// Deprecated: Use SidecarVersions instead.
	SidecarVersion int `json:"sidecarVersion,omitempty"`

	// PodLabels defines custom labels to apply to the FDB pods.
	//
	// Deprecated: Use the PodTemplate field instead.
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// Resources defines the resource requirements for the foundationdb
	// containers.
	//
	// Deprecated: Use the PodTemplate field instead.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// InitContainers defines custom init containers for the FDB pods.
	//
	// Deprecated: Use the PodTemplate field instead.
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// Containers defines custom containers for the FDB pods.
	//
	// Deprecated: Use the PodTemplate field instead.
	Containers []corev1.Container `json:"containers,omitempty"`

	// Volumes defines custom volumes for the FDB pods.
	//
	// Deprecated: Use the PodTemplate field instead.
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// PodSecurityContext defines the security context to apply to the FDB pods.
	//
	// Deprecated: Use the PodTemplate field instead.
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// AutomountServiceAccountToken defines whether we should automount the
	// service account tokens in the FDB pods.
	//
	// Deprecated: Use the PodTemplate field instead.
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`

	// NextInstanceID defines the ID to use when creating the next instance.
	//
	// Deprecated: This is no longer used.
	NextInstanceID int `json:"nextInstanceID,omitempty"`

	// StorageClass defines the storage class for the volumes in the cluster.
	//
	// Deprecated: Use the VolumeClaim field instead.
	StorageClass *string `json:"storageClass,omitempty"`

	// VolumeSize defines the size of the volume to use for stateful processes.
	//
	// Deprecated: Use the VolumeClaim field instead.
	VolumeSize string `json:"volumeSize,omitempty"`

	// RunningVersion defines the version of FoundationDB that the cluster is
	// currently running.
	//
	// Deprecated: Consult the running version in the status instead.
	RunningVersion string `json:"runningVersion,omitempty"`

	// ConnectionString defines the contents of the cluster file.
	//
	// Deprecated: You can use SeedConnectionString for bootstrapping, and
	// you can use the ConnectionString in the status to get the latest
	// connection string.
	ConnectionString string `json:"connectionString,omitempty"`

	// Configured defines whether we have configured the database yet.
	// Deprecated: This field has been moved to the status.
	Configured bool `json:"configured,omitempty"`

	// PodTemplate allows customizing the FoundationDB pods.
	// Deprecated: use the Processes field instead.
	PodTemplate *corev1.PodTemplateSpec `json:"podTemplate,omitempty"`

	// VolumeClaim allows customizing the persistent volume claim for the
	// FoundationDB pods.
	// Deprecated: use the Processes field instead.
	VolumeClaim *corev1.PersistentVolumeClaim `json:"volumeClaim,omitempty"`

	// CustomParameters defines additional parameters to pass to the fdbserver
	// processes.
	// Deprecated: use the Processes field instead.
	CustomParameters []string `json:"customParameters,omitempty"`

	// PendingRemovals defines the processes that are pending removal.
	// This maps the name of a pod to its IP address. If a value is left blank,
	// the controller will provide the pod's current IP.
	//
	// Deprecated: To indicate that a process should be removed, use the
	// InstancesToRemove field. To get information about pending removals,
	// use the PendingRemovals field in the status.
	PendingRemovals map[string]string `json:"pendingRemovals,omitempty"`
}

// FoundationDBClusterStatus defines the observed state of FoundationDBCluster
type FoundationDBClusterStatus struct {
	// ProcessCounts defines the number of processes that are currently running
	// in the cluster.
	ProcessCounts `json:"processCounts,omitempty"`

	// IncorrectProcesses provides the processes that do not have the correct
	// configuration.
	//
	// This will map the instance ID to the timestamp when we observed the
	// incorrect configuration.
	IncorrectProcesses map[string]int64 `json:"incorrectProcesses,omitempty"`

	// IncorrectPods provides the pods that do not have the correct
	// spec.
	//
	// This will contain the name of the pod.
	IncorrectPods []string `json:"incorrectPods,omitempty"`

	// FailingPods provides the pods that are not starting correctly.
	//
	// This will contain the name of the pod.
	FailingPods []string `json:"failingPods,omitempty"`

	// MissingProcesses provides the processes that are not reporting to the
	// cluster.
	// This will map the names of the pod to the timestamp when we observed
	// that the process was missing.
	MissingProcesses map[string]int64 `json:"missingProcesses,omitempty"`

	// DatabaseConfiguration provides the running configuration of the database.
	DatabaseConfiguration DatabaseConfiguration `json:"databaseConfiguration,omitempty"`

	// Generations provides information about the latest generation to be
	// reconciled, or to reach other stages at which reconciliation can halt.
	Generations ClusterGenerationStatus `json:"generations,omitempty"`

	// Health provides information about the health of the database.
	Health ClusterHealth `json:"health,omitempty"`

	// RequiredAddresses define that addresses that we need to enable for the
	// processes in the cluster.
	RequiredAddresses RequiredAddressSet `json:"requiredAddresses,omitempty"`

	// HasIncorrectConfigMap indicates whether the latest config map is out
	// of date with the cluster spec.
	HasIncorrectConfigMap bool `json:"hasIncorrectConfigMap,omitempty"`

	// HasIncorrectServiceConfig indicates whether the cluster has service
	// config that is out of date with the cluster spec.
	HasIncorrectServiceConfig bool `json:"hasIncorrectServiceConfig,omitempty"`

	// NeedsNewCoordinators indicates whether the cluster needs to recruit
	// new coordinators to fulfill its fault tolerance requirements.
	NeedsNewCoordinators bool `json:"needsNewCoordinators,omitempty"`

	// RunningVersion defines the version of FoundationDB that the cluster is
	// currently running.
	RunningVersion string `json:"runningVersion,omitempty"`

	// ConnectionString defines the contents of the cluster file.
	ConnectionString string `json:"connectionString,omitempty"`

	// Configured defines whether we have configured the database yet.
	Configured bool `json:"configured,omitempty"`

	// PendingRemovals defines the processes that are pending removal.
	// This maps the instance ID to its removal state.
	PendingRemovals map[string]PendingRemovalState `json:"pendingRemovals,omitempty"`

	// NeedsSidecarConfInConfigMap determines whether we need to include the
	// sidecar conf in the config map even when the latest version should not
	// require it.
	NeedsSidecarConfInConfigMap bool `json:"needsSidecarConfInConfigMap,omitempty"`
}

// ClusterGenerationStatus stores information on which generations have reached
// different stages in reconciliation for the cluster.
type ClusterGenerationStatus struct {
	// Reconciled provides the last generation that was fully reconciled.
	Reconciled int64 `json:"reconciled,omitempty"`

	// NeedsConfigurationChange provides the last generation that is pending
	// a change to configuration.
	NeedsConfigurationChange int64 `json:"needsConfigurationChange,omitempty"`

	// NeedsCoordinatorChange provides the last generation that is pending
	// a change to its coordinators.
	NeedsCoordinatorChange int64 `json:"needsCoordinatorChange,omitempty"`

	// NeedsBounce provides the last generation that is pending a bounce of
	// fdbserver.
	NeedsBounce int64 `json:"needsBounce,omitempty"`

	// NeedsPodDeletion provides the last generation that is pending pods being
	// deleted and recreated.
	NeedsPodDeletion int64 `json:"needsPodDeletion,omitempty"`

	// NeedsShrink provides the last generation that is pending pods being
	// excluded and removed.
	NeedsShrink int64 `json:"needsShrink,omitempty"`

	// NeedsGrow provides the last generation that is pending pods being
	// added.
	NeedsGrow int64 `json:"needsGrow,omitempty"`

	// NeedsMonitorConfUpdate provides the last generation that needs an update
	// through the fdbmonitor conf.
	NeedsMonitorConfUpdate int64 `json:"needsMonitorConfUpdate,omitempty"`

	// DatabaseUnavailable provides the last generation that could not
	// complete reconciliation due to the database being unavailable.
	DatabaseUnavailable int64 `json:"missingDatabaseStatus,omitempty"`

	// HasExtraListeners provides the last generation that could not
	// complete reconciliation because it has more listeners than it is supposed
	// to.
	HasExtraListeners int64 `json:"hasExtraListeners,omitempty"`

	// NeedsServiceUpdate provides the last generation that needs an update
	// to the service config.
	NeedsServiceUpdate int64 `json:"needsServiceUpdate,omitempty"`

	// NeedsBackupAgentUpdate provides the last generation that could not
	// complete reconciliation because the backup agent deployment needs to be
	// updated.
	// Deprecated: This needs to get moved into FoundationDBBackup
	NeedsBackupAgentUpdate int64 `json:"needsBackupAgentUpdate,omitempty"`

	// HasPendingRemoval provides the last generation that has pods that have
	// been excluded but are pending being removed.
	//
	// A cluster in this state is considered reconciled, but we track this in
	// the status to allow users of the operator to track when the removal
	// is fully complete.
	HasPendingRemoval int64 `json:"hasPendingRemoval,omitempty"`

	// HasFailingPods provides the last generation that has pods that are
	// failing to start.
	HasFailingPods int64 `json:"hasFailingPods,omitempty"`
}

// ClusterHealth represents different views into health in the cluster status.
type ClusterHealth struct {
	// Available reports whether the database is accepting reads and writes.
	Available bool `json:"available,omitempty"`

	// Healthy reports whether the database is in a fully healthy state.
	Healthy bool `json:"healthy,omitempty"`

	// FullReplication reports whether all data are fully replicated according
	// to the current replication policy.
	FullReplication bool `json:"fullReplication,omitempty"`

	// DataMovementPriority reports the priority of the highest-priority data
	// movement in the cluster.
	DataMovementPriority int `json:"dataMovementPriority,omitempty"`
}

// PendingRemovalState holds information about a process that is being removed.
type PendingRemovalState struct {
	// The name of the pod that is being removed.
	PodName string `json:"podName,omitempty"`

	// The public address of the process.
	Address string `json:"address,omitempty"`

	// Whether we have started the exclusion.
	ExclusionStarted bool `json:"exclusionStarted,omitempty"`

	// Whether we have completed the exclusion.
	ExclusionComplete bool `json:"exclusionComplete,omitempty"`
}

// RoleCounts represents the roles whose counts can be customized.
type RoleCounts struct {
	Storage    int `json:"storage,omitempty"`
	Logs       int `json:"logs,omitempty"`
	Proxies    int `json:"proxies,omitempty"`
	Resolvers  int `json:"resolvers,omitempty"`
	LogRouters int `json:"log_routers,omitempty"`
	RemoteLogs int `json:"remote_logs,omitempty"`
}

// Map returns a map from process classes to the desired count for that role
func (counts RoleCounts) Map() map[string]int {
	countMap := make(map[string]int, len(roleIndices))
	countValue := reflect.ValueOf(counts)
	for role, index := range roleIndices {
		if role != "storage" {
			value := int(countValue.Field(index).Int())
			countMap[role] = value
		}
	}
	return countMap
}

// VersionFlags defines internal flags for new features in the database.
type VersionFlags struct {
	LogSpill   int `json:"log_spill,omitempty"`
	LogVersion int `json:"log_version,omitempty"`
}

// Map returns a map from process classes to the desired count for that role
func (flags VersionFlags) Map() map[string]int {
	flagMap := make(map[string]int, len(versionFlagIndices))
	flagValue := reflect.ValueOf(flags)
	for flag, index := range versionFlagIndices {
		value := int(flagValue.Field(index).Int())
		flagMap[flag] = value
	}
	return flagMap
}

// ProcessCounts represents the number of processes we have for each valid
// process class.
//
// If one of the counts in the spec is set to 0, we will infer the process count
// for that class from the role counts. If one of the counts in the spec is set
// to -1, we will not create any processes for that class. See
// GetProcessCountsWithDefaults for more information on the rules for inferring
// process counts.
type ProcessCounts struct {
	// Storage defines the number of storage class processes.
	Storage int `json:"storage,omitempty"`

	// Transaction defines the number of transaction class processes.
	Transaction int `json:"transaction,omitempty"`

	// Stateless defines the number of stateless class processes.
	Stateless int `json:"stateless,omitempty"`

	// Resolution defines the number of resolution class processes.
	Resolution        int `json:"resolution,omitempty"`
	Unset             int `json:"unset,omitempty"`
	Log               int `json:"log,omitempty"`
	Master            int `json:"master,omitempty"`
	ClusterController int `json:"cluster_controller,omitempty"`
	Proxy             int `json:"proxy,omitempty"`
	Resolver          int `json:"resolver,omitempty"`
	Router            int `json:"router,omitempty"`
	Ratekeeper        int `json:"ratekeeper,omitempty"`
	DataDistributor   int `json:"data_distributor,omitempty"`
	FastRestore       int `json:"fast_restore,omitempty"`
	BackupWorker      int `json:"backup,omitempty"`
}

// Map returns a map from process classes to the number of processes with that
// class.
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

// IncreaseCount adds to one of the process counts based on the name.
func (counts *ProcessCounts) IncreaseCount(name string, amount int) {
	index, present := processClassIndices[name]
	if present {
		countValue := reflect.ValueOf(counts)
		value := countValue.Elem().Field(index)
		value.SetInt(value.Int() + int64(amount))
	}
}

// fieldNames provides the names of fields on a structure.
func fieldNames(value interface{}) []string {
	countType := reflect.TypeOf(value)
	names := make([]string, 0, countType.NumField())
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		names = append(names, tag[0])
	}
	return names
}

// fieldIndices provides a map from the names of fields in a structure to the
// index of each field in the list of fields.
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

// processClassIndices provides the indices of each process class in the list
// of process classes.
var processClassIndices = fieldIndices(ProcessCounts{})

// roleNames provides a consistent ordered list of the supported roles.
var roleNames = fieldNames(RoleCounts{})

// roleIndices provides the indices of each role in the list of roles.
var roleIndices = fieldIndices(RoleCounts{})

// versionFlagIndices provides the indices of each flag in the list of supported
// version flags..
var versionFlagIndices = fieldIndices(VersionFlags{})

// FoundationDBClusterAutomationOptions provides flags for enabling or disabling
// operations that can be performed on a cluster.
type FoundationDBClusterAutomationOptions struct {
	// ConfigureDatabase defines whether the operator is allowed to reconfigure
	// the database.
	ConfigureDatabase *bool `json:"configureDatabase,omitempty"`

	// KillProcesses defines whether the operator is allowed to bounce fdbserver
	// processes.
	KillProcesses *bool `json:"killProcesses,omitempty"`

	// DeletePods defines whether the operator is allowed to delete pods in
	// order to recreate them.
	DeletePods *bool `json:"deletePods,omitempty"`
}

// ProcessSettings defines process-level settings.
type ProcessSettings struct {
	// PodTemplate allows customizing the pod.
	PodTemplate *corev1.PodTemplateSpec `json:"podTemplate,omitempty"`

	// VolumeClaim allows customizing the persistent volume claim for the
	// pod.
	// Deprecated: Use the VolumeClaimTemplate field instead.
	VolumeClaim *corev1.PersistentVolumeClaim `json:"volumeClaim,omitempty"`

	// VolumeClaimTemplate allows customizing the persistent volume claim for the
	// pod.
	VolumeClaimTemplate *corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`

	// CustomParameters defines additional parameters to pass to the fdbserver
	// process.
	CustomParameters *[]string `json:"customParameters,omitempty"`
}

// GetProcessSettings gets settings for a process.
func (cluster *FoundationDBCluster) GetProcessSettings(processClass string) ProcessSettings {
	merged := ProcessSettings{}
	entries := make([]ProcessSettings, 0, 2)

	entry, present := cluster.Spec.Processes[processClass]
	if present {
		entries = append(entries, entry)
	}

	entries = append(entries, cluster.Spec.Processes["general"])

	for _, entry := range entries {
		if merged.PodTemplate == nil {
			merged.PodTemplate = entry.PodTemplate
		}
		if merged.VolumeClaim == nil {
			merged.VolumeClaim = entry.VolumeClaim
		}
		if merged.VolumeClaimTemplate == nil {
			merged.VolumeClaimTemplate = entry.VolumeClaimTemplate
		}
		if merged.CustomParameters == nil {
			merged.CustomParameters = entry.CustomParameters
		}
	}
	return merged
}

// GetRoleCountsWithDefaults gets the role counts from the cluster spec and
// fills in default values for any role counts that are 0.
//
// The default Storage value will be 2F + 1, where F is the cluster's fault
// tolerance.
//
// The default Logs value will be 3.
//
// The default Proxies value will be 3.
//
// The default Resolvers value will be 1.
//
// The default RemoteLogs value will be equal to the Logs value when the
// UsableRegions is greater than 1. It will be equal to -1 when the
// UsableRegions is less than or equal to 1.
//
// The default LogRouters value will be equal to 3 times the Logs value when
// the UsableRegions is greater than 1. It will be equal to -1 when the
// UsableRegions is less than or equal to 1.
func (cluster *FoundationDBCluster) GetRoleCountsWithDefaults() RoleCounts {
	counts := cluster.Spec.RoleCounts.DeepCopy()
	if counts.Storage == 0 {
		counts.Storage = 2*cluster.DesiredFaultTolerance() + 1
	}
	if counts.Logs == 0 {
		counts.Logs = 3
	}
	if counts.Proxies == 0 {
		counts.Proxies = 3
	}
	if counts.Resolvers == 0 {
		counts.Resolvers = 1
	}
	if counts.RemoteLogs == 0 {
		if cluster.Spec.UsableRegions > 1 {
			counts.RemoteLogs = counts.Logs
		} else {
			counts.RemoteLogs = -1
		}
	}
	if counts.LogRouters == 0 {
		if cluster.Spec.UsableRegions > 1 {
			counts.LogRouters = counts.Logs
		} else {
			counts.LogRouters = -1
		}
	}
	return *counts
}

// calculateProcessCount determines the process count from a given role count.
//
// alternatives provides a list of other process counts that can fulfill this
// role instead. If any of those process counts is positive, then this will
// return 0.
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

// calculateProcessCount calculates the process count for a process class based
// on the counts for the roles it can fulfill.
//
// If addFaultTolerance is true, this will add the cluster's desired fault
// tolerance to the result.
//
// If the cluster is using multi-KC replication, this will divide the total
// count across the number of KCs in the data center.
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

// GetProcessCountsWithDefaults gets the process counts from the cluster spec
// and fills in default values for any counts that are 0.
func (cluster *FoundationDBCluster) GetProcessCountsWithDefaults() (ProcessCounts, error) {
	roleCounts := cluster.GetRoleCountsWithDefaults()
	processCounts := cluster.Spec.ProcessCounts.DeepCopy()

	isSatellite := false
	isMain := false

	satelliteLogs := 0
	for _, region := range cluster.Spec.DatabaseConfiguration.Regions {
		for _, dataCenter := range region.DataCenters {
			if dataCenter.ID == cluster.Spec.DataCenter {
				if dataCenter.Satellite == 0 {
					isMain = true
				} else {
					isSatellite = true
					if region.SatelliteLogs > satelliteLogs {
						satelliteLogs = region.SatelliteLogs
					}
				}
			}
		}
	}

	if isSatellite && !isMain {
		if processCounts.Log == 0 {
			processCounts.Log = 1 + satelliteLogs
			return *processCounts, nil
		}
	}

	version, err := ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return ProcessCounts{}, err
	}

	if processCounts.Storage == 0 {
		processCounts.Storage = cluster.calculateProcessCount(false,
			roleCounts.Storage)
	}
	if processCounts.Log == 0 {
		processCounts.Log = cluster.calculateProcessCount(true,
			cluster.calculateProcessCountFromRole(roleCounts.Logs+satelliteLogs, processCounts.Log),
			cluster.calculateProcessCountFromRole(roleCounts.RemoteLogs, processCounts.Log),
		)
	}
	if processCounts.Stateless == 0 {
		primaryStatelessCount := cluster.calculateProcessCountFromRole(1, processCounts.Master) +
			cluster.calculateProcessCountFromRole(1, processCounts.ClusterController) +
			cluster.calculateProcessCountFromRole(roleCounts.Proxies, processCounts.Proxy) +
			cluster.calculateProcessCountFromRole(roleCounts.Resolvers, processCounts.Resolution, processCounts.Resolver)
		if version.HasRatekeeperRole() {
			primaryStatelessCount += cluster.calculateProcessCountFromRole(1, processCounts.Ratekeeper) +
				cluster.calculateProcessCountFromRole(1, processCounts.DataDistributor)
		}

		processCounts.Stateless = cluster.calculateProcessCount(true,
			primaryStatelessCount,
			cluster.calculateProcessCountFromRole(roleCounts.LogRouters),
		)
	}
	return *processCounts, nil
}

// DesiredFaultTolerance returns the number of replicas we should be able to
// lose when the cluster is at full replication health.
func (cluster *FoundationDBCluster) DesiredFaultTolerance() int {
	switch cluster.Spec.RedundancyMode {
	case "single":
		return 0
	case "double", "":
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
	switch cluster.Spec.RedundancyMode {
	case "single":
		return 1
	case "double", "":
		return 2
	case "triple":
		return 3
	default:
		return 1
	}
}

// DesiredCoordinatorCount returns the number of coordinators to recruit for
// a cluster.
func (cluster *FoundationDBCluster) DesiredCoordinatorCount() int {
	if cluster.Spec.UsableRegions > 1 {
		return 9
	}
	return cluster.MinimumFaultDomains() + cluster.DesiredFaultTolerance()
}

// CheckReconciliation compares the spec and the status to determine if
// reconciliation is complete.
func (cluster *FoundationDBCluster) CheckReconciliation() (bool, error) {
	var reconciled = true
	if !cluster.Status.Configured {
		cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
		return false, nil
	}

	cluster.Status.Generations = ClusterGenerationStatus{Reconciled: cluster.Status.Generations.Reconciled}

	if len(cluster.Status.PendingRemovals) > 0 {
		needsShrink := false
		for _, state := range cluster.Status.PendingRemovals {
			if !state.ExclusionComplete {
				needsShrink = true
			}
		}
		if needsShrink {
			cluster.Status.Generations.NeedsShrink = cluster.ObjectMeta.Generation
			reconciled = false
		} else {
			cluster.Status.Generations.HasPendingRemoval = cluster.ObjectMeta.Generation
		}
	} else if len(cluster.Spec.PendingRemovals) > 0 {
		cluster.Status.Generations.NeedsShrink = cluster.ObjectMeta.Generation
		reconciled = false
	}

	desiredCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return false, err
	}

	diff := desiredCounts.diff(cluster.Status.ProcessCounts)

	for _, delta := range diff {
		if delta > 0 {
			cluster.Status.Generations.NeedsGrow = cluster.ObjectMeta.Generation
			reconciled = false
		} else if delta < 0 {
			cluster.Status.Generations.NeedsShrink = cluster.ObjectMeta.Generation
			reconciled = false
		}
	}

	if len(cluster.Status.IncorrectProcesses) > 0 {
		cluster.Status.Generations.NeedsMonitorConfUpdate = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if len(cluster.Status.IncorrectPods) > 0 {
		cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if len(cluster.Status.FailingPods) > 0 {
		cluster.Status.Generations.HasFailingPods = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if !cluster.Status.Health.Available {
		cluster.Status.Generations.DatabaseUnavailable = cluster.ObjectMeta.Generation
		reconciled = false
	}

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	if !reflect.DeepEqual(cluster.Status.DatabaseConfiguration, desiredConfiguration) {
		cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if cluster.Status.HasIncorrectConfigMap {
		cluster.Status.Generations.NeedsMonitorConfUpdate = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if cluster.Status.HasIncorrectServiceConfig {
		cluster.Status.Generations.NeedsServiceUpdate = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if cluster.Status.NeedsNewCoordinators {
		cluster.Status.Generations.NeedsCoordinatorChange = cluster.ObjectMeta.Generation
		reconciled = false
	}

	desiredAddressSet := RequiredAddressSet{}
	if cluster.Spec.MainContainer.EnableTLS {
		desiredAddressSet.TLS = true
	} else {
		desiredAddressSet.NonTLS = true
	}

	if cluster.Status.RequiredAddresses != desiredAddressSet {
		cluster.Status.Generations.HasExtraListeners = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if reconciled {
		cluster.Status.Generations.Reconciled = cluster.ObjectMeta.Generation
	}
	return reconciled, nil
}

// CountsAreSatisfied checks whether the current counts of processes satisfy
// a desired set of counts.
func (counts ProcessCounts) CountsAreSatisfied(currentCounts ProcessCounts) bool {
	return len(counts.diff(currentCounts)) == 0
}

// diff gets the diff between two sets of process counts.
func (counts ProcessCounts) diff(currentCounts ProcessCounts) map[string]int64 {
	diff := make(map[string]int64)
	desiredValue := reflect.ValueOf(counts)
	currentValue := reflect.ValueOf(currentCounts)
	for label, index := range processClassIndices {
		desired := desiredValue.Field(index).Int()
		current := currentValue.Field(index).Int()
		if (desired > 0 || current > 0) && desired != current {
			diff[label] = desired - current
		}
	}
	return diff
}

// FoundationDBStatus describes the status of the cluster as provided by
// FoundationDB itself.
type FoundationDBStatus struct {
	// Client provides the client section of the status.
	Client FoundationDBStatusLocalClientInfo `json:"client,omitempty"`

	// Cluster provides the cluster section of the status.
	Cluster FoundationDBStatusClusterInfo `json:"cluster,omitempty"`
}

// FoundationDBStatusLocalClientInfo contains information about the
// client connection from the process getting the status.
type FoundationDBStatusLocalClientInfo struct {
	// Coordinators provides information about the cluster's coordinators.
	Coordinators FoundationDBStatusCoordinatorInfo `json:"coordinators,omitempty"`

	// DatabaseStatus provides a summary of the database's health.
	DatabaseStatus FoundationDBStatusClientDBStatus `json:"database_status,omitempty"`
}

// FoundationDBStatusCoordinatorInfo contains information about the client's
// connection to the coordinators.
type FoundationDBStatusCoordinatorInfo struct {
	// Coordinators provides a list with coordinator details.
	Coordinators []FoundationDBStatusCoordinator `json:"coordinators,omitempty"`
}

// FoundationDBStatusCoordinator contains information about one of the
// coordinators.
type FoundationDBStatusCoordinator struct {
	// Address provides the coordinator's address.
	Address string `json:"address,omitempty"`

	// Reachable indicates whether the coordinator is reachable.
	Reachable bool `json:"reachable,omitempty"`
}

// FoundationDBStatusClusterInfo describes the "cluster" portion of the
// cluster status
type FoundationDBStatusClusterInfo struct {
	// DatabaseConfiguration describes the current configuration of the
	// database.
	DatabaseConfiguration DatabaseConfiguration `json:"configuration,omitempty"`

	// Processes provides details on the processes that are reporting to the
	// cluster.
	Processes map[string]FoundationDBStatusProcessInfo `json:"processes,omitempty"`

	// Data provides information about the data in the database.
	Data FoundationDBStatusDataStatistics `json:"data,omitempty"`

	// FullReplication indicates whether the database is fully replicated.
	FullReplication bool `json:"full_replication,omitempty"`

	// Clients provides information about clients that are connected to the
	// database.
	Clients FoundationDBStatusClusterClientInfo `json:"clients,omitempty"`

	// Layers provides information about layers that are running against the
	// cluster.
	Layers FoundationDBStatusLayerInfo `json:"layers,omitempty"`
}

// FoundationDBStatusProcessInfo describes the "processes" portion of the
// cluster status
type FoundationDBStatusProcessInfo struct {
	// ProcessAddresses provides the addresses of the process.
	ProcessAddresses ProcessAddressSlice `json:"address,omitempty"`

	// ProcessClass provides the process class the process has been given.
	ProcessClass string `json:"class_type,omitempty"`

	// CommandLine provides the command-line invocation for the process.
	CommandLine string `json:"command_line,omitempty"`

	// Excluded indicates whether the process has been excluded.
	Excluded bool `json:"excluded,omitempty"`

	// The locality information for the process.
	Locality map[string]string `json:"locality,omitempty"`

	// The version of FoundationDB the process is running.
	Version string `json:"version,omitempty"`

	// The time that the process has been up for.
	UptimeSeconds float64 `json:"uptime_seconds,omitempty"`
}

// FoundationDBStatusDataStatistics provides information about the data in
// the database
type FoundationDBStatusDataStatistics struct {
	// KVBytes provides the total Key Value Bytes in the database.
	KVBytes int `json:"total_kv_size_bytes,omitempty"`

	// MovingData provides information about the current data movement.
	MovingData FoundationDBStatusMovingData `json:"moving_data,omitempty"`
}

// FoundationDBStatusMovingData provides information about the current data
// movement
type FoundationDBStatusMovingData struct {
	// HighestPriority provides the priority of the highest-priority data
	// movement.
	HighestPriority int `json:"highest_priority,omitempty"`

	// InFlightBytes provides how many bytes are being actively moved.
	InFlightBytes int `json:"in_flight_bytes,omitempty"`

	// InQueueBytes provides how many bytes are pending data movement.
	InQueueBytes int `json:"in_queue_bytes,omitempty"`
}

// alphanum provides the characters that are used for the generation ID in the
// connection string.
var alphanum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// connectionStringPattern provides a regular expression for parsing the
// connection string.
var connectionStringPattern = regexp.MustCompile("(?m)^([^#][^:@]+):([^:@]+)@(.*)$")

// ConnectionString models the contents of a cluster file in a structured way
type ConnectionString struct {
	// DatabaseName provides an identifier for the database which persists
	// across coordinator changes.
	DatabaseName string

	// GenerationID provides a unique ID for the current generation of
	// coordinators.
	GenerationID string

	// Coordinators provides the addresses of the current coordinators.
	Coordinators ProcessAddressSlice
}

// ParseConnectionString parses a connection string from its string
// representation
func ParseConnectionString(str string) (ConnectionString, error) {
	components := connectionStringPattern.FindStringSubmatch(str)
	if components == nil {
		return ConnectionString{}, fmt.Errorf("invalid connection string %s", str)
	}

	coordinatorsStrings := strings.Split(components[3], ",")
	coordindators := make([]ProcessAddress, len(coordinatorsStrings))

	for idx, coordinatorsString := range coordinatorsStrings {
		coordindatorAddress, err := ParseProcessAddress(coordinatorsString)
		if err != nil {
			return ConnectionString{}, err
		}
		coordindators[idx] = coordindatorAddress
	}

	return ConnectionString{
		components[1],
		components[2],
		coordindators,
	}, nil
}

// String formats a connection string as a string
func (str *ConnectionString) String() string {
	return fmt.Sprintf("%s:%s@%s", str.DatabaseName, str.GenerationID, ProcessAddressesString(str.Coordinators, ","))
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

// ProcessAddressSlice is a helper type for the json parsing
type ProcessAddressSlice []ProcessAddress

// ProcessAddress provides a structured address for a process.
type ProcessAddress struct {
	// TODO (johscheuer) change to net.IP
	IPAddress string
	Port      int
	Flags     map[string]bool
}

// UnmarshalJSON defines the parsing method for the address field from JSON to struct
func (p *ProcessAddressSlice) UnmarshalJSON(data []byte) error {
	trimmed := strings.Trim(string(data), "\"")

	for _, addr := range strings.Split(trimmed, ",") {
		parsedAddr, err := ParseProcessAddress(addr)
		if err != nil {
			return err
		}

		*p = append(*p, parsedAddr)
	}

	return nil
}

// MarshalJSON defines the parsing method for the address field from struct to JSON
func (p ProcessAddressSlice) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", ProcessAddressesString(p, ","))), nil
}

// Equal checks if two ProcessAddress are the same
func (address ProcessAddress) Equal(addressB ProcessAddress) bool {
	if address.IPAddress != addressB.IPAddress {
		return false
	}

	if address.Port != addressB.Port {
		return false
	}

	if len(address.Flags) != len(addressB.Flags) {
		return false
	}

	for k, v := range address.Flags {
		if v != addressB.Flags[k] {
			return false
		}
	}

	return true
}

// ParseProcessAddress parses a structured address from its string
// representation.
func ParseProcessAddress(address string) (ProcessAddress, error) {
	if !strings.Contains(address, "[") && !strings.Contains(address, "]") {
		return parseProcessAddressWithIPv4(address)
	}

	return parseProcessAddressWithIPv6(address)
}

func parseProcessAddressWithIPv6(address string) (ProcessAddress, error) {
	result := ProcessAddress{}

	components := strings.Split(address, "]")
	if len(components) < 2 {
		return result, fmt.Errorf("invalid address: %s", address)
	}

	result.IPAddress = strings.TrimLeft(components[0], "[")

	portAndFlags := strings.Split(strings.TrimLeft(components[1], ":"), ":")
	port, err := strconv.Atoi(portAndFlags[0])
	if err != nil {
		return result, err
	}
	result.Port = port

	if len(portAndFlags) > 1 {
		result.Flags = make(map[string]bool, len(portAndFlags)-1)
		for _, flag := range portAndFlags[1:] {
			result.Flags[flag] = true
		}
	}

	return result, nil
}

func parseProcessAddressWithIPv4(address string) (ProcessAddress, error) {
	result := ProcessAddress{}

	components := strings.Split(address, ":")
	if len(components) < 2 {
		return result, fmt.Errorf("invalid address: %s", address)
	}

	result.IPAddress = components[0]

	port, err := strconv.Atoi(components[1])
	if err != nil {
		return result, err
	}
	result.Port = port

	if len(components) > 2 {
		result.Flags = make(map[string]bool, len(components)-2)
		for _, flag := range components[2:] {
			result.Flags[flag] = true
		}
	}

	return result, nil
}

// String gets the string representation of a ProcessAddress.
func (address ProcessAddress) String() string {
	result := net.JoinHostPort(address.IPAddress, strconv.Itoa(address.Port))

	flags := make([]string, 0, len(address.Flags))
	for flag, set := range address.Flags {
		if set {
			flags = append(flags, flag)
		}
	}

	sort.Slice(flags, func(i int, j int) bool {
		return flags[i] < flags[j]
	})

	// Add result as the first element in the slice
	flags = append(flags, "")
	copy(flags[1:], flags[0:])
	flags[0] = result

	return strings.Join(flags, ":")
}

// StringWithoutFlags gets the string representation of a ProcessAddress without adding the flags.
func (address ProcessAddress) StringWithoutFlags() string {
	return net.JoinHostPort(address.IPAddress, strconv.Itoa(address.Port))
}

// GetFullAddress gets the full public address we should use for a process.
// This will include the IP address, the port, and any additional flags.
func (cluster *FoundationDBCluster) GetFullAddress(ipAddress string) ProcessAddress {
	addresses := cluster.GetFullAddressList(ipAddress, true)
	if len(addresses) < 1 {
		return ProcessAddress{}
	}

	// First element will always be the primary
	return addresses[0]
}

// ProcessAddressesString converts a slice of ProcessAddress to a string separating the
// ProcessAddresses
func ProcessAddressesString(p []ProcessAddress, sep string) string {
	var s strings.Builder
	lenP := len(p)
	for idx, addr := range p {
		s.WriteString(addr.String())
		if idx < lenP-1 {
			s.WriteString(sep)
		}
	}

	return s.String()
}

// GetFullAddressList gets the full list of public addresses we should use for a
// process.
//
// This will include the IP address, the port, and any additional flags.
//
// If a process needs multiple addresses, this will include all of them,
// separated by commas. If you pass true for primaryOnly, this will return only
// the primary address.
func (cluster *FoundationDBCluster) GetFullAddressList(ipAddress string, primaryOnly bool) []ProcessAddress {
	// TODO (johscheuer) optimize and don't parse it multiple times
	addressMap := make(map[string]bool)
	if cluster.Status.RequiredAddresses.TLS {
		processAddress := ProcessAddress{
			IPAddress: ipAddress,
			Port:      4500,
			Flags:     map[string]bool{"tls": true},
		}
		addressMap[processAddress.String()] = cluster.Spec.MainContainer.EnableTLS
	}
	if cluster.Status.RequiredAddresses.NonTLS {
		processAddress := ProcessAddress{
			IPAddress: ipAddress,
			Port:      4501,
		}
		addressMap[processAddress.String()] = !cluster.Spec.MainContainer.EnableTLS
	}

	addresses := make([]ProcessAddress, 1, 1+len(addressMap))
	for address, primary := range addressMap {
		parsedAddr, err := ParseProcessAddress(address)
		if err != nil {
			// TODO (johscheuer): log error
			continue
		}

		if primary {
			addresses[0] = parsedAddr
		} else if !primaryOnly {
			addresses = append(addresses, parsedAddr)
		}
	}

	return addresses
}

// GetFullSidecarVersion gets the version of the image for the sidecar,
// including the main FoundationDB version and the sidecar version suffix.
func (cluster *FoundationDBCluster) GetFullSidecarVersion(useRunningVersion bool) string {
	version := ""
	if useRunningVersion {
		version = cluster.Status.RunningVersion
	}
	if version == "" {
		version = cluster.Spec.Version
	}
	sidecarVersion := cluster.Spec.SidecarVersions[version]
	if sidecarVersion < 1 {
		sidecarVersion = cluster.Spec.SidecarVersion
	}
	if sidecarVersion < 1 {
		sidecarVersion = 1
	}
	return fmt.Sprintf("%s-%d", version, sidecarVersion)
}

// HasCoordinators checks whether this connection string matches a set of
// coordinators.
func (str *ConnectionString) HasCoordinators(coordinators []ProcessAddress) bool {
	matchedCoordinators := make(map[string]bool, len(str.Coordinators))

	for _, address := range str.Coordinators {
		matchedCoordinators[address.String()] = false
	}

	for _, address := range coordinators {
		_, matched := matchedCoordinators[address.String()]
		if matched {
			matchedCoordinators[address.String()] = true
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
// replicated across.
type FoundationDBClusterFaultDomain struct {
	// Key provides a topology key for the fault domain to replicate across.
	Key string `json:"key,omitempty"`

	// Value provides a harcoded value to use for the zoneid for the pods.
	Value string `json:"value,omitempty"`

	// ValueFrom provides a field selector to use as the source of the fault
	// domain.
	ValueFrom string `json:"valueFrom,omitempty"`

	// ZoneCount provides the number of fault domains in the data center where
	// these processes are running. This is only used in the
	// `kubernetes-cluster` fault domain strategy.
	ZoneCount int `json:"zoneCount,omitempty"`

	// ZoneIndex provides the index of this Kubernetes cluster in the list of
	// KCs in the data center. This is only used in the `kubernetes-cluster`
	// fault domain strategy.
	ZoneIndex int `json:"zoneIndex,omitempty"`
}

// DatabaseConfiguration represents the configuration of the database
type DatabaseConfiguration struct {
	// RedundancyMode defines the core replication factor for the database.
	RedundancyMode string `json:"redundancy_mode,omitempty"`

	// StorageEngine defines the storage engine the database uses.
	StorageEngine string `json:"storage_engine,omitempty"`

	// UsableRegions defines how many regions the database should store data in.
	UsableRegions int `json:"usable_regions,omitempty"`

	// Regions defines the regions that the database can replicate in.
	Regions []Region `json:"regions,omitempty"`

	// RoleCounts defines how many processes the database should recruit for
	// each role.
	RoleCounts `json:""`

	// VersionFlags defines internal flags for testing new features in the
	// database.
	VersionFlags `json:""`
}

// Region represents a region in the database configuration
type Region struct {
	// The data centers in this region.
	DataCenters []DataCenter `json:"datacenters,omitempty"`

	// The number of satellite logs that we should recruit.
	SatelliteLogs int `json:"satellite_logs,omitempty"`

	// The replication strategy for satellite logs.
	SatelliteRedundancyMode string `json:"satellite_redundancy_mode,omitempty"`
}

// DataCenter represents a data center in the region configuration
type DataCenter struct {
	// The ID of the data center. This must match the dcid locality field.
	ID string `json:"id,omitempty"`

	// The priority of this data center when we have to choose a location.
	// Higher priorities are preferred over lower priorities.
	Priority int `json:"priority,omitempty"`

	// Satellite indicates whether the data center is serving as a satellite for
	// the region. A value of 1 indicates that it is a satellite, and a value of
	// 0 indicates that it is not a satellite.
	Satellite int `json:"satellite,omitempty"`
}

// FoundationDBStatusClientDBStatus represents the databaseStatus field in the
// JSON database status
type FoundationDBStatusClientDBStatus struct {
	// Available indicates whether the database is accepting traffic.
	Available bool `json:"available,omitempty"`

	// Healthy indicates whether the database is fully healthy.
	Healthy bool `json:"healthy,omitempty"`
}

// FoundationDBStatusClusterClientInfo represents the connected client details in the
// cluster status.
type FoundationDBStatusClusterClientInfo struct {
	// Count provides the number of clients connected to the database.
	Count int `json:"count,omitempty"`

	// SupportedVersions provides information about the versions supported by
	// the connected clients.
	SupportedVersions []FoundationDBStatusSupportedVersion `json:"supported_versions,omitempty"`
}

// FoundationDBStatusSupportedVersion provides information about a version of
// FDB supported by the connected clients.
type FoundationDBStatusSupportedVersion struct {
	// ClientVersion provides the version of FDB the client is connecting
	// through.
	ClientVersion string `json:"client_version,omitempty"`

	// ConnectedClient provides the clients that are using this version.
	ConnectedClients []FoundationDBStatusConnectedClient `json:"connected_clients"`

	// MaxProtocolClients provides the clients that are using this version as
	// their highest supported protocol version.
	MaxProtocolClients []FoundationDBStatusConnectedClient `json:"max_protocol_clients"`

	// ProtocolVersion is the version of the wire protocol the client is using.
	ProtocolVersion string `json:"protocol_version,omitempty"`

	// SourceVersion is the version of the source code that the client library
	// was built from.
	SourceVersion string `json:"source_version,omitempty"`
}

// FoundationDBStatusConnectedClient provides information about a client that
// is connected to the database.
type FoundationDBStatusConnectedClient struct {
	// Address provides the address the client is connecting from.
	Address string `json:"address,omitempty"`

	// LogGroup provides the trace log group the client has set.
	LogGroup string `json:"log_group,omitempty"`
}

// Description returns a string description of the a connected client.
func (client FoundationDBStatusConnectedClient) Description() string {
	if client.LogGroup == "default" || client.LogGroup == "" {
		return client.Address
	}
	return fmt.Sprintf("%s (%s)", client.Address, client.LogGroup)
}

// FoundationDBStatusLayerInfo provides information about layers that are
// running against the cluster.
type FoundationDBStatusLayerInfo struct {
	// Backup provides information about backups that have been started.
	Backup FoundationDBStatusBackupInfo `json:"backup,omitempty"`

	// The error from the layer status.
	Error string `json:"_error,omitempty"`
}

// FoundationDBStatusBackupInfo provides information about backups that have been started.
type FoundationDBStatusBackupInfo struct {
	// Paused tells whether the backups are paused.
	Paused bool `json:"paused,omitempty"`

	// Tags provides information about specific backups.
	Tags map[string]FoundationDBStatusBackupTag `json:"tags,omitempty"`
}

// FoundationDBStatusBackupTag provides information about a backup under a tag
// in the cluster status.
type FoundationDBStatusBackupTag struct {
	CurrentContainer string `json:"current_container,omitempty"`
	RunningBackup    bool   `json:"running_backup,omitempty"`
	Restorable       bool   `json:"running_backup_is_restorable,omitempty"`
}

// ContainerOverrides provides options for customizing a container created by
// the operator.
type ContainerOverrides struct {

	// EnableTLS controls whether we should be listening on a TLS connection.
	EnableTLS bool `json:"enableTls,omitempty"`

	// PeerVerificationRules provides the rules for what client certificates
	// the process should accept.
	PeerVerificationRules string `json:"peerVerificationRules,omitempty"`

	// Env provides environment variables.
	//
	// Deprecated: Use the PodTemplate field instead.
	Env []corev1.EnvVar `json:"env,omitempty"`

	// VolumeMounts provides volume mounts.
	//
	// Deprecated: Use the PodTemplate field instead.
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// ImageName provides the name of the image to use for the container,
	// without the version tag.
	//
	// Deprecated: Use the PodTemplate field instead.
	ImageName string `json:"imageName,omitempty"`

	// SecurityContext provides the container's security context.
	//
	// Deprecated: Use the PodTemplate field instead.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
}

// GetConfigurationString gets the CLI command for configuring a database.
func (configuration DatabaseConfiguration) GetConfigurationString() (string, error) {
	configurationString := fmt.Sprintf("%s %s", configuration.RedundancyMode, configuration.StorageEngine)

	counts := configuration.RoleCounts.Map()
	configurationString += fmt.Sprintf(" usable_regions=%d", configuration.UsableRegions)
	for _, role := range roleNames {
		if role != "storage" {
			configurationString += fmt.Sprintf(" %s=%d", role, counts[role])
		}
	}

	flags := configuration.VersionFlags.Map()
	for flag, value := range flags {
		if value != 0 {
			configurationString += fmt.Sprintf(" %s:=%d", flag, value)
		}
	}

	var regionString string
	if configuration.Regions == nil {
		regionString = "[]"
	} else {
		regionBytes, err := json.Marshal(configuration.Regions)
		if err != nil {
			return "", err
		}
		regionString = template.JSEscapeString(string(regionBytes))
	}

	configurationString += " regions=" + regionString

	return configurationString, nil
}

// DesiredDatabaseConfiguration builds the database configuration for the
// cluster based on its spec.
func (cluster *FoundationDBCluster) DesiredDatabaseConfiguration() DatabaseConfiguration {
	configuration := cluster.Spec.DatabaseConfiguration.NormalizeConfiguration()

	configuration.RoleCounts = cluster.GetRoleCountsWithDefaults()
	configuration.RoleCounts.Storage = 0
	if configuration.StorageEngine == "ssd" {
		configuration.StorageEngine = "ssd-2"
	}
	if configuration.StorageEngine == "memory" {
		configuration.StorageEngine = "memory-2"
	}
	return configuration
}

// ClearMissingVersionFlags clears any version flags in the given configuration that are not
// set in the configuration in the cluster spec.
//
// This allows us to compare the spec to the live configuration while ignoring
// version flags that are unset in the spec.
func (cluster *FoundationDBCluster) ClearMissingVersionFlags(configuration *DatabaseConfiguration) {
	if cluster.Spec.DatabaseConfiguration.LogVersion == 0 {
		configuration.LogVersion = 0
	}
	if cluster.Spec.DatabaseConfiguration.LogSpill == 0 {
		configuration.LogSpill = 0
	}
}

// IsBeingUpgraded determines whether the cluster has a pending upgrade.
func (cluster *FoundationDBCluster) IsBeingUpgraded() bool {
	return cluster.Status.RunningVersion != "" && cluster.Status.RunningVersion != cluster.Spec.Version
}

// InstanceIsBeingRemoved determines if an instance is pending removal.
func (cluster *FoundationDBCluster) InstanceIsBeingRemoved(instanceID string) bool {

	if cluster.Status.PendingRemovals != nil {
		_, present := cluster.Status.PendingRemovals[instanceID]
		if present {
			return true
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		podName := fmt.Sprintf("%s-%s", cluster.Name, instanceID)
		_, pendingRemoval := cluster.Spec.PendingRemovals[podName]
		if pendingRemoval {
			return true
		}
	}

	for _, id := range cluster.Spec.InstancesToRemove {
		if id == instanceID {
			return true
		}
	}

	for _, id := range cluster.Spec.InstancesToRemoveWithoutExclusion {
		if id == instanceID {
			return true
		}
	}

	return false
}

// ShouldUseLocks determine whether we should use locks to coordinator global
// operations.
func (cluster *FoundationDBCluster) ShouldUseLocks() bool {
	disabled := cluster.Spec.LockOptions.DisableLocks
	return disabled == nil || !*disabled
}

// GetLockPrefix gets the prefix for the keys where we store locking
// information.
func (cluster *FoundationDBCluster) GetLockPrefix() string {
	if cluster.Spec.LockOptions.LockKeyPrefix != "" {
		return cluster.Spec.LockOptions.LockKeyPrefix
	}

	return "\xff\x02/org.foundationdb.kubernetes-operator"
}

// GetLockID gets the identifier for this instance of the operator when taking
// locks.
func (cluster *FoundationDBCluster) GetLockID() string {
	return cluster.Spec.InstanceIDPrefix
}

// FillInDefaultsFromStatus adds in missing fields from the database
// configuration in the database status to make sure they match the fields that
// will appear in the cluster spec.
//
// Deprecated: Use NormalizeConfiguration instead.
func (configuration DatabaseConfiguration) FillInDefaultsFromStatus() DatabaseConfiguration {
	result := configuration.DeepCopy()

	if result.RemoteLogs == 0 {
		result.RemoteLogs = -1
	}
	if result.LogRouters == 0 {
		result.LogRouters = -1
	}
	return *result
}

// FillInDefaultVersionFlags adds in missing version flags so they match the
// running configuration.
//
// Deprecated: Use ClearMissingVersionFlags instead on the live configuration
// instead.
func (configuration *DatabaseConfiguration) FillInDefaultVersionFlags(liveConfiguration DatabaseConfiguration) {
	if configuration.LogSpill == 0 {
		configuration.LogSpill = liveConfiguration.LogSpill
	}
}

func getMainDataCenter(region Region) (string, int) {
	for _, dataCenter := range region.DataCenters {
		if dataCenter.Satellite == 0 {
			return dataCenter.ID, dataCenter.Priority
		}
	}
	return "", -1
}

// NormalizeConfiguration ensures a standardized format and defaults when
// comparing database configuration in the cluster spec with database
// configuration in the cluster status.
//
// This will fill in defaults of -1 for some fields that have a default of 0,
// and will ensure that the region configuration is ordered consistently.
func (configuration DatabaseConfiguration) NormalizeConfiguration() DatabaseConfiguration {
	result := configuration.DeepCopy()

	if result.RemoteLogs == 0 {
		result.RemoteLogs = -1
	}
	if result.LogRouters == 0 {
		result.LogRouters = -1
	}

	if result.UsableRegions < 1 {
		result.UsableRegions = 1
	}

	if result.RedundancyMode == "" {
		result.RedundancyMode = "double"
	}

	if result.StorageEngine == "" {
		result.StorageEngine = "ssd-2"
	}

	for _, region := range result.Regions {
		sort.Slice(region.DataCenters, func(leftIndex int, rightIndex int) bool {
			if region.DataCenters[leftIndex].Satellite != region.DataCenters[rightIndex].Satellite {
				return region.DataCenters[leftIndex].Satellite < region.DataCenters[rightIndex].Satellite
			} else if region.DataCenters[leftIndex].Priority != region.DataCenters[rightIndex].Priority {
				return region.DataCenters[leftIndex].Priority > region.DataCenters[rightIndex].Priority
			} else {
				return region.DataCenters[leftIndex].ID < region.DataCenters[rightIndex].ID
			}
		})
	}

	sort.Slice(result.Regions, func(leftIndex int, rightIndex int) bool {
		leftID, leftPriority := getMainDataCenter(result.Regions[leftIndex])
		rightID, rightPriority := getMainDataCenter(result.Regions[rightIndex])
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return leftID < rightID
	})

	return *result
}

func (configuration DatabaseConfiguration) getRegion(id string, priority int) Region {
	var matchingRegion Region

	for _, region := range configuration.Regions {
		for dataCenterIndex, dataCenter := range region.DataCenters {
			if dataCenter.Satellite == 0 && dataCenter.ID == id {
				matchingRegion = *region.DeepCopy()
				matchingRegion.DataCenters[dataCenterIndex].Priority = priority
				break
			}
		}
		if len(matchingRegion.DataCenters) > 0 {
			break
		}
	}

	if len(matchingRegion.DataCenters) == 0 {
		matchingRegion.DataCenters = append(matchingRegion.DataCenters, DataCenter{ID: id, Priority: priority})
	}

	return matchingRegion
}

// GetNextConfigurationChange produces the next marginal change that should
// be made to transform this configuration into another configuration.
//
// If there are multiple changes between the two configurations that can not be
// made simultaneously, this will produce a subset of the changes that move
// in the correct direction. Applying this method repeatedly will eventually
// converge on the final configuration.
func (configuration DatabaseConfiguration) GetNextConfigurationChange(finalConfiguration DatabaseConfiguration) DatabaseConfiguration {
	if !reflect.DeepEqual(configuration.Regions, finalConfiguration.Regions) {
		result := configuration.DeepCopy()
		currentPriorities := configuration.getRegionPriorities()
		nextPriorities := finalConfiguration.getRegionPriorities()
		finalPriorities := finalConfiguration.getRegionPriorities()

		// Step 1: Apply any changes to the satellites and satellite redundancy
		// from the final configuration to the next configuration.
		for regionIndex, region := range result.Regions {
			for _, dataCenter := range region.DataCenters {
				if dataCenter.Satellite == 0 {
					result.Regions[regionIndex] = finalConfiguration.getRegion(dataCenter.ID, dataCenter.Priority)
					break
				}
			}
		}

		// Step 2: If we have a region that is in the final config that is not
		// in the current config, add it.
		//
		// We can currently only add a maximum of two regions at a time.
		//
		// The new region will join at a negative priority, unless it is the
		// first region in the list.
		regionToAdd := "none"
		for len(result.Regions) < 2 && regionToAdd != "" {
			regionToAdd = ""

			for id, priority := range nextPriorities {
				_, present := currentPriorities[id]
				if !present && (regionToAdd == "" || priority > nextPriorities[regionToAdd]) {
					regionToAdd = id
				}
			}

			if regionToAdd != "" {
				priority := -1
				if len(result.Regions) == 0 {
					priority = 1
				}
				result.Regions = append(result.Regions, finalConfiguration.getRegion(regionToAdd, priority))
				currentPriorities[regionToAdd] = priority
			}
		}
		if len(result.Regions) != len(configuration.Regions) {
			return *result
		}

		currentRegions := make([]string, 0, len(configuration.Regions))
		for _, region := range configuration.Regions {
			for _, dataCenter := range region.DataCenters {
				if dataCenter.Satellite == 0 {
					currentRegions = append(currentRegions, dataCenter.ID)
				}
			}
		}

		// Step 3: If we currently have multiple regions, and one of them is not
		// in the final config, remove it.
		//
		// If that region has a positive priority, we must first give it a
		// negative priority.
		//
		// Before removing regions, the UsableRegions must be set to the next
		// region count.
		//
		// We skip this step if we are going to be removing region configuration
		// entirely.
		for _, regionID := range currentRegions {
			_, present := finalPriorities[regionID]
			if !present && len(configuration.Regions) > 1 && len(finalConfiguration.Regions) > 0 {
				if currentPriorities[regionID] >= 0 {
					continue
				} else if result.UsableRegions != len(result.Regions)-1 {
					result.UsableRegions = len(result.Regions) - 1
				} else {
					newRegions := make([]Region, 0, len(result.Regions)-1)
					for _, region := range result.Regions {
						toRemove := false
						for _, dataCenter := range region.DataCenters {
							if dataCenter.Satellite == 0 && dataCenter.ID == regionID {
								toRemove = true
							}
						}
						if !toRemove {
							newRegions = append(newRegions, region)
						}
					}
					result.Regions = newRegions
				}
				return *result
			}
		}

		for _, regionID := range currentRegions {
			priority := currentPriorities[regionID]
			_, present := finalPriorities[regionID]
			if !present && len(configuration.Regions) > 1 && len(finalConfiguration.Regions) > 0 {
				if priority > 0 && configuration.UsableRegions < 2 {
					continue
				} else if priority >= 0 {
					hasAlternativePrimary := false
					for regionIndex, region := range result.Regions {
						for dataCenterIndex, dataCenter := range region.DataCenters {
							if dataCenter.Satellite == 0 {
								if dataCenter.ID == regionID {
									result.Regions[regionIndex].DataCenters[dataCenterIndex].Priority = -1
								} else if dataCenter.Priority > 0 {
									hasAlternativePrimary = true
								}

							}
						}
					}

					if !hasAlternativePrimary {
						for regionIndex, region := range result.Regions {
							for dataCenterIndex, dataCenter := range region.DataCenters {
								if dataCenter.Satellite == 0 {
									if dataCenter.ID != regionID {
										result.Regions[regionIndex].DataCenters[dataCenterIndex].Priority = 1
										break
									}
								}
							}
						}
					}

					return *result
				} else if result.UsableRegions != len(result.Regions)-1 {
					result.UsableRegions = len(result.Regions) - 1
				} else {
					newRegions := make([]Region, 0, len(result.Regions)-1)
					for _, region := range result.Regions {
						toRemove := false
						for _, dataCenter := range region.DataCenters {
							if dataCenter.Satellite == 0 && dataCenter.ID == regionID {
								toRemove = true
							}
						}
						if !toRemove {
							newRegions = append(newRegions, region)
						}
					}
					result.Regions = newRegions
				}
				return *result
			}
		}

		// Step 4: Set all priorities for the regions to the desired value.
		//
		// If no region is configured to have a positive priority, ensure that
		// at least one region has a positive priority.
		//
		// Before changing priorities, we must ensure that all regions are
		// usable.

		maxCurrent := ""
		maxNext := ""

		for id, priority := range currentPriorities {
			_, present := nextPriorities[id]
			if !present {
				nextPriorities[id] = -1
			}
			if maxCurrent == "" || currentPriorities[maxCurrent] < priority {
				maxCurrent = id
			}
		}

		for id, priority := range nextPriorities {
			_, present := currentPriorities[id]
			if !present {
				currentPriorities[id] = -1
			}
			if maxNext == "" || nextPriorities[maxNext] < priority {
				maxNext = id
			}
		}

		if maxNext == "" || nextPriorities[maxNext] < 0 {
			nextPriorities[maxCurrent] = currentPriorities[maxCurrent]
		}

		if !reflect.DeepEqual(currentPriorities, nextPriorities) {
			if configuration.UsableRegions != len(configuration.Regions) {
				result.UsableRegions = len(configuration.Regions)
			} else {
				for regionIndex, region := range result.Regions {
					for dataCenterIndex, dataCenter := range region.DataCenters {
						if dataCenter.Satellite == 0 {
							result.Regions[regionIndex].DataCenters[dataCenterIndex].Priority = nextPriorities[dataCenter.ID]
							break
						}
					}
				}
			}
			return *result
		}

		// Step 5: Set the final region count.
		if configuration.UsableRegions != finalConfiguration.UsableRegions {
			result.UsableRegions = finalConfiguration.UsableRegions
			return *result
		}

		// Step 6: Set the final region config.
		result.Regions = finalConfiguration.Regions
		return *result
	}
	return finalConfiguration
}

func (configuration DatabaseConfiguration) getRegionPriorities() map[string]int {
	priorities := make(map[string]int, len(configuration.Regions))

	for _, region := range configuration.Regions {
		for _, dataCenter := range region.DataCenters {
			if dataCenter.Satellite == 0 {
				priorities[dataCenter.ID] = dataCenter.Priority
			}
		}
	}
	return priorities
}

// LockOptions provides customization for locking global operations.
type LockOptions struct {
	// DisableLocks determines whether we should disable locking entirely.
	DisableLocks *bool `json:"disableLocks,omitempty"`

	// LockKeyPrefix provides a custom prefix for the keys in the database we
	// use to store locks.
	LockKeyPrefix string `json:"lockKeyPrefix,omitempty"`
}

// ServiceConfig allows configuring services that sit in front of our pods.
type ServiceConfig struct {
	// Headless determines whether we want to run a headless service for the
	// cluster.
	Headless *bool `json:"headless,omitempty"`
}

// RequiredAddressSet provides settings for which addresses we need to listen
// on.
type RequiredAddressSet struct {
	// TLS defines whether we need to listen on a TLS address.
	TLS bool `json:"tls,omitempty"`

	// NonTLS defines whether we need to listen on a non-TLS address.
	NonTLS bool `json:"nonTLS,omitempty"`
}

// FdbVersion represents a version of FoundationDB.
//
// This provides convenience methods for checking features available in
// different versions.
type FdbVersion struct {
	// Major is the major version
	Major int

	// Minor is the minor version
	Minor int

	// Patch is the patch version
	Patch int
}

var fdbVersionRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// ParseFdbVersion parses a version from its string representation.
func ParseFdbVersion(version string) (FdbVersion, error) {
	matches := fdbVersionRegex.FindStringSubmatch(version)
	if matches == nil {
		return FdbVersion{}, fmt.Errorf("Could not parse FDB version from %s", version)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return FdbVersion{}, err
	}

	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return FdbVersion{}, err
	}

	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return FdbVersion{}, err
	}

	return FdbVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// String gets the string representation of an FDB version.
func (version FdbVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

// IsAtLeast determines if a version is greater than or equal to another version.
func (version FdbVersion) IsAtLeast(other FdbVersion) bool {
	if version.Major < other.Major {
		return false
	}
	if version.Major > other.Major {
		return true
	}
	if version.Minor < other.Minor {
		return false
	}
	if version.Minor > other.Minor {
		return true
	}
	if version.Patch < other.Patch {
		return false
	}
	if version.Patch > other.Patch {
		return true
	}
	return true
}

// IsProtocolCompatible determines whether two versions of FDB are protocol
// compatible.
func (version FdbVersion) IsProtocolCompatible(other FdbVersion) bool {
	return version.Major == other.Major && version.Minor == other.Minor
}

// HasInstanceIDInSidecarSubstitutions determines if a version has
// FDB_INSTANCE_ID supported natively in the variable substitutions in the
// sidecar.
func (version FdbVersion) HasInstanceIDInSidecarSubstitutions() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 15})
}

// PrefersCommandLineArgumentsInSidecar determines if a version has
// support for configuring the sidecar exclusively through command-line
// arguments.
func (version FdbVersion) PrefersCommandLineArgumentsInSidecar() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 15})
}

// SupportsUsingBinariesFromMainContainer determines if a version has
// support for having the sidecar dynamically switch between using binaries
// from the main container and binaries provided by the sidecar.
func (version FdbVersion) SupportsUsingBinariesFromMainContainer() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 15})
}

// HasRatekeeperRole determines if a version has a dedicated role for
// ratekeeper.
func (version FdbVersion) HasRatekeeperRole() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 0})
}

// HasMaxProtocolClientsInStatus determines if a version has the
// max_protocol_clients field in the cluster status.
func (version FdbVersion) HasMaxProtocolClientsInStatus() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 0})
}

// HasSidecarCrashOnEmpty determines if a version has the flag to have the
// sidecar crash on a file being empty.
func (version FdbVersion) HasSidecarCrashOnEmpty() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 2, Patch: 20})
}

// HasNonBlockingExcludes determines if a version has support for non-blocking
// exclude commands.
//
// This is currently set to false across the board, pending investigation into
// potential bugs with non-blocking excludes.
func (version FdbVersion) HasNonBlockingExcludes() bool {
	return version.IsAtLeast(FdbVersion{Major: 6, Minor: 3, Patch: 5})
}
