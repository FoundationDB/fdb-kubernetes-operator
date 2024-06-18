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
	chaosmeshv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// ScheduleInjectPodKill schedules a Pod Kill action.
func (factory *Factory) ScheduleInjectPodKill(target chaosmeshv1alpha1.PodSelectorSpec, schedule string, mode chaosmeshv1alpha1.SelectorMode) *ChaosMeshExperiment {
	return factory.scheduleInjectPod(
		chaosmeshv1alpha1.PodSelector{
			Selector: target,
			Mode:     mode,
		},
		schedule,
		chaosmeshv1alpha1.PodKillAction,
		RandStringRunes(32))
}

// ScheduleInjectPodKillWithName schedules a Pod Kill action with the specified name.
func (factory *Factory) ScheduleInjectPodKillWithName(target chaosmeshv1alpha1.PodSelectorSpec, schedule string, mode chaosmeshv1alpha1.SelectorMode, name string) *ChaosMeshExperiment {
	return factory.scheduleInjectPod(
		chaosmeshv1alpha1.PodSelector{
			Selector: target,
			Mode:     mode,
		},
		schedule,
		chaosmeshv1alpha1.PodKillAction,
		name)
}

func (factory *Factory) scheduleInjectPod(target chaosmeshv1alpha1.PodSelector, schedule string, action chaosmeshv1alpha1.PodChaosAction, name string) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmeshv1alpha1.Schedule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmeshv1alpha1.ScheduleSpec{
			Schedule:          schedule,
			ConcurrencyPolicy: chaosmeshv1alpha1.AllowConcurrent,
			Type:              chaosmeshv1alpha1.ScheduleTypePodChaos,
			HistoryLimit:      1,
			ScheduleItem: chaosmeshv1alpha1.ScheduleItem{
				EmbedChaos: chaosmeshv1alpha1.EmbedChaos{
					PodChaos: &chaosmeshv1alpha1.PodChaosSpec{
						Action: action,
						ContainerSelector: chaosmeshv1alpha1.ContainerSelector{
							PodSelector: target,
						},
					},
				},
			},
		},
	})
}

// PodFailure injects a pod failure for the provided target.
func (factory *Factory) PodFailure(target chaosmeshv1alpha1.PodSelectorSpec) *ChaosMeshExperiment {
	return factory.injectPodChaos(target, chaosmeshv1alpha1.PodFailureAction, RandStringRunes(32))
}

func (factory *Factory) injectPodChaos(target chaosmeshv1alpha1.PodSelectorSpec, action chaosmeshv1alpha1.PodChaosAction, name string) *ChaosMeshExperiment {
	return factory.CreateExperiment(&chaosmeshv1alpha1.PodChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmeshv1alpha1.PodChaosSpec{
			Action:   action,
			Duration: pointer.String(ChaosDurationForever),
			ContainerSelector: chaosmeshv1alpha1.ContainerSelector{
				PodSelector: chaosmeshv1alpha1.PodSelector{
					Selector: target,
					Mode:     chaosmeshv1alpha1.AllMode,
				},
			},
		},
	})
}
