/*
 * chaos_network.go
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
	"strconv"

	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// ensurePodPhaseSelectorIsSet this method takes a PodSelectorSpec and add the PodPhaseSelector if missing. If the
// PodPhaseSelector is not set chaos-mesh will try to inject the failure to all Pods, even to Pods that are in Pending
// or Failed. This can lead to test failures, since chaos-mesh is not able to inject the chaos to all Pods (it's impossible
// to inject chaos to non running Pods). See: https://chaos-mesh.org/docs/define-chaos-experiment-scope/#podphase-selectors
// Arguably this should be the default in chaos-mesh itself.
func ensurePodPhaseSelectorIsSet(selector *chaosmesh.PodSelectorSpec) {
	if len(selector.PodPhaseSelectors) > 0 {
		return
	}

	selector.PodPhaseSelectors = []string{string(corev1.PodRunning)}
}

// InjectNetworkLoss injects network loss between the source and the target.
func (factory *Factory) InjectNetworkLoss(lossPercentage string, source chaosmesh.PodSelectorSpec, target chaosmesh.PodSelectorSpec, direction chaosmesh.Direction) *ChaosMeshExperiment {
	ensurePodPhaseSelectorIsSet(&source)
	ensurePodPhaseSelectorIsSet(&target)

	return factory.CreateExperiment(&chaosmesh.NetworkChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      factory.RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.NetworkChaosSpec{
			Action:   chaosmesh.LossAction,
			Duration: pointer.String(ChaosDurationForever),
			PodSelector: chaosmesh.PodSelector{
				Selector: source,
				Mode:     chaosmesh.AllMode,
			},
			Target: &chaosmesh.PodSelector{
				Mode:     chaosmesh.AllMode,
				Selector: target,
			},
			Direction: direction,
			TcParameter: chaosmesh.TcParameter{
				Loss: &chaosmesh.LossSpec{
					Loss:        lossPercentage,
					Correlation: lossPercentage,
				},
			},
		},
	})
}

// InjectNetworkLossBetweenPods Injects network loss b/w each combination of podGroups.
func (factory *Factory) InjectNetworkLossBetweenPods(pods []chaosmesh.PodSelectorSpec, loss string) {
	count := len(pods)
	for i := 0; i < count; i++ {
		for j := i + 1; j < count; j++ {
			factory.InjectNetworkLoss(loss, pods[i], pods[j], chaosmesh.Both)
		}
	}
}

// InjectPartition injects a partition to the provided selector.
func (factory *Factory) InjectPartition(selector chaosmesh.PodSelectorSpec) *ChaosMeshExperiment {
	namespaces := make([]string, 0, len(selector.Pods))
	for namespace := range selector.Pods {
		namespaces = append(namespaces, namespace)
	}

	// Ensure we only select running Pods
	target := chaosmesh.PodSelectorSpec{
		GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
			Namespaces: namespaces,
		},
	}

	return factory.InjectPartitionBetween(selector, target)
}

// InjectPartitionWithExternalTargets injects a partition to the provided selector with the external targets
func (factory *Factory) InjectPartitionWithExternalTargets(selector chaosmesh.PodSelectorSpec, externalTargets []string) *ChaosMeshExperiment {
	return factory.injectPartitionBetween(selector, nil, chaosmesh.Both, externalTargets)
}

// InjectPartitionBetween injects a partition between the source and the target.
func (factory *Factory) InjectPartitionBetween(
	source chaosmesh.PodSelectorSpec,
	target chaosmesh.PodSelectorSpec,
) *ChaosMeshExperiment {
	return factory.InjectPartitionBetweenWithDirection(source, target, chaosmesh.Both)
}

// InjectPartitionBetweenWithDirection injects a partition between the source and the target for the specified direction.
func (factory *Factory) InjectPartitionBetweenWithDirection(
	source chaosmesh.PodSelectorSpec,
	target chaosmesh.PodSelectorSpec,
	direction chaosmesh.Direction,
) *ChaosMeshExperiment {
	return factory.injectPartitionBetween(source, &chaosmesh.PodSelector{
		Mode:     chaosmesh.AllMode,
		Selector: target,
	}, direction, nil)
}

func (factory *Factory) injectPartitionBetween(
	source chaosmesh.PodSelectorSpec,
	target *chaosmesh.PodSelector,
	direction chaosmesh.Direction,
	externalTargets []string,
) *ChaosMeshExperiment {
	ensurePodPhaseSelectorIsSet(&source)
	if target != nil {
		ensurePodPhaseSelectorIsSet(&target.Selector)
	}

	return factory.CreateExperiment(&chaosmesh.NetworkChaos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      factory.RandStringRunes(32),
			Namespace: factory.GetChaosNamespace(),
			Labels:    factory.GetDefaultLabels(),
		},
		Spec: chaosmesh.NetworkChaosSpec{
			Action:   chaosmesh.PartitionAction,
			Duration: pointer.String(ChaosDurationForever),
			PodSelector: chaosmesh.PodSelector{
				Selector: source,
				Mode:     chaosmesh.AllMode,
			},
			Target:          target,
			Direction:       direction,
			ExternalTargets: externalTargets,
		},
	})
}

// InjectPartitionOnSomeTargetPods injects a partition on some Pods instead of all Pods.
// It picks a specific number of pods randomly. If the specified "fixedNumber" is greater than the number of all targets, all targets will be chosen.
func (factory *Factory) InjectPartitionOnSomeTargetPods(
	source chaosmesh.PodSelectorSpec,
	target chaosmesh.PodSelectorSpec,
	fixedNumber int,
) *ChaosMeshExperiment {
	return factory.injectPartitionBetween(source, &chaosmesh.PodSelector{
		Mode:     chaosmesh.FixedMode,
		Value:    strconv.Itoa(fixedNumber),
		Selector: target,
	}, chaosmesh.Both, nil)
}
