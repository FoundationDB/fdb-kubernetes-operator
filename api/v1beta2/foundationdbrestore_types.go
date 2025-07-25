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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=fdbrestore
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations="foundationdb.org/release=v2.10.0"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:storageversion

// FoundationDBRestore is the Schema for the foundationdbrestores API
type FoundationDBRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FoundationDBRestoreSpec   `json:"spec,omitempty"`
	Status FoundationDBRestoreStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FoundationDBRestoreList contains a list of FoundationDBRestore objects
type FoundationDBRestoreList struct {
	metav1.TypeMeta `                      json:",inline"`
	metav1.ListMeta `                      json:"metadata,omitempty"`
	Items           []FoundationDBRestore `json:"items"`
}

// FoundationDBRestoreSpec describes the desired state of the backup for a cluster.
type FoundationDBRestoreSpec struct {
	// DestinationClusterName provides the name of the cluster that the data is
	// being restored into.
	DestinationClusterName string `json:"destinationClusterName"`

	// The key ranges to restore.
	KeyRanges []FoundationDBKeyRange `json:"keyRanges,omitempty"`

	// This is the configuration of the target blobstore for this backup.
	BlobStoreConfiguration *BlobStoreConfiguration `json:"blobStoreConfiguration,omitempty"`

	// CustomParameters defines additional parameters to pass to the backup
	// agents.
	CustomParameters FoundationDBCustomParameters `json:"customParameters,omitempty"`

	// The path to the encryption key used to encrypt the backup.
	// +kubebuilder:validation:MaxLength=4096
	EncryptionKeyPath string `json:"encryptionKeyPath,omitempty"`
}

// FoundationDBRestoreStatus describes the current status of the restore for a cluster.
type FoundationDBRestoreStatus struct {
	// Running describes whether the restore is currently running.
	Running bool `json:"running,omitempty"`
	// State describes the FoundationDBRestoreState state.
	State FoundationDBRestoreState `json:"state,omitempty"`
}

// FoundationDBRestoreState represents the states for a restore in FDB:
// https://github.com/apple/foundationdb/blob/fe47ce24d361a8c2d625c4d549f86ff98363de9e/fdbclient/FileBackupAgent.actor.cpp#L120-L140
// +kubebuilder:validation:MaxLength=50
type FoundationDBRestoreState string

const (
	// UninitializedFoundationDBRestoreState represents the uninitialized state.
	UninitializedFoundationDBRestoreState FoundationDBRestoreState = "uninitialized"
	// QueuedFoundationDBRestoreState represents the queued state.
	QueuedFoundationDBRestoreState FoundationDBRestoreState = "queued"
	// StartingFoundationDBRestoreState represents the starting state.
	StartingFoundationDBRestoreState FoundationDBRestoreState = "starting"
	// RunningFoundationDBRestoreState represents the running state.
	RunningFoundationDBRestoreState FoundationDBRestoreState = "running"
	// CompletedFoundationDBRestoreState represents the completed state.
	CompletedFoundationDBRestoreState FoundationDBRestoreState = "completed"
	// AbortedFoundationDBRestoreState represents the aborted state.
	AbortedFoundationDBRestoreState FoundationDBRestoreState = "aborted"
	// UnknownFoundationDBRestoreState represents the unknown state.
	UnknownFoundationDBRestoreState FoundationDBRestoreState = "Unknown"
)

// FoundationDBKeyRange describes a range of keys for a command.
//
// The keys in the key range must match the following pattern:
// `^[A-Za-z0-9\/\\-]+$`. All other characters can be escaped with `\xBB`, where
// `BB` is the hexadecimal value of the byte.
type FoundationDBKeyRange struct {
	// Start provides the beginning of the key range.
	// +kubebuilder:validation:Pattern:=^[A-Za-z0-9\/\\-]+$
	Start string `json:"start"`

	// End provides the end of the key range.
	// +kubebuilder:validation:Pattern:=^[A-Za-z0-9\/\\-]+$
	End string `json:"end"`
}

// BackupName gets the name of the backup for the source backup.
// This will fill in a default value if the backup name in the spec is empty.
func (restore *FoundationDBRestore) BackupName() string {
	if restore.Spec.BlobStoreConfiguration == nil ||
		restore.Spec.BlobStoreConfiguration.BackupName == "" {
		return restore.Name
	}

	return restore.Spec.BlobStoreConfiguration.BackupName
}

// BackupURL gets the destination url of the backup.
func (restore *FoundationDBRestore) BackupURL() string {
	return restore.Spec.BlobStoreConfiguration.getURL(
		restore.BackupName(),
		restore.Spec.BlobStoreConfiguration.BucketName(),
	)
}

func init() {
	SchemeBuilder.Register(&FoundationDBRestore{}, &FoundationDBRestoreList{})
}
