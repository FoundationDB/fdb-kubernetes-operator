// Copyright 2021 Chaos Mesh Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +chaos-mesh:experiment

// NetworkChaos is the Schema for the networkchaos API
type NetworkChaos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the behavior of a pod chaos experiment
	Spec NetworkChaosSpec `json:"spec"`

	// Most recently observed status of the chaos experiment about pods
	Status NetworkChaosStatus `json:"status,omitempty"`
}

var _ InnerObjectWithCustomStatus = (*NetworkChaos)(nil)
var _ InnerObjectWithSelector = (*NetworkChaos)(nil)
var _ InnerObject = (*NetworkChaos)(nil)

// NetworkChaosAction represents the chaos action about network.
type NetworkChaosAction string

const (
	// NetemAction is a combination of several chaos actions i.e. delay, loss, duplicate, corrupt.
	// When using this action multiple specs are merged into one Netem RPC and sends to chaos daemon.
	NetemAction NetworkChaosAction = "netem"

	// DelayAction represents the chaos action of adding delay on pods.
	DelayAction NetworkChaosAction = "delay"

	// LossAction represents the chaos action of losing packets on pods.
	LossAction NetworkChaosAction = "loss"

	// DuplicateAction represents the chaos action of duplicating packets on pods.
	DuplicateAction NetworkChaosAction = "duplicate"

	// CorruptAction represents the chaos action of corrupting packets on pods.
	CorruptAction NetworkChaosAction = "corrupt"

	// PartitionAction represents the chaos action of network partition of pods.
	PartitionAction NetworkChaosAction = "partition"

	// BandwidthAction represents the chaos action of network bandwidth of pods.
	BandwidthAction NetworkChaosAction = "bandwidth"
)

// Direction represents traffic direction from source to target,
// it could be netem, delay, loss, duplicate, corrupt or partition,
// check comments below for detail direction flow.
type Direction string

const (
	// To represents network packet from source to target
	To Direction = "to"

	// From represents network packet to source from target
	From Direction = "from"

	// Both represents both directions
	Both Direction = "both"
)

// NetworkChaosSpec defines the desired state of NetworkChaos
type NetworkChaosSpec struct {
	PodSelector `json:",inline"`

	// Action defines the specific network chaos action.
	// Supported action: partition, netem, delay, loss, duplicate, corrupt
	// Default action: delay
	Action NetworkChaosAction `json:"action"`

	// Device represents the network device to be affected.
	Device string `json:"device,omitempty"`

	// Duration represents the duration of the chaos action
	Duration *string `json:"duration,omitempty" webhook:"Duration"`

	// TcParameter represents the traffic control definition
	TcParameter `json:",inline"`

	// Direction represents the direction, this applies on netem and network partition action
	Direction Direction `json:"direction,omitempty"`

	// Target represents network target, this applies on netem and network partition action
	Target *PodSelector `json:"target,omitempty" webhook:",nilable"`

	// TargetDevice represents the network device to be affected in target scope.
	TargetDevice string `json:"targetDevice,omitempty"`

	// ExternalTargets represents network targets outside k8s
	ExternalTargets []string `json:"externalTargets,omitempty"`

	// RemoteCluster represents the remote cluster where the chaos will be deployed
	RemoteCluster string `json:"remoteCluster,omitempty"`
}

// NetworkChaosStatus defines the observed state of NetworkChaos
type NetworkChaosStatus struct {
	ChaosStatus `json:",inline"`
	// Instances always specifies podnetworkchaos generation or empty
	Instances map[string]int64 `json:"instances,omitempty"`
}

// DelaySpec defines detail of a delay action
type DelaySpec struct {
	Latency     string       `json:"latency" webhook:"Duration"`
	Correlation string       `json:"correlation,omitempty" default:"0" webhook:"FloatStr"`
	Jitter      string       `json:"jitter,omitempty" default:"0ms" webhook:"Duration"`
	Reorder     *ReorderSpec `json:"reorder,omitempty"`
}

// LossSpec defines detail of a loss action
type LossSpec struct {
	Loss        string `json:"loss" webhook:"FloatStr"`
	Correlation string `json:"correlation,omitempty" default:"0" webhook:"FloatStr"`
}

// DuplicateSpec defines detail of a duplicate action
type DuplicateSpec struct {
	Duplicate   string `json:"duplicate" webhook:"FloatStr"`
	Correlation string `json:"correlation,omitempty" default:"0" webhook:"FloatStr"`
}

// CorruptSpec defines detail of a corrupt action
type CorruptSpec struct {
	Corrupt     string `json:"corrupt" webhook:"FloatStr"`
	Correlation string `json:"correlation,omitempty" default:"0" webhook:"FloatStr"`
}

// BandwidthSpec defines detail of bandwidth limit.
type BandwidthSpec struct {
	// Rate is the speed knob. Allows bps, kbps, mbps, gbps, tbps unit. bps means bytes per second.
	Rate string `json:"rate" webhook:"Rate"`
	// Limit is the number of bytes that can be queued waiting for tokens to become available.
	Limit uint32 `json:"limit"`
	// Buffer is the maximum amount of bytes that tokens can be available for instantaneously.
	Buffer uint32 `json:"buffer"`
	// Peakrate is the maximum depletion rate of the bucket.
	// The peakrate does not need to be set, it is only necessary
	// if perfect millisecond timescale shaping is required.
	Peakrate *uint64 `json:"peakrate,omitempty"`
	// Minburst specifies the size of the peakrate bucket. For perfect
	// accuracy, should be set to the MTU of the interface.  If a
	// peakrate is needed, but some burstiness is acceptable, this
	// size can be raised. A 3000 byte minburst allows around 3mbit/s
	// of peakrate, given 1000 byte packets.
	Minburst *uint32 `json:"minburst,omitempty"`
}

// ReorderSpec defines details of packet reorder.
type ReorderSpec struct {
	Reorder     string `json:"reorder" webhook:"FloatStr"`
	Correlation string `json:"correlation,omitempty" default:"0" webhook:"FloatStr"`
	Gap         int    `json:"gap"`
}

func (obj *NetworkChaos) GetSelectorSpecs() map[string]interface{} {
	selectors := map[string]interface{}{
		".": &obj.Spec.PodSelector,
	}
	if obj.Spec.Target != nil {
		selectors[".Target"] = obj.Spec.Target
	}
	return selectors
}

func (obj *NetworkChaos) GetCustomStatus() interface{} {
	return &obj.Status.Instances
}
