/*
 * allowed_pod_modifications.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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

package v1beta2

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// volumeSourceType returns the name of the used corev1.VolumeSource, e.g. "secret'.
var volumeSourceType func(corev1.VolumeSource) string

// The method will run once to initialize all known corev1.VolumeSource types. Once everything is initialized the volumeSourceType
// method can be used to get the type of the corev1.VolumeSource. Doing this in the init method will reduce the JSON
// parsing in the hot path.
func init() {
	t := reflect.TypeOf(corev1.VolumeSource{})
	// The idea is to fetch the name from the json tag, e.g. "secret" for the field "Secret". Since the json tag
	// values will be used by a user to define the allow list.
	names := make([]string, t.NumField())
	for i := range t.NumField() {
		names[i] = strings.SplitN(t.Field(i).Tag.Get("json"), ",", 2)[0]
	}

	// We have to iterate over all fields that could be set in the corev1.VolumeSource and return the name for the
	// field that is set. Since slices are stable we can use just the index for the names slice.
	volumeSourceType = func(volumeSource corev1.VolumeSource) string {
		v := reflect.ValueOf(volumeSource)
		for i := range v.NumField() {
			if v.Field(i).Kind() == reflect.Ptr && !v.Field(i).IsNil() {
				return names[i]
			}
		}

		// Unknown type
		return ""
	}
}

// AllowedPodModifications defines the pod modification that are allowed for the user provided configuration. If a field
// is unset in AllowedPodModifications, we allow everything to avoid breaking the current setups. In a new major release we could
// change this and enforce that fields are only allowed to change if the corresponding AllowedPodModifications is set.
type AllowedPodModifications struct {
	// AllowedServiceAccountNames defines the allowed ServiceAccountNames. If set, this limits the ServiceAccountNames
	// a user can specify.
	AllowedServiceAccountNames map[string]None
	// AllowAutomountServiceAccountToken defines if the spec.AutomountServiceAccountToken setting in the corev1.PodSpec can
	// be set to true.
	AllowAutomountServiceAccountToken *bool
	// AllowHostNetwork defines if a user is allowed to set the HostNetwork setting.
	AllowHostNetwork *bool
	// AllowHostPID defines if a user is allowed to set the HostPID setting.
	AllowHostPID *bool
	// AllowHostIPC defines if a user is allowed to set the HostIPC setting.
	AllowHostIPC *bool
	// AllowedAdditionalVolumeSources defines the allowed corev1.VolumeSources a user can define in the user provided
	// corev1.PodSpec.
	AllowedAdditionalVolumeSources map[string]None
	// AllowedAdditionalVolumeMounts defines the allowed volume mounts a user can define in the user provided
	// corev1.PodSpec.
	AllowedAdditionalVolumeMounts map[string]None
}

// PodSpecIsSanitized validates if the user provided modification are allowed by the operator settings.
func PodSpecIsSanitized(
	podSpec *corev1.PodSpec,
	allowedPodModifications *AllowedPodModifications,
) error {
	// Short-circuit, if no AllowedPodModifications are set, we can skip all further work.
	if allowedPodModifications == nil {
		return nil
	}

	// Check if the AutomountServiceAccountToken setting is set, if so, we have to check if the allowedPodModifications
	// allow to set the setting. The default value of podSpec.AutomountServiceAccountToken is true.
	if ptr.Deref(podSpec.AutomountServiceAccountToken, true) {
		if !ptr.Deref(allowedPodModifications.AllowAutomountServiceAccountToken, true) {
			return fmt.Errorf(
				"spec.AutomountServiceAccountToken is set to %v, which is forbidden",
				ptr.Deref(podSpec.AutomountServiceAccountToken, true),
			)
		}
	}

	// Check if the HostNetwork setting is set and if this is actually allowed.
	if podSpec.HostNetwork {
		if !ptr.Deref(allowedPodModifications.AllowHostNetwork, true) {
			return fmt.Errorf(
				"spec.HostNetwork is set to %v, which is forbidden",
				podSpec.HostNetwork,
			)
		}
	}

	// Check if the HostIPC setting is set and if this is actually allowed.
	if podSpec.HostIPC {
		if !ptr.Deref(allowedPodModifications.AllowHostIPC, true) {
			return fmt.Errorf("spec.HostIPC is set to %v, which is forbidden", podSpec.HostIPC)
		}
	}

	// Check if the HostPID setting is set and if this is actually allowed.
	if podSpec.HostPID {
		if !ptr.Deref(allowedPodModifications.AllowHostPID, true) {
			return fmt.Errorf("spec.HostPID is set to %v, which is forbidden", podSpec.HostPID)
		}
	}

	// Check if the ServiceAccountName is set and if this is actually allowed.
	if podSpec.ServiceAccountName != "" {
		if len(allowedPodModifications.AllowedServiceAccountNames) > 0 {
			if _, ok := allowedPodModifications.AllowedServiceAccountNames[podSpec.ServiceAccountName]; !ok {
				return fmt.Errorf(
					"spec.ServiceAccountName is set to %s, which is forbidden, allowed values: %v",
					podSpec.ServiceAccountName,
					strings.Join(
						slices.Sorted(
							maps.Keys(allowedPodModifications.AllowedServiceAccountNames),
						),
						",",
					),
				)
			}
		}
	}

	// Check if the DeprecatedServiceAccount is set and if this is actually allowed.
	if podSpec.DeprecatedServiceAccount != "" {
		if len(allowedPodModifications.AllowedServiceAccountNames) > 0 {
			if _, ok := allowedPodModifications.AllowedServiceAccountNames[podSpec.DeprecatedServiceAccount]; !ok {
				return fmt.Errorf(
					"spec.DeprecatedServiceAccount is set to %s, which is forbidden, allowed values: %v",
					podSpec.DeprecatedServiceAccount,
					strings.Join(
						slices.Sorted(
							maps.Keys(allowedPodModifications.AllowedServiceAccountNames),
						),
						",",
					),
				)
			}
		}
	}

	// Validate all volumes and check if they are allowed.
	if len(allowedPodModifications.AllowedAdditionalVolumeSources) > 0 {
		for _, volume := range podSpec.Volumes {
			volumeSource := volumeSourceType(volume.VolumeSource)
			if _, ok := allowedPodModifications.AllowedAdditionalVolumeSources[volumeSource]; !ok {
				return fmt.Errorf(
					"spec.Volumes has a %s volume, which is forbidden, allowed volume types: %v",
					volumeSource,
					strings.Join(
						slices.Sorted(
							maps.Keys(allowedPodModifications.AllowedAdditionalVolumeSources),
						),
						",",
					),
				)
			}
		}
	}

	// Validate the volume mounts for all containers.
	err := validateVolumeMounts(
		podSpec.Containers,
		allowedPodModifications.AllowedAdditionalVolumeMounts,
	)
	if err != nil {
		return err
	}

	// Validate the volume mounts for all init containers.
	return validateVolumeMounts(
		podSpec.InitContainers,
		allowedPodModifications.AllowedAdditionalVolumeMounts,
	)
}

// validateVolumeMounts validates if any of the containers have a forbidden (not allowed) volumeMount.
func validateVolumeMounts(containers []corev1.Container, allowed map[string]None) error {
	if len(allowed) > 0 {
		for _, container := range containers {
			for _, volumeMount := range container.VolumeMounts {
				if _, ok := allowed[volumeMount.Name]; !ok {
					return fmt.Errorf(
						"spec.container.volumeMounts has volume mount: %s for container %s, which is forbidden, allowed volume mounts: %v",
						volumeMount.Name,
						container.Name,
						strings.Join(slices.Sorted(maps.Keys(allowed)), ","),
					)
				}
			}
		}
	}

	return nil
}
