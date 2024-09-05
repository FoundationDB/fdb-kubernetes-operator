/*
 * chaos_stress.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2040 Apple Inc. and the FoundationDB project authors
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
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// InjectPodStress injects pod stress on the target.
func (factory *Factory) InjectPodStress(target chaosmesh.PodSelectorSpec, containerNames []string, memoryStressor *chaosmesh.MemoryStressor, cpuStressor *chaosmesh.CPUStressor) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmesh.StressChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      factory.RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.StressChaosSpec{
			Duration: pointer.String(ChaosDurationForever),
			Stressors: &chaosmesh.Stressors{
				MemoryStressor: memoryStressor,
				CPUStressor:    cpuStressor,
			},
			ContainerSelector: chaosmesh.ContainerSelector{
				PodSelector: chaosmesh.PodSelector{
					Selector: target,
					Mode:     chaosmesh.AllMode,
				},
				ContainerNames: containerNames,
			},
		},
	})
}
