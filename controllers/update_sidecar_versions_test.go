/*
 * update_sidecar_versions_test.go
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

package controllers

import (
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createClusterSpec(sidecarOverrides fdbtypes.ContainerOverrides, processes map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings) *fdbtypes.FoundationDBCluster {
	cluster := createDefaultCluster()

	cluster.Spec.SidecarContainer = sidecarOverrides
	cluster.Spec.Processes = processes

	return cluster
}

var _ = Describe("update_sidecar_versions", func() {
	trueBool := true
	Context("When fetching the sidecar image", func() {
		type testCase struct {
			instance FdbInstance
			cluster  *fdbtypes.FoundationDBCluster
			hasError bool
		}

		DescribeTable("should return the correct image",
			func(input testCase, expected string) {
				err := internal.NormalizeClusterSpec(&input.cluster.Spec, internal.DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				image, err := getSidecarImage(input.cluster, input.instance)
				if input.hasError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}

				Expect(image).To(Equal(expected))
			},
			Entry("only defaults used",
				testCase{
					instance: FdbInstance{
						Metadata: &metav1.ObjectMeta{
							Labels: map[string]string{
								fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
							},
						},
					},
					cluster: createClusterSpec(
						fdbtypes.ContainerOverrides{},
						map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{}),
					hasError: false,
				}, "foundationdb/foundationdb-kubernetes-sidecar:6.2.20-1"),
			Entry("sidecar override is set",
				testCase{
					instance: FdbInstance{
						Metadata: &metav1.ObjectMeta{
							Labels: map[string]string{
								fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
							},
						},
					},
					cluster: createClusterSpec(
						fdbtypes.ContainerOverrides{ImageName: "sidecar-override"},
						map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{}),
					hasError: false,
				}, "sidecar-override:6.2.20-1"),
			Entry("settings override sidecar",
				testCase{
					instance: FdbInstance{
						Metadata: &metav1.ObjectMeta{
							Labels: map[string]string{
								fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
							},
						},
					},
					cluster: createClusterSpec(
						fdbtypes.ContainerOverrides{ImageName: "sidecar-override"},
						map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "foundationdb-kubernetes-sidecar",
												Image: "settings-override",
											},
										},
									},
								},
							},
						}),
					hasError: false,
				}, "settings-override:6.2.20-1"),
			Entry("settings override sidecar with tag without override",
				testCase{
					instance: FdbInstance{
						Metadata: &metav1.ObjectMeta{
							Labels: map[string]string{
								fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
							},
						},
					},
					cluster: createClusterSpec(
						fdbtypes.ContainerOverrides{ImageName: "sidecar-override"},
						map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "foundationdb-kubernetes-sidecar",
												Image: "settings-override:1.2.3",
											},
										},
									},
								},
							},
						}),
					hasError: true,
				}, ""),
			Entry("settings override sidecar with tag with override",
				testCase{
					instance: FdbInstance{
						Metadata: &metav1.ObjectMeta{
							Labels: map[string]string{
								fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
							},
						},
					},
					cluster: createClusterSpec(
						fdbtypes.ContainerOverrides{ImageName: "sidecar-override"},
						map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								AllowTagOverride: &trueBool,
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "foundationdb-kubernetes-sidecar",
												Image: "settings-override:1.2.3",
											},
										},
									},
								},
							},
						}),
					hasError: false,
				}, "settings-override:1.2.3"),
		)
	})
})
