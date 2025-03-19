/*
 * deprecations.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/pointer"
)

// DeprecationOptions controls how deprecations and changes to defaults
// get applied to our specs.
type DeprecationOptions struct {
	// Whether we should apply the latest defaults rather than the defaults that
	// were initially established for this major version.
	UseFutureDefaults bool

	// Whether we should only fill in defaults that have changes between major
	// versions of the operator.
	OnlyShowChanges bool
}

// NormalizeClusterSpec converts a cluster spec into an unambiguous,
// future-proof form, by applying any implicit defaults and moving configuration
// from deprecated fields into fully-supported fields.
func NormalizeClusterSpec(cluster *fdbv1beta2.FoundationDBCluster, options DeprecationOptions) error {
	// Validate customParameters
	for processClass := range cluster.Spec.Processes {
		if setting, ok := cluster.Spec.Processes[processClass]; ok {
			if setting.CustomParameters == nil {
				continue
			}

			err := setting.CustomParameters.ValidateCustomParameters()
			if err != nil {
				return err
			}
		}
	}

	if !options.OnlyShowChanges {
		err := updateFutureDefaults(cluster, options)
		if err != nil {
			return err
		}

		// Set up resource requirements for the main container.
		updatePodTemplates(&cluster.Spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.Containers, _ = ensureContainerPresent(template.Spec.Containers, fdbv1beta2.MainContainerName, 0)

			template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, fdbv1beta2.MainContainerName, func(container *corev1.Container) {
				if container.Resources.Requests == nil {
					// See: https://apple.github.io/foundationdb/configuration.html#system-requirements
					container.Resources.Requests = corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					}
				}

				if container.Resources.Limits == nil {
					container.Resources.Limits = container.Resources.Requests
				}
			})

			sidecarUpdater := func(container *corev1.Container) {
				if container.Resources.Requests == nil {
					container.Resources.Requests = corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					}
				}

				if container.Resources.Limits == nil {
					container.Resources.Limits = container.Resources.Requests
				}
			}

			if !cluster.UseUnifiedImage() {
				template.Spec.InitContainers = customizeContainerFromList(template.Spec.InitContainers, fdbv1beta2.InitContainerName, sidecarUpdater)
			}
			template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, fdbv1beta2.SidecarContainerName, sidecarUpdater)
		})

		updateImageConfigs(&cluster.Spec, cluster.UseUnifiedImage())
	}

	if len(cluster.Spec.Buggify.CrashLoop) > 0 {
		crashLoopContainers := cluster.GetCrashLoopContainerProcessGroups()
		if _, ok := crashLoopContainers[fdbv1beta2.MainContainerName]; !ok {
			crashLoopContainers[fdbv1beta2.MainContainerName] = make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None)
		}

		for _, pid := range cluster.Spec.Buggify.CrashLoop {
			crashLoopContainers[fdbv1beta2.MainContainerName][pid] = fdbv1beta2.None{}
		}

		result := make([]fdbv1beta2.CrashLoopContainerObject, len(crashLoopContainers))
		i := 0
		for name, procs := range crashLoopContainers {
			result[i].ContainerName = name
			result[i].Targets = make([]fdbv1beta2.ProcessGroupID, len(procs))

			j := 0
			for pid := range procs {
				result[i].Targets[j] = pid
				j++
			}
			i++
		}

		cluster.Spec.Buggify.CrashLoopContainers = result
	}

	return nil
}

func updateImageConfigs(spec *fdbv1beta2.FoundationDBClusterSpec, useUnifiedImage bool) {
	if useUnifiedImage {
		ensureImageConfigPresent(&spec.MainContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: fdbv1beta2.FoundationDBKubernetesBaseImage})
	} else {
		ensureImageConfigPresent(&spec.MainContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: fdbv1beta2.FoundationDBBaseImage})
		ensureImageConfigPresent(&spec.SidecarContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: fdbv1beta2.FoundationDBSidecarBaseImage, TagSuffix: "-1"})
	}
}

func ensureImageConfigPresent(imageConfigs *[]fdbv1beta2.ImageConfig, expected fdbv1beta2.ImageConfig) {
	for _, config := range *imageConfigs {
		if equality.Semantic.DeepEqual(config, expected) {
			return
		}
	}

	*imageConfigs = append(*imageConfigs, expected)
}

// ensurePodTemplatePresent defines a pod template in the general process
// settings.
func ensurePodTemplatePresent(spec *fdbv1beta2.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbv1beta2.ProcessClassGeneral]
	if generalSettings.PodTemplate == nil {
		generalSettings.PodTemplate = &corev1.PodTemplateSpec{}
	}
	spec.Processes[fdbv1beta2.ProcessClassGeneral] = generalSettings
}

// updatePodTemplates updates all of the pod templates in the cluster spec.
// This will also ensure that the general settings contain a pod template, so
// the customization function is guaranteed to be called at least once.
func updatePodTemplates(spec *fdbv1beta2.FoundationDBClusterSpec, customizer func(*corev1.PodTemplateSpec)) {
	ensurePodTemplatePresent(spec)
	updateProcessSettings(spec, func(settings *fdbv1beta2.ProcessSettings) {
		if settings.PodTemplate == nil {
			return
		}
		customizer(settings.PodTemplate)
	})
}

// updateProcessSettings runs a customization function on all of the process
// settings in the cluster spec.
func updateProcessSettings(spec *fdbv1beta2.FoundationDBClusterSpec, customizer func(*fdbv1beta2.ProcessSettings)) {
	for processClass, settings := range spec.Processes {
		customizer(&settings)
		spec.Processes[processClass] = settings
	}
}

// customizeContainerFromList finds a container by name and runs a customization
// function on the container.
func customizeContainerFromList(containers []corev1.Container, name string, customizer func(*corev1.Container)) []corev1.Container {
	containers, index := ensureContainerPresent(containers, name, -1)
	container := containers[index]
	customizer(&container)
	containers[index] = container
	return containers
}

// ensureContainerPresent looks for a container by name from a list, and adds
// an empty container with that name if none is present.
func ensureContainerPresent(containers []corev1.Container, name string, insertIndex int) ([]corev1.Container, int) {
	for index, container := range containers {
		if container.Name == name {
			return containers, index
		}
	}

	if insertIndex < 0 || insertIndex >= len(containers) {
		containers = append(containers, corev1.Container{Name: name})
		return containers, len(containers) - 1
	}
	containerCount := 1 + len(containers)
	newContainers := make([]corev1.Container, 0, containerCount)
	for indexToCopy := range containers {
		if indexToCopy == insertIndex {
			newContainers = append(newContainers, corev1.Container{
				Name: name,
			})
		}
		newContainers = append(newContainers, containers[indexToCopy])
	}

	return newContainers, insertIndex
}

// updateFutureDefaults will update all unset fields with the new default values for the operator release 3.0
func updateFutureDefaults(cluster *fdbv1beta2.FoundationDBCluster, options DeprecationOptions) error {
	if !options.UseFutureDefaults {
		return nil
	}

	// The operator will use the unified image as default image type in the operator release 2.0.
	if cluster.Spec.ImageType == nil {
		imageType := fdbv1beta2.ImageTypeUnified
		cluster.Spec.ImageType = &imageType
	}

	// Locality based exclusions will be the default in the next operator release.
	if cluster.Spec.AutomationOptions.UseLocalitiesForExclusion == nil {
		cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(true)
	}

	// The DNS feature will be the default in the next operator release.
	if cluster.Spec.Routing.UseDNSInClusterFile == nil {
		cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
	}

	return nil
}
