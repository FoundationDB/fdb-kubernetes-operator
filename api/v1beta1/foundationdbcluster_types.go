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
	"math"
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"time"

	"k8s.io/utils/pointer"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=fdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Generation",type="integer",JSONPath=".metadata.generation",description="Latest generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Reconciled",type="integer",JSONPath=".status.generations.reconciled",description="Last reconciled generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Available",type="boolean",JSONPath=".status.health.available",description="Database available",priority=0
// +kubebuilder:printcolumn:name="FullReplication",type="boolean",JSONPath=".status.health.fullReplication",description="Database fully replicated",priority=0
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.runningVersion",description="Running version",priority=0
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:deprecatedversion

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

var conditionsThatNeedReplacement = []ProcessGroupConditionType{MissingProcesses, PodFailing}

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
	// +kubebuilder:validation:Pattern:=(\d+)\.(\d+)\.(\d+)
	Version string `json:"version"`

	// SidecarVersions defines the build version of the sidecar to run. This
	// maps an FDB version to the corresponding sidecar build version.
	// Deprecated: Use SidecarContainer.ImageConfigs instead.
	SidecarVersions map[string]int `json:"sidecarVersions,omitempty"`

	// DatabaseConfiguration defines the database configuration.
	DatabaseConfiguration DatabaseConfiguration `json:"databaseConfiguration,omitempty"`

	// Processes defines process-level settings.
	Processes map[ProcessClass]ProcessSettings `json:"processes,omitempty"`

	// ProcessCounts defines the number of processes to configure for each
	// process class. You can generally omit this, to allow the operator to
	// infer the process counts based on the database configuration.
	ProcessCounts ProcessCounts `json:"processCounts,omitempty"`

	// SeedConnectionString provides a connection string for the initial
	// reconciliation.
	//
	// After the initial reconciliation, this will not be used.
	SeedConnectionString string `json:"seedConnectionString,omitempty"`

	// PartialConnectionString provides a way to specify part of the
	// connection string (e.g. the database name and coordinator generation)
	// without specifying the entire string. This does not allow for setting
	// the coordinator IPs. If `SeedConnectionString` is set,
	// `PartialConnectionString` will have no effect. They cannot be used
	// together.
	PartialConnectionString ConnectionString `json:"partialConnectionString,omitempty"`

	// FaultDomain defines the rules for what fault domain to replicate across.
	FaultDomain FoundationDBClusterFaultDomain `json:"faultDomain,omitempty"`

	// InstancesToRemove defines the instances that we should remove from the
	// cluster. This list contains the instance IDs.
	// Deprecated: Use ProcessGroupsToRemove instead.
	InstancesToRemove []string `json:"instancesToRemove,omitempty"`

	// ProcessGroupsToRemove defines the process groups that we should remove from the
	// cluster. This list contains the process group IDs.
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=500
	ProcessGroupsToRemove []string `json:"processGroupsToRemove,omitempty"`

	// InstancesToRemoveWithoutExclusion defines the instances that we should
	// remove from the cluster without excluding them. This list contains the
	// instance IDs.
	//
	// This should be used for cases where a pod does not have an IP address and
	// you want to remove it and destroy its volume without confirming the data
	// is fully replicated.
	// Deprecated: Use ProcessGroupsToRemoveWithoutExclusion instead.
	InstancesToRemoveWithoutExclusion []string `json:"instancesToRemoveWithoutExclusion,omitempty"`

	// ProcessGroupsToRemoveWithoutExclusion defines the process groups that we should
	// remove from the cluster without excluding them. This list contains the
	// process group IDs.
	//
	// This should be used for cases where a pod does not have an IP address and
	// you want to remove it and destroy its volume without confirming the data
	// is fully replicated.
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=500
	ProcessGroupsToRemoveWithoutExclusion []string `json:"processGroupsToRemoveWithoutExclusion,omitempty"`

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
	//
	// This must be a valid Kubernetes label value. See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
	// for more details on that.
	// Deprecated: Use ProcessGroupIDPrefix instead.
	InstanceIDPrefix string `json:"instanceIDPrefix,omitempty"`

	// ProcessGroupIDPrefix defines a prefix to append to the process group IDs in the
	// locality fields.
	//
	// This must be a valid Kubernetes label value. See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
	// for more details on that.
	// +kubebuilder:validation:MaxLength=43
	ProcessGroupIDPrefix string `json:"processGroupIDPrefix,omitempty"`

	// UpdatePodsByReplacement determines whether we should update pod config
	// by replacing the pods rather than deleting them.
	// Deprecated: use PodUpdateStrategy instead
	UpdatePodsByReplacement bool `json:"updatePodsByReplacement,omitempty"`

	// LockOptions allows customizing how we manage locks for global operations.
	LockOptions LockOptions `json:"lockOptions,omitempty"`

	// Services defines the configuration for services that sit in front of our
	// pods.
	// Deprecated: Use Routing instead.
	Services ServiceConfig `json:"services,omitempty"`

	// Routing defines the configuration for routing to our pods.
	Routing RoutingConfig `json:"routing,omitempty"`

	// IgnoreUpgradabilityChecks determines whether we should skip the check for
	// client compatibility when performing an upgrade.
	IgnoreUpgradabilityChecks bool `json:"ignoreUpgradabilityChecks,omitempty"`

	// Buggify defines settings for injecting faults into a cluster for testing.
	Buggify BuggifyConfig `json:"buggify,omitempty"`

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
	CustomParameters FoundationDBCustomParameters `json:"customParameters,omitempty"`

	// PendingRemovals defines the processes that are pending removal.
	// This maps the name of a pod to its IP address. If a value is left blank,
	// the controller will provide the pod's current IP.
	//
	// Deprecated: To indicate that a process should be removed, use the
	// ProcessGroupsToRemove field. To get information about pending removals,
	// use the PendingRemovals field in the status.
	PendingRemovals map[string]string `json:"pendingRemovals,omitempty"`

	// StorageServersPerPod defines how many Storage Servers should run in
	// a single process group (Pod). This number defines the number of processes running
	// in one Pod whereas the ProcessCounts defines the number of Pods created.
	// This means that you end up with ProcessCounts["storage"] * StorageServersPerPod
	// storage processes
	StorageServersPerPod int `json:"storageServersPerPod,omitempty"`

	// MinimumUptimeSecondsForBounce defines the minimum time, in seconds, that the
	// processes in the cluster must have been up for before the operator can
	// execute a bounce.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=600
	MinimumUptimeSecondsForBounce int `json:"minimumUptimeSecondsForBounce,omitempty"`

	// ReplaceInstancesWhenResourcesChange defines if an instance should be replaced
	// when the resource requirements are increased. This can be useful with the combination of
	// local storage.
	// +kubebuilder:default:=false
	ReplaceInstancesWhenResourcesChange *bool `json:"replaceInstancesWhenResourcesChange,omitempty"`

	// Skip defines if the cluster should be skipped for reconciliation. This can be useful for
	// investigating in issues or if the environment is unstable.
	// +kubebuilder:default:=false
	Skip bool `json:"skip,omitempty"`

	// CoordinatorSelection defines which process classes are eligible for coordinator selection.
	// If empty all stateful processes classes are equally eligible.
	// A higher priority means that a process class is preferred over another process class.
	// If the FoundationDB cluster is spans across multiple Kubernetes clusters or DCs the
	// CoordinatorSelection must match in all FoundationDB cluster resources otherwise
	// the coordinator selection process could conflict.
	CoordinatorSelection []CoordinatorSelectionSetting `json:"coordinatorSelection,omitempty"`

	// LabelConfig allows customizing labels used by the operator.
	LabelConfig LabelConfig `json:"labels,omitempty"`

	// UseExplicitListenAddress determines if we should add a listen address
	// that is separate from the public address.
	UseExplicitListenAddress *bool `json:"useExplicitListenAddress,omitempty"`

	// UseUnifiedImage determines if we should use the unified image rather than
	// separate images for the main container and the sidecar container.
	UseUnifiedImage *bool `json:"useUnifiedImage,omitempty"`
}

// ImageType defines a single kind of images used in the cluster.
// +kubebuilder:validation:MaxLength=1024
type ImageType string

// FoundationDBClusterStatus defines the observed state of FoundationDBCluster
type FoundationDBClusterStatus struct {
	// ProcessCounts defines the number of processes that are currently running
	// in the cluster.
	// Deprecated: Use ProcessGroups instead.
	ProcessCounts ProcessCounts `json:"processCounts,omitempty"`

	// IncorrectProcesses provides the processes that do not have the correct
	// configuration.
	//
	// This will map the process group ID to the timestamp when we observed the
	// incorrect configuration.
	// Deprecated: Use ProcessGroups instead.
	IncorrectProcesses map[string]int64 `json:"incorrectProcesses,omitempty"`

	// IncorrectPods provides the pods that do not have the correct
	// spec.
	//
	// This will contain the name of the pod.
	// Deprecated: Use ProcessGroups instead.
	IncorrectPods []string `json:"incorrectPods,omitempty"`

	// FailingPods provides the pods that are not starting correctly.
	//
	// This will contain the name of the pod.
	// Deprecated: Use ProcessGroups instead.
	FailingPods []string `json:"failingPods,omitempty"`

	// MissingProcesses provides the processes that are not reporting to the
	// cluster.
	// This will map the names of the pod to the timestamp when we observed
	// that the process was missing.
	// Deprecated: Use ProcessGroups instead.
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

	// HasListenIPsForAllPods defines whether every pod has an environment
	// variable for its listen address.
	HasListenIPsForAllPods bool `json:"hasListenIPsForAllPods,omitempty"`

	// PendingRemovals defines the processes that are pending removal.
	// This maps the process group ID to its removal state.
	// Deprecated: Use ProcessGroups instead.
	PendingRemovals map[string]PendingRemovalState `json:"pendingRemovals,omitempty"`

	// NeedsSidecarConfInConfigMap determines whether we need to include the
	// sidecar conf in the config map even when the latest version should not
	// require it.
	// Deprecated: will be removed in the next release.
	NeedsSidecarConfInConfigMap bool `json:"needsSidecarConfInConfigMap,omitempty"`

	// StorageServersPerDisk defines the storageServersPerPod observed in the cluster.
	// If there are more than one value in the slice the reconcile phase is not finished.
	StorageServersPerDisk []int `json:"storageServersPerDisk,omitempty"`

	// ImageTypes defines the kinds of images that are in use in the cluster.
	// If there is more than one value in the slice the reconcile phase is not
	// finished.
	// +kubebuilder:validation:MaxItems=10
	ImageTypes []ImageType `json:"imageTypes,omitempty"`

	// ProcessGroups contain information about a process group.
	// This information is used in multiple places to trigger the according action.
	ProcessGroups []*ProcessGroupStatus `json:"processGroups,omitempty"`

	// Locks contains information about the locking system.
	Locks LockSystemStatus `json:"locks,omitempty"`
}

// LockSystemStatus provides a summary of the status of the locking system.
type LockSystemStatus struct {
	// DenyList contains a list of operator instances that are prevented
	// from taking locks.
	DenyList []string `json:"lockDenyList,omitempty"`
}

// ProcessGroupStatus represents the status of a ProcessGroup.
type ProcessGroupStatus struct {
	// ProcessGroupID represents the ID of the process group
	ProcessGroupID string `json:"processGroupID,omitempty"`
	// ProcessClass represents the class the process group has.
	ProcessClass ProcessClass `json:"processClass,omitempty"`
	// Addresses represents the list of addresses the process group has been known to have.
	Addresses []string `json:"addresses,omitempty"`
	// Remove defines if the process group is marked for removal.
	// Deprecated: Use RemovalTimestamp instead.
	Remove bool `json:"remove,omitempty"`
	// RemoveTimestamp if not empty defines when the process group was marked for removal.
	RemovalTimestamp *metav1.Time `json:"removalTimestamp,omitempty"`
	// Excluded defines if the process group has been fully excluded.
	// This is only used within the reconciliation process, and should not be considered authoritative.
	// Deprecated: Use ExclusionTimestamp instead.
	Excluded bool `json:"excluded,omitempty"`
	// ExclusionTimestamp defines when the process group has been fully excluded.
	// This is only used within the reconciliation process, and should not be considered authoritative.
	ExclusionTimestamp *metav1.Time `json:"exclusionTimestamp,omitempty"`
	// ExclusionSkipped determines if exclusion has been skipped for a process, which will allow the process group to be removed without exclusion.
	ExclusionSkipped bool `json:"exclusionSkipped,omitempty"`
	// ProcessGroupConditions represents a list of degraded conditions that the process group is in.
	ProcessGroupConditions []*ProcessGroupCondition `json:"processGroupConditions,omitempty"`
}

// IsExcluded returns if a process group is excluded
func (processGroupStatus *ProcessGroupStatus) IsExcluded() bool {
	return processGroupStatus.Excluded || (processGroupStatus.ExclusionTimestamp != nil && !processGroupStatus.ExclusionTimestamp.IsZero()) || processGroupStatus.ExclusionSkipped
}

// SetExclude marks a process group as excluded
func (processGroupStatus *ProcessGroupStatus) SetExclude() {
	processGroupStatus.Excluded = true
	processGroupStatus.ExclusionTimestamp = &metav1.Time{Time: time.Now()}
}

// IsMarkedForRemoval returns if a process group is marked for removal
func (processGroupStatus *ProcessGroupStatus) IsMarkedForRemoval() bool {
	return processGroupStatus.Remove || (processGroupStatus.RemovalTimestamp != nil && !processGroupStatus.RemovalTimestamp.IsZero())
}

// MarkForRemoval marks a process group for removal
func (processGroupStatus *ProcessGroupStatus) MarkForRemoval() {
	processGroupStatus.Remove = true
	processGroupStatus.RemovalTimestamp = &metav1.Time{Time: time.Now()}
}

// NeedsReplacement checks if the ProcessGroupStatus has conditions so that it should be removed
func (processGroupStatus *ProcessGroupStatus) NeedsReplacement(failureTime int) (bool, int64) {
	var missingTime *int64
	for _, condition := range conditionsThatNeedReplacement {
		conditionTime := processGroupStatus.GetConditionTime(condition)
		if conditionTime != nil && (missingTime == nil || *missingTime > *conditionTime) {
			missingTime = conditionTime
		}
	}

	failureWindowStart := time.Now().Add(-1 * time.Duration(failureTime) * time.Second).Unix()
	if missingTime != nil && *missingTime < failureWindowStart && !processGroupStatus.IsMarkedForRemoval() {
		return true, *missingTime
	}

	return false, 0
}

// AddAddresses adds the new address to the ProcessGroupStatus and removes duplicates and old addresses
// if the process group is not marked as removal.
func (processGroupStatus *ProcessGroupStatus) AddAddresses(addresses []string, includeOldAddresses bool) {
	newAddresses := make([]string, 0, len(addresses))
	// Currently this only contains one address but might include in the future multiple addresses
	// e.g. for dual stack
	for _, addr := range addresses {
		// empty address in the address list that means the Pod has no IP address assigned
		if addr == "" {
			continue
		}

		newAddresses = append(newAddresses, addr)
	}

	// If the newAddresses contains at least one IP address use this list as the new addresses
	// and return
	if len(newAddresses) > 0 && !includeOldAddresses {
		processGroupStatus.Addresses = newAddresses
		return
	}

	if includeOldAddresses {
		processGroupStatus.Addresses = cleanAddressList(append(processGroupStatus.Addresses, newAddresses...))
		return
	}
}

// This method removes duplicates and empty strings from a list of addresses.
func cleanAddressList(addresses []string) []string {
	result := make([]string, 0, len(addresses))
	resultMap := make(map[string]bool)

	for _, value := range addresses {
		if value != "" && !resultMap[value] {
			result = append(result, value)
			resultMap[value] = true
		}
	}

	return result
}

// AllAddressesExcluded checks if the process group is excluded or if there are still addresses included in the remainingMap.
// This will return true if the process group skips exclusion or has no remaining addresses.
func (processGroupStatus *ProcessGroupStatus) AllAddressesExcluded(remainingMap map[string]bool) (bool, error) {
	if processGroupStatus.ExclusionSkipped {
		return true, nil
	}

	for _, address := range processGroupStatus.Addresses {
		isRemaining, isPresent := remainingMap[address]
		if !isPresent || isRemaining {
			return false, fmt.Errorf("process has missing address in exclusion results: %s", address)
		}
	}

	return true, nil
}

// NewProcessGroupStatus returns a new GroupStatus for the given processGroupID and processClass.
func NewProcessGroupStatus(processGroupID string, processClass ProcessClass, addresses []string) *ProcessGroupStatus {
	return &ProcessGroupStatus{
		ProcessGroupID: processGroupID,
		ProcessClass:   processClass,
		Addresses:      addresses,
		Remove:         false,
		Excluded:       false,
		ProcessGroupConditions: []*ProcessGroupCondition{
			NewProcessGroupCondition(MissingProcesses),
			NewProcessGroupCondition(MissingPod),
			NewProcessGroupCondition(MissingPVC),
			// TODO(johscheuer): currently we never set this condition
			// NewProcessGroupCondition(MissingService),
		},
	}
}

// FindProcessGroupByID finds a process group status for a given processGroupID.
func FindProcessGroupByID(processGroups []*ProcessGroupStatus, processGroupID string) *ProcessGroupStatus {
	for _, processGroup := range processGroups {
		if processGroup.ProcessGroupID == processGroupID {
			return processGroup
		}
	}

	return nil
}

// ContainsProcessGroupID evaluates if the ProcessGroupStatus contains a given processGroupID.
func ContainsProcessGroupID(processGroups []*ProcessGroupStatus, processGroupID string) bool {
	return FindProcessGroupByID(processGroups, processGroupID) != nil
}

// MarkProcessGroupForRemoval sets the remove flag for the given process and ensures that the address is added.
func MarkProcessGroupForRemoval(processGroups []*ProcessGroupStatus, processGroupID string, processClass ProcessClass, address string) (bool, *ProcessGroupStatus) {
	for _, processGroup := range processGroups {
		if processGroup.ProcessGroupID != processGroupID {
			continue
		}

		hasAddress := false
		for _, addr := range processGroup.Addresses {
			if addr != address {
				continue
			}

			hasAddress = true
			break
		}

		if !hasAddress && address != "" {
			processGroup.Addresses = append(processGroup.Addresses, address)
		}

		processGroup.MarkForRemoval()
		return true, nil
	}

	var addresses []string
	if address == "" {
		addresses = nil
	} else {
		addresses = []string{address}
	}

	processGroup := NewProcessGroupStatus(processGroupID, processClass, addresses)
	processGroup.MarkForRemoval()

	return false, processGroup
}

// UpdateCondition will add or remove a condition in the ProcessGroupStatus.
// If the old ProcessGroupStatus already contains the condition, and the condition is being set,
// the condition is reused to contain the same timestamp.
func (processGroupStatus *ProcessGroupStatus) UpdateCondition(conditionType ProcessGroupConditionType, set bool, oldProcessGroups []*ProcessGroupStatus, processGroupID string) {
	if set {
		processGroupStatus.addCondition(oldProcessGroups, processGroupID, conditionType)
	} else {
		processGroupStatus.removeCondition(conditionType)
	}
}

// addCondition will add the condition to the ProcessGroupStatus.
// If the old ProcessGroupStatus already contains the condition the condition is reused to contain the same timestamp.
func (processGroupStatus *ProcessGroupStatus) addCondition(oldProcessGroups []*ProcessGroupStatus, processGroupID string, conditionType ProcessGroupConditionType) {
	var oldProcessGroupStatus *ProcessGroupStatus

	// Check if we got a ProcessGroupStatus for the processGroupID
	for _, oldGroupStatus := range oldProcessGroups {
		if oldGroupStatus.ProcessGroupID != processGroupID {
			continue
		}

		oldProcessGroupStatus = oldGroupStatus
		break
	}

	// Check if we got a condition for the condition type for the ProcessGroupStatus
	if oldProcessGroupStatus != nil {
		for _, condition := range oldProcessGroupStatus.ProcessGroupConditions {
			if condition.ProcessGroupConditionType == conditionType {
				// We found a condition with the above condition type
				processGroupStatus.ProcessGroupConditions = append(processGroupStatus.ProcessGroupConditions, condition)
				return
			}
		}
	}

	// Check if we already got this condition in the current ProcessGroupStatus
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType == conditionType {
			return
		}
	}

	// We didn't find any condition so we create a new one
	processGroupStatus.ProcessGroupConditions = append(processGroupStatus.ProcessGroupConditions, NewProcessGroupCondition(conditionType))
}

// removeCondition will remove a condition from the ProcessGroupStatus, if it is
// present.
func (processGroupStatus *ProcessGroupStatus) removeCondition(conditionType ProcessGroupConditionType) {
	conditions := make([]*ProcessGroupCondition, 0, len(processGroupStatus.ProcessGroupConditions))
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType != conditionType {
			conditions = append(conditions, condition)
		}
	}
	processGroupStatus.ProcessGroupConditions = conditions
}

// CreateProcessCountsFromProcessGroupStatus creates a ProcessCounts struct from the current ProcessGroupStatus.
func CreateProcessCountsFromProcessGroupStatus(processGroupStatus []*ProcessGroupStatus, includeRemovals bool) ProcessCounts {
	processCounts := ProcessCounts{}

	for _, groupStatus := range processGroupStatus {
		if !groupStatus.IsMarkedForRemoval() || includeRemovals {
			processCounts.IncreaseCount(groupStatus.ProcessClass, 1)
		}
	}

	return processCounts
}

// FilterByCondition returns a string slice of all ProcessGroupIDs that contains a condition with the given type.
func FilterByCondition(processGroupStatus []*ProcessGroupStatus, conditionType ProcessGroupConditionType, ignoreRemoved bool) []string {
	return FilterByConditions(processGroupStatus, map[ProcessGroupConditionType]bool{conditionType: true}, ignoreRemoved)
}

// FilterByConditions returns a string slice of all ProcessGroupIDs whose
// conditions match a set of rules.
//
// If a condition is mapped to true in the conditionRules map, only process
// groups with that condition will be returned. If a condition is mapped to
// false in the conditionRules map, only process groups without that condition
// will be returned.
func FilterByConditions(processGroupStatus []*ProcessGroupStatus, conditionRules map[ProcessGroupConditionType]bool, ignoreRemoved bool) []string {
	result := make([]string, 0)

	for _, groupStatus := range processGroupStatus {
		if ignoreRemoved && groupStatus.IsMarkedForRemoval() {
			continue
		}

		matchingConditions := make(map[ProcessGroupConditionType]bool, len(conditionRules))
		for conditionRule := range conditionRules {
			matchingConditions[conditionRule] = false
		}
		for _, condition := range groupStatus.ProcessGroupConditions {
			if _, hasRule := conditionRules[condition.ProcessGroupConditionType]; hasRule {
				matchingConditions[condition.ProcessGroupConditionType] = true
			}
		}
		if reflect.DeepEqual(matchingConditions, conditionRules) {
			result = append(result, groupStatus.ProcessGroupID)
		}
	}

	return result
}

// ProcessGroupsByProcessClass returns a slice of all Process Groups that contains a given process class.
func (clusterStatus FoundationDBClusterStatus) ProcessGroupsByProcessClass(processClass ProcessClass) []*ProcessGroupStatus {
	result := make([]*ProcessGroupStatus, 0)

	for _, groupStatus := range clusterStatus.ProcessGroups {
		if groupStatus.ProcessClass == processClass {
			result = append(result, groupStatus)
		}
	}

	return result
}

// GetConditionTime returns the timestamp when we detected a condition on a
// process group.
// If there is no matching condition this will return nil.
func (processGroupStatus *ProcessGroupStatus) GetConditionTime(conditionType ProcessGroupConditionType) *int64 {
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType == conditionType {
			return &condition.Timestamp
		}
	}

	return nil
}

// NewProcessGroupCondition creates a new ProcessGroupCondition of the given time with the current timestamp.
func NewProcessGroupCondition(conditionType ProcessGroupConditionType) *ProcessGroupCondition {
	return &ProcessGroupCondition{
		ProcessGroupConditionType: conditionType,
		Timestamp:                 time.Now().Unix(),
	}
}

// ProcessGroupCondition represents a degraded condition that a process group is in.
type ProcessGroupCondition struct {
	// Name of the condition
	ProcessGroupConditionType ProcessGroupConditionType `json:"type,omitempty"`
	// Timestamp when the Condition was observed
	Timestamp int64 `json:"timestamp,omitempty"`
}

// ProcessGroupConditionType represents a concrete ProcessGroupCondition.
type ProcessGroupConditionType string

const (
	// IncorrectPodSpec represents a process group that has an incorrect Pod spec.
	IncorrectPodSpec ProcessGroupConditionType = "IncorrectPodSpec"
	// IncorrectConfigMap represents a process group that has an incorrect ConfigMap.
	IncorrectConfigMap ProcessGroupConditionType = "IncorrectConfigMap"
	// IncorrectCommandLine represents a process group that has an incorrect commandline configuration.
	IncorrectCommandLine ProcessGroupConditionType = "IncorrectCommandLine"
	// PodFailing represents a process group which Pod keeps failing.
	PodFailing ProcessGroupConditionType = "PodFailing"
	// MissingPod represents a process group that doesn't have a Pod assigned.
	MissingPod ProcessGroupConditionType = "MissingPod"
	// MissingPVC represents a process group that doesn't have a PVC assigned.
	MissingPVC ProcessGroupConditionType = "MissingPVC"
	// MissingService represents a process group that doesn't have a Service assigned.
	MissingService ProcessGroupConditionType = "MissingService"
	// MissingProcesses represents a process group that misses a process.
	MissingProcesses ProcessGroupConditionType = "MissingProcesses"
	// ResourcesTerminating represents a process group whose resources are being
	// terminated.
	ResourcesTerminating ProcessGroupConditionType = "ResourcesTerminating"
	// SidecarUnreachable represents a process group where the sidecar is not reachable
	// because of networking or TLS issues.
	SidecarUnreachable ProcessGroupConditionType = "SidecarUnreachable"
	// PodPending represents a process group where the pod is in a pending state.
	PodPending ProcessGroupConditionType = "PodPending"
	// ReadyCondition is currently only used in the metrics.
	ReadyCondition ProcessGroupConditionType = "Ready"
)

// AllProcessGroupConditionTypes returns all ProcessGroupConditionType
func AllProcessGroupConditionTypes() []ProcessGroupConditionType {
	return []ProcessGroupConditionType{
		IncorrectPodSpec,
		IncorrectConfigMap,
		IncorrectCommandLine,
		PodFailing,
		MissingPod,
		MissingPVC,
		MissingService,
		MissingProcesses,
		SidecarUnreachable,
		PodPending,
		ReadyCondition,
	}
}

// GetProcessGroupConditionType returns the ProcessGroupConditionType for the matching string or an error
func GetProcessGroupConditionType(processGroupConditionType string) (ProcessGroupConditionType, error) {
	switch processGroupConditionType {
	case "IncorrectPodSpec":
		return IncorrectPodSpec, nil
	case "IncorrectConfigMap":
		return IncorrectConfigMap, nil
	case "IncorrectCommandLine":
		return IncorrectCommandLine, nil
	case "PodFailing":
		return PodFailing, nil
	case "MissingPod":
		return MissingPod, nil
	case "MissingPVC":
		return MissingPVC, nil
	case "MissingService":
		return MissingService, nil
	case "MissingProcesses":
		return MissingProcesses, nil
	case "SidecarUnreachable":
		return SidecarUnreachable, nil
	case "PodPending":
		return PodPending, nil
	}

	return "", fmt.Errorf("unknown process group condition type: %s", processGroupConditionType)
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
	// Deprecated: This is no longer used.
	HasFailingPods int64 `json:"hasFailingPods,omitempty"`

	// HasUnhealthyProcess provides the last generation that has at least one
	// process group with a negative condition.
	HasUnhealthyProcess int64 `json:"hasUnhealthyProcess,omitempty"`

	// NeedsLockConfigurationChanges provides the last generation that is
	// pending a change to the configuration of the locking system.
	NeedsLockConfigurationChanges int64 `json:"needsLockConfigurationChanges,omitempty"`
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
// Deprecated: This is modeled in the process group status instead.
type PendingRemovalState struct {
	// The name of the pod that is being removed.
	PodName string `json:"podName,omitempty"`

	// The public address of the process.
	Address string `json:"address,omitempty"`

	// Whether we have started the exclusion.
	// Deprecated: This field is no longer filled in.
	ExclusionStarted bool `json:"exclusionStarted,omitempty"`

	// Whether we have completed the exclusion.
	ExclusionComplete bool `json:"exclusionComplete,omitempty"`

	// Whether this removal has ever corresponded to a real instance.
	HadInstance bool `json:"hadInstance,omitempty"`
}

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
	// Deprecated: Use DeletionMode with PodUpdateModeNone to prevent the operator from deleting Pods.
	DeletePods *bool `json:"deletePods,omitempty"`

	// Replacements contains options for automatically replacing failed
	// processes.
	Replacements AutomaticReplacementOptions `json:"replacements,omitempty"`

	// IgnorePendingPodsDuration defines how long a Pod has to be in the Pending Phase before
	// we ignore it during reconciliation. This prevents Pod that are stuck in Pending to block
	// further reconciliation.
	IgnorePendingPodsDuration time.Duration `json:"ignorePendingPodsDuration,omitempty"`

	// IgnoreTerminatingPodsSeconds defines how long a Pod has to be in the Terminating Phase before
	// we ignore it during reconciliation. This prevents Pod that are stuck in Terminating to block
	// further reconciliation.
	IgnoreTerminatingPodsSeconds *int `json:"ignoreTerminatingPodsSeconds,omitempty"`

	// EnforceFullReplicationForDeletion defines if the operator is only allowed to delete Pods
	// if the cluster is fully replicated. If the cluster is not fully replicated the Operator won't
	// delete any Pods that are marked for removal.
	// Defaults to true.
	// Deprecated: Will be enforced by default in 1.0.0 without disabling.
	EnforceFullReplicationForDeletion *bool `json:"enforceFullReplicationForDeletion,omitempty"`

	// UseNonBlockingExcludes defines whether the operator is allowed to use non blocking exclude commands.
	// The default is false.
	UseNonBlockingExcludes *bool `json:"useNonBlockingExcludes,omitempty"`

	// MaxConcurrentReplacements defines how many process groups can be concurrently
	// replaced if they are misconfigured. If the value will be set to 0 this will block replacements
	// and these misconfigured Pods must be replaced manually or by another process. For each reconcile
	// loop the operator calculates the maximum number of possible replacements by taken this value as the
	// upper limit and removes all ongoing replacements that have not finished. Which means if the value is
	// set to 5 and we have 4 ongoing replacements (process groups marked with remove but not excluded) the
	// operator is allowed to replace on further process group.
	// +kubebuilder:validation:Minimum=0
	MaxConcurrentReplacements *int `json:"maxConcurrentReplacements,omitempty"`

	// DeletionMode defines the deletion mode for this cluster. This can be
	// PodUpdateModeNone, PodUpdateModeAll, PodUpdateModeZone or PodUpdateModeProcessGroup. The
	// DeletionMode defines how Pods are deleted in order to update them or
	// when they are removed.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=All;Zone;ProcessGroup;None
	// +kubebuilder:default:=Zone
	DeletionMode PodUpdateMode `json:"deletionMode,omitempty"`

	// RemovalMode defines the removal mode for this cluster. This can be
	// PodUpdateModeNone, PodUpdateModeAll, PodUpdateModeZone or PodUpdateModeProcessGroup. The
	// RemovalMode defines how process groups are deleted in order when they
	// are marked for removal.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=All;Zone;ProcessGroup;None
	// +kubebuilder:default:=Zone
	RemovalMode PodUpdateMode `json:"removalMode,omitempty"`

	// WaitBetweenRemovalsSeconds defines how long to wait between the last removal and the next removal. This is only an
	// upper limit if the process group and the according resources are deleted faster than the provided duration the
	// operator will move on with the next removal. The idea is to prevent a race condition were the operator deletes
	// a resource but the Kubernetes API is slower to trigger the actual deletion, and we are running into a situation
	// where the fault tolerance check still includes the already deleted processes.
	// Defaults to 60.
	WaitBetweenRemovalsSeconds *int `json:"waitBetweenRemovalsSeconds,omitempty"`

	// PodUpdateStrategy defines how Pod spec changes are rolled out either by replacing Pods or by deleting Pods.
	// The default for this is ReplaceTransactionSystem.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=Replace;ReplaceTransactionSystem;Delete
	// +kubebuilder:default:=ReplaceTransactionSystem
	PodUpdateStrategy PodUpdateStrategy `json:"podUpdateStrategy,omitempty"`
}

// AutomaticReplacementOptions controls options for automatically replacing
// failed processes.
type AutomaticReplacementOptions struct {
	// Enabled controls whether automatic replacements are enabled.
	// The default is false.
	Enabled *bool `json:"enabled,omitempty"`

	// FailureDetectionTimeSeconds controls how long a process must be
	// failed or missing before it is automatically replaced.
	// The default is 7200 seconds, or 2 hours.
	FailureDetectionTimeSeconds *int `json:"failureDetectionTimeSeconds,omitempty"`

	// MaxConcurrentReplacements controls how many automatic replacements are allowed to take part.
	// This will take the list of current replacements and then calculate the difference between
	// maxConcurrentReplacements and the size of the list. e.g. if currently 3 replacements are
	// queued (e.g. in the processGroupsToRemove list) and maxConcurrentReplacements is 5 the operator
	// is allowed to replace at most 2 process groups. Setting this to 0 will basically disable the automatic
	// replacements.
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=0
	MaxConcurrentReplacements *int `json:"maxConcurrentReplacements,omitempty"`
}

// ProcessSettings defines process-level settings.
type ProcessSettings struct {
	// PodTemplate allows customizing the pod. If a container image with a tag is specified the operator
	// will throw an error and stop processing the cluster.
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
	CustomParameters FoundationDBCustomParameters `json:"customParameters,omitempty"`

	// This setting defines if a user provided image can have it's own tag
	// rather than getting the provided version appended.
	// You have to ensure that the specified version in the Spec is compatible
	// with the given version in your custom image.
	// +kubebuilder:default:=false
	// Deprecated: Use ImageConfigs instead.
	AllowTagOverride *bool `json:"allowTagOverride,omitempty"`
}

// GetAllowTagOverride returns the bool value for AllowTagOverride
func (processSettings *ProcessSettings) GetAllowTagOverride() bool {
	if processSettings.AllowTagOverride == nil {
		return false
	}

	return *processSettings.AllowTagOverride
}

// GetProcessSettings gets settings for a process.
func (cluster *FoundationDBCluster) GetProcessSettings(processClass ProcessClass) ProcessSettings {
	merged := ProcessSettings{}
	entries := make([]ProcessSettings, 0, 2)

	entry, present := cluster.Spec.Processes[processClass]
	if present {
		entries = append(entries, entry)
	}

	entries = append(entries, cluster.Spec.Processes[ProcessClassGeneral])
	for _, entry := range entries {
		if merged.PodTemplate == nil {
			merged.PodTemplate = entry.PodTemplate
		}
		if merged.VolumeClaimTemplate == nil {
			merged.VolumeClaimTemplate = entry.VolumeClaimTemplate
		}
		if merged.CustomParameters == nil {
			merged.CustomParameters = entry.CustomParameters
		}

		if merged.AllowTagOverride == nil {
			merged.AllowTagOverride = entry.AllowTagOverride
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
	counts := cluster.Spec.DatabaseConfiguration.RoleCounts.DeepCopy()
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
		if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 {
			counts.RemoteLogs = counts.Logs
		} else {
			counts.RemoteLogs = -1
		}
	}
	if counts.LogRouters == 0 {
		if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 {
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
		primaryStatelessCount += cluster.calculateProcessCountFromRole(1, processCounts.Ratekeeper) +
			cluster.calculateProcessCountFromRole(1, processCounts.DataDistributor)
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
	return DesiredFaultTolerance(cluster.Spec.DatabaseConfiguration.RedundancyMode)
}

// MinimumFaultDomains returns the number of fault domains the cluster needs
// to function.
func (cluster *FoundationDBCluster) MinimumFaultDomains() int {
	return MinimumFaultDomains(cluster.Spec.DatabaseConfiguration.RedundancyMode)
}

// DesiredCoordinatorCount returns the number of coordinators to recruit for
// a cluster.
func (cluster *FoundationDBCluster) DesiredCoordinatorCount() int {
	if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 {
		return 9
	}

	return cluster.MinimumFaultDomains() + cluster.DesiredFaultTolerance()
}

// CheckReconciliation compares the spec and the status to determine if
// reconciliation is complete.
func (cluster *FoundationDBCluster) CheckReconciliation(log logr.Logger) (bool, error) {
	logger := log.WithValues("method", "CheckReconciliation", "namespace", cluster.Namespace, "cluster", cluster.Name)
	var reconciled = true
	if !cluster.Status.Configured {
		cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
		return false, nil
	}

	cluster.Status.Generations = ClusterGenerationStatus{Reconciled: cluster.Status.Generations.Reconciled}

	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.IsMarkedForRemoval() {
			continue
		}

		if processGroup.GetConditionTime(ResourcesTerminating) != nil {
			logger.Info("Has process group pending to remove", "processGroupID", processGroup.ProcessGroupID, "state", "HasPendingRemoval")
			cluster.Status.Generations.HasPendingRemoval = cluster.ObjectMeta.Generation
		} else {
			logger.Info("Has process group with pending shrink", "processGroupID", processGroup.ProcessGroupID, "state", "NeedsShrink")
			cluster.Status.Generations.NeedsShrink = cluster.ObjectMeta.Generation
			reconciled = false
		}
	}

	desiredCounts, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return false, err
	}

	currentCounts := CreateProcessCountsFromProcessGroupStatus(cluster.Status.ProcessGroups, false)

	diff := desiredCounts.Diff(currentCounts)

	for _, delta := range diff {
		if delta > 0 {
			cluster.Status.Generations.NeedsGrow = cluster.ObjectMeta.Generation
			reconciled = false
		} else if delta < 0 {
			cluster.Status.Generations.NeedsShrink = cluster.ObjectMeta.Generation
			reconciled = false
		}
	}

	for _, processGroup := range cluster.Status.ProcessGroups {
		if len(processGroup.ProcessGroupConditions) > 0 && !processGroup.IsMarkedForRemoval() {
			logger.Info("Has unhealthy process group", "processGroupID", processGroup.ProcessGroupID, "state", "HasUnhealthyProcess")
			cluster.Status.Generations.HasUnhealthyProcess = cluster.ObjectMeta.Generation
			reconciled = false
		}
	}

	if !cluster.Status.Health.Available {
		logger.Info("Database unavailable", "state", "DatabaseUnavailable")
		cluster.Status.Generations.DatabaseUnavailable = cluster.ObjectMeta.Generation
		reconciled = false
	}

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	if !equality.Semantic.DeepEqual(cluster.Status.DatabaseConfiguration, desiredConfiguration) {
		logger.Info("Pending database configuration change", "state", "NeedsConfigurationChange")
		cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if cluster.Status.HasIncorrectConfigMap {
		logger.Info("Pending ConfigMap (Monitor config) configuration change", "state", "NeedsMonitorConfUpdate")
		cluster.Status.Generations.NeedsMonitorConfUpdate = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if cluster.Status.HasIncorrectServiceConfig {
		logger.Info("Pending Service configuration change", "state", "NeedsServiceUpdate")
		cluster.Status.Generations.NeedsServiceUpdate = cluster.ObjectMeta.Generation
		reconciled = false
	}

	if cluster.Status.NeedsNewCoordinators {
		logger.Info("Pending coordinator change", "state", "NeedsNewCoordinators")
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
		logger.Info("Pending TLS change", "state", "HasExtraListeners")
		cluster.Status.Generations.HasExtraListeners = cluster.ObjectMeta.Generation
		reconciled = false
	}

	lockDenyMap := make(map[string]bool, len(cluster.Spec.LockOptions.DenyList))
	for _, denyListEntry := range cluster.Spec.LockOptions.DenyList {
		lockDenyMap[denyListEntry.ID] = denyListEntry.Allow
	}

	for _, denyListID := range cluster.Status.Locks.DenyList {
		allow, present := lockDenyMap[denyListID]
		if !present {
			continue
		}
		if allow {
			logger.Info("Pending lock acquire for configuration changes", "state", "NeedsLockConfigurationChanges", "allowed", allow)
			cluster.Status.Generations.NeedsLockConfigurationChanges = cluster.ObjectMeta.Generation
			reconciled = false
		} else {
			delete(lockDenyMap, denyListID)
		}
	}

	for _, allow := range lockDenyMap {
		if !allow {
			logger.Info("Pending lock acquire for configuration changes", "state", "NeedsLockConfigurationChanges", "allowed", allow)
			cluster.Status.Generations.NeedsLockConfigurationChanges = cluster.ObjectMeta.Generation
			reconciled = false
			break
		}
	}

	if reconciled {
		cluster.Status.Generations.Reconciled = cluster.ObjectMeta.Generation
	} else if cluster.Status.Generations.Reconciled == cluster.ObjectMeta.Generation {
		cluster.Status.Generations.Reconciled = 0
	}

	return reconciled, nil
}

// GetStorageServersPerPod returns the StorageServer per Pod.
func (cluster *FoundationDBCluster) GetStorageServersPerPod() int {
	if cluster.Spec.StorageServersPerPod <= 1 {
		return 1
	}

	return cluster.Spec.StorageServersPerPod
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
	DatabaseName string `json:"databaseName,omitempty"`

	// GenerationID provides a unique ID for the current generation of
	// coordinators.
	GenerationID string `json:"generationID,omitempty"`

	// Coordinators provides the addresses of the current coordinators.
	Coordinators []string `json:"coordinators,omitempty"`
}

// ParseConnectionString parses a connection string from its string
// representation
func ParseConnectionString(str string) (ConnectionString, error) {
	components := connectionStringPattern.FindStringSubmatch(str)
	if components == nil {
		return ConnectionString{}, fmt.Errorf("invalid connection string %s", str)
	}

	coordinatorsStrings := strings.Split(components[3], ",")
	coordinators := make([]string, len(coordinatorsStrings))
	for idx, coordinatorsString := range coordinatorsStrings {
		coordinatorAddress, err := ParseProcessAddress(coordinatorsString)
		if err != nil {
			return ConnectionString{}, err
		}

		coordinators[idx] = coordinatorAddress.String()
	}

	return ConnectionString{
		components[1],
		components[2],
		coordinators,
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
func (cluster *FoundationDBCluster) GetFullAddress(address string, processNumber int) ProcessAddress {
	addresses := cluster.GetFullAddressList(address, true, processNumber)
	if len(addresses) < 1 {
		return ProcessAddress{}
	}

	// First element will always be the primary
	return addresses[0]
}

// GetFullAddressList gets the full list of public addresses we should use for a
// process.
//
// This will include the IP address, the port, and any additional flags.
//
// If a process needs multiple addresses, this will include all of them,
// separated by commas. If you pass false for primaryOnly, this will return only
// the primary address.
func (cluster *FoundationDBCluster) GetFullAddressList(address string, primaryOnly bool, processNumber int) []ProcessAddress {
	return GetFullAddressList(
		address,
		primaryOnly,
		processNumber,
		cluster.Status.RequiredAddresses.TLS,
		cluster.Status.RequiredAddresses.NonTLS)
}

// HasCoordinators checks whether this connection string matches a set of
// coordinators.
func (str *ConnectionString) HasCoordinators(coordinators []ProcessAddress) bool {
	matchedCoordinators := make(map[string]bool, len(str.Coordinators))
	for _, address := range str.Coordinators {
		matchedCoordinators[address] = false
	}

	for _, pAddr := range coordinators {
		address := pAddr.String()
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

// ContainerOverrides provides options for customizing a container created by
// the operator.
type ContainerOverrides struct {
	// EnableLivenessProbe defines if the sidecar should have a livenessProbe.
	// This setting will be ignored on the main container.
	EnableLivenessProbe *bool `json:"enableLivenessProbe,omitempty"`

	// EnableReadinessProbe defines if the sidecar should have a readinessProbe.
	// This setting will be ignored on the main container.
	EnableReadinessProbe *bool `json:"enableReadinessProbe,omitempty"`

	// EnableTLS controls whether we should be listening on a TLS connection.
	EnableTLS bool `json:"enableTls,omitempty"`

	// PeerVerificationRules provides the rules for what client certificates
	// the process should accept.
	PeerVerificationRules string `json:"peerVerificationRules,omitempty"`

	// ImageConfigs allows customizing the image that we use for
	// a container.
	ImageConfigs []ImageConfig `json:"imageConfigs,omitempty"`

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

// ImageConfig provides a policy for customizing an image.
//
// When multiple image configs are provided, they will be merged into a single
// config that will be used to define the final image. For each field, we select
// the value from the first entry in the config list that defines a value for
// that field, and matches the version of FoundationDB the image is for. Any
// config that specifies a different version than the one under consideration
// will be ignored for the purposes of defining that image.
type ImageConfig struct {
	// Version is the version of FoundationDB this policy applies to. If this is
	// blank, the policy applies to all FDB versions.
	Version string `json:"version,omitempty"`

	// BaseImage specifies the part of the image before the tag.
	BaseImage string `json:"baseImage,omitempty"`

	// Tag specifies a full image tag.
	Tag string `json:"tag,omitempty"`

	// TagSuffix specifies a suffix that will be added after the version to form
	// the full tag.
	TagSuffix string `json:"tagSuffix,omitempty"`
}

// SelectImageConfig selects image configs that apply to a version of FDB and
// merges them into a single config.
func SelectImageConfig(allConfigs []ImageConfig, versionString string) ImageConfig {
	config := ImageConfig{Version: versionString}
	for _, nextConfig := range allConfigs {
		if nextConfig.Version != "" && nextConfig.Version != versionString {
			continue
		}
		if config.BaseImage == "" {
			config.BaseImage = nextConfig.BaseImage
		}
		if config.Tag == "" {
			config.Tag = nextConfig.Tag
		}
		if config.TagSuffix == "" {
			config.TagSuffix = nextConfig.TagSuffix
		}
	}
	return config
}

// Image generates an image using a config.
func (config ImageConfig) Image() string {
	if config.Tag == "" {
		return fmt.Sprintf("%s:%s%s", config.BaseImage, config.Version, config.TagSuffix)
	}
	return fmt.Sprintf("%s:%s", config.BaseImage, config.Tag)
}

// DesiredDatabaseConfiguration builds the database configuration for the
// cluster based on its spec.
func (cluster *FoundationDBCluster) DesiredDatabaseConfiguration() DatabaseConfiguration {
	configuration := cluster.Spec.DatabaseConfiguration.NormalizeConfiguration()

	configuration.RoleCounts = cluster.GetRoleCountsWithDefaults()
	configuration.RoleCounts.Storage = 0
	if configuration.StorageEngine == StorageEngineSSD {
		configuration.StorageEngine = StorageEngineSSD2
	}
	if configuration.StorageEngine == StorageEngineMemory {
		configuration.StorageEngine = StorageEngineMemory2
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

// ProcessGroupIsBeingRemoved determines if an instance is pending removal.
func (cluster *FoundationDBCluster) ProcessGroupIsBeingRemoved(processGroupID string) bool {
	if processGroupID == "" {
		return false
	}

	if cluster.Status.PendingRemovals != nil {
		_, present := cluster.Status.PendingRemovals[processGroupID]
		if present {
			return true
		}
	}

	if cluster.Spec.PendingRemovals != nil {
		podName := fmt.Sprintf("%s-%s", cluster.Name, processGroupID)
		_, pendingRemoval := cluster.Spec.PendingRemovals[podName]
		if pendingRemoval {
			return true
		}
	}

	for _, id := range cluster.Spec.InstancesToRemove {
		if id == processGroupID {
			return true
		}
	}

	for _, id := range cluster.Spec.InstancesToRemoveWithoutExclusion {
		if id == processGroupID {
			return true
		}
	}

	for _, status := range cluster.Status.ProcessGroups {
		if status.ProcessGroupID == processGroupID && status.IsMarkedForRemoval() {
			return true
		}
	}

	for _, id := range cluster.Spec.ProcessGroupsToRemove {
		if id == processGroupID {
			return true
		}
	}

	for _, id := range cluster.Spec.ProcessGroupsToRemoveWithoutExclusion {
		if id == processGroupID {
			return true
		}
	}

	return false
}

// ShouldUseLocks determine whether we should use locks to coordinator global
// operations.
func (cluster *FoundationDBCluster) ShouldUseLocks() bool {
	disabled := cluster.Spec.LockOptions.DisableLocks
	if disabled != nil {
		return !*disabled
	}

	return cluster.Spec.FaultDomain.ZoneCount > 1 || len(cluster.Spec.DatabaseConfiguration.Regions) > 1
}

// GetLockPrefix gets the prefix for the keys where we store locking
// information.
func (cluster *FoundationDBCluster) GetLockPrefix() string {
	if cluster.Spec.LockOptions.LockKeyPrefix != "" {
		return cluster.Spec.LockOptions.LockKeyPrefix
	}

	return "\xff\x02/org.foundationdb.kubernetes-operator"
}

// GetLockDuration determines how long we hold locks for.
func (cluster *FoundationDBCluster) GetLockDuration() time.Duration {
	minutes := 10
	if cluster.Spec.LockOptions.LockDurationMinutes != nil {
		minutes = *cluster.Spec.LockOptions.LockDurationMinutes
	}
	return time.Duration(minutes) * time.Minute
}

// GetLockID gets the identifier for this instance of the operator when taking
// locks.
func (cluster *FoundationDBCluster) GetLockID() string {
	return cluster.Spec.InstanceIDPrefix
}

// NeedsExplicitListenAddress determines whether we pass a listen address
// parameter to fdbserver.
func (cluster *FoundationDBCluster) NeedsExplicitListenAddress() bool {
	source := cluster.Spec.Routing.PublicIPSource
	requiredForSource := source != nil && *source == PublicIPSourceService
	flag := cluster.Spec.UseExplicitListenAddress
	requiredForFlag := flag != nil && *flag
	return requiredForSource || requiredForFlag
}

// GetPublicIPSource returns the set PublicIPSource or the default PublicIPSourcePod
func (cluster *FoundationDBCluster) GetPublicIPSource() PublicIPSource {
	source := cluster.Spec.Routing.PublicIPSource
	if source == nil {
		return PublicIPSourcePod
	}

	return *source
}

// LockOptions provides customization for locking global operations.
type LockOptions struct {
	// DisableLocks determines whether we should disable locking entirely.
	DisableLocks *bool `json:"disableLocks,omitempty"`

	// LockKeyPrefix provides a custom prefix for the keys in the database we
	// use to store locks.
	LockKeyPrefix string `json:"lockKeyPrefix,omitempty"`

	// LockDurationMinutes determines the duration that locks should be valid
	// for.
	LockDurationMinutes *int `json:"lockDurationMinutes,omitempty"`

	// DenyList manages configuration for whether an instance of the operator
	// should be denied from taking locks.
	DenyList []LockDenyListEntry `json:"denyList,omitempty"`
}

// LockDenyListEntry models an entry in the deny list for the locking system.
type LockDenyListEntry struct {
	// The ID of the operator instance this entry is targeting.
	ID string `json:"id,omitempty"`

	// Whether the instance is allowed to take locks.
	Allow bool `json:"allow,omitempty"`
}

// ServiceConfig allows configuring services that sit in front of our pods.
// Deprecated: Use RoutingConfig instead.
type ServiceConfig struct {
	// Headless determines whether we want to run a headless service for the
	// cluster.
	Headless *bool `json:"headless,omitempty"`

	// PublicIPSource specifies what source a process should use to get its
	// public IPs.
	//
	// This supports the values `pod` and `service`.
	PublicIPSource *PublicIPSource `json:"publicIPSource,omitempty"`
}

// RoutingConfig allows configuring routing to our pods, and services that sit
// in front of them.
type RoutingConfig struct {
	// Headless determines whether we want to run a headless service for the
	// cluster.
	HeadlessService *bool `json:"headlessService,omitempty"`

	// PublicIPSource specifies what source a process should use to get its
	// public IPs.
	//
	// This supports the values `pod` and `service`.
	PublicIPSource *PublicIPSource `json:"publicIPSource,omitempty"`

	// PodIPFamily tells the pod which family of IP addresses to use.
	// You can use 4 to represent IPv4, and 6 to represent IPv6.
	// This feature is only supported in FDB 7.0 or later, and requires
	// dual-stack support in your Kubernetes environment.
	PodIPFamily *int `json:"podIPFamily,omitempty"`

	// UseDNSInClusterFile determines whether to use DNS names rather than IP
	// addresses to identify coordinators in the cluster file.
	// NOTE: This is an experimental feature, and is not supported in the
	// latest stable version of FoundationDB.
	UseDNSInClusterFile *bool `json:"useDNSInClusterFile,omitempty"`

	// DNSDomain defines the cluster domain used in a DNS name generated for a
	// service.
	// The default is `cluster.local`.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	DNSDomain *string `json:"dnsDomain,omitempty"`
}

// RequiredAddressSet provides settings for which addresses we need to listen
// on.
type RequiredAddressSet struct {
	// TLS defines whether we need to listen on a TLS address.
	TLS bool `json:"tls,omitempty"`

	// NonTLS defines whether we need to listen on a non-TLS address.
	NonTLS bool `json:"nonTLS,omitempty"`
}

// BuggifyConfig provides options for injecting faults into a cluster for testing.
type BuggifyConfig struct {
	// NoSchedule defines a list of process group IDs that should fail to schedule.
	NoSchedule []string `json:"noSchedule,omitempty"`

	// CrashLoops defines a list of process group IDs that should be put into a
	// crash looping state.
	CrashLoop []string `json:"crashLoop,omitempty"`

	// EmptyMonitorConf instructs the operator to update all of the fdbmonitor.conf
	// files to have zero fdbserver processes configured.
	EmptyMonitorConf bool `json:"emptyMonitorConf,omitempty"`
}

// LabelConfig allows customizing labels used by the operator.
type LabelConfig struct {
	// MatchLabels provides the labels that the operator should use to identify
	// resources owned by the cluster. These will automatically be applied to
	// all resources the operator creates.
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// ResourceLabels provides additional labels that the operator should apply to
	// resources it creates.
	ResourceLabels map[string]string `json:"resourceLabels,omitempty"`

	// ProcessGroupIDLabels provides the labels that we use for the process group ID
	// field. The first label will be used by the operator when filtering
	// resources.
	// +kubebuilder:validation:MaxItems=100
	ProcessGroupIDLabels []string `json:"processGroupIDLabels,omitempty"`

	// ProcessClassLabels provides the labels that we use for the process class
	// field. The first label will be used by the operator when filtering
	// resources.
	// +kubebuilder:validation:MaxItems=100
	ProcessClassLabels []string `json:"processClassLabels,omitempty"`

	// FilterOnOwnerReferences determines whether we should check that resources
	// are owned by the cluster object, in addition to the constraints provided
	// by the match labels.
	FilterOnOwnerReferences *bool `json:"filterOnOwnerReference,omitempty"`
}

// PublicIPSource models options for how a pod gets its public IP.
type PublicIPSource string

const (
	// PublicIPSourcePod specifies that a pod gets its IP from the pod IP.
	PublicIPSourcePod PublicIPSource = "pod"

	// PublicIPSourceService specifies that a pod gets its IP from a service.
	PublicIPSourceService PublicIPSource = "service"
)

// AddStorageServerPerDisk adds serverPerDisk to the status field to keep track which ConfigMaps should be kept
func (clusterStatus *FoundationDBClusterStatus) AddStorageServerPerDisk(serversPerDisk int) {
	for _, curServersPerDisk := range clusterStatus.StorageServersPerDisk {
		if curServersPerDisk == serversPerDisk {
			return
		}
	}

	clusterStatus.StorageServersPerDisk = append(clusterStatus.StorageServersPerDisk, serversPerDisk)
}

// GetMaxConcurrentAutomaticReplacements returns the cluster setting for MaxConcurrentReplacements, defaults to 1 if unset.
func (cluster *FoundationDBCluster) GetMaxConcurrentAutomaticReplacements() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements, 1)
}

// CoordinatorSelectionSetting defines the process class and the priority of it.
// A higher priority means that the process class is preferred over another.
type CoordinatorSelectionSetting struct {
	ProcessClass ProcessClass `json:"processClass,omitempty"`
	Priority     int          `json:"priority,omitempty"`
}

// IsEligibleAsCandidate checks if the given process has the right process class to be considered a valid coordinator.
// This method will always return false for non stateful process classes.
func (cluster *FoundationDBCluster) IsEligibleAsCandidate(pClass ProcessClass) bool {
	if !pClass.IsStateful() {
		return false
	}

	if len(cluster.Spec.CoordinatorSelection) == 0 {
		return pClass.IsStateful()
	}

	for _, setting := range cluster.Spec.CoordinatorSelection {
		if pClass == setting.ProcessClass {
			return true
		}
	}

	return false
}

// GetClassCandidatePriority returns the priority for a class. This will be used to sort the processes for coordinator selection
func (cluster *FoundationDBCluster) GetClassCandidatePriority(pClass ProcessClass) int {
	for _, setting := range cluster.Spec.CoordinatorSelection {
		if pClass == setting.ProcessClass {
			return setting.Priority
		}
	}

	return math.MinInt64
}

// ShouldFilterOnOwnerReferences determines if we should check owner references
// when determining if a resource is related to this cluster.
func (cluster *FoundationDBCluster) ShouldFilterOnOwnerReferences() bool {
	return cluster.Spec.LabelConfig.FilterOnOwnerReferences != nil && *cluster.Spec.LabelConfig.FilterOnOwnerReferences
}

// SkipProcessGroup checks if a ProcessGroupStatus should be skip during reconciliation.
func (cluster *FoundationDBCluster) SkipProcessGroup(processGroup *ProcessGroupStatus) bool {
	if processGroup == nil {
		return true
	}

	pendingTime := processGroup.GetConditionTime(PodPending)
	if pendingTime == nil {
		return false
	}

	return time.Unix(*pendingTime, 0).Add(cluster.GetIgnorePendingPodsDuration()).Before(time.Now())
}

// GetIgnorePendingPodsDuration returns the value of IgnorePendingPodsDuration or 5 minutes if unset.
func (cluster *FoundationDBCluster) GetIgnorePendingPodsDuration() time.Duration {
	if cluster.Spec.AutomationOptions.IgnorePendingPodsDuration == 0 {
		return 5 * time.Minute
	}

	return cluster.Spec.AutomationOptions.IgnorePendingPodsDuration
}

// GetIgnoreTerminatingPodsSeconds returns the value of IgnoreTerminatingPodsSeconds or defaults to 10 minutes.
func (cluster *FoundationDBCluster) GetIgnoreTerminatingPodsSeconds() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.IgnoreTerminatingPodsSeconds, int((10 * time.Minute).Seconds()))
}

// GetEnforceFullReplicationForDeletion returns the value of enforceFullReplicationForDeletion or true if unset.
func (cluster *FoundationDBCluster) GetEnforceFullReplicationForDeletion() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.EnforceFullReplicationForDeletion, true)
}

// GetUseNonBlockingExcludes returns the value of useNonBlockingExcludes or false if unset.
func (cluster *FoundationDBCluster) GetUseNonBlockingExcludes() bool {
	if cluster.Spec.AutomationOptions.UseNonBlockingExcludes == nil {
		return false
	}

	return *cluster.Spec.AutomationOptions.UseNonBlockingExcludes
}

// GetProcessClassLabel provides the label that this cluster is using for the
// process class when identifying resources.
func (cluster *FoundationDBCluster) GetProcessClassLabel() string {
	labels := cluster.GetProcessClassLabels()
	if len(labels) == 0 {
		return FDBProcessClassLabel
	}
	return labels[0]
}

// GetProcessGroupIDLabel provides the label that this cluster is using for the
// process group ID when identifying resources.
func (cluster *FoundationDBCluster) GetProcessGroupIDLabel() string {
	labels := cluster.GetProcessGroupIDLabels()
	if len(labels) == 0 {
		return FDBProcessGroupIDLabel
	}
	return labels[0]
}

// GetMaxConcurrentReplacements returns the maxConcurrentReplacements or defaults to math.MaxInt64
func (cluster *FoundationDBCluster) GetMaxConcurrentReplacements() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.MaxConcurrentReplacements, math.MaxInt64)
}

// PodUpdateMode defines the deletion mode for the cluster
type PodUpdateMode string

const (
	// PodUpdateModeAll deletes all process groups at once
	PodUpdateModeAll PodUpdateMode = "All"
	// PodUpdateModeZone deletes process groups in the same zone at the same time
	PodUpdateModeZone PodUpdateMode = "Zone"
	// PodUpdateModeProcessGroup deletes one process group at a time
	PodUpdateModeProcessGroup PodUpdateMode = "ProcessGroup"
	// PodUpdateModeNone defines that the operator is not allowed to update/delete any Pods.
	PodUpdateModeNone PodUpdateMode = "None"
)

// NeedsHeadlessService determines whether we need to create a headless service
// for this cluster.
func (cluster *FoundationDBCluster) NeedsHeadlessService() bool {
	return cluster.UseDNSInClusterFile() || pointer.BoolDeref(cluster.Spec.Routing.HeadlessService, false)
}

// UseDNSInClusterFile determines whether we need to use DNS entries in the
// cluster file for this cluster.
func (cluster *FoundationDBCluster) UseDNSInClusterFile() bool {
	return pointer.BoolDeref(cluster.Spec.Routing.UseDNSInClusterFile, false)
}

// GetDNSDomain gets the domain used when forming DNS names generated for a
// service.
func (cluster *FoundationDBCluster) GetDNSDomain() string {
	return pointer.StringDeref(cluster.Spec.Routing.DNSDomain, "cluster.local")
}

// GetRemovalMode returns the removal mode of the cluster or default to PodUpdateModeZone if unset.
func (cluster *FoundationDBCluster) GetRemovalMode() PodUpdateMode {
	if cluster.Spec.AutomationOptions.DeletionMode == "" {
		return PodUpdateModeZone
	}

	return cluster.Spec.AutomationOptions.DeletionMode
}

// GetWaitBetweenRemovalsSeconds returns the WaitDurationBetweenRemovals if set or defaults to 60s.
func (cluster *FoundationDBCluster) GetWaitBetweenRemovalsSeconds() int {
	duration := pointer.IntDeref(cluster.Spec.AutomationOptions.WaitBetweenRemovalsSeconds, -1)
	if duration < 0 {
		return 60
	}

	return duration
}

// PodUpdateStrategy defines how Pod spec changes should be applied.
type PodUpdateStrategy string

const (
	// PodUpdateStrategyReplacement replace all Pods if there is a spec change.
	PodUpdateStrategyReplacement PodUpdateStrategy = "Replace"
	// PodUpdateStrategyTransactionReplacement replace all transaction system Pods if there is a spec change.
	PodUpdateStrategyTransactionReplacement PodUpdateStrategy = "ReplaceTransactionSystem"
	// PodUpdateStrategyDelete delete all Pods if there is a spec change.
	PodUpdateStrategyDelete PodUpdateStrategy = "Delete"
)

// NeedsReplacement returns true if the Pod should be replaced if the Pod spec has changed
func (cluster *FoundationDBCluster) NeedsReplacement(processGroup *ProcessGroupStatus) bool {
	if cluster.Spec.AutomationOptions.PodUpdateStrategy == PodUpdateStrategyDelete {
		return false
	}

	if cluster.Spec.AutomationOptions.PodUpdateStrategy == PodUpdateStrategyReplacement {
		return true
	}

	// Default is ReplaceTransactionSystem.
	return processGroup.ProcessClass.IsTransaction()
}

// GetResourceLabels returns the resource labels for all created resources
func (cluster *FoundationDBCluster) GetResourceLabels() map[string]string {
	if cluster.Spec.LabelConfig.ResourceLabels != nil {
		return cluster.Spec.LabelConfig.ResourceLabels
	}

	return map[string]string{
		FDBClusterLabel: cluster.Name,
	}
}

// GetProcessGroupIDLabels returns the process group ID labels
func (cluster *FoundationDBCluster) GetProcessGroupIDLabels() []string {
	if cluster.Spec.LabelConfig.ProcessGroupIDLabels != nil {
		return cluster.Spec.LabelConfig.ProcessGroupIDLabels
	}

	return []string{FDBProcessGroupIDLabel}
}

// GetProcessClassLabels returns the process class labels
func (cluster *FoundationDBCluster) GetProcessClassLabels() []string {
	if cluster.Spec.LabelConfig.ProcessClassLabels != nil {
		return cluster.Spec.LabelConfig.ProcessClassLabels
	}

	return []string{FDBProcessClassLabel}
}
