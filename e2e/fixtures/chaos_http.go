/*
 * chaos_http.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	chaosmeshv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// InjectHTTPClientChaosWrongResultFdbMonitorConf  this method can be used to simulate a bad response from the operator to the sidecar. Currently this method returns as body the value "wrong"
// when the operator does a request against the check_hash/fdbmonitor.conf endpoint, e.g. during upgrades.
func (factory *Factory) InjectHTTPClientChaosWrongResultFdbMonitorConf(selector chaosmeshv1alpha1.PodSelectorSpec, namespace string) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmeshv1alpha1.HTTPChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      factory.RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmeshv1alpha1.HTTPChaosSpec{
			Target:   chaosmeshv1alpha1.PodHttpResponse,
			Duration: pointer.String(ChaosDurationForever),
			PodSelector: chaosmeshv1alpha1.PodSelector{
				Selector: selector,
				Mode:     chaosmeshv1alpha1.AllMode,
			},
			Port:   8080,
			Method: pointer.String("GET"),
			Path:   pointer.String("check_hash/fdbmonitor.conf"),
			PodHttpChaosActions: chaosmeshv1alpha1.PodHttpChaosActions{
				Replace: &chaosmeshv1alpha1.PodHttpChaosReplaceActions{
					Body: []byte("wrong"),
				},
			},
			TLS: &chaosmeshv1alpha1.PodHttpChaosTLS{
				SecretName:      factory.GetSecretName(),
				SecretNamespace: namespace,
				CertName:        "tls.crt",
				KeyName:         "tls.key",
				CAName:          pointer.String("ca.pem"),
			},
		},
	})
}
