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
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("controller")

// LastSpecKey provides the annotation name we use to store the hash of the
// pod spec.
const LastSpecKey = "foundationdb.org/last-applied-spec"

// LastConfigMapKey provides the annotation name we use to store the hash of the
// config map.
const LastConfigMapKey = "foundationdb.org/last-applied-config-map"

// BackupDeploymentLabel probvides the label we use to connect backup
// deployments to a cluster.
const BackupDeploymentLabel = "foundationdb.org/backup-for"

// The default timeout for CLI commands.
var DefaultCLITimeout = 10

// MinimumUptimeSecondsForBounce defines the minimum time, in seconds, that the
// processes in the cluster must have been up for before the operator can
// execute a bounce.
const MinimumUptimeSecondsForBounce = 600

// metadataMatches determines if the current metadata on an object matches the
// metadata specified by the cluster spec.
func metadataMatches(currentMetadata metav1.ObjectMeta, desiredMetadata metav1.ObjectMeta) bool {
	return containsAll(currentMetadata.Labels, desiredMetadata.Labels) && containsAll(currentMetadata.Annotations, desiredMetadata.Annotations)
}

// mergeAnnotations merges the the annotations specified by the operator into
// on object's metadata.
//
// This will return whether the target's annotations have changed.
func mergeAnnotations(target *metav1.ObjectMeta, desired metav1.ObjectMeta) bool {
	if desired.Annotations == nil {
		return false
	}
	if target.Annotations == nil {
		target.Annotations = desired.Annotations
		return true
	}
	changed := false
	for key, value := range desired.Annotations {
		if target.Annotations[key] != value {
			target.Annotations[key] = value
			changed = true
		}
	}
	return changed
}
