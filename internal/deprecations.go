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
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdb"
	"k8s.io/apimachinery/pkg/api/equality"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
func NormalizeClusterSpec(cluster *fdbtypes.FoundationDBCluster, options DeprecationOptions) error {
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

	// Set up resource requirements for the main container.
	updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
		template.Spec.Containers, _ = ensureContainerPresent(template.Spec.Containers, "foundationdb", 0)

		template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb", func(container *v1.Container) {
			if container.Resources.Requests == nil {
				container.Resources.Requests = v1.ResourceList{
					"cpu":    resource.MustParse("1"),
					"memory": resource.MustParse("1Gi"),
				}
			}

			if container.Resources.Limits == nil {
				container.Resources.Limits = container.Resources.Requests
			}
		})
	})

	updateImageConfigs(&cluster.Spec, cluster.GetUseUnifiedImage())

	updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
		sidecarUpdater := func(container *v1.Container) {
			if container.Resources.Requests == nil {
				container.Resources.Requests = v1.ResourceList{
					"cpu":    resource.MustParse("100m"),
					"memory": resource.MustParse("256Mi"),
				}
			}

			if container.Resources.Limits == nil {
				container.Resources.Limits = container.Resources.Requests
			}
		}

		if !cluster.GetUseUnifiedImage() {
			template.Spec.InitContainers = customizeContainerFromList(template.Spec.InitContainers, "foundationdb-kubernetes-init", sidecarUpdater)
		}
		template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb-kubernetes-sidecar", sidecarUpdater)
	})

	return nil
}

func updateImageConfigs(spec *fdbtypes.FoundationDBClusterSpec, useUnifiedImage bool) {
	if useUnifiedImage {
		ensureImageConfigPresent(&spec.MainContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: "foundationdb/foundationdb-kubernetes"})
	} else {
		ensureImageConfigPresent(&spec.MainContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: "foundationdb/foundationdb"})
		ensureImageConfigPresent(&spec.SidecarContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar", TagSuffix: "-1"})
	}
}

func ensureImageConfigPresent(imageConfigs *[]fdbtypes.ImageConfig, expected fdbtypes.ImageConfig) {
	for _, config := range *imageConfigs {
		if equality.Semantic.DeepEqual(config, expected) {
			return
		}
	}

	*imageConfigs = append(*imageConfigs, expected)
}

// ensurePodTemplatePresent defines a pod template in the general process
// settings.
func ensurePodTemplatePresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdb.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdb.ProcessClassGeneral]
	if generalSettings.PodTemplate == nil {
		generalSettings.PodTemplate = &corev1.PodTemplateSpec{}
	}
	spec.Processes[fdb.ProcessClassGeneral] = generalSettings
}

// updatePodTemplates updates all of the pod templates in the cluster spec.
// This will also ensure that the general settings contain a pod template, so
// the customization function is guaranteed to be called at least once.
func updatePodTemplates(spec *fdbtypes.FoundationDBClusterSpec, customizer func(*corev1.PodTemplateSpec)) {
	ensurePodTemplatePresent(spec)
	updateProcessSettings(spec, func(settings *fdbtypes.ProcessSettings) {
		if settings.PodTemplate == nil {
			return
		}
		customizer(settings.PodTemplate)
	})
}

// updateProcessSettings runs a customization function on all of the process
// settings in the cluster spec.
func updateProcessSettings(spec *fdbtypes.FoundationDBClusterSpec, customizer func(*fdbtypes.ProcessSettings)) {
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
	for indexToCopy := 0; indexToCopy < len(containers); indexToCopy++ {
		if indexToCopy == insertIndex {
			newContainers = append(newContainers, corev1.Container{
				Name: name,
			})
		}
		newContainers = append(newContainers, containers[indexToCopy])
	}

	return newContainers, insertIndex
}
