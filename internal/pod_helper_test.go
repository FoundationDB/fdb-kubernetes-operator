/*
 * pod_helper_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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
	"errors"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("pod_helper", func() {
	DescribeTable("when getting the IP family from a pod", func(pod *corev1.Pod, expected int, expectedErr error) {
		result, err := GetIPFamily(pod)
		if expectedErr != nil {
			Expect(err).To(MatchError(expectedErr))
		} else {
			Expect(err).To(Succeed())
		}
		Expect(result).To(Equal(expected))
	},
		Entry("empty pod",
			nil,
			nil,
			errors.New("failed to fetch IP family from nil Pod"),
		),
		Entry("pod has IP family annotation for IPv4",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.IPFamilyAnnotation: "4",
					},
				},
			},
			fdbv1beta2.PodIPFamilyIPv4,
			nil,
		),
		Entry("pod has IP family annotation for IPv6",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.IPFamilyAnnotation: "6",
					},
				},
			},
			fdbv1beta2.PodIPFamilyIPv6,
			nil,
		),
		Entry("pod has IP family annotation with invalid value",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.IPFamilyAnnotation: "1337",
					},
				},
			},
			fdbv1beta2.PodIPFamilyUnset,
			errors.New("unsupported IP family 1337"),
		),
		Entry("pod has no IP family annotation and nothing specified in the pod spec",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			fdbv1beta2.PodIPFamilyUnset,
			nil,
		),
		Entry("pod has no IP family annotation and uses IP family IPv4 with split image",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: fdbv1beta2.SidecarContainerName,
							Args: []string{
								"--public-ip-family",
								"4",
							},
						},
					},
				},
			},
			fdbv1beta2.PodIPFamilyIPv4,
			nil,
		),
		Entry("pod has no IP family annotation and uses IP family IPv6 with split image",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: fdbv1beta2.SidecarContainerName,
							Args: []string{
								"--public-ip-family",
								"6",
							},
						},
					},
				},
			},
			fdbv1beta2.PodIPFamilyIPv6,
			nil,
		),
		Entry("pod has no IP family annotation and uses unsupported IP family with split image",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: fdbv1beta2.SidecarContainerName,
							Args: []string{
								"--public-ip-family",
								"1337",
							},
						},
					},
				},
			},
			nil,
			errors.New("unsupported IP family 1337"),
		),

		Entry("pod has no IP family annotation and uses IP family IPv4 with unified image",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.ImageTypeAnnotation:            string(fdbv1beta2.ImageTypeUnified),
						monitorapi.CurrentConfigurationAnnotation: "{\"arguments\": [{\"type\": \"IPList\", \"ipFamily\": 4}]}",
					},
				},
			},
			fdbv1beta2.PodIPFamilyIPv4,
			nil,
		),
		Entry("pod has no IP family annotation and uses IP family IPv6 with unified image",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.ImageTypeAnnotation:            string(fdbv1beta2.ImageTypeUnified),
						monitorapi.CurrentConfigurationAnnotation: "{\"arguments\": [{\"type\": \"IPList\", \"ipFamily\": 6}]}",
					},
				},
			},
			fdbv1beta2.PodIPFamilyIPv6,
			nil,
		),
		Entry("pod has no IP family annotation and uses unsupported IP family with unified image",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.ImageTypeAnnotation:            string(fdbv1beta2.ImageTypeUnified),
						monitorapi.CurrentConfigurationAnnotation: "{\"arguments\": [{\"type\": \"IPList\", \"ipFamily\": 1337}]}",
					},
				},
			},
			nil,
			errors.New("unsupported IP family 1337"),
		),
	)
})
