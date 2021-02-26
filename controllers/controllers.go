/*
 * controllers.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var log = logf.Log.WithName("controller")

// DefaultCLITimeout is the default timeout for CLI commands.
var DefaultCLITimeout = 10

const (
	// LastSpecKey provides the annotation name we use to store the hash of the
	// pod spec.
	LastSpecKey = "foundationdb.org/last-applied-spec"

	// LastConfigMapKey provides the annotation name we use to store the hash of the
	// config map.
	LastConfigMapKey = "foundationdb.org/last-applied-config-map"

	// BackupDeploymentLabel provides the label we use to connect backup
	// deployments to a cluster.
	BackupDeploymentLabel = "foundationdb.org/backup-for"

	// MinimumUptimeSecondsForBounce defines the minimum time, in seconds, that the
	// processes in the cluster must have been up for before the operator can
	// execute a bounce.
	MinimumUptimeSecondsForBounce = 600

	// PublicIPSourceAnnotation is an annotation key that specifies where a pod
	// gets its public IP from.
	PublicIPSourceAnnotation = "foundationdb.org/public-ip-source"

	// PublicIPAnnotation is an annotation key that specifies the current public
	// IP for a pod.
	PublicIPAnnotation = "foundationdb.org/public-ip"

	// FDBInstanceIDLabel represents the label that is used to represent a instance ID
	FDBInstanceIDLabel = "fdb-instance-id"

	// FDBProcessClassLabel represents the label that is used to represent the process class
	FDBProcessClassLabel = "fdb-process-class"

	// FDBClusterLabel represents the label that is used to represent the cluster of an instance
	FDBClusterLabel = "fdb-cluster-name"

	// NodeSelectorNoScheduleLabel is a label used when adding node selectors to block scheduling.
	NodeSelectorNoScheduleLabel = "foundationdb.org/no-schedule-allowed"

	// FDBLocalityInstanceIDKey represents the key in the locality map that
	// holds the instance ID.
	FDBLocalityInstanceIDKey = "instance_id"

	// FDBLocalityZoneIDKey represents the key in the locality map that holds
	// the zone ID.
	FDBLocalityZoneIDKey = "zoneid"

	// FDBLocalityDCIDKey represents the key in the locality map that holds
	// the DC ID.
	FDBLocalityDCIDKey = "dcid"
)

// metadataMatches determines if the current metadata on an object matches the
// metadata specified by the cluster spec.
func metadataMatches(currentMetadata metav1.ObjectMeta, desiredMetadata metav1.ObjectMeta) bool {
	return containsAll(currentMetadata.Labels, desiredMetadata.Labels) && containsAll(currentMetadata.Annotations, desiredMetadata.Annotations)
}

// mergeLabels merges the the labels specified by the operator into
// on object's metadata.
//
// This will return whether the target's labels have changed.
func mergeLabelsInMetadata(target *metav1.ObjectMeta, desired metav1.ObjectMeta) bool {
	return mergeMap(target.Labels, desired.Labels)
}

// mergeAnnotations merges the the annotations specified by the operator into
// on object's metadata.
//
// This will return whether the target's annotations have changed.
func mergeAnnotations(target *metav1.ObjectMeta, desired metav1.ObjectMeta) bool {
	return mergeMap(target.Annotations, desired.Annotations)
}

// mergeMap merges a map into another map.
//
// This will return whether the target's values have changed.
func mergeMap(target map[string]string, desired map[string]string) bool {
	changed := false
	for key, value := range desired {
		if target[key] != value {
			target[key] = value
			changed = true
		}
	}
	return changed
}

// processClassFromLabel extracts the ProcessClass label from the metav1.ObjectMeta.Labels map
func processClassFromLabels(labels map[string]string) fdbtypes.ProcessClass {
	return fdbtypes.ProcessClass(labels[FDBProcessClassLabel])
}

// DeprecationOptions controls how deprecations and changes to defaults
// get applied to our specs.
type DeprecationOptions struct {
	// Whether we should apply the latest defaults rather than the defaults that
	// were initially established for this major version.
	UseFutureDefaults bool

	// Whether we should only fill in defaults that have changes between major
	// versions of the operator.
	OnlyShowChanges bool
}
