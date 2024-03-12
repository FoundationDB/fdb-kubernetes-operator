/*
Copyright 2020-2022 FoundationDB project authors.

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

package v1beta2

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=fdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Generation",type="integer",JSONPath=".metadata.generation",description="Latest generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Reconciled",type="integer",JSONPath=".status.generations.reconciled",description="Last reconciled generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Available",type="boolean",JSONPath=".status.health.available",description="Database available",priority=0
// +kubebuilder:printcolumn:name="FullReplication",type="boolean",JSONPath=".status.health.fullReplication",description="Database fully replicated",priority=0
// +kubebuilder:printcolumn:name="ReconciledProcessGroups",type="integer",JSONPath=".status.reconciledProcessGroups",description="Number of reconciled process groups",priority=1
// +kubebuilder:printcolumn:name="DesiredProcessGroups",type="integer",JSONPath=".status.desiredProcessGroups",description="Desired number of process groups",priority=1
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.runningVersion",description="Running version",priority=0
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion

// FoundationDBCluster is the Schema for the foundationdbclusters API
type FoundationDBCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FoundationDBClusterSpec   `json:"spec,omitempty"`
	Status FoundationDBClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FoundationDBClusterList contains a list of FoundationDBCluster objects
type FoundationDBClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FoundationDBCluster `json:"items"`
}

var conditionsThatNeedReplacement = []ProcessGroupConditionType{MissingProcesses, PodFailing, MissingPod, MissingPVC,
	MissingService, PodPending, NodeTaintReplacing, ProcessIsMarkedAsExcluded}

const (
	oneHourDuration = 1 * time.Hour
)

func init() {
	SchemeBuilder.Register(&FoundationDBCluster{}, &FoundationDBClusterList{})
}

// FoundationDBClusterSpec defines the desired state of a cluster.
type FoundationDBClusterSpec struct {
	// Version defines the version of FoundationDB the cluster should run.
	// +kubebuilder:validation:Pattern:=(\d+)\.(\d+)\.(\d+)
	Version string `json:"version"`

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

	// ProcessGroupsToRemove defines the process groups that we should remove from the
	// cluster. This list contains the process group IDs.
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=500
	ProcessGroupsToRemove []ProcessGroupID `json:"processGroupsToRemove,omitempty"`

	// ProcessGroupsToRemoveWithoutExclusion defines the process groups that we should
	// remove from the cluster without excluding them. This list contains the
	// process group IDs.
	//
	// This should be used for cases where a pod does not have an IP address and
	// you want to remove it and destroy its volume without confirming the data
	// is fully replicated.
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=500
	ProcessGroupsToRemoveWithoutExclusion []ProcessGroupID `json:"processGroupsToRemoveWithoutExclusion,omitempty"`

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

	// ProcessGroupIDPrefix defines a prefix to append to the process group IDs in the
	// locality fields.
	//
	// This must be a valid Kubernetes label value. See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
	// for more details on that.
	// +kubebuilder:validation:MaxLength=43
	// +kubebuilder:validation:Pattern:=^[a-z0-9A-Z]([\-._a-z0-9A-Z])*[a-z0-9A-Z]$
	ProcessGroupIDPrefix string `json:"processGroupIDPrefix,omitempty"`

	// LockOptions allows customizing how we manage locks for global operations.
	LockOptions LockOptions `json:"lockOptions,omitempty"`

	// Routing defines the configuration for routing to our pods.
	Routing RoutingConfig `json:"routing,omitempty"`

	// IgnoreUpgradabilityChecks determines whether we should skip the check for
	// client compatibility when performing an upgrade.
	IgnoreUpgradabilityChecks bool `json:"ignoreUpgradabilityChecks,omitempty"`

	// Buggify defines settings for injecting faults into a cluster for testing.
	Buggify BuggifyConfig `json:"buggify,omitempty"`

	// StorageServersPerPod defines how many Storage Servers should run in
	// a single process group (Pod). This number defines the number of processes running
	// in one Pod whereas the ProcessCounts defines the number of Pods created.
	// This means that you end up with ProcessCounts["storage"] * StorageServersPerPod
	// storage processes.
	StorageServersPerPod int `json:"storageServersPerPod,omitempty"`

	// LogServersPerPod defines how many Log Servers should run in
	// a single process group (Pod). This number defines the number of processes running
	// in one Pod whereas the ProcessCounts defines the number of Pods created.
	// This means that you end up with ProcessCounts["Log"] * LogServersPerPod
	// log processes. This also affects processes with the transaction class.
	LogServersPerPod int `json:"logServersPerPod,omitempty"`

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
	// Deprecated: This setting will be removed in the next major release.
	UseExplicitListenAddress *bool `json:"useExplicitListenAddress,omitempty"`

	// UseUnifiedImage determines if we should use the unified image rather than
	// separate images for the main container and the sidecar container.
	UseUnifiedImage *bool `json:"useUnifiedImage,omitempty"`

	// MaxZonesWithUnavailablePods defines the maximum number of zones that can have unavailable pods during the update process.
	// When unset, there is no limit to the  number of zones with unavailable pods.
	MaxZonesWithUnavailablePods *int `json:"maxZonesWithUnavailablePods,omitempty"`
}

// ImageType defines a single kind of images used in the cluster.
// +kubebuilder:validation:MaxLength=1024
type ImageType string

// FoundationDBClusterStatus defines the observed state of FoundationDBCluster
type FoundationDBClusterStatus struct {
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

	// StorageServersPerDisk defines the storageServersPerPod observed in the cluster.
	// If there are more than one value in the slice the reconcile phase is not finished.
	// +kubebuilder:validation:MaxItems=5
	StorageServersPerDisk []int `json:"storageServersPerDisk,omitempty"`

	// LogServersPerDisk defines the LogServersPerDisk observed in the cluster.
	// If there are more than one value in the slice the reconcile phase is not finished.
	// +kubebuilder:validation:MaxItems=5
	LogServersPerDisk []int `json:"logServersPerDisk,omitempty"`

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

	// MaintenenanceModeInfo contains information regarding process groups in maintenance mode
	// Deprecated: This setting it not used anymore.
	MaintenanceModeInfo MaintenanceModeInfo `json:"maintenanceModeInfo,omitempty"`

	// DesiredProcessGroups reflects the number of expected running process groups.
	DesiredProcessGroups int `json:"desiredProcessGroups,omitempty"`

	// ReconciledProcessGroups reflects the number of process groups that have no condition and are not marked for removal.
	ReconciledProcessGroups int `json:"reconciledProcessGroups,omitempty"`
}

// MaintenanceModeInfo contains information regarding the zone and process groups that are put
// into maintenance mode by the operator
type MaintenanceModeInfo struct {
	// StartTimestamp provides the timestamp when this zone is put into maintenance mode
	// Deprecated: This setting it not used anymore.
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`
	// ZoneID that is placed in maintenance mode
	// Deprecated: This setting it not used anymore.
	ZoneID FaultDomain `json:"zoneID,omitempty"`
	// ProcessGroups that are placed in maintenance mode
	// +kubebuilder:validation:MaxItems=200
	// Deprecated: This setting it not used anymore.
	ProcessGroups []string `json:"processGroups,omitempty"`
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
	ProcessGroupID ProcessGroupID `json:"processGroupID,omitempty"`
	// ProcessClass represents the class the process group has.
	ProcessClass ProcessClass `json:"processClass,omitempty"`
	// Addresses represents the list of addresses the process group has been known to have.
	Addresses []string `json:"addresses,omitempty"`
	// RemoveTimestamp if not empty defines when the process group was marked for removal.
	RemovalTimestamp *metav1.Time `json:"removalTimestamp,omitempty"`
	// ExclusionTimestamp defines when the process group has been fully excluded.
	// This is only used within the reconciliation process, and should not be considered authoritative.
	ExclusionTimestamp *metav1.Time `json:"exclusionTimestamp,omitempty"`
	// ExclusionSkipped determines if exclusion has been skipped for a process, which will allow the process group to be removed without exclusion.
	ExclusionSkipped bool `json:"exclusionSkipped,omitempty"`
	// ProcessGroupConditions represents a list of degraded conditions that the process group is in.
	ProcessGroupConditions []*ProcessGroupCondition `json:"processGroupConditions,omitempty"`
	// FaultDomain represents the last seen fault domain from the cluster status. This can be used if a Pod or process
	// is not running and would be missing in the cluster status.
	FaultDomain FaultDomain `json:"faultDomain,omitempty"`
}

// String returns string representation.
func (processGroupStatus *ProcessGroupStatus) String() string {
	var sb strings.Builder

	sb.WriteString("ID: ")
	sb.WriteString(string(processGroupStatus.ProcessGroupID))
	sb.WriteString(", Class: ")
	sb.WriteString(string(processGroupStatus.ProcessClass))
	sb.WriteString(", Addresses: ")
	sb.WriteString(strings.Join(processGroupStatus.Addresses, ","))

	sb.WriteString(", RemovalTimestamp: ")
	if processGroupStatus.RemovalTimestamp.IsZero() {
		sb.WriteString("-")
	} else {
		sb.WriteString(processGroupStatus.RemovalTimestamp.String())
	}

	sb.WriteString(", ExclusionTimestamp: ")
	if processGroupStatus.ExclusionTimestamp.IsZero() {
		sb.WriteString("-")
	} else {
		sb.WriteString(processGroupStatus.ExclusionTimestamp.String())
	}

	sb.WriteString(", ExclusionSkipped: ")
	sb.WriteString(strconv.FormatBool(processGroupStatus.ExclusionSkipped))

	sb.WriteString(", ProcessGroupConditions: ")
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		sb.WriteString(condition.String())
	}

	sb.WriteString(", FaultDomain: ")
	sb.WriteString(string(processGroupStatus.FaultDomain))

	return sb.String()
}

// FaultDomain represents the FaultDomain of a process group
// +kubebuilder:validation:MaxLength=512
type FaultDomain string

// ProcessGroupID represents the ID of the process group
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern:=^(([\w-]+)-(\d+)|\*)$
type ProcessGroupID string

// GetIDNumber returns the ID number of the provided process group ID. This will be the suffix number, e.g. for the
// process group ID "testing-storage-12" this will return 12.
func (processGroupID ProcessGroupID) GetIDNumber() (int, error) {
	tmp := string(processGroupID)
	idx := strings.LastIndex(tmp, "-")
	// The ID number will always be the suffix after the last '-'.
	idNum, err := strconv.Atoi(tmp[idx+1:])
	if err != nil || idx <= 0 {
		return -1, fmt.Errorf("could not parse process group ID %s", tmp)
	}

	return idNum, nil
}

// GetExclusionString returns the exclusion string
func (processGroupStatus *ProcessGroupStatus) GetExclusionString() string {
	return fmt.Sprintf("%s:%s", FDBLocalityExclusionPrefix, processGroupStatus.ProcessGroupID)
}

// IsExcluded returns if a process group is excluded
func (processGroupStatus *ProcessGroupStatus) IsExcluded() bool {
	return !processGroupStatus.ExclusionTimestamp.IsZero() || processGroupStatus.ExclusionSkipped
}

// SetExclude marks a process group as excluded and will reset the process group conditions to only include the ResourcesTerminating
// if already set, otherwise the conditions will be an empty slice. This reflects the operator behaviour that process
// groups that are marked for removal and are fully excluded will only have the ResourcesTerminating condition.
func (processGroupStatus *ProcessGroupStatus) SetExclude() {
	if !processGroupStatus.ExclusionTimestamp.IsZero() {
		return
	}

	processGroupStatus.ExclusionTimestamp = &metav1.Time{Time: time.Now()}
	// Reset all previous conditions as the operator will only track the ResourcesTerminating condition for process
	// groups marked as removal. If the ResourcesTerminating condition is already set we are not removing it.
	newConditions := make([]*ProcessGroupCondition, 0, 1)
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType != ResourcesTerminating {
			continue
		}

		newConditions = append(newConditions, condition)
	}

	processGroupStatus.ProcessGroupConditions = newConditions
}

// IsMarkedForRemoval returns if a process group is marked for removal
func (processGroupStatus *ProcessGroupStatus) IsMarkedForRemoval() bool {
	return !processGroupStatus.RemovalTimestamp.IsZero()
}

// MarkForRemoval marks a process group for removal. If the RemovalTimestamp is already set it won't be changed.
func (processGroupStatus *ProcessGroupStatus) MarkForRemoval() {
	if !processGroupStatus.RemovalTimestamp.IsZero() {
		return
	}

	processGroupStatus.RemovalTimestamp = &metav1.Time{Time: time.Now()}
}

// GetPodName returns the Pod name for the associated Process Group.
func (processGroupStatus *ProcessGroupStatus) GetPodName(cluster *FoundationDBCluster) string {
	var sb strings.Builder
	sb.WriteString(cluster.Name)
	sb.WriteString("-")
	// The Pod name will always be in the format ${cluster}-${process-class}-${id}. The ID is currently not available
	// in the processGroupStatus without doing any parsing, so we have to use the Process Group ID, which might contain
	// a prefix, so we take the part after the prefix, which will be ${process-class}-${id}.
	sanitizedProcessGroup := strings.ReplaceAll(string(processGroupStatus.ProcessGroupID), "_", "-")
	sanitizedProcessClass := strings.ReplaceAll(string(processGroupStatus.ProcessClass), "_", "-")

	idx := strings.Index(sanitizedProcessGroup, sanitizedProcessClass)
	sb.WriteString(sanitizedProcessGroup[idx:])

	return sb.String()
}

// NeedsReplacement checks if the ProcessGroupStatus has conditions that require a replacement of the failed Process Group.
// The method will return the failure condition and the timestamp. If no failure is detected an empty condition and a 0
// will be returned.
func (processGroupStatus *ProcessGroupStatus) NeedsReplacement(failureTime int, taintReplacementTime int) (ProcessGroupConditionType, int64) {
	var earliestFailureTime int64 = math.MaxInt64
	var earliestTaintReplacementTime int64 = math.MaxInt64

	// If the process group is already marked for removal we can ignore it.
	if processGroupStatus.IsMarkedForRemoval() {
		return "", 0
	}

	var failureCondition ProcessGroupConditionType
	for _, conditionType := range conditionsThatNeedReplacement {
		conditionTimePtr := processGroupStatus.GetConditionTime(conditionType)
		if conditionTimePtr == nil {
			continue
		}

		conditionTime := *conditionTimePtr
		if conditionType == NodeTaintReplacing {
			if earliestTaintReplacementTime > conditionTime {
				earliestTaintReplacementTime = conditionTime
			}

			failureCondition = conditionType
			continue
		}

		if earliestFailureTime > conditionTime {
			earliestFailureTime = conditionTime
			failureCondition = conditionType
		}
	}

	failureWindowStart := time.Now().Add(-1 * time.Duration(failureTime) * time.Second).Unix()
	if earliestFailureTime < failureWindowStart {
		return failureCondition, earliestFailureTime
	}

	taintWindowStart := time.Now().Add(-1 * time.Duration(taintReplacementTime) * time.Second).Unix()
	if earliestTaintReplacementTime < taintWindowStart {
		return failureCondition, earliestTaintReplacementTime
	}

	// No failure detected.
	return "", 0
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
func (processGroupStatus *ProcessGroupStatus) AllAddressesExcluded(logger logr.Logger, remainingMap map[string]bool) (bool, error) {
	if processGroupStatus.IsExcluded() {
		return true, nil
	}

	localityExclusionString := processGroupStatus.GetExclusionString()
	if isRemaining, isPresent := remainingMap[localityExclusionString]; isPresent {
		if isRemaining {
			return false, fmt.Errorf("process has missing exclusion string in exclusion results: %s", localityExclusionString)
		}
		logger.V(1).Info("process group is fully excluded based on locality based exclusions", "processGroupID", processGroupStatus.ProcessGroupID, "exclusionString", localityExclusionString)
		return true, nil
	}

	// If the process group has no addresses assigned we cannot remove it safely and we have to set the skip exclusion.
	if len(processGroupStatus.Addresses) == 0 {
		pendingTime := processGroupStatus.GetConditionTime(PodPending)
		// If the process group has the PodPending condition for more than 1 hour we allow to remove this process group.
		if pendingTime != nil && time.Since(time.Unix(*pendingTime, 0)) > oneHourDuration {
			logger.Info("allow removal of process group without address that is stuck in pending over 1 hour", "processGroupID", processGroupStatus.ProcessGroupID)
			return true, nil
		}

		return false, fmt.Errorf("process has no addresses, cannot safely determine if process can be removed")
	}

	// If a process group has more than one address we have to make sure that all the provided addresses are part
	// of the remainingMap as excluded.
	for _, address := range processGroupStatus.Addresses {
		isRemaining, isPresent := remainingMap[address]
		if !isPresent || isRemaining {
			return false, fmt.Errorf("process has missing address in exclusion results: %s", address)
		}
	}

	return true, nil
}

// NewProcessGroupStatus returns a new GroupStatus for the given processGroupID and processClass.
func NewProcessGroupStatus(processGroupID ProcessGroupID, processClass ProcessClass, addresses []string) *ProcessGroupStatus {
	initialConditions := []*ProcessGroupCondition{
		NewProcessGroupCondition(MissingProcesses),
		NewProcessGroupCondition(MissingPod),
	}

	if processClass.IsStateful() {
		initialConditions = append(initialConditions, NewProcessGroupCondition(MissingPVC))
	}

	return &ProcessGroupStatus{
		ProcessGroupID:         processGroupID,
		ProcessClass:           processClass,
		Addresses:              addresses,
		ProcessGroupConditions: initialConditions,
	}
}

// FindProcessGroupByID finds a process group status for a given processGroupID.
func FindProcessGroupByID(processGroups []*ProcessGroupStatus, processGroupID ProcessGroupID) *ProcessGroupStatus {
	for _, processGroup := range processGroups {
		if processGroup.ProcessGroupID == processGroupID {
			return processGroup
		}
	}

	return nil
}

// ContainsProcessGroupID evaluates if the ProcessGroupStatus contains a given processGroupID.
func ContainsProcessGroupID(processGroups []*ProcessGroupStatus, processGroupID ProcessGroupID) bool {
	return FindProcessGroupByID(processGroups, processGroupID) != nil
}

// MarkProcessGroupForRemoval sets the remove flag for the given process and ensures that the address is added.
func MarkProcessGroupForRemoval(processGroups []*ProcessGroupStatus, processGroupID ProcessGroupID, processClass ProcessClass, address string) (bool, *ProcessGroupStatus) {
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
func (processGroupStatus *ProcessGroupStatus) UpdateCondition(conditionType ProcessGroupConditionType, set bool) {
	if set {
		processGroupStatus.addCondition(conditionType)
		return
	}

	processGroupStatus.removeCondition(conditionType)
}

// UpdateConditionTime will update the conditionType's condition time to newTime
// If the conditionType does not exist, the function is no-op
func (processGroupStatus *ProcessGroupStatus) UpdateConditionTime(conditionType ProcessGroupConditionType, newTime int64) {
	for i, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType == conditionType {
			processGroupStatus.ProcessGroupConditions[i].Timestamp = newTime
			break
		}
	}
}

// addCondition will add the condition to the ProcessGroupStatus. If the condition is already present this method will not
// change the timestamp. If a process group is marked for removal and exclusion only the ResourcesTerminating can be added
// and all other conditions will be reset.
func (processGroupStatus *ProcessGroupStatus) addCondition(conditionType ProcessGroupConditionType) {
	// Check if we already got this condition in the current ProcessGroupStatus.
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType == conditionType {
			return
		}
	}

	// If a process group is marked for removal and is fully excluded we only keep the ResourcesTerminating condition.
	if processGroupStatus.IsMarkedForRemoval() && processGroupStatus.IsExcluded() {
		if conditionType != ResourcesTerminating {
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
func FilterByCondition(processGroupStatus []*ProcessGroupStatus, conditionType ProcessGroupConditionType, ignoreRemoved bool) []ProcessGroupID {
	return FilterByConditions(processGroupStatus, map[ProcessGroupConditionType]bool{conditionType: true}, ignoreRemoved)
}

// FilterByConditions returns a string slice of all ProcessGroupIDs whose
// conditions match a set of rules.
//
// If a condition is mapped to true in the conditionRules map, only process
// groups with that condition will be returned. If a condition is mapped to
// false in the conditionRules map, only process groups without that condition
// will be returned.
func FilterByConditions(processGroupStatus []*ProcessGroupStatus, conditionRules map[ProcessGroupConditionType]bool, ignoreRemoved bool) []ProcessGroupID {
	result := make([]ProcessGroupID, 0, len(processGroupStatus))

	for _, groupStatus := range processGroupStatus {
		if ignoreRemoved && groupStatus.IsMarkedForRemoval() {
			continue
		}

		if groupStatus.MatchesConditions(conditionRules) {
			result = append(result, groupStatus.ProcessGroupID)
		}
	}

	return result
}

// MatchesConditions checks if the provided conditionRules matches in the process group's conditions
//
// If a condition is mapped to true in the conditionRules map, this condition must be present in the process group.
// If a condition is mapped to false in the conditionRules map, the condition must be absent in the process group.
func (processGroupStatus *ProcessGroupStatus) MatchesConditions(conditionRules map[ProcessGroupConditionType]bool) bool {
	matchingConditions := make(map[ProcessGroupConditionType]bool, len(conditionRules))

	for conditionRule := range conditionRules {
		matchingConditions[conditionRule] = false
	}

	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if _, hasRule := conditionRules[condition.ProcessGroupConditionType]; hasRule {
			matchingConditions[condition.ProcessGroupConditionType] = true
		}
	}

	return equality.Semantic.DeepEqual(matchingConditions, conditionRules)
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

// IsUnderMaintenance checks if the process is in maintenance zone.
func (processGroupStatus *ProcessGroupStatus) IsUnderMaintenance(maintenanceZone FaultDomain) bool {
	// Only storage processes are affected by the maintenance zone.
	if processGroupStatus.ProcessClass != ProcessClassStorage {
		return false
	}

	// If the maintenanceZone is not set or the Process Group has not fault domain set, return false.
	if maintenanceZone == "" || processGroupStatus.FaultDomain == "" {
		return false
	}

	return processGroupStatus.FaultDomain == maintenanceZone
}

// GetCondition returns the ProcessGroupStatus's ProcessGroupCondition that matches the conditionType;
// It returns nil if the ProcessGroupStatus doesn't have a matching condition
func (processGroupStatus *ProcessGroupStatus) GetCondition(conditionType ProcessGroupConditionType) *ProcessGroupCondition {
	for _, condition := range processGroupStatus.ProcessGroupConditions {
		if condition.ProcessGroupConditionType == conditionType {
			return condition
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

// String returns the string representation for the condition.
func (condition *ProcessGroupCondition) String() string {
	var sb strings.Builder

	sb.WriteString("Condition: ")
	sb.WriteString(string(condition.ProcessGroupConditionType))
	sb.WriteString(" Timestamp: ")
	sb.WriteString(time.Unix(condition.Timestamp, 0).String())

	return sb.String()
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
	// NodeTaintDetected represents a Pod's node is tainted but not long enough for operator to replace it.
	// If a node is tainted with a taint that shouldn't trigger replacements, NodeTaintDetected won't be added to the pod
	NodeTaintDetected ProcessGroupConditionType = "NodeTaintDetected"
	// NodeTaintReplacing represents a Pod whose node has been tainted and the operator should replace the Pod
	NodeTaintReplacing ProcessGroupConditionType = "NodeTaintReplacing"
	// ProcessIsMarkedAsExcluded represents a process group where at least one process is excluded.
	ProcessIsMarkedAsExcluded ProcessGroupConditionType = "ProcessIsMarkedAsExcluded"
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
		NodeTaintDetected,
		NodeTaintReplacing,
		ProcessIsMarkedAsExcluded,
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
	case "ResourcesTerminating":
		return ResourcesTerminating, nil
	case "SidecarUnreachable":
		return SidecarUnreachable, nil
	case "PodPending":
		return PodPending, nil
	case "Ready":
		return ReadyCondition, nil
	case "NodeTaintDetected":
		return NodeTaintDetected, nil
	case "NodeTaintReplacing":
		return NodeTaintReplacing, nil
	case "ProcessIsMarkedAsExcluded":
		return ProcessIsMarkedAsExcluded, nil
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

	// HasPendingRemoval provides the last generation that has pods that have
	// been excluded but are pending being removed.
	//
	// A cluster in this state is considered reconciled, but we track this in
	// the status to allow users of the operator to track when the removal
	// is fully complete.
	HasPendingRemoval int64 `json:"hasPendingRemoval,omitempty"`

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

// FoundationDBClusterAutomationOptions provides flags for enabling or disabling
// operations that can be performed on a cluster.
type FoundationDBClusterAutomationOptions struct {
	// ConfigureDatabase defines whether the operator is allowed to reconfigure
	// the database.
	ConfigureDatabase *bool `json:"configureDatabase,omitempty"`

	// KillProcesses defines whether the operator is allowed to bounce fdbserver
	// processes.
	KillProcesses *bool `json:"killProcesses,omitempty"`

	// CacheDatabaseStatusForReconciliation defines whether the operator is using the same FoundationDB machine-readable
	// status for all sub-reconcilers or if the machine-readable status should be fetched by ever sub-reconciler if
	// required. Enabling this setting might improve the operator reconciliation speed for large clusters.
	CacheDatabaseStatusForReconciliation *bool `json:"cacheDatabaseStatusForReconciliation,omitempty"`

	// Replacements contains options for automatically replacing failed
	// processes.
	Replacements AutomaticReplacementOptions `json:"replacements,omitempty"`

	// IgnorePendingPodsDuration defines how long a Pod has to be in the Pending Phase before
	// ignore it during reconciliation. This prevents Pod that are stuck in Pending to block
	// further reconciliation.
	IgnorePendingPodsDuration time.Duration `json:"ignorePendingPodsDuration,omitempty"`

	// UseNonBlockingExcludes defines whether the operator is allowed to use non blocking exclude commands.
	// The default is false.
	UseNonBlockingExcludes *bool `json:"useNonBlockingExcludes,omitempty"`

	// UseLocalitiesForExclusion defines whether the exclusions are done using localities instead of IP addresses. This
	// feature requires at least FDB 7.1.42 or 7.3.26.
	// The default is false.
	UseLocalitiesForExclusion *bool `json:"useLocalitiesForExclusion,omitempty"`

	// IgnoreTerminatingPodsSeconds defines how long a Pod has to be in the Terminating Phase before
	// we ignore it during reconciliation. This prevents Pod that are stuck in Terminating to block
	// further reconciliation.
	IgnoreTerminatingPodsSeconds *int `json:"ignoreTerminatingPodsSeconds,omitempty"`

	// IgnoreMissingProcessesSeconds defines how long a process group has to be in the MissingProcess condition until
	// it will be ignored during reconciliation. This prevents that a process will block reconciliation.
	IgnoreMissingProcessesSeconds *int `json:"ignoreMissingProcessesSeconds,omitempty"`

	// FailedPodDurationSeconds defines the duration a Pod can stay in the deleted state (deletionTimestamp != 0) before
	// it gets marked as PodFailed. This is important in cases where a fdbserver process is still reporting but the
	// Pod resource is marked for deletion. This can happen when the kubelet or a node fails. Setting this condition
	// will ensure that the operator is replacing affected Pods.
	FailedPodDurationSeconds *int `json:"failedPodDurationSeconds,omitempty"`

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

	// UseManagementAPI defines if the operator should make use of the management API instead of
	// using fdbcli to interact with the FoundationDB cluster.
	UseManagementAPI *bool `json:"useManagementAPI,omitempty"`

	// MaintenanceModeOptions contains options for maintenance mode related settings.
	MaintenanceModeOptions MaintenanceModeOptions `json:"maintenanceModeOptions,omitempty"`

	// IgnoreLogGroupsForUpgrade defines the list of LogGroups that should be ignored during fdb version upgrade.
	// The default is a list that includes "fdb-kubernetes-operator".
	// +kubebuilder:validation:MaxItems=10
	IgnoreLogGroupsForUpgrade []LogGroup `json:"ignoreLogGroupsForUpgrade,omitempty"`
}

// LogGroup represents a LogGroup used by a FoundationDB process to log trace events. The LogGroup can be used to filter
// clients during an upgrade.
// +kubebuilder:validation:MaxLength=256
type LogGroup string

// MaintenanceModeOptions controls options for placing zones in maintenance mode.
type MaintenanceModeOptions struct {
	// UseMaintenanceModeChecker defines whether the operator is allowed to use maintenance mode before updating pods.
	// If this setting is set to true the operator will set and reset the maintenance mode when updating pods.
	// Default is false.
	UseMaintenanceModeChecker *bool `json:"UseMaintenanceModeChecker,omitempty"`

	// ResetMaintenanceMode defines whether the operator should reset the maintenance mode if all storage processes
	// under the maintenance zone have been restarted. The default is false. If UseMaintenanceModeChecker is set to true
	// the operator will be allowed to reset the maintenance mode.
	// TODO (johscheuer): Link to documentation!
	ResetMaintenanceMode *bool `json:"resetMaintenanceMode,omitempty"`

	// MaintenanceModeTimeSeconds provides the duration for the zone to be in maintenance. It will automatically be switched off after the time elapses.
	// Default is 600.
	MaintenanceModeTimeSeconds *int `json:"maintenanceModeTimeSeconds,omitempty"`
}

// TaintReplacementOption defines the taint key and taint duration the operator will react to a tainted node
// Example of TaintReplacementOption
//   - key: "example.org/maintenance"
//     durationInSeconds: 7200 # Ensure the taint is present for at least 2 hours before replacing Pods on a node with this taint.
//   - key: "*" # The wildcard would allow to define a catch all configuration
//     durationInSeconds: 3600 # Ensure the taint is present for at least 1 hour before replacing Pods on a node with this taint
//
// Setting durationInSeconds to the maximum of int64 will practically disable the taint key.
// When a Node taint key matches both an exact TaintReplacementOption key and a wildcard key, the exact matched key will be used.
type TaintReplacementOption struct {
	// Tainted key
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern:=^([\-._\/a-z0-9A-Z\*])*$
	// +kubebuilder:validation:Required
	Key *string `json:"key,omitempty"`

	// The tainted key must be present for DurationInSeconds before operator replaces pods on the node with this taint;
	// DurationInSeconds cannot be a negative number.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	DurationInSeconds *int64 `json:"durationInSeconds,omitempty"`
}

// AutomaticReplacementOptions controls options for automatically replacing
// failed processes.
type AutomaticReplacementOptions struct {
	// Enabled controls whether automatic replacements are enabled.
	// The default is false.
	Enabled *bool `json:"enabled,omitempty"`

	// FaultDomainBasedReplacements controls whether automatic replacements are targeting all failed process groups
	// in a fault domain or only specific Process Groups. If this setting is enabled, the number of different fault
	// domains that can have all their failed process groups replaced at the same time will be equal to MaxConcurrentReplacements.
	// e.g. MaxConcurrentReplacements = 2 would mean that at most 2 different fault domains can have
	// their failed process groups replaced at the same time.
	// The default is false.
	FaultDomainBasedReplacements *bool `json:"faultDomainBasedReplacements,omitempty"`

	// FailureDetectionTimeSeconds controls how long a process must be
	// failed or missing before it is automatically replaced.
	// The default is 7200 seconds, or 2 hours.
	FailureDetectionTimeSeconds *int `json:"failureDetectionTimeSeconds,omitempty"`

	// TaintReplacementTimeSeconds controls how long a pod stays in NodeTaintReplacing condition
	// before it is automatically replaced.
	// The default is 1800 seconds, i.e., 30min
	TaintReplacementTimeSeconds *int `json:"taintReplacementTimeSeconds,omitempty"`

	// MaxConcurrentReplacements controls how many automatic replacements are allowed to take part.
	// This will take the list of current replacements and then calculate the difference between
	// maxConcurrentReplacements and the size of the list. e.g. if currently 3 replacements are
	// queued (e.g. in the processGroupsToRemove list) and maxConcurrentReplacements is 5 the operator
	// is allowed to replace at most 2 process groups. Setting this to 0 will basically disable the automatic
	// replacements.
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=0
	MaxConcurrentReplacements *int `json:"maxConcurrentReplacements,omitempty"`

	// TaintReplacementOption controls which taint label the operator will react to.
	// +kubebuilder:validation:MaxItems=32
	TaintReplacementOptions []TaintReplacementOption `json:"taintReplacementOptions,omitempty"`
}

// ProcessSettings defines process-level settings.
type ProcessSettings struct {
	// PodTemplate allows customizing the pod. If a container image with a tag is specified the operator
	// will throw an error and stop processing the cluster.
	PodTemplate *corev1.PodTemplateSpec `json:"podTemplate,omitempty"`

	// VolumeClaimTemplate allows customizing the persistent volume claim for the
	// pod.
	VolumeClaimTemplate *corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`

	// CustomParameters defines additional parameters to pass to the fdbserver
	// process.
	CustomParameters FoundationDBCustomParameters `json:"customParameters,omitempty"`
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
	}

	return merged
}

// GetRoleCountsWithDefaults gets the role counts from the cluster spec and
// fills in default values for any role counts that are 0.
//
// The default Storage value will be 2F + 1, where F is the cluster's fault
// tolerance.
//
// The default Logs value will be 3 or 4 for three_data_hall.
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
	// We can ignore the error here since the version will be validated in an earlier step.
	version, _ := ParseFdbVersion(cluster.GetRunningVersion())
	return cluster.Spec.DatabaseConfiguration.GetRoleCountsWithDefaults(version, cluster.DesiredFaultTolerance())
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
// and fills in default values for any counts that are 0. The number of storage processes
// will only reflect the number of Pods that will be created to host storage server processes.
// If storageServersPerPod is set the total amount of storage server processes will be
// the storage process count multiplied by storageServersPerPod.
func (cluster *FoundationDBCluster) GetProcessCountsWithDefaults() (ProcessCounts, error) {
	roleCounts := cluster.GetRoleCountsWithDefaults()
	processCounts := cluster.Spec.ProcessCounts.DeepCopy()

	var isSatellite, isMain bool
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
			cluster.calculateProcessCountFromRole(roleCounts.Resolvers, processCounts.Resolution)
		primaryStatelessCount += cluster.calculateProcessCountFromRole(1, processCounts.Ratekeeper) +
			cluster.calculateProcessCountFromRole(1, processCounts.DataDistributor)

		fdbVersion, err := ParseFdbVersion(cluster.GetRunningVersion())
		if err != nil {
			return *processCounts, err
		}

		if fdbVersion.HasSeparatedProxies() && cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured() {
			primaryStatelessCount += cluster.calculateProcessCountFromRole(roleCounts.GrvProxies, processCounts.GrvProxy)
			primaryStatelessCount += cluster.calculateProcessCountFromRole(roleCounts.CommitProxies, processCounts.CommitProxy)
		} else {
			primaryStatelessCount += cluster.calculateProcessCountFromRole(roleCounts.Proxies, processCounts.Proxy)
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
	return DesiredFaultTolerance(cluster.Spec.DatabaseConfiguration.RedundancyMode)
}

// MinimumFaultDomains returns the number of fault domains the cluster needs
// to function.
func (cluster *FoundationDBCluster) MinimumFaultDomains() int {
	return MinimumFaultDomains(cluster.Spec.DatabaseConfiguration.RedundancyMode)
}

// DesiredCoordinatorCount returns the number of coordinators to recruit for a cluster.
func (cluster *FoundationDBCluster) DesiredCoordinatorCount() int {
	if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 || cluster.Spec.DatabaseConfiguration.RedundancyMode == RedundancyModeThreeDataHall {
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
		logger.Info("Pending initial database configuration", "state", "NeedsConfigurationChange")
		cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
		return false, nil
	}

	cluster.Status.Generations = ClusterGenerationStatus{
		Reconciled: cluster.Status.Generations.Reconciled,
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

	cluster.Status.DesiredProcessGroups = desiredCounts.Total()

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			if processGroup.GetConditionTime(ResourcesTerminating) != nil {
				logger.Info("Has process group pending to remove", "processGroupID", processGroup.ProcessGroupID, "state", "HasPendingRemoval")
				cluster.Status.Generations.HasPendingRemoval = cluster.ObjectMeta.Generation
			} else {
				logger.Info("Has process group with pending shrink", "processGroupID", processGroup.ProcessGroupID, "state", "NeedsShrink")
				cluster.Status.Generations.NeedsShrink = cluster.ObjectMeta.Generation
				reconciled = false
			}

			continue
		}

		if len(processGroup.ProcessGroupConditions) > 0 {
			conditions := make([]ProcessGroupConditionType, 0, len(processGroup.ProcessGroupConditions))
			for _, condition := range processGroup.ProcessGroupConditions {
				// If there is at least one process with an incorrect command line, that means the operator has to restart
				// processes.
				if condition.ProcessGroupConditionType == IncorrectCommandLine && cluster.Status.Generations.NeedsBounce == 0 {
					logger.V(1).Info("Pending restart of fdbserver processes", "state", "NeedsBounce")
					cluster.Status.Generations.NeedsBounce = cluster.ObjectMeta.Generation
				}

				// If there is at least one Pod with a IncorrectPodSpec condition we have to delete/recreate that Pod.
				if condition.ProcessGroupConditionType == IncorrectPodSpec && cluster.Status.Generations.NeedsPodDeletion == 0 {
					logger.V(1).Info("Pending restart of fdbserver processes", "state", "NeedsPodDeletion")
					cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
				}

				conditions = append(conditions, condition.ProcessGroupConditionType)
			}

			logger.Info("Has unhealthy process group", "processGroupID", processGroup.ProcessGroupID, "state", "HasUnhealthyProcess", "conditions", conditions)
			cluster.Status.Generations.HasUnhealthyProcess = cluster.ObjectMeta.Generation
			reconciled = false
			continue
		}

		cluster.Status.ReconciledProcessGroups++
	}

	if cluster.Status.DesiredProcessGroups != cluster.Status.ReconciledProcessGroups {
		logger.Info("Not all process groups are reconciled", "desiredProcessGroups", cluster.Status.DesiredProcessGroups, "reconciledProcessGroups", cluster.Status.ReconciledProcessGroups)
	}

	if !cluster.Status.Health.Available {
		logger.Info("Database unavailable", "state", "DatabaseUnavailable")
		cluster.Status.Generations.DatabaseUnavailable = cluster.ObjectMeta.Generation
		reconciled = false
	}

	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	if !equality.Semantic.DeepEqual(cluster.Status.DatabaseConfiguration, desiredConfiguration) {
		logger.Info("Pending database configuration change", "state", "NeedsConfigurationChange", "current", cluster.Status.DatabaseConfiguration, "desired", desiredConfiguration)
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

// GetDesiredServersPerPod will return the expected server per Pod for the provided process class.
func (cluster *FoundationDBCluster) GetDesiredServersPerPod(pClass ProcessClass) int {
	if pClass == ProcessClassStorage {
		return cluster.GetStorageServersPerPod()
	}

	if pClass.SupportsMultipleLogServers() {
		return cluster.GetLogServersPerPod()
	}

	return 1
}

// GetStorageServersPerPod returns the StorageServer per Pod.
func (cluster *FoundationDBCluster) GetStorageServersPerPod() int {
	if cluster.Spec.StorageServersPerPod <= 1 {
		return 1
	}

	return cluster.Spec.StorageServersPerPod
}

// GetLogServersPerPod returns the TLog processes per Pod.
func (cluster *FoundationDBCluster) GetLogServersPerPod() int {
	if cluster.Spec.LogServersPerPod <= 1 {
		return 1
	}

	return cluster.Spec.LogServersPerPod
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
	// Deprecated: Will be removed in the next major release.
	EnableReadinessProbe *bool `json:"enableReadinessProbe,omitempty"`

	// EnableTLS controls whether we should be listening on a TLS connection.
	EnableTLS bool `json:"enableTls,omitempty"`

	// PeerVerificationRules provides the rules for what client certificates
	// the process should accept.
	// +kubebuilder:validation:MaxLength=10000
	PeerVerificationRules string `json:"peerVerificationRules,omitempty"`

	// ImageConfigs allows customizing the image that we use for
	// a container.
	// +kubebuilder:validation:MaxItems=100
	ImageConfigs []ImageConfig `json:"imageConfigs,omitempty"`
}

// DesiredDatabaseConfiguration builds the database configuration for the
// cluster based on its spec.
func (cluster *FoundationDBCluster) DesiredDatabaseConfiguration() DatabaseConfiguration {
	configuration := cluster.Spec.DatabaseConfiguration.NormalizeConfigurationWithSeparatedProxies(cluster.GetRunningVersion(), cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured())
	configuration.RoleCounts = cluster.GetRoleCountsWithDefaults()
	configuration.RoleCounts.Storage = 0

	version, _ := ParseFdbVersion(cluster.GetRunningVersion())
	if version.HasSeparatedProxies() && cluster.Spec.DatabaseConfiguration.AreSeparatedProxiesConfigured() {
		configuration.RoleCounts.Proxies = 0
	} else {
		configuration.RoleCounts.GrvProxies = 0
		configuration.RoleCounts.CommitProxies = 0
	}

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

// IsBeingUpgradedWithVersionIncompatibleVersion determines whether the cluster has a pending upgrade to a version incompatible version.
func (cluster *FoundationDBCluster) IsBeingUpgradedWithVersionIncompatibleVersion() bool {
	if !cluster.IsBeingUpgraded() {
		return false
	}

	runningVersion, _ := ParseFdbVersion(cluster.Status.RunningVersion)
	desiredVersion, _ := ParseFdbVersion(cluster.Spec.Version)

	return !runningVersion.IsProtocolCompatible(desiredVersion)
}

// VersionCompatibleUpgradeInProgress returns true if the cluster is currently being upgraded and the upgrade is to
// a version compatible version.
func (cluster *FoundationDBCluster) VersionCompatibleUpgradeInProgress() bool {
	if !cluster.IsBeingUpgraded() {
		return false
	}

	runningVersion, _ := ParseFdbVersion(cluster.Status.RunningVersion)
	desiredVersion, _ := ParseFdbVersion(cluster.Spec.Version)

	return runningVersion.IsProtocolCompatible(desiredVersion)
}

// ProcessGroupIsBeingRemoved determines if an instance is pending removal.
func (cluster *FoundationDBCluster) ProcessGroupIsBeingRemoved(processGroupID ProcessGroupID) bool {
	if processGroupID == "" {
		return false
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

	return cluster.Spec.FaultDomain.ZoneCount > 1 || len(cluster.Spec.DatabaseConfiguration.Regions) > 1 ||
		cluster.Spec.DatabaseConfiguration.RedundancyMode == RedundancyModeThreeDataHall
}

// GetLockPrefix gets the prefix for the keys where we store locking
// information.
func (cluster *FoundationDBCluster) GetLockPrefix() string {
	if cluster.Spec.LockOptions.LockKeyPrefix != "" {
		return cluster.Spec.LockOptions.LockKeyPrefix
	}

	return "\xff\x02/org.foundationdb.kubernetes-operator"
}

// GetMaintenancePrefix returns the prefix that is used by the operator to store and read maintenance related information.
// The prefix will be the provided GetLockPrefix appended by "maintenance".
func (cluster *FoundationDBCluster) GetMaintenancePrefix() string {
	return fmt.Sprintf("%s/maintenance", cluster.GetLockPrefix())
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
// locks. This is the `ProcessGroupIDPrefix` defined for this cluster.
func (cluster *FoundationDBCluster) GetLockID() string {
	return cluster.Spec.ProcessGroupIDPrefix
}

// NeedsExplicitListenAddress determines whether we pass a listen address
// parameter to fdbserver.
func (cluster *FoundationDBCluster) NeedsExplicitListenAddress() bool {
	return cluster.GetPublicIPSource() == PublicIPSourceService || cluster.GetUseExplicitListenAddress()
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
	// addresses to identify coordinators in the cluster file. This requires
	// FoundationDB 7.0+.
	UseDNSInClusterFile *bool `json:"useDNSInClusterFile,omitempty"`

	// DefineDNSLocalityFields determines whether to define pod DNS names on pod
	// specs and provide them in the locality arguments to fdbserver.
	//
	// This is ignored if UseDNSInCluster is true.
	DefineDNSLocalityFields *bool `json:"defineDNSLocalityFields,omitempty"`

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

// CrashLoopContainerObject specifies crash-loop target for specific container.
type CrashLoopContainerObject struct {
	// Name of the target container.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ContainerName string `json:"containerName,omitempty"`

	// Target processes to kill inside the container.
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=10000
	Targets []ProcessGroupID `json:"targets,omitempty"`
}

// BuggifyConfig provides options for injecting faults into a cluster for testing.
type BuggifyConfig struct {
	// NoSchedule defines a list of process group IDs that should fail to schedule.
	NoSchedule []ProcessGroupID `json:"noSchedule,omitempty"`

	// CrashLoops defines a list of process group IDs that should be put into a
	// crash looping state.
	// Deprecated: use CrashLoopContainers instead.
	CrashLoop []ProcessGroupID `json:"crashLoop,omitempty"`

	// CrashLoopContainers defines a list of process group IDs and containers
	// that should be put into a crash looping state.
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=8
	CrashLoopContainers []CrashLoopContainerObject `json:"crashLoopContainers,omitempty"`

	// EmptyMonitorConf instructs the operator to update all of the fdbmonitor.conf
	// files to have zero fdbserver processes configured.
	EmptyMonitorConf bool `json:"emptyMonitorConf,omitempty"`

	// IgnoreDuringRestart instructs the operator to ignore the provided process groups IDs during the
	// restart command. This can be useful to simulate cases where the kill command is not restarting all
	// processes. IgnoreDuringRestart does not support the wildcard option to ignore all of this specific cluster processes.
	// +kubebuilder:validation:MaxItems=1000
	IgnoreDuringRestart []ProcessGroupID `json:"ignoreDuringRestart,omitempty"`

	// BlockRemoval defines a list of process group IDs that will not be removed, even if they are marked for removal.
	// The operator will trigger the exclusion but the removal of the resources will be blocked until they are removed
	// from this list. This setting can be used to simulate cases where a process group is marked for removal but the
	// resources are not yet removed.
	// +kubebuilder:validation:MaxItems=1000
	BlockRemoval []ProcessGroupID `json:"blockRemoval,omitempty"`
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
	// Deprecated: This setting will be removed in the next major release.
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

// AddServersPerDisk adds serverPerDisk to the status field to keep track which ConfigMaps should be kept
func (clusterStatus *FoundationDBClusterStatus) AddServersPerDisk(serversPerDisk int, pClass ProcessClass) {
	if pClass == ProcessClassStorage {
		for _, curServersPerDisk := range clusterStatus.StorageServersPerDisk {
			if curServersPerDisk == serversPerDisk {
				return
			}
		}
		clusterStatus.StorageServersPerDisk = append(clusterStatus.StorageServersPerDisk, serversPerDisk)
		return
	}

	if pClass.SupportsMultipleLogServers() {
		for _, curServersPerDisk := range clusterStatus.LogServersPerDisk {
			if curServersPerDisk == serversPerDisk {
				return
			}
		}
		clusterStatus.LogServersPerDisk = append(clusterStatus.LogServersPerDisk, serversPerDisk)
	}
}

// GetMaxConcurrentAutomaticReplacements returns the cluster setting for MaxConcurrentReplacements, defaults to 1 if unset.
func (cluster *FoundationDBCluster) GetMaxConcurrentAutomaticReplacements() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.Replacements.MaxConcurrentReplacements, 1)
}

// FaultDomainBasedReplacements returns true if the operator is allowed to replace all failed process groups of a
// fault domain. Default is false
func (cluster *FoundationDBCluster) FaultDomainBasedReplacements() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.Replacements.FaultDomainBasedReplacements, false)
}

// CoordinatorSelectionSetting defines the process class and the priority of it.
// A higher priority means that the process class is preferred over another.
type CoordinatorSelectionSetting struct {
	// ProcessClass defines the process class to associate with priority with.
	ProcessClass ProcessClass `json:"processClass,omitempty"`
	// Priority defines the ordering of different process classes.
	Priority int `json:"priority,omitempty"`
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

// GetEligibleCandidateClasses returns process classes that are eligible to become coordinators.
func (cluster *FoundationDBCluster) GetEligibleCandidateClasses() []ProcessClass {
	candidateClasses := []ProcessClass{}

	for _, processGroup := range cluster.Status.ProcessGroups {
		if cluster.IsEligibleAsCandidate(processGroup.ProcessClass) {
			candidateClasses = append(candidateClasses, processGroup.ProcessClass)
		}
	}

	return candidateClasses
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
	return pointer.BoolDeref(cluster.Spec.LabelConfig.FilterOnOwnerReferences, false)
}

// SkipProcessGroup checks if a ProcessGroupStatus should be skipped during reconciliation.
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

// GetIgnoreMissingProcessesSeconds returns the value of IgnoreMissingProcessesSecond or 30 seconds if unset.
func (cluster *FoundationDBCluster) GetIgnoreMissingProcessesSeconds() time.Duration {
	return time.Duration(pointer.IntDeref(cluster.Spec.AutomationOptions.IgnoreMissingProcessesSeconds, 30)) * time.Second
}

// GetFailedPodDuration returns the value of FailedPodDuration or 5 minutes if unset.
func (cluster *FoundationDBCluster) GetFailedPodDuration() time.Duration {
	return time.Duration(pointer.IntDeref(cluster.Spec.AutomationOptions.FailedPodDurationSeconds, 300)) * time.Second
}

// GetUseNonBlockingExcludes returns the value of useNonBlockingExcludes or false if unset.
func (cluster *FoundationDBCluster) GetUseNonBlockingExcludes() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.UseNonBlockingExcludes, false)
}

// UseLocalitiesForExclusion returns the value of UseLocalitiesForExclusion or false if unset.
func (cluster *FoundationDBCluster) UseLocalitiesForExclusion() bool {
	fdbVersion, err := ParseFdbVersion(cluster.GetRunningVersion())
	if err != nil {
		// Fall back to use exclusions with IP if we can't parse the version.
		// This should never happen since the version is validated in earlier steps.
		return false
	}

	return fdbVersion.SupportsLocalityBasedExclusions() && pointer.BoolDeref(cluster.Spec.AutomationOptions.UseLocalitiesForExclusion, false)
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

// UseManagementAPI returns the value of UseManagementAPI or false if unset.
func (cluster *FoundationDBCluster) UseManagementAPI() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.UseManagementAPI, false)
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
	return cluster.DefineDNSLocalityFields() || pointer.BoolDeref(cluster.Spec.Routing.HeadlessService, false)
}

// UseDNSInClusterFile determines whether we need to use DNS entries in the
// cluster file for this cluster.
func (cluster *FoundationDBCluster) UseDNSInClusterFile() bool {
	runningVersion, err := ParseFdbVersion(cluster.Status.RunningVersion)
	// If the version cannot be parsed fall back to false.
	if err != nil {
		return false
	}

	return runningVersion.SupportsDNSInClusterFile() && pointer.BoolDeref(cluster.Spec.Routing.UseDNSInClusterFile, false)
}

// DefineDNSLocalityFields determines whether we need to put DNS entries in the
// pod spec and process locality.
func (cluster *FoundationDBCluster) DefineDNSLocalityFields() bool {
	return pointer.BoolDeref(cluster.Spec.Routing.DefineDNSLocalityFields, false) || cluster.UseDNSInClusterFile()
}

// GetDNSDomain gets the domain used when forming DNS names generated for a
// service.
func (cluster *FoundationDBCluster) GetDNSDomain() string {
	return pointer.StringDeref(cluster.Spec.Routing.DNSDomain, "cluster.local")
}

// GetRemovalMode returns the removal mode of the cluster or default to PodUpdateModeZone if unset.
func (cluster *FoundationDBCluster) GetRemovalMode() PodUpdateMode {
	if cluster.Spec.AutomationOptions.RemovalMode == "" {
		return PodUpdateModeZone
	}

	return cluster.Spec.AutomationOptions.RemovalMode
}

// GetWaitBetweenRemovalsSeconds returns the WaitDurationBetweenRemovals if set or defaults to 60s.
func (cluster *FoundationDBCluster) GetWaitBetweenRemovalsSeconds() int {
	duration := pointer.IntDeref(cluster.Spec.AutomationOptions.WaitBetweenRemovalsSeconds, -1)
	if duration < 0 {
		return 60
	}

	return duration
}

// UseMaintenaceMode returns true if UseMaintenanceModeChecker is set.
func (cluster *FoundationDBCluster) UseMaintenaceMode() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker, false)
}

// ResetMaintenanceMode returns true if the operator should reset the maintenance mode once all processes in the fault domain
// have been restarted. This method will return true if either ResetMaintenanceMode or UseMaintenaceMode is set to true.
func (cluster *FoundationDBCluster) ResetMaintenanceMode() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker, false) || pointer.BoolDeref(cluster.Spec.AutomationOptions.MaintenanceModeOptions.ResetMaintenanceMode, false)
}

// GetMaintenaceModeTimeoutSeconds returns the timeout for maintenance zone after which it will be reset.
func (cluster *FoundationDBCluster) GetMaintenaceModeTimeoutSeconds() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.MaintenanceModeOptions.MaintenanceModeTimeSeconds, 600)
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

// GetMatchLabels returns the match labels for all created resources
func (cluster *FoundationDBCluster) GetMatchLabels() map[string]string {
	if cluster.Spec.LabelConfig.MatchLabels != nil {
		return cluster.Spec.LabelConfig.MatchLabels
	}

	return map[string]string{
		FDBClusterLabel: cluster.Name,
	}
}

// GetUseExplicitListenAddress returns the UseExplicitListenAddress or if unset the default true
func (cluster *FoundationDBCluster) GetUseExplicitListenAddress() bool {
	return pointer.BoolDeref(cluster.Spec.UseExplicitListenAddress, true)
}

// GetMinimumUptimeSecondsForBounce returns the MinimumUptimeSecondsForBounce if set otherwise 600
func (cluster *FoundationDBCluster) GetMinimumUptimeSecondsForBounce() int {
	if cluster.Spec.MinimumUptimeSecondsForBounce == 0 {
		return 600
	}

	return cluster.Spec.MinimumUptimeSecondsForBounce
}

// GetEnableAutomaticReplacements returns cluster.Spec.AutomationOptions.Replacements.Enabled or if unset the default true
func (cluster *FoundationDBCluster) GetEnableAutomaticReplacements() bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.Replacements.Enabled, true)
}

// GetFailureDetectionTimeSeconds returns cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds or if unset the default 7200
func (cluster *FoundationDBCluster) GetFailureDetectionTimeSeconds() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds, 7200)
}

// GetTaintReplacementTimeSeconds returns cluster.Spec.AutomationOptions.Replacements.TaintReplacementTimeSeconds or if unset the default 1800
func (cluster *FoundationDBCluster) GetTaintReplacementTimeSeconds() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.Replacements.TaintReplacementTimeSeconds, 1800)
}

// GetSidecarContainerEnableLivenessProbe returns cluster.Spec.SidecarContainer.EnableLivenessProbe or if unset the default true
func (cluster *FoundationDBCluster) GetSidecarContainerEnableLivenessProbe() bool {
	return pointer.BoolDeref(cluster.Spec.SidecarContainer.EnableLivenessProbe, true)
}

// GetSidecarContainerEnableReadinessProbe returns cluster.Spec.SidecarContainer.EnableReadinessProbe or if unset the default false
func (cluster *FoundationDBCluster) GetSidecarContainerEnableReadinessProbe() bool {
	return pointer.BoolDeref(cluster.Spec.SidecarContainer.EnableReadinessProbe, false)
}

// GetUseUnifiedImage returns cluster.Spec.UseUnifiedImage or if unset the default false
func (cluster *FoundationDBCluster) GetUseUnifiedImage() bool {
	return pointer.BoolDeref(cluster.Spec.UseUnifiedImage, false)
}

// GetIgnoreTerminatingPodsSeconds returns the value of IgnoreTerminatingPodsSeconds or defaults to 10 minutes.
func (cluster *FoundationDBCluster) GetIgnoreTerminatingPodsSeconds() int {
	return pointer.IntDeref(cluster.Spec.AutomationOptions.IgnoreTerminatingPodsSeconds, int((10 * time.Minute).Seconds()))
}

// GetProcessGroupsToRemove will returns the list of Process Group IDs that must be added to the ProcessGroupsToRemove
// it will filter out all Process Group IDs that are already marked for removal to make sure those are clean up. If a
// provided process group ID doesn't exit it will be ignored.
func (cluster *FoundationDBCluster) GetProcessGroupsToRemove(processGroupIDs []ProcessGroupID) []ProcessGroupID {
	currentProcessGroupsToRemove := map[ProcessGroupID]None{}

	for _, id := range cluster.Spec.ProcessGroupsToRemove {
		currentProcessGroupsToRemove[id] = None{}
	}

	for _, id := range processGroupIDs {
		currentProcessGroupsToRemove[id] = None{}
	}

	filteredProcessGroupsToRemove := make([]ProcessGroupID, 0, len(currentProcessGroupsToRemove))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if _, ok := currentProcessGroupsToRemove[processGroup.ProcessGroupID]; !ok {
			continue
		}

		if processGroup.IsMarkedForRemoval() {
			continue
		}

		filteredProcessGroupsToRemove = append(filteredProcessGroupsToRemove, processGroup.ProcessGroupID)
	}

	return filteredProcessGroupsToRemove
}

// GetProcessGroupsToRemoveWithoutExclusion will returns the list of Process Group IDs that must be added to the ProcessGroupsToRemove
// it will filter out all Process Group IDs that are already marked for removal and are marked as excluded to make sure those are clean up.
// If a provided process group ID doesn't exit it will be ignored.
func (cluster *FoundationDBCluster) GetProcessGroupsToRemoveWithoutExclusion(processGroupIDs []ProcessGroupID) []ProcessGroupID {
	currentProcessGroupsToRemove := map[ProcessGroupID]None{}

	for _, id := range cluster.Spec.ProcessGroupsToRemoveWithoutExclusion {
		currentProcessGroupsToRemove[id] = None{}
	}

	for _, id := range processGroupIDs {
		currentProcessGroupsToRemove[id] = None{}
	}

	filteredProcessGroupsToRemove := make([]ProcessGroupID, 0, len(currentProcessGroupsToRemove))
	for _, processGroup := range cluster.Status.ProcessGroups {
		if _, ok := currentProcessGroupsToRemove[processGroup.ProcessGroupID]; !ok {
			continue
		}

		if processGroup.IsMarkedForRemoval() && !processGroup.ExclusionTimestamp.IsZero() {
			continue
		}

		filteredProcessGroupsToRemove = append(filteredProcessGroupsToRemove, processGroup.ProcessGroupID)
	}

	return filteredProcessGroupsToRemove
}

// AddProcessGroupsToRemovalList adds the provided process group IDs to the remove list.
// If a process group ID is already present on that list it won't be added a second time.
// Deprecated: Use GetProcessGroupsToRemove instead and set the cluster.Spec.ProcessGroupsToRemove value to the return value.
func (cluster *FoundationDBCluster) AddProcessGroupsToRemovalList(processGroupIDs []ProcessGroupID) {
	removals := map[ProcessGroupID]None{}

	for _, id := range cluster.Spec.ProcessGroupsToRemove {
		removals[id] = None{}
	}

	for _, processGroupID := range processGroupIDs {
		if _, ok := removals[processGroupID]; ok {
			continue
		}

		cluster.Spec.ProcessGroupsToRemove = append(cluster.Spec.ProcessGroupsToRemove, processGroupID)
	}
}

// AddProcessGroupsToNoScheduleList adds the provided process group IDs to the no-schedule list.
// If a process group ID is already present on that list it won't be added a second time.
func (cluster *FoundationDBCluster) AddProcessGroupsToNoScheduleList(processGroupIDs []ProcessGroupID) {
	noScheduleProcesses := map[ProcessGroupID]None{}

	for _, id := range cluster.Spec.Buggify.NoSchedule {
		noScheduleProcesses[id] = None{}
	}

	for _, processGroupID := range processGroupIDs {
		if _, ok := noScheduleProcesses[processGroupID]; ok {
			continue
		}

		cluster.Spec.Buggify.NoSchedule = append(cluster.Spec.Buggify.NoSchedule, processGroupID)
	}
}

// RemoveProcessGroupsFromNoScheduleList removes the provided process group IDs from the no-schedule list.
func (cluster *FoundationDBCluster) RemoveProcessGroupsFromNoScheduleList(processGroupIDs []ProcessGroupID) {
	processGroupIDsToRemove := make(map[ProcessGroupID]None)
	for _, processGroupID := range processGroupIDs {
		processGroupIDsToRemove[processGroupID] = None{}
	}

	idx := 0
	for _, processGroupID := range cluster.Spec.Buggify.NoSchedule {
		if _, ok := processGroupIDsToRemove[processGroupID]; ok {
			continue
		}
		cluster.Spec.Buggify.NoSchedule[idx] = processGroupID
		idx++
	}

	cluster.Spec.Buggify.NoSchedule = cluster.Spec.Buggify.NoSchedule[:idx]
}

// AddProcessGroupsToCrashLoopList adds the provided process group IDs to the crash-loop list.
// If a process group ID is already present on that list or all the processes are set into crash-loop
// it won't be added a second time.
func (cluster *FoundationDBCluster) AddProcessGroupsToCrashLoopList(processGroupIDs []ProcessGroupID) {
	crashLoop, _ := cluster.GetCrashLoopProcessGroups()

	for _, processGroupID := range processGroupIDs {
		if _, ok := crashLoop[processGroupID]; ok {
			continue
		}

		cluster.Spec.Buggify.CrashLoop = append(cluster.Spec.Buggify.CrashLoop, processGroupID)
	}
}

// AddProcessGroupsToCrashLoopContainerList adds the provided process group IDs to the crash-loop list.
// If a process group ID is already present on that list it won't be added a second time.
func (cluster *FoundationDBCluster) AddProcessGroupsToCrashLoopContainerList(processGroupIDs []ProcessGroupID, containerName string) {
	crashLoopProcessIDs := cluster.GetCrashLoopContainerProcessGroups()[containerName]

	if len(crashLoopProcessIDs) == 0 {
		containerObj := CrashLoopContainerObject{
			ContainerName: containerName,
			Targets:       processGroupIDs,
		}
		cluster.Spec.Buggify.CrashLoopContainers = append(cluster.Spec.Buggify.CrashLoopContainers, containerObj)
		return
	}

	containerIdx := 0
	for _, crashLoopContainerObj := range cluster.Spec.Buggify.CrashLoopContainers {
		if containerName != crashLoopContainerObj.ContainerName {
			containerIdx++
			continue
		}
		for _, processGroupID := range processGroupIDs {
			if _, ok := crashLoopProcessIDs[processGroupID]; ok {
				continue
			}
			crashLoopContainerObj.Targets = append(crashLoopContainerObj.Targets, processGroupID)
		}
		cluster.Spec.Buggify.CrashLoopContainers[containerIdx] = crashLoopContainerObj
		return
	}
}

// RemoveProcessGroupsFromCrashLoopList removes the provided process group IDs from the crash-loop list.
func (cluster *FoundationDBCluster) RemoveProcessGroupsFromCrashLoopList(processGroupIDs []ProcessGroupID) {
	processGroupIDsToRemove := make(map[ProcessGroupID]None)
	for _, processGroupID := range processGroupIDs {
		processGroupIDsToRemove[processGroupID] = None{}
	}

	idx := 0
	for _, processGroupID := range cluster.Spec.Buggify.CrashLoop {
		if _, ok := processGroupIDsToRemove[processGroupID]; ok {
			continue
		}
		cluster.Spec.Buggify.CrashLoop[idx] = processGroupID
		idx++
	}
	cluster.Spec.Buggify.CrashLoop = cluster.Spec.Buggify.CrashLoop[:idx]
}

// RemoveProcessGroupsFromCrashLoopContainerList removes the provided process group IDs from the crash-loop container list.
func (cluster *FoundationDBCluster) RemoveProcessGroupsFromCrashLoopContainerList(processGroupIDs []ProcessGroupID, containerName string) {
	processGroupIDsToRemove := make(map[ProcessGroupID]None)
	for _, processGroupID := range processGroupIDs {
		processGroupIDsToRemove[processGroupID] = None{}
	}

	crashLoopIdx := 0
	for _, crashLoopContainerObj := range cluster.Spec.Buggify.CrashLoopContainers {
		if containerName != crashLoopContainerObj.ContainerName {
			crashLoopIdx++
			continue
		}
		newTargets := make([]ProcessGroupID, 0)
		for _, processGroupID := range crashLoopContainerObj.Targets {
			if _, ok := processGroupIDsToRemove[processGroupID]; ok {
				continue
			}
			newTargets = append(newTargets, processGroupID)
		}
		crashLoopContainerObj.Targets = newTargets
		cluster.Spec.Buggify.CrashLoopContainers[crashLoopIdx] = crashLoopContainerObj
		return
	}
}

// AddProcessGroupsToRemovalWithoutExclusionList adds the provided process group IDs to the remove without exclusion list.
// If a process group ID is already present on that list it won't be added a second time.
// Deprecated: Use GetProcessGroupsToRemoveWithoutExclusion instead and set the cluster.Spec.ProcessGroupsToRemoveWithoutExclusion value to the return value.
func (cluster *FoundationDBCluster) AddProcessGroupsToRemovalWithoutExclusionList(processGroupIDs []ProcessGroupID) {
	removals := map[ProcessGroupID]None{}

	for _, id := range cluster.Spec.ProcessGroupsToRemoveWithoutExclusion {
		removals[id] = None{}
	}

	for _, processGroupID := range processGroupIDs {
		if _, ok := removals[processGroupID]; ok {
			continue
		}

		cluster.Spec.ProcessGroupsToRemoveWithoutExclusion = append(cluster.Spec.ProcessGroupsToRemoveWithoutExclusion, processGroupID)
	}
}

// GetRunningVersion returns the running version of the cluster defined in the cluster status or if not defined the version
// defined in the cluster spec.
func (cluster *FoundationDBCluster) GetRunningVersion() string {
	// We have to use the running version here otherwise if the operator gets killed during an upgrade it will try to apply
	// the grv and commit proxies.
	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		return cluster.Spec.Version
	}

	return versionString
}

// GetCrashLoopProcessGroups returns the process group IDs that are marked for crash looping. The second return value indicates
// if all process group IDs in a cluster should be crash looping.
func (cluster *FoundationDBCluster) GetCrashLoopProcessGroups() (map[ProcessGroupID]None, bool) {
	crashLoopPods := make(map[ProcessGroupID]None, len(cluster.Spec.Buggify.CrashLoop))
	crashLoopAll := false
	for _, processGroupID := range cluster.Spec.Buggify.CrashLoop {
		if processGroupID == "*" {
			crashLoopAll = true
		}
		crashLoopPods[processGroupID] = None{}
	}

	return crashLoopPods, crashLoopAll
}

// GetCrashLoopContainerProcessGroups returns the process group IDs in containers that are marked for crash looping.
// Returns map[ContainerName](map[ProcessGroupID]None).
func (cluster *FoundationDBCluster) GetCrashLoopContainerProcessGroups() map[string]map[ProcessGroupID]None {
	crashLoopTargets := make(map[string]map[ProcessGroupID]None)
	for _, target := range cluster.Spec.Buggify.CrashLoopContainers {
		if _, ok := crashLoopTargets[target.ContainerName]; !ok {
			crashLoopTargets[target.ContainerName] = make(map[ProcessGroupID]None)
		}

		for _, processID := range target.Targets {
			crashLoopTargets[target.ContainerName][processID] = None{}
		}
	}
	return crashLoopTargets
}

// Validate checks if all settings in the cluster are valid, if not and error will be returned. If multiple issues are
// found all of them will be returned in a single error.
func (cluster *FoundationDBCluster) Validate() error {
	var validations []string

	// Check if the provided storage engine is valid for the defined FDB version.
	version, err := ParseFdbVersion(cluster.Spec.Version)
	if err != nil {
		return err
	}

	if !version.IsSupported() {
		return fmt.Errorf("version: %s is not supported, minimum supported version is: %s", version.String(), Versions.MinimumVersion.String())
	}

	if !version.IsStorageEngineSupported(cluster.Spec.DatabaseConfiguration.StorageEngine) {
		validations = append(validations, fmt.Sprintf("storage engine %s is not supported on version %s", cluster.Spec.DatabaseConfiguration.StorageEngine, cluster.Spec.Version))
	}

	// Check if all coordinator processes are stateful
	for _, selection := range cluster.Spec.CoordinatorSelection {
		if !selection.ProcessClass.IsStateful() {
			validations = append(validations, fmt.Sprintf("%s is not a valid process class for coordinators", selection.ProcessClass))
		}
	}

	if len(validations) == 0 {
		return nil
	}

	return fmt.Errorf(strings.Join(validations, ", "))
}

// IsTaintFeatureDisabled return true if operator is configured to not replace Pods tainted Nodes OR
// if operator's TaintReplacementOptions is not set.
func (cluster *FoundationDBCluster) IsTaintFeatureDisabled() bool {
	return len(cluster.Spec.AutomationOptions.Replacements.TaintReplacementOptions) == 0
}

// GetMaxZonesWithUnavailablePods returns the maximum number of zones that can have unavailable pods.
func (cluster *FoundationDBCluster) GetMaxZonesWithUnavailablePods() int {
	return pointer.IntDeref(cluster.Spec.MaxZonesWithUnavailablePods, math.MaxInt)
}

// CacheDatabaseStatusForReconciliation returns if the sub-reconcilers should use a cached machine-readable status. If
// enabled the machine-readable status will be fetched only once per reconciliation loop and not multiple times. If the
// value is unset the provided default value will be returned.
func (cluster *FoundationDBCluster) CacheDatabaseStatusForReconciliation(defaultValue bool) bool {
	return pointer.BoolDeref(cluster.Spec.AutomationOptions.CacheDatabaseStatusForReconciliation, defaultValue)
}

// GetIgnoreLogGroupsForUpgrade will return the IgnoreLogGroupsForUpgrade, if the value is not set it will include the default `fdb-kubernetes-operator`
// LogGroup.
func (cluster *FoundationDBCluster) GetIgnoreLogGroupsForUpgrade() []LogGroup {
	if len(cluster.Spec.AutomationOptions.IgnoreLogGroupsForUpgrade) > 0 {
		return cluster.Spec.AutomationOptions.IgnoreLogGroupsForUpgrade
	}

	// Should we better read the FDB_NETWORK_OPTION_TRACE_LOG_GROUP env variable?
	return []LogGroup{"fdb-kubernetes-operator"}
}

// GetCurrentProcessGroupsAndProcessCounts will return the process counts of Process Groups, that are not marked for removal based on the
// FoundationDBClusterStatus and will return all used ProcessGroupIDs
func (cluster *FoundationDBCluster) GetCurrentProcessGroupsAndProcessCounts() (map[ProcessClass]int, map[ProcessClass]map[int]bool, error) {
	processCounts := make(map[ProcessClass]int)
	processGroupIDs := make(map[ProcessClass]map[int]bool)

	for _, processGroup := range cluster.Status.ProcessGroups {
		idNum, err := processGroup.ProcessGroupID.GetIDNumber()
		if err != nil {
			return nil, nil, err
		}

		if len(processGroupIDs[processGroup.ProcessClass]) == 0 {
			processGroupIDs[processGroup.ProcessClass] = map[int]bool{}
		}
		processGroupIDs[processGroup.ProcessClass][idNum] = true

		if !processGroup.IsMarkedForRemoval() {
			processCounts[processGroup.ProcessClass]++
		}
	}

	return processCounts, processGroupIDs, nil
}

// GetNextProcessGroupID will return the next unused ProcessGroupID and the ID number based on the provided ProcessClass
// and the mapping of used ProcessGroupID.
func (cluster *FoundationDBCluster) GetNextProcessGroupID(processClass ProcessClass, processGroupIDs map[int]bool, idNum int) (ProcessGroupID, int) {
	var processGroupID ProcessGroupID

	for idNum > 0 {
		_, processGroupID = cluster.GetProcessGroupID(processClass, idNum)
		if !cluster.ProcessGroupIsBeingRemoved(processGroupID) && !processGroupIDs[idNum] {
			break
		}

		idNum++
	}

	return processGroupID, idNum
}

// GetProcessGroupID generates a ProcessGroupID for a process group.
//
// This will return the Pod name and the ProcessGroupID.
func (cluster *FoundationDBCluster) GetProcessGroupID(processClass ProcessClass, idNum int) (string, ProcessGroupID) {
	var processGroupID ProcessGroupID
	if cluster.Spec.ProcessGroupIDPrefix != "" {
		processGroupID = ProcessGroupID(fmt.Sprintf("%s-%s-%d", cluster.Spec.ProcessGroupIDPrefix, processClass, idNum))
	} else {
		processGroupID = ProcessGroupID(fmt.Sprintf("%s-%d", processClass, idNum))
	}

	return fmt.Sprintf("%s-%s-%d", cluster.Name, processClass.GetProcessClassForPodName(), idNum), processGroupID
}

// IsPodIPFamily6 determines whether the podIPFamily setting in cluster is set to use the IPv6 family.
func (cluster *FoundationDBCluster) IsPodIPFamily6() bool {
	return cluster.Spec.Routing.PodIPFamily != nil && *cluster.Spec.Routing.PodIPFamily == 6
}
