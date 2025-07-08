/*
 * chaos_pod.go
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
	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ScheduleInjectPodKill schedules a Pod Kill action.
func (factory *Factory) ScheduleInjectPodKill(
	target chaosmesh.PodSelectorSpec,
	schedule string,
	mode chaosmesh.SelectorMode,
) *ChaosMeshExperiment {
	return factory.scheduleInjectPod(
		chaosmesh.PodSelector{
			Selector: target,
			Mode:     mode,
		},
		schedule,
		chaosmesh.PodKillAction,
		factory.RandStringRunes(32))
}

// ScheduleInjectPodKillWithName schedules a Pod Kill action with the specified name.
func (factory *Factory) ScheduleInjectPodKillWithName(
	target chaosmesh.PodSelectorSpec,
	schedule string,
	mode chaosmesh.SelectorMode,
	name string,
) *ChaosMeshExperiment {
	return factory.scheduleInjectPod(
		chaosmesh.PodSelector{
			Selector: target,
			Mode:     mode,
		},
		schedule,
		chaosmesh.PodKillAction,
		name)
}

func (factory *Factory) scheduleInjectPod(
	target chaosmesh.PodSelector,
	schedule string,
	action chaosmesh.PodChaosAction,
	name string,
) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmesh.Schedule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.ScheduleSpec{
			Schedule:          schedule,
			ConcurrencyPolicy: chaosmesh.AllowConcurrent,
			Type:              chaosmesh.ScheduleTypePodChaos,
			HistoryLimit:      1,
			ScheduleItem: chaosmesh.ScheduleItem{
				EmbedChaos: chaosmesh.EmbedChaos{
					PodChaos: &chaosmesh.PodChaosSpec{
						Action: action,
						ContainerSelector: chaosmesh.ContainerSelector{
							PodSelector: target,
						},
					},
				},
			},
		},
	})
}
