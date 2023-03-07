/*
 * chaos_disk.go
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
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// InjectDiskFailure injects a disk failure for all Pods.
func (factory *Factory) InjectDiskFailure(selector chaosmesh.PodSelectorSpec) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmesh.IOChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.IOChaosSpec{
			Action:   chaosmesh.IoFaults,
			Duration: pointer.String(ChaosDurationForever),
			ContainerSelector: chaosmesh.ContainerSelector{
				PodSelector: chaosmesh.PodSelector{
					Selector: selector,
					Mode:     chaosmesh.AllMode,
				},
			},
			VolumePath: "/var/fdb/data",
			Path:       "/var/fdb/data/**/*",
			Errno:      5,
			Percent:    100,
			Methods: []chaosmesh.IoMethod{
				chaosmesh.Write,
				chaosmesh.Read,
				chaosmesh.Open,
				chaosmesh.Flush,
				chaosmesh.Fsync,
			},
		},
	})
}

// InjectIOLatency injects IOLatency for single pod.
func (factory *Factory) InjectIOLatency(selector chaosmesh.PodSelectorSpec, delay string) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmesh.IOChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.IOChaosSpec{
			Action:   chaosmesh.IoLatency,
			Duration: pointer.String(ChaosDurationForever),
			ContainerSelector: chaosmesh.ContainerSelector{
				PodSelector: chaosmesh.PodSelector{
					Selector: selector,
					Mode:     chaosmesh.AllMode,
				},
			},
			VolumePath: "/var/fdb/data",
			Path:       "/var/fdb/data/**/*",
			Percent:    100,
			Delay:      delay,
			Methods: []chaosmesh.IoMethod{
				chaosmesh.Write,
				chaosmesh.Read,
				chaosmesh.Open,
				chaosmesh.Flush,
				chaosmesh.Fsync,
			},
		},
	})
}
