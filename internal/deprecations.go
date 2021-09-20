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
	"sort"
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
func NormalizeClusterSpec(cluster *fdbtypes.FoundationDBCluster, options DeprecationOptions) error {
	if cluster.Spec.PodTemplate != nil {
		ensurePodTemplatePresent(&cluster.Spec)
		generalSettings := cluster.Spec.Processes[fdbtypes.ProcessClassGeneral]
		if reflect.DeepEqual(generalSettings.PodTemplate, &v1.PodTemplateSpec{}) {
			generalSettings.PodTemplate = cluster.Spec.PodTemplate
		}
		cluster.Spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
		cluster.Spec.PodTemplate = nil
	}

	if cluster.Spec.SidecarVersion != 0 {
		cluster.Spec.SidecarContainer.ImageConfigs = append(cluster.Spec.SidecarContainer.ImageConfigs,
			fdbtypes.ImageConfig{TagSuffix: fmt.Sprintf("-%d", cluster.Spec.SidecarVersion)},
		)
		cluster.Spec.SidecarVersion = 0
	}

	if cluster.Spec.VolumeClaim != nil {
		if cluster.Spec.Processes == nil {
			cluster.Spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
		}
		generalSettings := cluster.Spec.Processes[fdbtypes.ProcessClassGeneral]
		if generalSettings.VolumeClaimTemplate == nil {
			generalSettings.VolumeClaimTemplate = cluster.Spec.VolumeClaim.DeepCopy()
		}
		cluster.Spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
		cluster.Spec.VolumeClaim = nil
	}

	if cluster.Spec.Processes != nil {
		for key, settings := range cluster.Spec.Processes {
			if settings.VolumeClaim != nil && settings.VolumeClaimTemplate == nil {
				settings.VolumeClaimTemplate = settings.VolumeClaim
				settings.VolumeClaim = nil
				cluster.Spec.Processes[key] = settings
			}
		}
	}

	if cluster.Spec.NextInstanceID != 0 {
		cluster.Spec.NextInstanceID = 0
	}

	if cluster.Spec.PodLabels != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			mergeLabels(&template.ObjectMeta, cluster.Spec.PodLabels)
		})

		updateVolumeClaims(&cluster.Spec, func(claim *v1.PersistentVolumeClaim) {
			mergeLabels(&claim.ObjectMeta, cluster.Spec.PodLabels)
		})

		ensureConfigMapPresent(&cluster.Spec)
		mergeLabels(&cluster.Spec.ConfigMap.ObjectMeta, cluster.Spec.PodLabels)

		cluster.Spec.PodLabels = nil
	}

	if cluster.Spec.Resources != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			template.Spec.Containers, _ = ensureContainerPresent(template.Spec.Containers, "foundationdb", 0)

			template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb", func(container *v1.Container) {
				container.Resources = *cluster.Spec.Resources
			})
		})

		cluster.Spec.Resources = nil
	}

	if cluster.Spec.InitContainers != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			template.Spec.InitContainers = append(template.Spec.InitContainers, cluster.Spec.InitContainers...)
		})

		cluster.Spec.InitContainers = nil
	}

	if cluster.Spec.Containers != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			template.Spec.Containers = append(template.Spec.Containers, cluster.Spec.Containers...)
		})

		cluster.Spec.Containers = nil
	}

	if cluster.Spec.Volumes != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			if template.Spec.Volumes == nil {
				template.Spec.Volumes = make([]v1.Volume, 0, len(cluster.Spec.Volumes))
			}
			template.Spec.Volumes = append(template.Spec.Volumes, cluster.Spec.Volumes...)
		})
		cluster.Spec.Volumes = nil
	}

	if cluster.Spec.PodSecurityContext != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			template.Spec.SecurityContext = cluster.Spec.PodSecurityContext
		})
		cluster.Spec.PodSecurityContext = nil
	}

	if cluster.Spec.AutomountServiceAccountToken != nil {
		updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
			template.Spec.AutomountServiceAccountToken = cluster.Spec.AutomountServiceAccountToken
		})
		cluster.Spec.AutomountServiceAccountToken = nil
	}

	if cluster.Spec.StorageClass != nil {
		updateVolumeClaims(&cluster.Spec, func(volumeClaim *v1.PersistentVolumeClaim) {
			volumeClaim.Spec.StorageClassName = cluster.Spec.StorageClass
		})
		cluster.Spec.StorageClass = nil
	}

	if cluster.Spec.VolumeSize != "" {
		storageQuantity, err := resource.ParseQuantity(cluster.Spec.VolumeSize)
		if err != nil {
			return err
		}
		updateVolumeClaims(&cluster.Spec, func(volumeClaim *v1.PersistentVolumeClaim) {
			if volumeClaim.Spec.Resources.Requests == nil {
				volumeClaim.Spec.Resources.Requests = v1.ResourceList{}
			}
			volumeClaim.Spec.Resources.Requests[v1.ResourceStorage] = storageQuantity
		})
		cluster.Spec.VolumeSize = ""
	}

	if cluster.Spec.RunningVersion != "" {
		cluster.Spec.RunningVersion = ""
	}

	if cluster.Spec.ConnectionString != "" {
		cluster.Spec.ConnectionString = ""
	}

	updateContainerOverrides(&cluster.Spec, &cluster.Spec.MainContainer, "foundationdb")
	updateContainerOverrides(&cluster.Spec, &cluster.Spec.SidecarContainer, "foundationdb-kubernetes-sidecar")

	if len(cluster.Spec.CustomParameters) > 0 {
		params := cluster.Spec.CustomParameters
		ensureCustomParametersPresent(&cluster.Spec)
		for processClass, settings := range cluster.Spec.Processes {
			settings.CustomParameters = &params
			cluster.Spec.Processes[processClass] = settings
		}
		cluster.Spec.CustomParameters = nil
	}

	if cluster.Spec.Services.Headless != nil {
		cluster.Spec.Routing.HeadlessService = cluster.Spec.Services.Headless
		cluster.Spec.Services.Headless = nil
	}

	if cluster.Spec.Services.PublicIPSource != nil {
		cluster.Spec.Routing.PublicIPSource = cluster.Spec.Services.PublicIPSource
		cluster.Spec.Services.PublicIPSource = nil
	}

	if cluster.Spec.SidecarVersions != nil {
		configs := make([]fdbtypes.ImageConfig, 0, len(cluster.Spec.SidecarVersions))
		for version, sidecarVersion := range cluster.Spec.SidecarVersions {
			configs = append(configs, fdbtypes.ImageConfig{Version: version, TagSuffix: fmt.Sprintf("-%d", sidecarVersion)})
		}
		sort.Slice(configs, func(i, j int) bool {
			return configs[i].Version < configs[j].Version
		})

		cluster.Spec.SidecarContainer.ImageConfigs = append(cluster.Spec.SidecarContainer.ImageConfigs, configs...)
		cluster.Spec.SidecarVersions = nil
	}

	if cluster.Spec.MainContainer.ImageName != "" {
		cluster.Spec.MainContainer.ImageConfigs = append(cluster.Spec.MainContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: cluster.Spec.MainContainer.ImageName})
		cluster.Spec.MainContainer.ImageName = ""
	}

	if cluster.Spec.SidecarContainer.ImageName != "" {
		cluster.Spec.SidecarContainer.ImageConfigs = append(cluster.Spec.SidecarContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: cluster.Spec.SidecarContainer.ImageName})
		cluster.Spec.SidecarContainer.ImageName = ""
	}

	// Validate customParameters
	for processClass := range cluster.Spec.Processes {
		if setting, ok := cluster.Spec.Processes[processClass]; ok {
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

		if cluster.Spec.Routing.PublicIPSource == nil {
			source := fdbtypes.PublicIPSourcePod
			cluster.Spec.Routing.PublicIPSource = &source
		}

		if cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds == nil {
			duration := 1800
			cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds = &duration
		}

		cluster.Spec.MainContainer.ImageConfigs = append(cluster.Spec.MainContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: "foundationdb/foundationdb"})
		cluster.Spec.SidecarContainer.ImageConfigs = append(cluster.Spec.SidecarContainer.ImageConfigs, fdbtypes.ImageConfig{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar", TagSuffix: "-1"})
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

	updatePodTemplates(&cluster.Spec, func(template *v1.PodTemplateSpec) {
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
	if cluster.Spec.MinimumUptimeSecondsForBounce == 0 {
		cluster.Spec.MinimumUptimeSecondsForBounce = 600
	}

	if cluster.Spec.AutomationOptions.Replacements.Enabled == nil {
		enabled := options.UseFutureDefaults
		cluster.Spec.AutomationOptions.Replacements.Enabled = &enabled
	}

	if cluster.Spec.SidecarContainer.EnableLivenessProbe == nil {
		enabled := options.UseFutureDefaults
		cluster.Spec.SidecarContainer.EnableLivenessProbe = &enabled
	}
	if cluster.Spec.SidecarContainer.EnableReadinessProbe == nil {
		enabled := !options.UseFutureDefaults
		cluster.Spec.SidecarContainer.EnableReadinessProbe = &enabled
	}

	if cluster.Spec.LabelConfig.FilterOnOwnerReferences == nil {
		enabled := !options.UseFutureDefaults
		cluster.Spec.LabelConfig.FilterOnOwnerReferences = &enabled
	}

	if cluster.Spec.LabelConfig.MatchLabels == nil {
		if options.UseFutureDefaults {
			cluster.Spec.LabelConfig.MatchLabels = map[string]string{
				fdbtypes.FDBClusterLabel: cluster.Name,
			}
		} else {
			cluster.Spec.LabelConfig.MatchLabels = map[string]string{
				OldFDBClusterLabel: cluster.Name,
			}

		}
	}

	if cluster.Spec.LabelConfig.ResourceLabels == nil && !options.UseFutureDefaults {
		cluster.Spec.LabelConfig.ResourceLabels = map[string]string{
			fdbtypes.FDBClusterLabel: cluster.Name,
		}
	}

	if cluster.Spec.LabelConfig.InstanceIDLabels == nil {
		if options.UseFutureDefaults {
			cluster.Spec.LabelConfig.InstanceIDLabels = []string{fdbtypes.FDBInstanceIDLabel}
		} else {
			cluster.Spec.LabelConfig.InstanceIDLabels = []string{
				OldFDBInstanceIDLabel, fdbtypes.FDBInstanceIDLabel,
			}
		}
	}

	if cluster.Spec.LabelConfig.ProcessClassLabels == nil {
		if options.UseFutureDefaults {
			cluster.Spec.LabelConfig.ProcessClassLabels = []string{fdbtypes.FDBProcessClassLabel}
		} else {
			cluster.Spec.LabelConfig.ProcessClassLabels = []string{
				OldFDBProcessClassLabel, fdbtypes.FDBProcessClassLabel,
			}
		}
	}

	if cluster.Spec.UseExplicitListenAddress == nil {
		enabled := options.UseFutureDefaults
		cluster.Spec.UseExplicitListenAddress = &enabled
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

// updateContainerOverrides moves deprecated fields from the container overrides
// into the pod template in the process settings.
func updateContainerOverrides(spec *fdbtypes.FoundationDBClusterSpec, overrides *fdbtypes.ContainerOverrides, containerName string) {
	if overrides.Env != nil {
		updatePodTemplates(spec, func(podSpec *corev1.PodTemplateSpec) {
			podSpec.Spec.Containers = customizeContainerFromList(podSpec.Spec.Containers, containerName, func(container *corev1.Container) {
				container.Env = append(container.Env, overrides.Env...)
			})
		})
		overrides.Env = nil
	}

	if overrides.VolumeMounts != nil {
		updatePodTemplates(spec, func(podSpec *corev1.PodTemplateSpec) {
			podSpec.Spec.Containers = customizeContainerFromList(podSpec.Spec.Containers, containerName, func(container *corev1.Container) {
				container.VolumeMounts = append(container.VolumeMounts, overrides.VolumeMounts...)
			})
		})
		overrides.VolumeMounts = nil
	}

	if overrides.SecurityContext != nil {
		updatePodTemplates(spec, func(podSpec *corev1.PodTemplateSpec) {
			podSpec.Spec.Containers = customizeContainerFromList(podSpec.Spec.Containers, containerName, func(container *corev1.Container) {
				container.SecurityContext = overrides.SecurityContext
			})
		})
		overrides.SecurityContext = nil
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
