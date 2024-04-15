/*
 * node_predicate.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2024 Apple Inc. and the FoundationDB project authors
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

package internal

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ predicate.Predicate = (*NodeTaintChangedPredicate)(nil)

// NodeTaintChangedPredicate filters events before enqueuing the keys. Only if the node taints have changed
// a reconciliation will be triggered.
type NodeTaintChangedPredicate struct {
	Logger logr.Logger
}

// Create implements Predicate.
func (n NodeTaintChangedPredicate) Create(_ event.CreateEvent) bool {
	n.Logger.V(1).Info("Got an CreateEvent")
	// TODO change to false again
	return true
}

// Delete implements Predicate.
func (n NodeTaintChangedPredicate) Delete(_ event.DeleteEvent) bool {
	n.Logger.V(1).Info("Got an DeleteEvent")
	return false
}

// Update returns true if the Update event should be processed. This is the case if the taints for the provided
// node has been changed.
func (n NodeTaintChangedPredicate) Update(event event.UpdateEvent) bool {
	n.Logger.V(1).Info("Got an UpdateEvent")
	if event.ObjectOld == nil {
		return false
	}

	if event.ObjectNew == nil {
		return false
	}

	oldNode, ok := event.ObjectOld.(*corev1.Node)
	if !ok {
		return false
	}

	newNode, ok := event.ObjectNew.(*corev1.Node)
	if !ok {
		return false
	}

	taintsChanged := !equality.Semantic.DeepEqual(oldNode.Spec.Taints, newNode.Spec.Taints)
	n.Logger.V(1).Info("Got an update", "taintsChanged", taintsChanged, "oldTaints", oldNode.Spec.Taints, "newTaints", newNode.Spec.Taints)

	return taintsChanged
}

// Generic implements Predicate.
func (n NodeTaintChangedPredicate) Generic(_ event.GenericEvent) bool {
	n.Logger.V(1).Info("Got an GenericEvent")
	return false
}
