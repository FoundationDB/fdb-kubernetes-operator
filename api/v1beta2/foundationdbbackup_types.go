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
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=fdbbackup
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations="foundationdb.org/release=v2.9.0"
// +kubebuilder:printcolumn:name="Generation",type="integer",JSONPath=".metadata.generation",description="Latest generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Reconciled",type="integer",JSONPath=".status.generations.reconciled",description="Last reconciled generation of the spec",priority=0
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion

// FoundationDBBackup is the Schema for the foundationdbbackups API
type FoundationDBBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FoundationDBBackupSpec   `json:"spec,omitempty"`
	Status FoundationDBBackupStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FoundationDBBackupList contains a list of FoundationDBBackup objects
type FoundationDBBackupList struct {
	metav1.TypeMeta `                     json:",inline"`
	metav1.ListMeta `                     json:"metadata,omitempty"`
	Items           []FoundationDBBackup `json:"items"`
}

// FoundationDBBackupSpec describes the desired state of the backup for a cluster.
type FoundationDBBackupSpec struct {
	// The version of FoundationDB that the backup agents should run.
	Version string `json:"version"`

	// The cluster this backup is for.
	ClusterName string `json:"clusterName"`

	// +kubebuilder:validation:Enum=Running;Stopped;Paused
	// The desired state of the backup.
	// The default is Running.
	BackupState BackupState `json:"backupState,omitempty"`

	// AgentCount defines the number of backup agents to run.
	// The default is run 2 agents.
	AgentCount *int `json:"agentCount,omitempty"`

	// The time window between new snapshots.
	// This is measured in seconds. The default is 864,000, or 10 days.
	SnapshotPeriodSeconds *int `json:"snapshotPeriodSeconds,omitempty"`

	// BackupDeploymentMetadata allows customizing labels and annotations on the
	// deployment for the backup agents.
	BackupDeploymentMetadata *metav1.ObjectMeta `json:"backupDeploymentMetadata,omitempty"`

	// PodTemplateSpec allows customizing the pod template for the backup
	// agents.
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`

	// CustomParameters defines additional parameters to pass to the backup
	// agents.
	CustomParameters FoundationDBCustomParameters `json:"customParameters,omitempty"`

	// This setting defines if a user provided image can have it's own tag
	// rather than getting the provided version appended.
	// You have to ensure that the specified version in the Spec is compatible
	// with the given version in your custom image.
	// +kubebuilder:default:=false
	// Deprecated: use ImageConfigs instead.
	AllowTagOverride *bool `json:"allowTagOverride,omitempty"`

	// This is the configuration of the target blobstore for this backup.
	BlobStoreConfiguration *BlobStoreConfiguration `json:"blobStoreConfiguration,omitempty"`

	// The path to the encryption key used to encrypt the backup.
	// +kubebuilder:validation:MaxLength=4096
	EncryptionKeyPath string `json:"encryptionKeyPath,omitempty"`

	// MainContainer defines customization for the foundationdb container.
	MainContainer ContainerOverrides `json:"mainContainer,omitempty"`

	// SidecarContainer defines customization for the
	// foundationdb-kubernetes-sidecar container.
	SidecarContainer ContainerOverrides `json:"sidecarContainer,omitempty"`

	// ImageType defines the image type that should be used for the FoundationDBCluster deployment. When the type
	// is set to "unified" the deployment will use the new fdb-kubernetes-monitor. Otherwise the main container and
	// the sidecar container will use different images.
	// Default: split
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=split;unified
	// +kubebuilder:default:=split
	ImageType *ImageType `json:"imageType,omitempty"`
}

// FoundationDBBackupStatus describes the current status of the backup for a cluster.
type FoundationDBBackupStatus struct {
	// AgentCount provides the number of agents that are up-to-date, ready,
	// and not terminated.
	AgentCount int `json:"agentCount,omitempty"`

	// DeploymentConfigured indicates whether the deployment is correctly
	// configured.
	DeploymentConfigured bool `json:"deploymentConfigured,omitempty"`

	// BackupDetails provides information about the state of the backup in the
	// cluster.
	BackupDetails *FoundationDBBackupStatusBackupDetails `json:"backupDetails,omitempty"`

	// Generations provides information about the latest generation to be
	// reconciled, or to reach other stages in reconciliation.
	Generations BackupGenerationStatus `json:"generations,omitempty"`
}

// FoundationDBBackupStatusBackupDetails provides information about the state
// of the backup in the cluster.
type FoundationDBBackupStatusBackupDetails struct {
	URL                   string `json:"url,omitempty"`
	Running               bool   `json:"running,omitempty"`
	Paused                bool   `json:"paused,omitempty"`
	SnapshotPeriodSeconds int    `json:"snapshotTime,omitempty"`
}

// BackupGenerationStatus stores information on which generations have reached
// different stages in reconciliation for the backup.
type BackupGenerationStatus struct {
	// Reconciled provides the last generation that was fully reconciled.
	Reconciled int64 `json:"reconciled,omitempty"`

	// NeedsBackupAgentUpdate provides the last generation that could not
	// complete reconciliation because the backup agent deployment needs to be
	// updated.
	NeedsBackupAgentUpdate int64 `json:"needsBackupAgentUpdate,omitempty"`

	// NeedsBackupStart provides the last generation that could not complete
	// reconciliation because we need to start a backup.
	NeedsBackupStart int64 `json:"needsBackupStart,omitempty"`

	// NeedsBackupStart provides the last generation that could not complete
	// reconciliation because we need to stop a backup.
	NeedsBackupStop int64 `json:"needsBackupStop,omitempty"`

	// NeedsBackupPauseToggle provides the last generation that needs to have
	// a backup paused or resumed.
	NeedsBackupPauseToggle int64 `json:"needsBackupPauseToggle,omitempty"`

	// NeedsBackupReconfiguration provides the last generation that could not
	// complete reconciliation because we need to modify backup parameters.
	NeedsBackupReconfiguration int64 `json:"needsBackupModification,omitempty"`
}

// BackupState defines the desired state of a backup
type BackupState string

const (
	// BackupStateRunning defines the running state
	BackupStateRunning BackupState = "Running"
	// BackupStatePaused defines the paused state
	BackupStatePaused BackupState = "Paused"
	// BackupStateStopped defines the stopped state
	BackupStateStopped BackupState = "Stopped"
)

// URLParameter defines a single URL parameter to pass to the blobstore.
// +kubebuilder:validation:MaxLength=1024
type URLParameter string

// BlobStoreConfiguration describes the blob store configuration.
type BlobStoreConfiguration struct {
	// The name for the backup.
	// If empty defaults to .metadata.name.
	// +kubebuilder:validation:MaxLength=1024
	BackupName string `json:"backupName,omitempty"`

	// The account name to use with the backup destination.
	// If no port is included, it will default to 443,
	// or 80 if secure_connection URL Parameter is set to 0.
	// +kubebuilder:validation:MaxLength=100
	// +kubebuilder:validation:Required
	AccountName string `json:"accountName"`

	// The backup bucket to write to.
	// The default is "fdb-backups".
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=63
	Bucket string `json:"bucket,omitempty"`

	// Additional URL parameters passed to the blobstore URL.
	// See: https://apple.github.io/foundationdb/backups.html#backup-urls
	// +kubebuilder:validation:MaxItems=100
	URLParameters []URLParameter `json:"urlParameters,omitempty"`
}

// ShouldRun determines whether a backup should be running.
func (backup *FoundationDBBackup) ShouldRun() bool {
	return backup.Spec.BackupState == "" || backup.Spec.BackupState == BackupStateRunning ||
		backup.Spec.BackupState == BackupStatePaused
}

// ShouldBePaused determines whether the backups should be paused.
func (backup *FoundationDBBackup) ShouldBePaused() bool {
	return backup.Spec.BackupState == BackupStatePaused
}

// Bucket gets the bucket this backup will use.
// This will fill in a default value if the bucket in the spec is empty.
func (backup *FoundationDBBackup) Bucket() string {
	if backup.Spec.BlobStoreConfiguration.Bucket == "" {
		return "fdb-backups"
	}

	return backup.Spec.BlobStoreConfiguration.Bucket
}

// BackupName gets the name of the backup in the destination.
// This will fill in a default value if the backup name in the spec is empty.
func (backup *FoundationDBBackup) BackupName() string {
	if backup.Spec.BlobStoreConfiguration.BackupName == "" {
		return backup.Name
	}

	return backup.Spec.BlobStoreConfiguration.BackupName
}

// BackupURL gets the destination url of the backup.
func (backup *FoundationDBBackup) BackupURL() string {
	return backup.Spec.BlobStoreConfiguration.getURL(backup.BackupName(), backup.Bucket())
}

// SnapshotPeriodSeconds gets the period between snapshots for a backup.
func (backup *FoundationDBBackup) SnapshotPeriodSeconds() int {
	return pointer.IntDeref(backup.Spec.SnapshotPeriodSeconds, 864000)
}

// FoundationDBLiveBackupStatus describes the live status of the backup for a
// cluster, as provided by the backup status command.
type FoundationDBLiveBackupStatus struct {
	// DestinationURL provides the URL that the backup is being written to.
	DestinationURL string `json:"DestinationURL,omitempty"`

	// SnapshotIntervalSeconds provides the interval of the snapshots.
	SnapshotIntervalSeconds int `json:"SnapshotIntervalSeconds,omitempty"`

	// Status provides the current state of the backup.
	Status FoundationDBLiveBackupStatusState `json:"Status,omitempty"`

	// BackupAgentsPaused describes whether the backup agents are paused.
	BackupAgentsPaused bool `json:"BackupAgentsPaused,omitempty"`
}

// FoundationDBLiveBackupStatusState provides the state of a backup in the
// backup status.
type FoundationDBLiveBackupStatusState struct {
	// Running determines whether the backup is currently running.
	Running bool `json:"Running,omitempty"`
}

// GetDesiredAgentCount determines how many backup agents we should run
// for a cluster.
func (backup *FoundationDBBackup) GetDesiredAgentCount() int {
	return pointer.IntDeref(backup.Spec.AgentCount, 2)
}

// CheckReconciliation compares the spec and the status to determine if
// reconciliation is complete.
func (backup *FoundationDBBackup) CheckReconciliation() (bool, error) {
	var reconciled = true

	desiredAgentCount := backup.GetDesiredAgentCount()
	if backup.Status.AgentCount != desiredAgentCount || !backup.Status.DeploymentConfigured {
		backup.Status.Generations.NeedsBackupAgentUpdate = backup.Generation
		reconciled = false
	}

	isRunning := backup.Status.BackupDetails != nil && backup.Status.BackupDetails.Running
	isPaused := backup.Status.BackupDetails != nil && backup.Status.BackupDetails.Paused

	if backup.ShouldRun() && !isRunning {
		backup.Status.Generations.NeedsBackupStart = backup.Generation
		reconciled = false
	}

	if !backup.ShouldRun() && isRunning {
		backup.Status.Generations.NeedsBackupStop = backup.Generation
		reconciled = false
	}

	if backup.ShouldBePaused() != isPaused {
		backup.Status.Generations.NeedsBackupPauseToggle = backup.Generation
		reconciled = false
	}

	if isRunning &&
		backup.SnapshotPeriodSeconds() != backup.Status.BackupDetails.SnapshotPeriodSeconds {
		backup.Status.Generations.NeedsBackupReconfiguration = backup.Generation
		reconciled = false
	}

	if reconciled {
		backup.Status.Generations = BackupGenerationStatus{
			Reconciled: backup.Generation,
		}
	}

	return reconciled, nil
}

// GetAllowTagOverride returns the bool value for AllowTagOverride
func (foundationDBBackupSpec *FoundationDBBackupSpec) GetAllowTagOverride() bool {
	return pointer.BoolDeref(foundationDBBackupSpec.AllowTagOverride, false)
}

// UseUnifiedImage returns true if the unified image should be used.
func (backup *FoundationDBBackup) UseUnifiedImage() bool {
	imageType := ImageTypeSplit
	if backup.Spec.ImageType != nil {
		imageType = *backup.Spec.ImageType
	}

	return imageType == ImageTypeUnified
}

// getURL returns the blobstore URL for the specific configuration
func (configuration *BlobStoreConfiguration) getURL(backup string, bucket string) string {
	if configuration.AccountName == "" {
		return ""
	}
	var (
		defaultPort string
		sb          strings.Builder
	)
	backupURL := &url.URL{Host: configuration.AccountName}
	if backupURL.Port() == "" {
		defaultPort = ":443"
	}
	for _, param := range configuration.URLParameters {
		sb.WriteString("&")
		sb.WriteString(string(param))
		// check if default port should be 80 instead of 443; see https://apple.github.io/foundationdb/backups.html#backup-urls
		suffix, exists := strings.CutPrefix(string(param), "sc")
		if !exists {
			suffix, exists = strings.CutPrefix(string(param), "secure_connection")
			if !exists { // then it's not setting secure connection
				continue
			}
		}
		if suffix == "=0" {
			if defaultPort != "" { // i.e. if a port was not provided
				defaultPort = ":80"
			}
		}
	}

	return fmt.Sprintf(
		"blobstore://%s%s/%s?bucket=%s%s",
		configuration.AccountName,
		defaultPort,
		backup,
		bucket,
		sb.String(),
	)
}

// BucketName gets the bucket this backup will use.
// This will fill in a default value if the bucket in the spec is empty.
func (configuration *BlobStoreConfiguration) BucketName() string {
	if configuration.Bucket != "" {
		return configuration.Bucket
	}

	return "fdb-backups"
}

func init() {
	SchemeBuilder.Register(&FoundationDBBackup{}, &FoundationDBBackupList{})
}
