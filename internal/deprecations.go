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
	"fmt"
	"reflect"
	"strings"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func NormalizeClusterSpec(spec *fdbtypes.FoundationDBClusterSpec, options DeprecationOptions) error {
	if spec.PodTemplate != nil {
		ensurePodTemplatePresent(spec)
		generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
		if reflect.DeepEqual(generalSettings.PodTemplate, &v1.PodTemplateSpec{}) {
			generalSettings.PodTemplate = spec.PodTemplate
		}
		spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
		spec.PodTemplate = nil
	}

	if spec.SidecarVersion != 0 {
		if spec.SidecarVersions == nil {
			spec.SidecarVersions = make(map[string]int)
		}
		spec.SidecarVersions[spec.Version] = spec.SidecarVersion
		spec.SidecarVersion = 0
	}

	if spec.VolumeClaim != nil {
		if spec.Processes == nil {
			spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
		}
		generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
		if generalSettings.VolumeClaimTemplate == nil {
			generalSettings.VolumeClaimTemplate = spec.VolumeClaim.DeepCopy()
		}
		spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
		spec.VolumeClaim = nil
	}

	if spec.Processes != nil {
		for key, settings := range spec.Processes {
			if settings.VolumeClaim != nil && settings.VolumeClaimTemplate == nil {
				settings.VolumeClaimTemplate = settings.VolumeClaim
				settings.VolumeClaim = nil
				spec.Processes[key] = settings
			}
		}
	}

	if spec.NextInstanceID != 0 {
		spec.NextInstanceID = 0
	}

	if spec.PodLabels != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			mergeLabels(&template.ObjectMeta, spec.PodLabels)
		})

		updateVolumeClaims(spec, func(claim *v1.PersistentVolumeClaim) {
			mergeLabels(&claim.ObjectMeta, spec.PodLabels)
		})

		ensureConfigMapPresent(spec)
		mergeLabels(&spec.ConfigMap.ObjectMeta, spec.PodLabels)

		spec.PodLabels = nil
	}

	if spec.Resources != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			template.Spec.Containers, _ = ensureContainerPresent(template.Spec.Containers, "foundationdb", 0)

			template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb", func(container *v1.Container) {
				container.Resources = *spec.Resources
			})
		})

		spec.Resources = nil
	}

	if spec.InitContainers != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			template.Spec.InitContainers = append(template.Spec.InitContainers, spec.InitContainers...)
		})

		spec.InitContainers = nil
	}

	if spec.Containers != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			template.Spec.Containers = append(template.Spec.Containers, spec.Containers...)
		})

		spec.Containers = nil
	}

	if spec.Volumes != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			if template.Spec.Volumes == nil {
				template.Spec.Volumes = make([]v1.Volume, 0, len(spec.Volumes))
			}
			template.Spec.Volumes = append(template.Spec.Volumes, spec.Volumes...)
		})
		spec.Volumes = nil
	}

	if spec.PodSecurityContext != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			template.Spec.SecurityContext = spec.PodSecurityContext
		})
		spec.PodSecurityContext = nil
	}

	if spec.AutomountServiceAccountToken != nil {
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
			template.Spec.AutomountServiceAccountToken = spec.AutomountServiceAccountToken
		})
		spec.AutomountServiceAccountToken = nil
	}

	if spec.StorageClass != nil {
		updateVolumeClaims(spec, func(volumeClaim *v1.PersistentVolumeClaim) {
			volumeClaim.Spec.StorageClassName = spec.StorageClass
		})
		spec.StorageClass = nil
	}

	if spec.VolumeSize != "" {
		storageQuantity, err := resource.ParseQuantity(spec.VolumeSize)
		if err != nil {
			return err
		}
		updateVolumeClaims(spec, func(volumeClaim *v1.PersistentVolumeClaim) {
			if volumeClaim.Spec.Resources.Requests == nil {
				volumeClaim.Spec.Resources.Requests = v1.ResourceList{}
			}
			volumeClaim.Spec.Resources.Requests[v1.ResourceStorage] = storageQuantity
		})
		spec.VolumeSize = ""
	}

	if spec.RunningVersion != "" {
		spec.RunningVersion = ""
	}

	if spec.ConnectionString != "" {
		spec.ConnectionString = ""
	}

	if len(spec.CustomParameters) > 0 {
		params := spec.CustomParameters
		ensureCustomParametersPresent(spec)
		for processClass, settings := range spec.Processes {
			settings.CustomParameters = &params
			spec.Processes[processClass] = settings
		}
		spec.CustomParameters = nil
	}

	// Validate customParameters
	for processClass := range spec.Processes {
		if setting, ok := spec.Processes[processClass]; ok {
			if setting.CustomParameters == nil {
				continue
			}

			err := ValidateCustomParameters(*setting.CustomParameters)
			if err != nil {
				return err
			}
		}
	}

	if !options.OnlyShowChanges {
		// Set up resource requirements for the main container.
		updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
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

		if spec.Services.PublicIPSource == nil {
			source := fdbtypes.PublicIPSourcePod
			spec.Services.PublicIPSource = &source
		}

		if spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds == nil {
			duration := 1800
			spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds = &duration
		}
	}

	// Apply changes between old and new defaults.
	// When we update the defaults in the next release, the following sections
	// should be moved under the `!OnlyShowChanges` section, and we should use
	// the latest defaults as the active defaults.
	// Set up sidecar resource requirements
	zeroQuantity, err := resource.ParseQuantity("0")
	if err != nil {
		return err
	}

	updatePodTemplates(spec, func(template *v1.PodTemplateSpec) {
		sidecarUpdater := func(container *v1.Container) {
			if options.UseFutureDefaults {
				if container.Resources.Requests == nil {
					container.Resources.Requests = v1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("256Mi"),
					}
				}
				if container.Resources.Limits == nil {
					container.Resources.Limits = container.Resources.Requests
				}
			} else {
				if options.OnlyShowChanges {
					if container.Resources.Requests == nil {
						container.Resources.Requests = v1.ResourceList{
							"org.foundationdb/empty": zeroQuantity,
						}
					}
					if container.Resources.Limits == nil {
						container.Resources.Limits = v1.ResourceList{
							"org.foundationdb/empty": zeroQuantity,
						}
					}
				}
			}
		}

		template.Spec.InitContainers = customizeContainerFromList(template.Spec.InitContainers, "foundationdb-kubernetes-init", sidecarUpdater)
		template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb-kubernetes-sidecar", sidecarUpdater)
	})

	// Setting the default since the CRD default enforces the default in the runtime
	if spec.MinimumUptimeSecondsForBounce == 0 {
		spec.MinimumUptimeSecondsForBounce = 600
	}

	if spec.AutomationOptions.Replacements.Enabled == nil {
		enabled := options.UseFutureDefaults
		spec.AutomationOptions.Replacements.Enabled = &enabled
	}

	return nil
}

// ensureConfigMapPresent defines a config map in the cluster spec.
func ensureConfigMapPresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.ConfigMap == nil {
		spec.ConfigMap = &v1.ConfigMap{}
	}
}

// ensurePodTemplatePresent defines a pod template in the general process
// settings.
func ensurePodTemplatePresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
	if generalSettings.PodTemplate == nil {
		generalSettings.PodTemplate = &corev1.PodTemplateSpec{}
	}
	spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
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

// updateVolumeClaims updates all of the volume claims in the cluster spec.
// This will also ensure that the general settings contain a volume claim, so
// the customization function is guaranteed to be called at least once.
func updateVolumeClaims(spec *fdbtypes.FoundationDBClusterSpec, customizer func(*corev1.PersistentVolumeClaim)) {
	ensureVolumeClaimPresent(spec)
	updateProcessSettings(spec, func(settings *fdbtypes.ProcessSettings) {
		if settings.VolumeClaimTemplate == nil {
			return
		}
		customizer(settings.VolumeClaimTemplate)
	})
}

// ensureVolumeClaimPresent defines a volume claim in the general process
// settings.
func ensureVolumeClaimPresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
	if generalSettings.VolumeClaimTemplate == nil {
		generalSettings.VolumeClaimTemplate = &corev1.PersistentVolumeClaim{}
	}
	spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
}

// mergeLabels merges labels from another part of the cluster spec into the
// object metadata.
func mergeLabels(metadata *metav1.ObjectMeta, labels map[string]string) {
	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string, len(labels))
	}
	for key, value := range labels {
		_, present := metadata.Labels[key]
		if !present {
			metadata.Labels[key] = value
		}
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

// ensureCustomParametersPresent defines custom parameters for the general process
// settings.
func ensureCustomParametersPresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
	if generalSettings.CustomParameters == nil {
		params := make([]string, 0)
		generalSettings.CustomParameters = &params
	}
	spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
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

// ValidateCustomParameters ensures that no duplicate values are set and that no
// protected/forbidden parameters are set. Theoretically we could also check if FDB
// supports the given parameter.
func ValidateCustomParameters(customParameters []string) error {
	protectedParameters := map[string]bool{"datadir": true}
	parameters := make(map[string]bool)
	violations := make([]string, 0)

	for _, parameter := range customParameters {
		parameterName := strings.Split(parameter, "=")[0]
		parameterName = strings.TrimSpace(parameterName)

		if _, ok := parameters[parameterName]; !ok {
			parameters[parameterName] = true
		} else {
			violations = append(violations, fmt.Sprintf("found duplicated customParameter: %v", parameterName))
		}

		if _, ok := protectedParameters[parameterName]; ok {
			violations = append(violations, fmt.Sprintf("found protected customParameter: %v, please remove this parameter from the customParameters list", parameterName))
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("found the following customParameters violations:\n%s", strings.Join(violations, "\n"))
	}

	return nil
}
