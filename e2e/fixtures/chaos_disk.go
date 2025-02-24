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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// InjectDiskFailure injects a disk failure for all Pods selected by the selector.
func (factory *Factory) InjectDiskFailure(selector chaosmesh.PodSelectorSpec) *ChaosMeshExperiment {
	return factory.InjectDiskFailureWithPath(selector, "/var/fdb/data", "/var/fdb/data/**/*", []chaosmesh.IoMethod{
		chaosmesh.Write,
		chaosmesh.Read,
		chaosmesh.Open,
		chaosmesh.Flush,
		chaosmesh.Fsync,
	}, []string{fdbv1beta2.MainContainerName})
}

// InjectDiskFailureWithDuration injects a disk failure for all Pods selected by the selector for the specified duration.
func (factory *Factory) InjectDiskFailureWithDuration(selector chaosmesh.PodSelectorSpec, duration string) *ChaosMeshExperiment {
	return factory.InjectDiskFailureWithPathAndDuration(selector, "/var/fdb/data", "/var/fdb/data/**/*", []chaosmesh.IoMethod{
		chaosmesh.Write,
		chaosmesh.Read,
		chaosmesh.Open,
		chaosmesh.Flush,
		chaosmesh.Fsync,
	}, []string{fdbv1beta2.MainContainerName}, duration)
}

// InjectDiskFailureWithPath injects a disk failure for all Pods selected by the selector. volumePath and path can be used to specify the affected files and methods
// allow to specify the affected methods.
func (factory *Factory) InjectDiskFailureWithPath(selector chaosmesh.PodSelectorSpec, volumePath string, path string, methods []chaosmesh.IoMethod, containers []string) *ChaosMeshExperiment {
	return factory.InjectDiskFailureWithPathAndDuration(selector, volumePath, path, methods, containers, ChaosDurationForever)
}

// InjectDiskFailureWithPathAndDuration injects a disk failure for all Pods selected by the selector. volumePath and path can be used to specify the affected files and methods
// allow to specify the affected methods. duration will define how long the chaos is injected.
func (factory *Factory) InjectDiskFailureWithPathAndDuration(selector chaosmesh.PodSelectorSpec, volumePath string, path string, methods []chaosmesh.IoMethod, containers []string, duration string) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmesh.IOChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      factory.RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.IOChaosSpec{
			Action:   chaosmesh.IoFaults,
			Duration: pointer.String(duration),
			ContainerSelector: chaosmesh.ContainerSelector{
				ContainerNames: containers,
				PodSelector: chaosmesh.PodSelector{
					Selector: selector,
					Mode:     chaosmesh.AllMode,
				},
			},
			VolumePath: volumePath,
			Path:       path,
			Errno:      5,
			Percent:    100,
			Methods:    methods,
		},
	})
}

// InjectIOLatency injects IOLatency for single pod.
func (factory *Factory) InjectIOLatency(selector chaosmesh.PodSelectorSpec, delay string) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmesh.IOChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      factory.RandStringRunes(32),
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
