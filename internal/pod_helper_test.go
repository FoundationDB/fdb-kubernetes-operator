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
	"strconv"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("pod_helper", func() {
	DescribeTable(
		"when getting the IP family from a pod",
		func(pod *corev1.Pod, expected int, expectedErr error) {
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
						fdbv1beta2.ImageTypeAnnotation: string(
							fdbv1beta2.ImageTypeUnified,
						),
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
						fdbv1beta2.ImageTypeAnnotation: string(
							fdbv1beta2.ImageTypeUnified,
						),
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
						fdbv1beta2.ImageTypeAnnotation: string(
							fdbv1beta2.ImageTypeUnified,
						),
						monitorapi.CurrentConfigurationAnnotation: "{\"arguments\": [{\"type\": \"IPList\", \"ipFamily\": 1337}]}",
					},
				},
			},
			nil,
			errors.New("unsupported IP family 1337"),
		),
	)

	When("merging labels", func() {
		var metadata *metav1.ObjectMeta

		When("the target map is populated", func() {
			BeforeEach(func() {
				metadata = &metav1.ObjectMeta{}
				metadata.Labels = map[string]string{
					"existing-label": "existing-value",
				}
			})

			Context("and the desired map contains a new label", func() {
				var desired = metav1.ObjectMeta{}
				desired.Labels = map[string]string{
					"new-label": "new-value",
				}

				It("should add the new label to the target", func() {
					Expect(MergeLabels(metadata, desired)).To(Equal(true))
					Expect(metadata.Labels).To(Equal(map[string]string{
						"existing-label": "existing-value",
						"new-label":      "new-value",
					}))
				})
			})
		})

		When("the target map is nil", func() {
			BeforeEach(func() {
				metadata = &metav1.ObjectMeta{}
			})

			Context("and the desired map contains a new label", func() {
				var desired = metav1.ObjectMeta{}
				desired.Labels = map[string]string{
					"new-label": "new-value",
				}

				It("should add the new label to the target", func() {
					Expect(MergeLabels(metadata, desired)).To(Equal(true))
					Expect(metadata.Labels).To(Equal(map[string]string{
						"new-label": "new-value",
					}))
				})
			})
		})
	})

	When("merging annotations", func() {
		var metadata *metav1.ObjectMeta

		When("the target map is populated", func() {
			BeforeEach(func() {
				metadata = &metav1.ObjectMeta{}
				metadata.Annotations = map[string]string{
					"existing-annotation": "existing-value",
				}
			})

			Context("and the desired map contains a new annotation", func() {
				var desired = metav1.ObjectMeta{}
				desired.Annotations = map[string]string{
					"new-annotation": "new-value",
				}

				It("should add the new annotation to the target", func() {
					Expect(MergeAnnotations(metadata, desired)).To(Equal(true))
					Expect(metadata.Annotations).To(Equal(map[string]string{
						"existing-annotation": "existing-value",
						"new-annotation":      "new-value",
					}))
				})
			})
		})

		When("the target map is nil", func() {
			BeforeEach(func() {
				metadata = &metav1.ObjectMeta{}
			})

			Context("and the desired map contains a new annotation", func() {
				var desired = metav1.ObjectMeta{}
				desired.Annotations = map[string]string{
					"new-annotation": "new-value",
				}

				It("should add the new annotation to the target", func() {
					Expect(MergeAnnotations(metadata, desired)).To(Equal(true))
					Expect(metadata.Annotations).To(Equal(map[string]string{
						"new-annotation": "new-value",
					}))
				})
			})
		})
	})

	When("merging maps", func() {
		When("the target map is populated", func() {
			var target map[string]string

			BeforeEach(func() {
				target = map[string]string{
					"test-key": "test-value",
				}
			})

			Context("and the desired map is populated with a new key/value pair", func() {
				var desired = map[string]string{
					"new-key": "new-value",
				}

				It("should add the new value to the target map", func() {
					Expect(mergeMap(target, desired)).To(Equal(true))
					Expect(target).To(Equal(map[string]string{
						"test-key": "test-value",
						"new-key":  "new-value",
					}))
				})
			})

			Context(
				"and the desired map is populated with a new value for an existing key",
				func() {
					var desired = map[string]string{
						"test-key": "new-value",
					}

					It("should add the new value to the target map", func() {
						Expect(mergeMap(target, desired)).To(Equal(true))
						Expect(target).To(Equal(map[string]string{
							"test-key": "new-value",
						}))
					})
				},
			)

			Context(
				"and the desired map is populated with the same value for an existing key",
				func() {
					var desired = map[string]string{
						"test-key": "test-value",
					}

					It("should not change the target map", func() {
						Expect(mergeMap(target, desired)).To(Equal(false))
						Expect(target).To(Equal(map[string]string{
							"test-key": "test-value",
						}))
					})
				},
			)

			Context("and the desired map is empty", func() {
				var desired = map[string]string{}

				It("should not change the target map", func() {
					Expect(mergeMap(target, desired)).To(Equal(false))
					Expect(target).To(Equal(map[string]string{
						"test-key": "test-value",
					}))
				})
			})

			Context("and the desired map is nil", func() {
				It("should not change the target map", func() {
					Expect(mergeMap(target, nil)).To(Equal(false))
					Expect(target).To(Equal(map[string]string{
						"test-key": "test-value",
					}))
				})
			})
		})

		When("the target map is empty", func() {
			var target map[string]string

			BeforeEach(func() {
				target = map[string]string{}
			})

			Context("and the desired map is populated with a new key/value pair", func() {
				var desired = map[string]string{
					"new-key": "new-value",
				}

				It("should add the new value to the target map", func() {
					Expect(mergeMap(target, desired)).To(Equal(true))
					Expect(target).To(Equal(map[string]string{
						"new-key": "new-value",
					}))
				})
			})

			Context("and the desired map is empty", func() {
				var desired = map[string]string{}

				It("should not change the target map", func() {
					Expect(mergeMap(target, desired)).To(Equal(false))
					Expect(target).To(Equal(map[string]string{}))
				})
			})

			Context("and the desired map is nil", func() {
				It("should not change the target map", func() {
					Expect(mergeMap(target, nil)).To(Equal(false))
					Expect(target).To(Equal(map[string]string{}))
				})
			})
		})
	})

	type testCase struct {
		pod          *corev1.Pod
		metadata     metav1.ObjectMeta
		expected     bool
		expectedMeta metav1.ObjectMeta
	}

	DescribeTable("test pod metadata correctness",
		func(tc testCase) {
			result, err := podMetadataCorrect(tc.metadata, tc.pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(tc.expected))
			Expect(tc.pod.ObjectMeta.Labels).To(Equal(tc.expectedMeta.Labels))
			Expect(tc.pod.ObjectMeta.Annotations).To(Equal(tc.expectedMeta.Annotations))
		},
		Entry("Metadata matches with Pod metadata",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Metadata last spec is not matching",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "2",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Metadata Annotation is not matching",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							"special":                      "43",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"special":                      "42",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: false,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"special":                      "42",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Missing annotation on metadata",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"controller/X":                 "wrong",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: false,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"controller/X":                 "wrong",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Ignore additional annotation",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							"controller/X":                 "wrong",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"controller/X":                 "wrong",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Annotation has wrong value",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							"controller/X":                 "true",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"controller/X":                 "wrong",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
					},
				},
				expected: false,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						"controller/X":                 "wrong",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Ignore additional label",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
						Labels: map[string]string{
							"test": "test",
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
					Labels: map[string]string{
						"test": "test",
					},
				},
			},
		),
		Entry("Label has wrong value",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel: "storage",
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
					Labels: map[string]string{
						fdbv1beta2.FDBProcessClassLabel: "globalControllerLogger",
					},
				},
				expected: false,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
					Labels: map[string]string{
						fdbv1beta2.FDBProcessClassLabel: "globalControllerLogger",
					},
				},
			},
		),
		Entry("Metadata for a Pod running on a node",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "testing-node",
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.NodeAnnotation:      "testing-node",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
					},
				},
				expected: false,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.NodeAnnotation:      "testing-node",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("Metadata for image type is not matching",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "testing-node",
					},
				},
				metadata: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.NodeAnnotation:      "testing-node",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeUnified),
					},
				},
				expected: false,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.NodeAnnotation:      "testing-node",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("PodTemplateGenerationLabel matches between pod and desired (steady state)",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.PodTemplateGenerationLabel: "abcdef0123456789",
						},
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Labels: map[string]string{
						fdbv1beta2.PodTemplateGenerationLabel: "abcdef0123456789",
					},
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fdbv1beta2.PodTemplateGenerationLabel: "abcdef0123456789",
					},
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("PodTemplateGenerationLabel preserved when pod and desired differ (mid-roll)",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.PodTemplateGenerationLabel: "oldhash000000000",
						},
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Labels: map[string]string{
						fdbv1beta2.PodTemplateGenerationLabel: "newhash000000000",
					},
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "2",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fdbv1beta2.PodTemplateGenerationLabel: "oldhash000000000",
					},
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
		Entry("PodTemplateGenerationLabel missing from pod is not patched in",
			testCase{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							fdbv1beta2.LastSpecKey:         "1",
							fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
							fdbv1beta2.IPFamilyAnnotation: strconv.Itoa(
								fdbv1beta2.PodIPFamilyUnset,
							),
						},
					},
				},
				metadata: metav1.ObjectMeta{
					Labels: map[string]string{
						fdbv1beta2.PodTemplateGenerationLabel: "newhash000000000",
					},
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
				expected: true,
				expectedMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fdbv1beta2.LastSpecKey:         "1",
						fdbv1beta2.ImageTypeAnnotation: string(fdbv1beta2.ImageTypeSplit),
						fdbv1beta2.IPFamilyAnnotation:  strconv.Itoa(fdbv1beta2.PodIPFamilyUnset),
					},
				},
			},
		),
	)

	Describe("GetPodGenerationHash", func() {
		var cluster *fdbv1beta2.FoundationDBCluster

		BeforeEach(func() {
			cluster = CreateDefaultCluster()
			Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).NotTo(HaveOccurred())
		})

		It("is deterministic for an unchanged cluster", func() {
			first, err := GetPodGenerationHash(cluster, fdbv1beta2.ProcessClassStorage)
			Expect(err).NotTo(HaveOccurred())
			second, err := GetPodGenerationHash(cluster, fdbv1beta2.ProcessClassStorage)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(Equal(second))
		})

		DescribeTable("rotation under input mutations",
			func(mutate func(*fdbv1beta2.FoundationDBCluster), expectChange bool) {
				baseline, err := GetPodGenerationHash(
					cluster,
					fdbv1beta2.ProcessClassStorage,
				)
				Expect(err).NotTo(HaveOccurred())

				mutate(cluster)
				mutated, err := GetPodGenerationHash(
					cluster,
					fdbv1beta2.ProcessClassStorage,
				)
				Expect(err).NotTo(HaveOccurred())

				if expectChange {
					Expect(mutated).NotTo(Equal(baseline))
				} else {
					Expect(mutated).To(Equal(baseline))
				}
			},
			// Inputs that MUST rotate the hash.
			Entry("Version change rotates",
				func(c *fdbv1beta2.FoundationDBCluster) { c.Spec.Version = "7.3.99999" },
				true,
			),
			Entry("MainContainer image config change rotates",
				func(c *fdbv1beta2.FoundationDBCluster) {
					c.Spec.MainContainer.ImageConfigs = append(
						c.Spec.MainContainer.ImageConfigs,
						fdbv1beta2.ImageConfig{BaseImage: "custom/image"},
					)
				},
				true,
			),
			Entry("SidecarContainer image config change rotates",
				func(c *fdbv1beta2.FoundationDBCluster) {
					c.Spec.SidecarContainer.ImageConfigs = append(
						c.Spec.SidecarContainer.ImageConfigs,
						fdbv1beta2.ImageConfig{BaseImage: "custom/sidecar"},
					)
				},
				true,
			),
			Entry("ImageType change rotates",
				func(c *fdbv1beta2.FoundationDBCluster) {
					t := fdbv1beta2.ImageTypeUnified
					c.Spec.ImageType = &t
				},
				true,
			),
			Entry("ProcessSettings podTemplate change rotates",
				func(c *fdbv1beta2.FoundationDBCluster) {
					settings := c.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					if settings.PodTemplate == nil {
						settings.PodTemplate = &corev1.PodTemplateSpec{}
					}
					if settings.PodTemplate.Labels == nil {
						settings.PodTemplate.Labels = map[string]string{}
					}
					settings.PodTemplate.Labels["test"] = "value"
					c.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = settings
				},
				true,
			),
			Entry("CustomParameters change rotates",
				func(c *fdbv1beta2.FoundationDBCluster) {
					settings := c.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					settings.CustomParameters = append(settings.CustomParameters, "knob_x=1")
					c.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = settings
				},
				true,
			),

			// Inputs that MUST NOT rotate the hash — they are not part of the
			// pod's generation identity.
			Entry("AutomationOptions change does NOT rotate",
				func(c *fdbv1beta2.FoundationDBCluster) {
					c.Spec.AutomationOptions.MaxConcurrentReplacements = ptr.To(99)
				},
				false,
			),
			Entry("DatabaseConfiguration change does NOT rotate",
				func(c *fdbv1beta2.FoundationDBCluster) {
					c.Spec.DatabaseConfiguration.Storage = 99
				},
				false,
			),
			Entry("Routing change does NOT rotate",
				func(c *fdbv1beta2.FoundationDBCluster) {
					c.Spec.Routing.HeadlessService = ptr.To(true)
				},
				false,
			),
			Entry("LabelConfig.MatchLabels change does NOT rotate",
				func(c *fdbv1beta2.FoundationDBCluster) {
					c.Spec.LabelConfig.MatchLabels = map[string]string{"foo": "bar"}
				},
				false,
			),
		)
	})
})
