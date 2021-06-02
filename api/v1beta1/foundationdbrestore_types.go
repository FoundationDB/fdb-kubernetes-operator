/*
 * foundationdbbackup_types.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=fdbrestore
// +kubebuilder:subresource:status

// FoundationDBRestore is the Schema for the FoundationDB Restore API
type FoundationDBRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FoundationDBRestoreSpec   `json:"spec,omitempty"`
	Status FoundationDBRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FoundationDBRestoreList contains a list of FoundationDBRestore objects.
type FoundationDBRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FoundationDBRestore `json:"items"`
}

// FoundationDBRestoreSpec describes the desired state of the backup for a cluster.
type FoundationDBRestoreSpec struct {
	// DestinationClusterName provides the name of the cluster that the data is
	// being restored into.
	DestinationClusterName string `json:"destinationClusterName"`

	// BackupURL provides the URL for the backup.
	BackupURL string `json:"backupURL"`

	// The key ranges to restore.
	KeyRanges []FoundationDBKeyRange `json:"keyRanges,omitempty"`
}

// FoundationDBRestoreStatus describes the current status of the restore for a cluster.
type FoundationDBRestoreStatus struct {
	// Running describes whether the restore is currently running.
	Running bool `json:"running,omitempty"`
}

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
