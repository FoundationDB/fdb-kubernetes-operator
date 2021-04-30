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
	clusterFile = "cluster-file"
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
	return fdbtypes.ProcessClass(labels[fdbtypes.FDBProcessClassLabel])
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
