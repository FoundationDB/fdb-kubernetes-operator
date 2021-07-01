/*
 * controllers.go
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

package controllers

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("controller")

const (
	clusterFileKey = "cluster-file"

	// podSchedulingDelayDuration determines how long we should delay a requeue
	// of reconciliation when a pod is not ready.
	podSchedulingDelayDuration = 15 * time.Second
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

// Requeue provides a wrapper around different results from a subreconciler.
type Requeue struct {
	// Delay provides an optional delay before requeueing reconciliattion.
	Delay time.Duration

	// Error provides an error that we encountered that forced a requeue.
	Error error

	// Message provides a log message that explains the reason for the requeue.
	Message string
}

// processRequeue interprets a requeue resulit from a subreconciler.
func processRequeue(requeue *Requeue, subReconciler interface{}, object runtime.Object, recorder record.EventRecorder, logger logr.Logger) (ctrl.Result, error) {
	if requeue.Message == "" && requeue.Error != nil {
		requeue.Message = requeue.Error.Error()
	}

	err := requeue.Error
	if err != nil && k8serrors.IsConflict(err) {
		err = nil
		if requeue.Delay == time.Duration(0) {
			requeue.Delay = time.Minute
		}
	}

	recorder.Event(object, corev1.EventTypeNormal, "ReconciliationTerminatedEarly", requeue.Message)

	if err != nil {
		logger.Error(err, "Error in reconciliation", "subReconciler", fmt.Sprintf("%T", subReconciler), "requeueAfter", requeue.Delay)
		return ctrl.Result{}, err
	}
	logger.Info("Reconciliation terminated early", "subReconciler", fmt.Sprintf("%T", subReconciler), "message", requeue.Message, "requeueAfter", requeue.Delay)
	return ctrl.Result{Requeue: true, RequeueAfter: requeue.Delay}, nil
}
