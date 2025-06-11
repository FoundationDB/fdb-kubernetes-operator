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

// +chaos-mesh:experiment

// HTTPChaos is the Schema for the HTTPchaos API
type HTTPChaos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HTTPChaosSpec   `json:"spec,omitempty"`
	Status HTTPChaosStatus `json:"status,omitempty"`
}

var _ InnerObjectWithCustomStatus = (*HTTPChaos)(nil)
var _ InnerObjectWithSelector = (*HTTPChaos)(nil)
var _ InnerObject = (*HTTPChaos)(nil)

type HTTPChaosSpec struct {
	PodSelector `json:",inline"`

	// Target is the object to be selected and injected.
	Target PodHttpChaosTarget `json:"target"`

	PodHttpChaosActions `json:",inline"`

	// Port represents the target port to be proxy of.
	Port int32 `json:"port,omitempty" webhook:"Port"`

	// Path is a rule to select target by uri path in http request.
	Path *string `json:"path,omitempty"`

	// Method is a rule to select target by http method in request.
	Method *string `json:"method,omitempty" webhook:"HTTPMethod"`

	// Code is a rule to select target by http status code in response.
	Code *int32 `json:"code,omitempty"`

	// RequestHeaders is a rule to select target by http headers in request.
	// The key-value pairs represent header name and header value pairs.
	RequestHeaders map[string]string `json:"request_headers,omitempty"`

	// ResponseHeaders is a rule to select target by http headers in response.
	// The key-value pairs represent header name and header value pairs.
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`

	// TLS is the tls config,
	// will override PodHttpChaos if there are multiple HTTPChaos experiments are applied
	TLS *PodHttpChaosTLS `json:"tls,omitempty"`

	// Duration represents the duration of the chaos action.
	Duration *string `json:"duration,omitempty" webhook:"Duration"`

	// RemoteCluster represents the remote cluster where the chaos will be deployed
	RemoteCluster string `json:"remoteCluster,omitempty"`
}

type HTTPChaosStatus struct {
	ChaosStatus `json:",inline"`

	// Instances always specifies podhttpchaos generation or empty
	Instances map[string]int64 `json:"instances,omitempty"`
}

func (obj *HTTPChaos) GetSelectorSpecs() map[string]interface{} {
	return map[string]interface{}{
		".": &obj.Spec.PodSelector,
	}
}

func (obj *HTTPChaos) GetCustomStatus() interface{} {
	return &obj.Status.Instances
}
