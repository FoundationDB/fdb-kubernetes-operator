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

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
)

var globalControllerLogger = logf.Log.WithName("controller")

const (
	// podSchedulingDelayDuration determines how long we should delay a requeue
	// of reconciliation when a pod is not ready.
	podSchedulingDelayDuration = 15 * time.Second
)

// requeue provides a wrapper around different results from a subreconciler.
type requeue struct {
	// delay provides an optional delay before requeueing reconciliation.
	delay time.Duration

	// curError provides an error that we encountered that forced a requeue.
	curError error

	// message provides a message that explains the reason for the requeue.
	message string

	// delayedRequeue defines that the reconciliation was not completed but the requeue should be delayed to the end.
	delayedRequeue bool
}

// processRequeue interprets a requeue result from a subreconciler.
func processRequeue(
	requeue *requeue,
	subReconciler interface{},
	object runtime.Object,
	recorder record.EventRecorder,
	logger logr.Logger,
) (ctrl.Result, error) {
	curLog := logger.WithValues(
		"reconciler",
		fmt.Sprintf("%T", subReconciler),
		"requeueAfter",
		requeue.delay,
	)
	if requeue.message == "" && requeue.curError != nil {
		requeue.message = requeue.curError.Error()
	}

	err := requeue.curError
	if err != nil && k8serrors.IsConflict(err) {
		err = nil
		if requeue.delay == time.Duration(0) {
			requeue.delay = time.Minute
		}
	}

	recorder.Event(object, corev1.EventTypeNormal, "ReconciliationTerminatedEarly", requeue.message)

	if err != nil {
		curLog.Error(err, "Error in reconciliation")
		return ctrl.Result{}, err
	}
	curLog.Info("Reconciliation terminated early", "message", requeue.message)

	return ctrl.Result{Requeue: true, RequeueAfter: requeue.delay}, nil
}
