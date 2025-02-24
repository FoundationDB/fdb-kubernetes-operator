/*
 * deprecations_test.go
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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[internal] deprecations", func() {
	Describe("NormalizeClusterSpec", func() {
		var spec *fdbv1beta2.FoundationDBClusterSpec
		var cluster *fdbv1beta2.FoundationDBCluster

		BeforeEach(func() {
			cluster = &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "operator-test-1",
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					Version: fdbv1beta2.Versions.Default.String(),
				},
			}
			spec = &cluster.Spec
		})

		Describe("Validations", func() {
			Context("with a duplicated custom parameter in the ProcessSettings", func() {
				It("an error should be returned", func() {
					spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_disable_posix_kernel_aio = 1",
								"knob_disable_posix_kernel_aio = 1",
							},
						},
					}
					err := NormalizeClusterSpec(cluster, DeprecationOptions{})
					Expect(err).To(HaveOccurred())
				})
			})

			Context("with a protected custom parameter in the ProcessSettings", func() {
				It("an error should be returned", func() {
					spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"datadir=1",
							},
						},
					}
					Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).NotTo(Succeed())
				})
			})
		})

		When("the future defaults shouldn't be used", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.ImageConfigs = append(cluster.Spec.MainContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: "foundationdb/foundationdb-test"})
				cluster.Spec.SidecarContainer.ImageConfigs = append(cluster.Spec.SidecarContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar-test"})
			})

			Context("with the current defaults", func() {
				JustBeforeEach(func() {
					Expect(NormalizeClusterSpec(cluster, DeprecationOptions{UseFutureDefaults: false, OnlyShowChanges: false})).NotTo(HaveOccurred())
				})

				It("should have a main container defined", func() {
					generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(containers).To(HaveLen(2))
					Expect(containers[0].Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					}))
					Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					}))
				})

				It("should have empty sidecar resource requirements", func() {
					generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(containers).To(HaveLen(2))
					Expect(containers[1].Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(containers[1].Resources.Requests).NotTo(BeNil())
					Expect(containers[1].Resources.Limits).NotTo(BeNil())
				})

				It("should not have an init container", func() {
					generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					Expect(generalProcessConfig.PodTemplate.Spec.InitContainers).To(HaveLen(0))
				})

				Context("with explicit resource requests for the main container", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
							fdbv1beta2.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: fdbv1beta2.MainContainerName,
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("1"),
												},
											},
										}},
									},
								},
							},
						}
					})

					It("should respect the values given", func() {
						generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(containers).To(HaveLen(2))
						Expect(containers[0].Name).To(Equal(fdbv1beta2.MainContainerName))
						Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						}))
						Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						}))
					})
				})

				Context("with explicit resource requests for the sidecar", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
							fdbv1beta2.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: fdbv1beta2.SidecarContainerName,
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("2"),
												},
											},
										}},
									},
								},
							},
						}
					})

					It("should respect the values given", func() {
						generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(len(containers)).To(Equal(2))
						Expect(containers[1].Name).To(Equal(fdbv1beta2.SidecarContainerName))
						Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						}))
						Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2"),
						}))
					})
				})

				It("should have the public IP source set to pod", func() {
					Expect(cluster.GetPublicIPSource()).NotTo(BeNil())
					Expect(cluster.GetPublicIPSource()).To(Equal(fdbv1beta2.PublicIPSourcePod))
				})

				It("should have automatic replacements enabled", func() {
					Expect(cluster.GetEnableAutomaticReplacements()).To(BeTrue())
					Expect(cluster.GetFailureDetectionTimeSeconds()).To(Equal(7200))
				})

				It("should have the probe settings for the sidecar", func() {
					Expect(cluster.GetSidecarContainerEnableLivenessProbe()).To(BeTrue())
					Expect(cluster.GetSidecarContainerEnableReadinessProbe()).To(BeFalse())
				})

				It("should have the default label config", func() {
					Expect(cluster.GetMatchLabels()).To(Equal(map[string]string{
						fdbv1beta2.FDBClusterLabel: cluster.Name,
					}))
					Expect(cluster.GetResourceLabels()).To(Equal(map[string]string{
						fdbv1beta2.FDBClusterLabel: cluster.Name,
					}))
					Expect(cluster.GetProcessGroupIDLabels()).To(Equal([]string{
						fdbv1beta2.FDBProcessGroupIDLabel,
					}))
					Expect(cluster.GetProcessClassLabels()).To(Equal([]string{
						fdbv1beta2.FDBProcessClassLabel,
					}))
					Expect(cluster.ShouldFilterOnOwnerReferences()).To(BeFalse())
				})

				It("should have explicit listen addresses disabled", func() {
					Expect(cluster.GetUseExplicitListenAddress()).To(BeTrue())
				})

				It("should append the standard image components to the image configs", func() {
					Expect(spec.MainContainer.ImageConfigs).To(Equal([]fdbv1beta2.ImageConfig{
						{BaseImage: "foundationdb/foundationdb-test"},
						{BaseImage: fdbv1beta2.FoundationDBKubernetesBaseImage},
					}))
					Expect(spec.SidecarContainer.ImageConfigs).To(Equal([]fdbv1beta2.ImageConfig{
						{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar-test"},
					}))
				})

				It("should have the unified image enabled", func() {
					Expect(cluster.UseUnifiedImage()).To(BeTrue())
				})

				Context("with unified images enabled", func() {
					BeforeEach(func() {
						imageType := fdbv1beta2.ImageTypeUnified
						spec.ImageType = &imageType
					})

					It("should use the default image config for the unified image", func() {
						Expect(spec.MainContainer.ImageConfigs).To(Equal([]fdbv1beta2.ImageConfig{
							{BaseImage: "foundationdb/foundationdb-test"},
							{BaseImage: fdbv1beta2.FoundationDBKubernetesBaseImage},
						}))
						Expect(spec.SidecarContainer.ImageConfigs).To(Equal([]fdbv1beta2.ImageConfig{
							{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar-test"},
						}))
					})

					It("should not have any init containers in the process settings", func() {
						Expect(spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.InitContainers).To(HaveLen(0))
					})
				})
			})
		})

		When("the future defaults should be used", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.ImageConfigs = append(cluster.Spec.MainContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: "foundationdb/foundationdb-test"})
				cluster.Spec.SidecarContainer.ImageConfigs = append(cluster.Spec.SidecarContainer.ImageConfigs, fdbv1beta2.ImageConfig{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar-test"})
			})

			JustBeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.SupportsLocalityBasedExclusions71.String()
				Expect(NormalizeClusterSpec(cluster, DeprecationOptions{UseFutureDefaults: true, OnlyShowChanges: false})).NotTo(HaveOccurred())
			})

			It("should have the unified image enabled", func() {
				Expect(cluster.DesiredImageType()).To(Equal(fdbv1beta2.ImageTypeUnified))
				Expect(cluster.UseLocalitiesForExclusion()).To(BeTrue())
				Expect(cluster.UseDNSInClusterFile()).To(BeTrue())
			})

			It("should have a main container defined", func() {
				generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
				Expect(present).To(BeTrue())
				containers := generalProcessConfig.PodTemplate.Spec.Containers
				Expect(containers).To(HaveLen(2))
				Expect(containers[0].Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				}))
				Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				}))
			})

			It("should have empty sidecar resource requirements", func() {
				generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
				Expect(present).To(BeTrue())
				containers := generalProcessConfig.PodTemplate.Spec.Containers
				Expect(containers).To(HaveLen(2))
				Expect(containers[1].Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(containers[1].Resources.Requests).NotTo(BeNil())
				Expect(containers[1].Resources.Limits).NotTo(BeNil())
			})

			It("should have no init containers", func() {
				generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
				Expect(present).To(BeTrue())
				Expect(generalProcessConfig.PodTemplate.Spec.InitContainers).To(HaveLen(0))
			})

			When("explicit resource requests for the main container are set", func() {
				BeforeEach(func() {
					spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							PodTemplate: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name: fdbv1beta2.MainContainerName,
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU: resource.MustParse("1"),
											},
											Limits: corev1.ResourceList{
												corev1.ResourceCPU: resource.MustParse("1"),
											},
										},
									}},
								},
							},
						},
					}
				})

				It("should respect the values given", func() {
					generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(containers).To(HaveLen(2))
					Expect(containers[0].Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					}))
					Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					}))
				})
			})

			When("explicit resource requests for the sidecar are set", func() {
				BeforeEach(func() {
					spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							PodTemplate: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name: fdbv1beta2.SidecarContainerName,
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU: resource.MustParse("1"),
											},
											Limits: corev1.ResourceList{
												corev1.ResourceCPU: resource.MustParse("2"),
											},
										},
									}},
								},
							},
						},
					}
				})

				It("should respect the values given", func() {
					generalProcessConfig, present := spec.Processes[fdbv1beta2.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(containers).To(HaveLen(2))
					Expect(containers[1].Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					}))
					Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("2"),
					}))
				})
			})

			It("should have the public IP source set to pod", func() {
				Expect(cluster.GetPublicIPSource()).NotTo(BeNil())
				Expect(cluster.GetPublicIPSource()).To(Equal(fdbv1beta2.PublicIPSourcePod))
			})

			It("should have automatic replacements enabled", func() {
				Expect(cluster.GetEnableAutomaticReplacements()).To(BeTrue())
				Expect(cluster.GetFailureDetectionTimeSeconds()).To(Equal(7200))
			})

			It("should have the probe settings for the sidecar", func() {
				Expect(cluster.GetSidecarContainerEnableLivenessProbe()).To(BeTrue())
				Expect(cluster.GetSidecarContainerEnableReadinessProbe()).To(BeFalse())
			})

			It("should have the default label config", func() {
				Expect(cluster.GetMatchLabels()).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel: cluster.Name,
				}))
				Expect(cluster.GetResourceLabels()).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel: cluster.Name,
				}))
				Expect(cluster.GetProcessGroupIDLabels()).To(Equal([]string{
					fdbv1beta2.FDBProcessGroupIDLabel,
				}))
				Expect(cluster.GetProcessClassLabels()).To(Equal([]string{
					fdbv1beta2.FDBProcessClassLabel,
				}))
				Expect(cluster.ShouldFilterOnOwnerReferences()).To(BeFalse())
			})

			It("should have explicit listen addresses disabled", func() {
				Expect(cluster.GetUseExplicitListenAddress()).To(BeTrue())
			})

			It("should use the default image config for the unified image", func() {
				Expect(spec.MainContainer.ImageConfigs).To(Equal([]fdbv1beta2.ImageConfig{
					{BaseImage: "foundationdb/foundationdb-test"},
					{BaseImage: fdbv1beta2.FoundationDBKubernetesBaseImage},
				}))

				Expect(spec.SidecarContainer.ImageConfigs).To(Equal([]fdbv1beta2.ImageConfig{
					{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar-test"},
				}))
			})

		})

		When("adding an image config", func() {
			When("no image config is set", func() {
				It("should be added", func() {
					var imageConfigs []fdbv1beta2.ImageConfig
					ensureImageConfigPresent(&imageConfigs, fdbv1beta2.ImageConfig{BaseImage: fdbv1beta2.FoundationDBBaseImage})
					Expect(imageConfigs).To(HaveLen(1))
				})
			})

			When("a image config is set", func() {
				var imageConfigs []fdbv1beta2.ImageConfig

				BeforeEach(func() {
					imageConfigs = []fdbv1beta2.ImageConfig{{BaseImage: fdbv1beta2.FoundationDBBaseImage}}
				})

				It("should not be added", func() {
					ensureImageConfigPresent(&imageConfigs, fdbv1beta2.ImageConfig{BaseImage: fdbv1beta2.FoundationDBBaseImage})
					Expect(imageConfigs).To(HaveLen(1))
				})
			})
		})
	})
})
