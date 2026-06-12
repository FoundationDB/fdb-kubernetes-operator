/*
 * allowed_pod_modifications_test.go
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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("AllowedPodModifications", func() {
	DescribeTable(
		"validating if the podSpec is sanitized",
		func(podSpec *corev1.PodSpec, allowedPodModifications *AllowedPodModifications, matcher gomegatypes.GomegaMatcher) {
			Expect(PodSpecIsSanitized(podSpec, allowedPodModifications)).To(matcher)
		},
		// nil AllowedPodModifications short-circuits all checks.
		Entry("nil AllowedPodModifications skips all checks",
			&corev1.PodSpec{
				HostNetwork: true,
				HostPID:     true,
				HostIPC:     true,
			},
			nil,
			Succeed(),
		),
		// Empty AllowedPodModifications — no restrictions configured.
		Entry("empty spec, no restrictions",
			&corev1.PodSpec{},
			&AllowedPodModifications{},
			Succeed(),
		),
		// AutomountServiceAccountToken
		Entry("automount=true, AllowAutomountServiceAccountToken not set",
			&corev1.PodSpec{AutomountServiceAccountToken: ptr.To(true)},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry("automount=true, explicitly allowed",
			&corev1.PodSpec{AutomountServiceAccountToken: ptr.To(true)},
			&AllowedPodModifications{AllowAutomountServiceAccountToken: ptr.To(true)},
			Succeed(),
		),
		Entry("automount=true, not allowed",
			&corev1.PodSpec{AutomountServiceAccountToken: ptr.To(true)},
			&AllowedPodModifications{AllowAutomountServiceAccountToken: ptr.To(false)},
			MatchError(ContainSubstring("spec.AutomountServiceAccountToken is set to")),
		),
		Entry("automount=false, token not allowed — false is acceptable",
			&corev1.PodSpec{AutomountServiceAccountToken: ptr.To(false)},
			&AllowedPodModifications{AllowAutomountServiceAccountToken: ptr.To(false)},
			Succeed(),
		),
		Entry("automount not set in podSpec, token not allowed",
			&corev1.PodSpec{},
			&AllowedPodModifications{AllowAutomountServiceAccountToken: ptr.To(false)},
			MatchError(ContainSubstring("spec.AutomountServiceAccountToken is set to")),
		),
		// HostNetwork
		Entry("hostNetwork=true, AllowHostNetwork not set",
			&corev1.PodSpec{HostNetwork: true},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry("hostNetwork=true, explicitly allowed",
			&corev1.PodSpec{HostNetwork: true},
			&AllowedPodModifications{AllowHostNetwork: ptr.To(true)},
			Succeed(),
		),
		Entry("hostNetwork=true, not allowed",
			&corev1.PodSpec{HostNetwork: true},
			&AllowedPodModifications{AllowHostNetwork: ptr.To(false)},
			MatchError(ContainSubstring("spec.HostNetwork is set to true, which is forbidden")),
		),
		Entry("hostNetwork=false, not allowed — false is acceptable",
			&corev1.PodSpec{HostNetwork: false},
			&AllowedPodModifications{AllowHostNetwork: ptr.To(false)},
			Succeed(),
		),
		// HostIPC
		Entry("hostIPC=true, AllowHostIPC not set",
			&corev1.PodSpec{HostIPC: true},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry("hostIPC=true, explicitly allowed",
			&corev1.PodSpec{HostIPC: true},
			&AllowedPodModifications{AllowHostIPC: ptr.To(true)},
			Succeed(),
		),
		Entry("hostIPC=true, not allowed",
			&corev1.PodSpec{HostIPC: true},
			&AllowedPodModifications{AllowHostIPC: ptr.To(false)},
			MatchError(ContainSubstring("spec.HostIPC is set to true, which is forbidden")),
		),
		// HostPID
		Entry("hostPID=true, AllowHostPID not set",
			&corev1.PodSpec{HostPID: true},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry("hostPID=true, explicitly allowed",
			&corev1.PodSpec{HostPID: true},
			&AllowedPodModifications{AllowHostPID: ptr.To(true)},
			Succeed(),
		),
		Entry("hostPID=true, not allowed",
			&corev1.PodSpec{HostPID: true},
			&AllowedPodModifications{AllowHostPID: ptr.To(false)},
			MatchError(ContainSubstring("spec.HostPID is set to true, which is forbidden")),
		),
		// ServiceAccountName
		Entry("serviceAccountName set, no allowlist configured",
			&corev1.PodSpec{ServiceAccountName: "my-sa"},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry("serviceAccountName set, name in allowlist",
			&corev1.PodSpec{ServiceAccountName: "my-sa"},
			&AllowedPodModifications{AllowedServiceAccountNames: map[string]None{"my-sa": {}}},
			Succeed(),
		),
		Entry(
			"serviceAccountName set, name not in allowlist",
			&corev1.PodSpec{ServiceAccountName: "operator-sa"},
			&AllowedPodModifications{AllowedServiceAccountNames: map[string]None{"other-sa": {}}},
			MatchError(
				ContainSubstring(
					"spec.ServiceAccountName is set to operator-sa, which is forbidden",
				),
			),
		),
		Entry("serviceAccountName empty, allowlist configured — empty name is not checked",
			&corev1.PodSpec{ServiceAccountName: ""},
			&AllowedPodModifications{AllowedServiceAccountNames: map[string]None{"other-sa": {}}},
			Succeed(),
		),
		// DeprecatedServiceAccount
		Entry("deprecatedServiceAccount set, name in allowlist",
			&corev1.PodSpec{DeprecatedServiceAccount: "my-sa"},
			&AllowedPodModifications{AllowedServiceAccountNames: map[string]None{"my-sa": {}}},
			Succeed(),
		),
		Entry(
			"deprecatedServiceAccount set, name not in allowlist",
			&corev1.PodSpec{DeprecatedServiceAccount: "operator-sa"},
			&AllowedPodModifications{AllowedServiceAccountNames: map[string]None{"other-sa": {}}},
			MatchError(
				ContainSubstring(
					"spec.DeprecatedServiceAccount is set to operator-sa, which is forbidden",
				),
			),
		),
		// Volume sources
		Entry("secret volume, no volume source allowlist configured",
			&corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "credentials",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
					},
				}},
			},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry("secret volume, allowlist contains secret",
			&corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "credentials",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
					},
				}},
			},
			&AllowedPodModifications{AllowedAdditionalVolumeSources: map[string]None{"secret": {}}},
			Succeed(),
		),
		Entry(
			"secret volume, allowlist does not contain secret",
			&corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "credentials",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
					},
				}},
			},
			&AllowedPodModifications{
				AllowedAdditionalVolumeSources: map[string]None{"configMap": {}},
			},
			MatchError(
				ContainSubstring(
					"spec.Volumes has a secret volume, which is forbidden, allowed volume types: configMap",
				),
			),
		),
		Entry(
			"configMap volume, allowlist contains configMap",
			&corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "config",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
					}},
				}},
			},
			&AllowedPodModifications{
				AllowedAdditionalVolumeSources: map[string]None{"configMap": {}},
			},
			Succeed(),
		),
		Entry(
			"multiple volume types, all in allowlist",
			&corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "my-config",
								},
							},
						},
					},
					{
						Name: "credentials",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
						},
					},
				},
			},
			&AllowedPodModifications{
				AllowedAdditionalVolumeSources: map[string]None{"configMap": {}, "secret": {}},
			},
			Succeed(),
		),
		Entry(
			"multiple volume types, one not in allowlist",
			&corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "my-config",
								},
							},
						},
					},
					{
						Name: "host-vol",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{Path: "/etc"},
						},
					},
				},
			},
			&AllowedPodModifications{
				AllowedAdditionalVolumeSources: map[string]None{"configMap": {}},
			},
			MatchError(ContainSubstring("spec.Volumes has a hostPath volume, which is forbidden")),
		),
		// Volume mounts — containers
		Entry("container volume mount, no allowlist configured",
			&corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:         "main",
					VolumeMounts: []corev1.VolumeMount{{Name: "my-secret"}},
				}},
			},
			&AllowedPodModifications{},
			Succeed(),
		),
		Entry(
			"container volume mount, name in allowlist",
			&corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:         "main",
					VolumeMounts: []corev1.VolumeMount{{Name: "my-secret"}},
				}},
			},
			&AllowedPodModifications{
				AllowedAdditionalVolumeMounts: map[string]None{"my-secret": {}},
			},
			Succeed(),
		),
		Entry(
			"container volume mount, name not in allowlist",
			&corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:         "main",
					VolumeMounts: []corev1.VolumeMount{{Name: "my-secret"}},
				}},
			},
			&AllowedPodModifications{AllowedAdditionalVolumeMounts: map[string]None{"other": {}}},
			MatchError(
				ContainSubstring(
					"spec.container.volumeMounts has volume mount: my-secret for container main, which is forbidden",
				),
			),
		),
		// Volume mounts — init containers
		Entry(
			"init container volume mount, name in allowlist",
			&corev1.PodSpec{
				InitContainers: []corev1.Container{{
					Name:         "init",
					VolumeMounts: []corev1.VolumeMount{{Name: "my-secret"}},
				}},
			},
			&AllowedPodModifications{
				AllowedAdditionalVolumeMounts: map[string]None{"my-secret": {}},
			},
			Succeed(),
		),
		Entry(
			"init container volume mount, name not in allowlist",
			&corev1.PodSpec{
				InitContainers: []corev1.Container{{
					Name:         "init",
					VolumeMounts: []corev1.VolumeMount{{Name: "my-secret"}},
				}},
			},
			&AllowedPodModifications{AllowedAdditionalVolumeMounts: map[string]None{"other": {}}},
			MatchError(
				ContainSubstring(
					"spec.container.volumeMounts has volume mount: my-secret for container init, which is forbidden",
				),
			),
		),
	)
})
