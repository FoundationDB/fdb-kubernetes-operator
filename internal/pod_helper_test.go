/*
 * pod_helper_test.go
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

	When("calculating the pod spec hash", func() {
		var cluster *fdbv1beta2.FoundationDBCluster
		var processGroup *fdbv1beta2.ProcessGroupStatus

		BeforeEach(func() {
			cluster = CreateDefaultCluster()
			cluster.Spec.ImageType = new(fdbv1beta2.ImageTypeUnified)
			cluster.Spec.Version = "7.3.63"
			cluster.Status.RunningVersion = "7.3.63"
			Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).To(Succeed())
			processGroup = GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1)
		})

		When("the main container image is not pinned", func() {
			It("should change the hash if the sidecar image changes", func() {
				oldHash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.Version = "7.3.64"
				cluster.Status.RunningVersion = "7.3.64"
				newHash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(newHash).NotTo(Equal(oldHash))
			})

			It("should be equal to the full hash", func() {
				hash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())
				fullHash, err := GetFullPodSpecHash(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				Expect(hash).To(Equal(fullHash))
			})
		})

		When("the main container image is pinned", func() {
			var unpinnedHash string

			BeforeEach(func() {
				var err error
				unpinnedHash, err = GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.MainContainer.MonitorVersion = new("7.3.63")
			})

			It("should keep the full hash equal to the hash before the image was pinned", func() {
				fullHash, err := GetFullPodSpecHash(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				Expect(fullHash).To(Equal(unpinnedHash))
			})

			It("should not use the full hash", func() {
				hash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(hash).NotTo(Equal(unpinnedHash))
			})

			It("should not change the hash if the version changes", func() {
				oldHash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.Version = "7.4.6"
				cluster.Status.RunningVersion = "7.4.6"
				newHash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(newHash).To(Equal(oldHash))
			})

			It("should change the hash if the monitor version changes", func() {
				oldHash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.MainContainer.MonitorVersion = new("7.4.6")
				newHash, err := GetPodSpecHash(cluster, processGroup, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(newHash).NotTo(Equal(oldHash))
			})

			When("the cluster is upgraded to a different patch version", func() {
				var capablePod *corev1.Pod
				var nonCapablePod *corev1.Pod

				BeforeEach(func() {
					// Build the pods for the current version, before the upgrade starts.
					nonCapablePod = &corev1.Pod{}
					nonCapableSpec, err := GetPodSpec(cluster, processGroup)
					Expect(err).NotTo(HaveOccurred())
					nonCapablePod.Spec = *nonCapableSpec

					cluster.Spec.Version = "7.3.64"
					capablePod = &corev1.Pod{}
					capableSpec, err := GetPodSpec(cluster, processGroup)
					Expect(err).NotTo(HaveOccurred())
					capablePod.Spec = *capableSpec
				})

				It("should not change the hash", func() {
					Expect(UseSharedBinaryLauncher(cluster)).To(BeTrue())
					hash, err := GetPodSpecHash(cluster, processGroup, nil)
					Expect(err).NotTo(HaveOccurred())
					oldHash, err := GetPodSpecHash(cluster, processGroup, &nonCapablePod.Spec)
					Expect(err).NotTo(HaveOccurred())
					Expect(hash).To(Equal(oldHash))
				})

				It("should detect pods that use the shared binary launcher", func() {
					Expect(PodUsesSharedBinaryLauncher(capablePod)).To(BeTrue())
					Expect(PodUsesSharedBinaryLauncher(nonCapablePod)).To(BeFalse())
				})

				It("should require pods to use the shared binary launcher", func() {
					hash, err := GetPodSpecHash(cluster, processGroup, nil)
					Expect(err).NotTo(HaveOccurred())
					for pod, expected := range map[*corev1.Pod]bool{capablePod: true, nonCapablePod: false} {
						pod.Annotations = map[string]string{fdbv1beta2.LastSpecKey: hash}
						Expect(PodHasDesiredSpec(cluster, pod, hash)).To(Equal(expected))
					}
				})

				It("should not match if the hash is different", func() {
					capablePod.Annotations = map[string]string{fdbv1beta2.LastSpecKey: "other"}
					Expect(PodHasDesiredSpec(cluster, capablePod, "hash")).To(BeFalse())
				})

				When(
					"the upgrade is done and the version is not compatible with the monitor version",
					func() {
						BeforeEach(func() {
							cluster.Spec.Version = "7.4.6"
							cluster.Status.RunningVersion = "7.4.6"
						})

						It("should accept pods that use the shared binary launcher", func() {
							hash, err := GetPodSpecHash(cluster, processGroup, nil)
							Expect(err).NotTo(HaveOccurred())
							capablePod.Annotations = map[string]string{fdbv1beta2.LastSpecKey: hash}
							nonCapablePod.Annotations = map[string]string{
								fdbv1beta2.LastSpecKey: hash,
							}

							Expect(UseSharedBinaryLauncher(cluster)).To(BeFalse())
							Expect(PodHasDesiredSpec(cluster, capablePod, hash)).To(BeTrue())
							Expect(PodHasDesiredSpec(cluster, nonCapablePod, hash)).To(BeTrue())
						})
					},
				)
			})

			When("the shared binary config is disabled after the Pods were created", func() {
				var pod *corev1.Pod

				BeforeEach(func() {
					cluster.Spec.MainContainer.MonitorVersion = new("7.4.10")
					cluster.Spec.Version = "7.4.11"
					cluster.Status.RunningVersion = "7.4.10"
					Expect(UseSharedBinaryLauncher(cluster)).To(BeFalse())

					spec, err := GetPodSpec(cluster, processGroup)
					Expect(err).NotTo(HaveOccurred())
					hash, err := GetPodSpecHash(cluster, processGroup, spec)
					Expect(err).NotTo(HaveOccurred())
					pod = &corev1.Pod{}
					pod.Spec = *spec
					pod.Annotations = map[string]string{fdbv1beta2.LastSpecKey: hash}

					cluster.Annotations = map[string]string{
						fdbv1beta2.DisableSharedBinaryConfigAnnotation: "true",
					}
				})

				It(
					"should not change the hash, but require the Pods to use the version file",
					func() {
						Expect(UseSharedBinaryLauncher(cluster)).To(BeTrue())
						hash, err := GetPodSpecHash(cluster, processGroup, nil)
						Expect(err).NotTo(HaveOccurred())
						Expect(hash).To(Equal(pod.Annotations[fdbv1beta2.LastSpecKey]))
						Expect(PodHasDesiredSpec(cluster, pod, hash)).To(BeFalse())
					},
				)
			})

			It("should not modify the provided spec", func() {
				spec, err := GetPodSpec(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				image := spec.Containers[1].Image
				Expect(image).NotTo(BeEmpty())

				_, err = GetPodSpecHash(cluster, processGroup, spec)
				Expect(err).NotTo(HaveOccurred())
				Expect(spec.Containers[1].Image).To(Equal(image))
			})
		})
	})

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
	)
})
